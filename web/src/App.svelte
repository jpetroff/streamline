<script lang="ts">
  import { onMount } from 'svelte';
  import { provideKeyboard } from '$lib/keyboard-context';
  import SourceViewer from '$lib/components/SourceViewer.svelte';
  import { HTTPSourceAPI, type SourcePreferences } from '$lib/transport/sources';
  import type { LogSource } from '$lib/transport/types';

  const keyboard = provideKeyboard();
  const api = new HTTPSourceAPI();
  const preferences = new Map<string, SourcePreferences>();
  let sources = $state<LogSource[]>([]);
  let selected = $state('stdin');
  let sourceKind = $state('stdin');
  let command = $state('');
  let mode = $state<'auto' | 'text'>('auto');
  let creating = $state(false);
  let changing = $state(false);
  let error = $state('');
  let connectionError = $state('');
  let alive = false;
  let sourceRevision = 0;
  let intent = 0;
  const selectedInfo = $derived(sources.find(source => source.id === selected));
  const running = $derived(selectedInfo && ['starting', 'running', 'stopping'].includes(selectedInfo.state));

  function receive(next: LogSource[]) {
    sourceRevision++;
    sources = next;
    connectionError = '';
    if (selected !== 'stdin' && !sources.some(source => source.id === selected)) select('stdin');
    for (const id of preferences.keys()) if (!sources.some(source => source.id === id)) preferences.delete(id);
  }
  onMount(() => {
    alive = true;
    const abort = new AbortController();
    const revision = sourceRevision;
    void api.list(abort.signal).then(next => { if (alive && revision === sourceRevision) receive(next); }).catch(err => { if (alive && err.name !== 'AbortError') connectionError = err.message; });
    const events = api.events(next => { if (alive) receive(next); }, () => { if (alive) connectionError = 'Reconnecting to log sources…'; });
    const detachKeyboard = keyboard.attach(window);
    return () => { alive = false; abort.abort(); events.close(); detachKeyboard(); };
  });

  function select(id: string) {
    intent++;
    selected = id;
    sourceKind = id === 'stdin' ? 'stdin' : 'command';
    error = '';
  }
  async function run(text = command, parseMode = mode) {
    if (creating || !text.trim()) return;
    creating = true; error = '';
    const requestIntent = intent;
    try {
      const created = await api.create({ command: text, mode: parseMode });
      if (!alive) return;
      // The SSE event can arrive before this HTTP response. Keep its newer state.
      if (!sources.some(source => source.id === created.id)) sources = [...sources, created];
      if (requestIntent === intent) select(created.id);
    } catch (err) { if (alive) error = err instanceof Error ? err.message : String(err); }
    finally { creating = false; }
  }
  async function changeSource(remove: boolean) {
    if (selected === 'stdin' || changing) return;
    const id = selected;
    changing = true; error = '';
    try {
      if (remove) {
        await api.remove(id);
        sources = sources.filter(source => source.id !== id);
        preferences.delete(id);
        if (selected === id) select('stdin');
      } else { await api.stop(id); }
    } catch (err) { if (alive) error = err instanceof Error ? err.message : String(err); }
    finally { changing = false; }
  }
</script>

<svelte:head>
  <title>Streamline</title>
  <meta name="description" content="Local streaming log viewer" />
</svelte:head>

<div class="flex h-full min-w-0 flex-col bg-background text-foreground">
  <header class="flex min-h-12 shrink-0 items-center gap-3 border-b bg-shell px-3 py-2" aria-label="Application toolbar">
    <label for="input-source" class="sr-only">Input source</label>
    <select id="input-source" bind:value={sourceKind} onchange={() => { if (sourceKind === 'stdin') select('stdin'); }} aria-label="Input source"
      class="h-8 min-w-32 rounded-md border border-input bg-background px-2 text-sm outline-none focus:ring-2 focus:ring-ring">
      <option value="stdin">stdin</option>
      <option value="file" disabled>file</option>
      <option value="command">command</option>
    </select>
    <nav class="flex min-w-0 flex-1 gap-1 overflow-x-auto" aria-label="Log sources">
      <button type="button" aria-pressed={selected === 'stdin'} onclick={() => select('stdin')} class="shrink-0 rounded border px-3 py-1 text-xs aria-pressed:bg-accent">stdin</button>
      {#each sources.filter(source => source.kind === 'command') as item (item.id)}
        <button type="button" aria-pressed={selected === item.id} onclick={() => select(item.id)} title={item.command}
          class="flex max-w-64 shrink-0 items-center gap-2 rounded border px-3 py-1 text-xs aria-pressed:bg-accent">
          <span class="truncate font-mono">{item.command}</span><span class="text-muted-foreground">{item.state}</span>
        </button>
      {/each}
    </nav>
  </header>
  {#if sourceKind === 'command'}
    <form class="shrink-0 space-y-2 border-b bg-shell px-3 py-2" aria-label="Run command" onsubmit={event => { event.preventDefault(); void run(); }}>
      <div class="flex items-start gap-2">
        <label for="command-input" class="sr-only">Command</label>
        <textarea id="command-input" bind:value={command} rows="2" placeholder="ssh -o BatchMode=yes host 'journalctl -f -o json --no-pager'" spellcheck="false"
          class="min-w-0 flex-1 resize-y rounded border border-input bg-background p-2 font-mono text-xs focus:outline-none focus:ring-2 focus:ring-ring"></textarea>
        <label class="flex flex-col gap-1 text-xs">Output mode
          <select bind:value={mode} class="h-8 rounded border border-input bg-background px-2"><option value="auto">Auto</option><option value="text">Text</option></select>
        </label>
        <button type="submit" disabled={creating || !command.trim()} class="mt-5 rounded bg-primary px-3 py-2 text-xs text-primary-foreground disabled:opacity-40">{creating ? 'Starting…' : 'Run'}</button>
      </div>
      <p class="text-xs text-muted-foreground">{mode === 'auto' ? 'Auto detects structured logs; plain output appears when the command ends. Choose Text to view every line live.' : 'Text displays each output line immediately.'}</p>
    </form>
  {/if}
  {#if selectedInfo?.kind === 'command'}
    <div class="flex shrink-0 items-center gap-2 border-b px-3 py-2 text-xs" aria-label="Command controls">
      <code class="min-w-0 flex-1 truncate" title={selectedInfo.command}>{selectedInfo.command}</code>
      <span role="status">{selectedInfo.state}{selectedInfo.exitCode !== undefined && selectedInfo.exitCode >= 0 ? ` · exit ${selectedInfo.exitCode}` : ''}</span>
      {#if running}<button type="button" disabled={changing || selectedInfo.state === 'stopping'} onclick={() => void changeSource(false)} class="rounded border px-2 py-1 disabled:opacity-40">Stop</button>{/if}
      <button type="button" disabled={creating} onclick={() => void run(selectedInfo?.command, selectedInfo?.mode)} class="rounded border px-2 py-1 disabled:opacity-40">Run again</button>
      <button type="button" disabled={changing} onclick={() => void changeSource(true)} class="rounded border px-2 py-1 disabled:opacity-40">Close and discard</button>
    </div>
  {/if}
  {#if error || connectionError}<div role="alert" class="shrink-0 border-b border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">{error || connectionError}</div>{/if}
  {#key selected}
    {@const id = selected}
    <SourceViewer sourceId={id} info={selectedInfo} preferences={preferences.get(id)} onSave={saved => { if (id === 'stdin' || sources.some(source => source.id === id)) preferences.set(id, saved); }} />
  {/key}
</div>
