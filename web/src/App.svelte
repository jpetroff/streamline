<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { provideKeyboard } from '$lib/keyboard-context';
  import SourceViewer from '$lib/components/SourceViewer.svelte';
  import SourceTabs from '$lib/components/SourceTabs.svelte';
  import ConfigurationSettings from '$lib/components/ConfigurationSettings.svelte';
  import { configurationFromPreferences, HTTPConfigurationAPI } from '$lib/configurations';
  import { HTTPSourceAPI } from '$lib/transport/sources';
  import { TabController, commandPreferences, type TabState } from '$lib/tabs';

  const keyboard = provideKeyboard();
  const api = new HTTPSourceAPI();
  const tabs = new TabController(api);
  let tabState = $state.raw<TabState>(tabs.state);
  let activeViewer = $state<ReturnType<typeof SourceViewer>>();
  let commandInput = $state<HTMLTextAreaElement>();
  let connectionError = $state('');
  const selected = $derived(tabState.tabs.find(tab => tab.id === tabState.selected)!);
  const running = $derived(selected.info && ['starting', 'running', 'stopping'].includes(selected.info.state));

  onMount(() => {
    let alive = true;
    const unsubscribe = tabs.subscribe(next => { tabState = next; });
    const abort = new AbortController();
    const revision = tabs.revision;
    void api.list(abort.signal).then(next => { if (alive && revision === tabs.revision) tabs.receive(next); })
      .catch(err => { if (alive && err.name !== 'AbortError') connectionError = err.message; });
    const events = api.events(next => { if (alive) { tabs.receive(next); connectionError = ''; } }, () => { if (alive) connectionError = 'Reconnecting to log sources…'; });
    const detachKeyboard = keyboard.attach(window);
    return () => { alive = false; unsubscribe(); abort.abort(); events.close(); detachKeyboard(); };
  });

  async function openTab() {
    tabs.open();
    await tick(); commandInput?.focus();
  }
  function snapshot() {
    return selected.sourceId && activeViewer ? activeViewer.snapshot() : selected.preferences ?? commandPreferences();
  }
  function run(again = false) {
    void tabs.run(selected.id, snapshot(), again);
  }
  function captureConfiguration() {
    if (selected.pending) throw new Error('Wait for the tab operation to finish.');
    if (selected.id === 'stdin' && !activeViewer) throw new Error('Wait for stdin to connect.');
    return configurationFromPreferences(snapshot(), selected.info?.command ?? selected.command, selected.info?.mode ?? selected.mode);
  }
  async function loadConfiguration(id: string) {
    const tabId = selected.id;
    const sourceId = selected.sourceId;
    const target = activeViewer;
    const requestIntent = tabs.selectionRevision;
    const unchanged = () => tabs.selectionRevision === requestIntent && tabs.get(tabId)?.sourceId === sourceId && !tabs.get(tabId)?.pending;
    if (selected.pending) throw new Error('Wait for the tab operation to finish.');
    const configurationAPI = new HTTPConfigurationAPI();
    const { document: doc } = await configurationAPI.get(id);
    await configurationAPI.validate(doc);
    if (!unchanged()) throw new Error('The active tab changed.');
    const prepared = { spec: { filter: doc.filters.filter, search: doc.filters.search, sort: 'input' as const },
      columns: doc.columns, following: true, rowLines: 1 as const, inheritOnRun: true };
    if (tabId === 'stdin' && doc.command.trim()) {
      const newId = tabs.open();
      tabs.patch(newId, { command: doc.command, mode: doc.mode, preferences: prepared });
      return;
    }
    if (sourceId) {
      if (!target) throw new Error('Wait for the source to connect.');
      await target.applyConfiguration(doc);
      if (!unchanged() || activeViewer !== target) throw new Error('The active tab changed.');
      tabs.patch(tabId, { command: doc.command, mode: doc.mode, preferences: target.snapshot(), error: '' });
    } else {
      tabs.patch(tabId, { command: doc.command, mode: doc.mode, preferences: prepared, error: '' });
    }
  }
</script>

<svelte:head>
  <title>Streamline</title>
  <meta name="description" content="Local streaming log viewer" />
</svelte:head>

<div class="flex h-full min-w-0 flex-col bg-background text-foreground">
  <header class="flex min-h-12 shrink-0 items-end gap-3 border-b bg-shell px-3" aria-label="Application toolbar">
    <SourceTabs tabs={tabState.tabs} selected={tabState.selected} onSelect={id => tabs.select(id)} onOpen={() => void openTab()} onClose={id => tabs.close(id)} />
    <div class="shrink-0 self-center"><ConfigurationSettings capture={captureConfiguration} onLoad={loadConfiguration} /></div>
  </header>
  <div role="tabpanel" tabindex="0" id={`source-panel-${selected.id}`} aria-labelledby={`source-tab-${selected.id}`} class="flex min-h-0 min-w-0 flex-1 flex-col">
    <div role="group" aria-label="Source controls" class="flex shrink-0 items-center gap-3 px-3 pt-2 pb-2 {selected.id === 'stdin' ? 'border-b' : ''}">
      <label for="input-source" class="text-xs text-muted-foreground">Input source</label>
      <select id="input-source" value={selected.id === 'stdin' ? 'stdin' : 'command'} disabled={selected.id === 'stdin' || !!selected.pending}
        class="h-8 min-w-32 rounded-md border border-input bg-background px-2 text-sm outline-none focus:ring-2 focus:ring-ring disabled:opacity-60">
        {#if selected.id === 'stdin'}<option value="stdin">stdin</option>{:else}<option value="command">command</option><option value="file" disabled>file</option>{/if}
      </select>
    </div>
    {#if selected.id !== 'stdin'}
      <form class="shrink-0 space-y-2 border-b px-3 pb-2" aria-label="Run command" onsubmit={event => { event.preventDefault(); run(); }}>
        <div class="flex flex-wrap items-start gap-2">
          <label for="command-input" class="sr-only">Command</label>
          <textarea id="command-input" bind:this={commandInput} value={selected.command} oninput={event => tabs.patch(selected.id, { command: event.currentTarget.value })}
            rows="2" placeholder="ssh -o BatchMode=yes host 'journalctl -f -o json --no-pager'" spellcheck="false"
            class="min-w-40 flex-1 resize-y rounded border border-input bg-background p-2 font-mono text-xs focus:outline-none focus:ring-2 focus:ring-ring"></textarea>
          <label class="flex flex-col gap-1 text-xs">Output mode
            <select value={selected.mode} onchange={event => tabs.patch(selected.id, { mode: event.currentTarget.value as 'auto' | 'text' })}
              class="h-8 rounded border border-input bg-background px-2"><option value="auto">Auto</option><option value="text">Text</option></select>
          </label>
          <button type="submit" disabled={!!selected.pending || !selected.command.trim()} class="mt-5 rounded bg-primary px-3 py-2 text-xs text-primary-foreground disabled:opacity-40">{selected.pending === 'run' ? 'Starting…' : 'Run'}</button>
        </div>
        <p class="text-xs text-muted-foreground">{selected.mode === 'auto' ? 'Auto detects structured logs; plain output appears when the command ends. Choose Text to view every line live.' : 'Text displays each output line immediately.'}</p>
      </form>
    {/if}
    {#if selected.info?.kind === 'command'}
      <div class="flex shrink-0 items-center gap-2 border-b px-3 py-2 text-xs" aria-label="Command controls">
        <code class="min-w-0 flex-1 truncate" title={selected.info.command}>{selected.info.command}</code>
        <span role="status">{selected.info.state}{selected.info.exitCode !== undefined && selected.info.exitCode >= 0 ? ` · exit ${selected.info.exitCode}` : ''}</span>
        {#if running}<button type="button" disabled={!!selected.pending || selected.info.state === 'stopping'} onclick={() => void tabs.stop(selected.id)} class="rounded border px-2 py-1 disabled:opacity-40">Stop</button>{/if}
        <button type="button" disabled={!!selected.pending} onclick={() => run(true)} class="rounded border px-2 py-1 disabled:opacity-40">Run again</button>
      </div>
    {/if}
    {#if selected.error || connectionError}<div role="alert" class="shrink-0 border-b border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">{selected.error || connectionError}</div>{/if}
    {#key `${selected.id}:${selected.sourceId ?? ''}`}
      {@const id = selected.id}
      {@const sourceId = selected.sourceId}
      {#if sourceId}
        <SourceViewer bind:this={activeViewer} {sourceId} info={selected.info} preferences={selected.preferences} onSave={saved => tabs.save(id, sourceId, saved)} />
      {:else}
        <div class="grid min-h-0 flex-1 place-items-center px-6 text-sm text-muted-foreground" role="status">
          {selected.pending === 'run' ? 'Starting command…' : 'Enter a command and click Run to view its output.'}
        </div>
      {/if}
    {/key}
  </div>
</div>
