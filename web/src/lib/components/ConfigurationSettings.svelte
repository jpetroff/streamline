<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { Settings } from '@lucide/svelte';
  import { registerCommand, registerOverlay } from '$lib/keyboard-context';
  import { HTTPConfigurationAPI, draftFromConfiguration, parseConfigurationDraft, issueText, type Configuration, type ConfigurationDraft, type ConfigurationEntry, type ConfigurationIssue, type ConfigurationListing } from '$lib/configurations';
  import { TransportError } from '$lib/transport/api';

  let { capture, onLoad }: { capture: () => Configuration; onLoad: (id: string) => Promise<void> } = $props();
  const api = new HTTPConfigurationAPI();
  let open = $state(false);
  let scope = $state<HTMLElement | null>(null);
  let listing = $state<ConfigurationListing>();
  let draft = $state<ConfigurationDraft>();
  let editingID = $state<string>();
  let baseline = $state('');
  let busy = $state(false);
  let refreshing = $state(false);
  let error = $state('');
  let status = $state('');
  let serverIssues = $state<ConfigurationIssue[]>([]);
  let rejectedDraft = $state('');
  let discardAction = $state<(() => void) | undefined>();
  const dirty = $derived(!!draft && JSON.stringify(draft) !== baseline);
  const parsed = $derived(draft ? parseConfigurationDraft(draft) : undefined);
  const visibleIssues = $derived([...(parsed?.errors ?? []), ...(JSON.stringify(draft) === rejectedDraft ? serverIssues : [])]);
  const button = 'rounded-md border px-3 py-1.5 text-xs hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-40';
  const input = 'w-full rounded-md border border-input bg-background px-2 py-2 text-xs outline-none focus:ring-2 focus:ring-ring';

  function guarded(action: () => void) {
    if (busy) return;
    if (dirty) { discardAction = action; return; }
    action();
  }
  function changeOpen(value: boolean) {
    if (value) { open = true; void refresh(); }
    else guarded(() => { open = false; draft = undefined; discardAction = undefined; });
  }
  registerOverlay({ open: () => open, modal: true, contains: target => !!scope?.contains(target as Node), close: () => changeOpen(false) });
  registerCommand({ id: 'configurations.save', label: 'Save configuration', bindings: ['Mod+Enter'], scope: () => scope ?? undefined, allowInInput: true, handler: save });
  async function refresh() {
    if (refreshing) return;
    refreshing = true; error = '';
    try { listing = await api.list(); listing.entries.sort((a, b) => a.document.name.localeCompare(b.document.name) || a.id.localeCompare(b.id)); }
    catch (err) { showError(err); }
    finally { refreshing = false; }
  }
  function showError(err: unknown) {
    error = err instanceof Error ? err.message : String(err);
    serverIssues = err instanceof TransportError ? err.configurationErrors ?? [] : [];
    rejectedDraft = JSON.stringify(draft);
  }
  function setDraft(doc: Configuration, id?: string) {
    draft = draftFromConfiguration(doc); editingID = id;
    baseline = id ? JSON.stringify(draft) : '';
    error = ''; status = ''; serverIssues = []; discardAction = undefined;
  }
  function newEntry() {
    guarded(() => { try { setDraft(capture()); } catch (err) { showError(err); } });
  }
  async function edit(entry: ConfigurationEntry, clone: boolean) {
    busy = true; error = ''; status = '';
    try {
      const current = await api.get(entry.id);
      setDraft({ ...current.document, name: current.document.name + (clone ? ' copy' : '') }, clone ? undefined : current.id);
    } catch (err) { showError(err); }
    finally { busy = false; }
  }
  async function load(entry: ConfigurationEntry) {
    busy = true; error = ''; status = '';
    try {
      await onLoad(entry.id);
      draft = undefined; open = false; discardAction = undefined;
    } catch (err) { showError(err); }
    finally { busy = false; }
  }
  async function save() {
    if (busy || !parsed?.document) return;
    busy = true; error = ''; status = ''; serverIssues = [];
    try {
      const entry = await api.save(parsed.document, editingID);
      setDraft(entry.document, entry.id);
      status = 'Configuration saved.';
      await refresh();
    } catch (err) { showError(err); }
    finally { busy = false; }
  }
</script>

<Dialog.Root bind:open={() => open, changeOpen}>
  <Dialog.Trigger aria-label="Saved configurations" title="Saved configurations" class="shrink-0 rounded-md p-2 hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"><Settings size={18} /></Dialog.Trigger>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/60" />
    <Dialog.Content bind:ref={scope}
      onEscapeKeydown={event => { if (dirty || busy) { event.preventDefault(); changeOpen(false); } }}
      onInteractOutside={event => { if (dirty || busy) { event.preventDefault(); changeOpen(false); } }} class="fixed left-1/2 top-1/2 z-50 flex max-h-[90vh] w-[min(64rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 flex-col gap-3 overflow-y-auto rounded-lg border bg-popover p-4 text-popover-foreground shadow-xl">
      <div class="flex items-center justify-between gap-3">
        <Dialog.Title class="text-base font-semibold">Saved configurations</Dialog.Title>
        <button type="button" class={button} disabled={busy} onclick={() => changeOpen(false)}>Close</button>
      </div>
      <Dialog.Description class="text-xs text-muted-foreground">Save applied tab settings for reuse. Load prepares the active tab and command editor; click Run to execute.</Dialog.Description>
      {#if listing}<p class="break-all font-mono text-xs text-muted-foreground">{listing.directory}</p>{/if}
      <div class="flex flex-wrap gap-2">
        <button type="button" class={button} disabled={busy} onclick={newEntry}>Save current as new</button>
        <button type="button" class={button} disabled={busy || refreshing} onclick={() => void refresh()}>{refreshing ? 'Refreshing…' : 'Refresh'}</button>
      </div>
      {#if error}<p role="alert" class="text-xs text-destructive">{error}</p>{/if}
      {#if status}<p role="status" class="text-xs">{status}</p>{/if}
      {#if discardAction}
        <div role="alert" class="space-y-2 rounded border p-3 text-xs">
          <p>Save your changes or discard them before continuing.</p>
          <div class="flex gap-2">
            <button type="button" class={button} disabled={busy || !parsed?.document} onclick={async () => { const action = discardAction; await save(); if (!dirty) action?.(); }}>Save and continue</button>
            <button type="button" class={button} onclick={() => { const action = discardAction; discardAction = undefined; draft = undefined; action?.(); }}>Discard changes</button>
            <button type="button" class={button} onclick={() => { discardAction = undefined; }}>Keep editing</button>
          </div>
        </div>
      {/if}
      <div class="grid min-h-0 gap-4 md:grid-cols-[minmax(14rem,1fr)_minmax(0,2fr)]">
        <div class="space-y-2" aria-label="Saved configuration list">
          {#if listing?.entries.length === 0}<p class="py-3 text-xs text-muted-foreground">No saved configurations yet.</p>{/if}
          {#each listing?.entries ?? [] as entry (entry.id)}
            <article class="space-y-2 rounded-md border p-3" aria-label={entry.document.name}>
              <p class="break-words text-sm font-medium">{entry.document.name}</p>
              <code class="block truncate text-xs text-muted-foreground" title={entry.document.command}>{entry.document.command || 'No command'}</code>
              <div class="flex flex-wrap gap-1">
                <button type="button" class={button} disabled={busy} onclick={() => guarded(() => { void load(entry); })}>Load</button>
                <button type="button" class={button} disabled={busy} onclick={() => guarded(() => { void edit(entry, false); })}>Edit</button>
                <button type="button" class={button} disabled={busy} onclick={() => guarded(() => { void edit(entry, true); })}>Clone and edit</button>
              </div>
            </article>
          {/each}
          {#each listing?.errors ?? [] as item}
            <div role="alert" class="break-words rounded border border-destructive/30 p-3 text-xs text-destructive">
              <p>{item.file}: {item.message}</p>
              {#each item.issues ?? [] as issue}<p>{issueText(issue)}</p>{/each}
              <p>Correct this file in the configuration folder, then Refresh.</p>
            </div>
          {/each}
        </div>
        {#if draft}
          <form class="min-w-0 space-y-3" aria-label="Configuration editor" onsubmit={event => { event.preventDefault(); void save(); }}>
            <fieldset disabled={busy} class="min-w-0 space-y-3">
              <div class="flex gap-3">
                <label class="min-w-0 flex-1 space-y-1 text-xs">Name<input class={input} bind:value={draft.name} /></label>
                <label class="space-y-1 text-xs">Output mode<select class={input} bind:value={draft.mode}><option value="auto">Auto</option><option value="text">Text</option></select></label>
              </div>
              <label class="block space-y-1 text-xs">Command<textarea class={`${input} min-h-20 font-mono`} rows="3" bind:value={draft.command} spellcheck="false"></textarea></label>
              <label class="block space-y-1 text-xs">Column setup (JSON)<textarea class={`${input} min-h-32 font-mono`} rows="7" bind:value={draft.columns} spellcheck="false" aria-describedby="saved-columns-help"></textarea></label>
              <p id="saved-columns-help" class="text-xs text-muted-foreground">Example: <code>{'[{"path":"message","dateFormat":"original"}]'}</code></p>
              <label class="block space-y-1 text-xs">Filters and search (JSON)<textarea class={`${input} min-h-40 font-mono`} rows="9" bind:value={draft.filters} spellcheck="false" aria-describedby="saved-filters-help"></textarea></label>
              <p id="saved-filters-help" class="text-xs text-muted-foreground">Example: <code>{'{"filter":[],"search":{"text":"","mode":"plain","operator":"or"}}'}</code></p>
              <div aria-live="polite" class="space-y-1 text-xs">
                {#each visibleIssues as issue}<p class="text-destructive">{issue.field}: {issueText(issue)}</p>{/each}
                {#each parsed?.warnings ?? [] as issue}<p class="text-muted-foreground">Browser regex check: {issueText(issue)} Go validates support on Save.</p>{/each}
              </div>
              <div class="flex justify-end gap-2">
                <button type="button" class={button} onclick={() => guarded(() => { draft = undefined; })}>Cancel</button>
                <button type="submit" class={button} disabled={!parsed?.document}>{busy ? 'Saving…' : 'Save'}</button>
              </div>
            </fieldset>
          </form>
        {:else}<p class="py-3 text-sm text-muted-foreground">Save the current tab, or edit or clone a saved entry.</p>{/if}
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
