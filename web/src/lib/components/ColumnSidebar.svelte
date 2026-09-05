<script lang="ts">
  import { untrack } from 'svelte';
  import { findSampleFields, parseColumnDraft } from '$lib/columns';
  import type { JSONValue } from '$lib/transport/types';
  import type { ViewerState } from '$lib/transport/viewer-state';

  let {
    viewer,
    appliedColumns,
    onApply,
  }: {
    viewer: ViewerState;
    appliedColumns: readonly string[];
    onApply: (columns: string[]) => void;
  } = $props();

  let draft = $state(untrack(() => appliedColumns.join('\n')));
  let gutterElement = $state<HTMLDivElement>();
  let sampleGeneration = $state('');
  let sampleFields = $state<Record<string, JSONValue>>();

  let draftColumns = $derived(parseColumnDraft(draft));
  let lineCount = $derived(Math.max(1, draft.split('\n').length));
  let unchanged = $derived(columnsEqual(draftColumns, appliedColumns));
  let canApply = $derived(draftColumns.length > 0 && !unchanged);
  let sampleJSON = $derived(sampleFields ? JSON.stringify(sampleFields, null, 2) : '');

  // Pin the first structured row observed for each input generation so the helper
  // does not change while the user scrolls through different virtual pages.
  $effect(() => {
    const generationId = viewer.session?.generationId ?? '';
    if (generationId !== sampleGeneration) {
      sampleGeneration = generationId;
      sampleFields = undefined;
    }
    if (sampleFields || generationId === '') return;
    sampleFields = findSampleFields(viewer.displayed?.pages ?? [], generationId);
  });

  function syncLineNumbers(event: Event) {
    if (gutterElement) gutterElement.scrollTop = (event.currentTarget as HTMLTextAreaElement).scrollTop;
  }

  function applyColumns() {
    if (canApply) onApply([...draftColumns]);
  }

  function columnsEqual(left: readonly string[], right: readonly string[]) {
    return left.length === right.length && left.every((column, index) => column === right[index]);
  }
</script>

<section class="flex h-full min-h-0 flex-col p-3 text-sidebar-foreground" aria-label="Column configuration">
  <div class="shrink-0">
    <h2 class="text-sm font-semibold">Columns</h2>
    <p id="columns-help" class="mt-1 text-xs leading-5 text-muted-foreground">
      Enter one JSON field per line. Use dot notation for nested fields, such as <code>request.host</code>.
    </p>

    <div class="mt-3 flex h-40 overflow-hidden rounded-md border border-input bg-background font-mono text-xs leading-5 focus-within:ring-2 focus-within:ring-ring">
      <div
        bind:this={gutterElement}
        class="w-9 shrink-0 overflow-hidden border-r bg-muted/40 py-2 text-right text-muted-foreground select-none"
        aria-hidden="true"
      >
        {#each Array(lineCount) as _, index}
          <div class="pr-2">{index + 1}</div>
        {/each}
      </div>
      <label for="column-paths" class="sr-only">Column paths</label>
      <textarea
        id="column-paths"
        bind:value={draft}
        onscroll={syncLineNumbers}
        wrap="off"
        spellcheck={false}
        aria-describedby="columns-help columns-validation"
        class="min-w-0 flex-1 resize-none overflow-auto bg-transparent px-2 py-2 text-foreground outline-none"
      ></textarea>
    </div>

    <div class="mt-2 flex min-h-8 items-center justify-between gap-2">
      <p id="columns-validation" class="text-xs text-destructive" aria-live="polite">
        {draftColumns.length === 0 ? 'Enter at least one column.' : ''}
      </p>
      <button
        type="button"
        onclick={applyColumns}
        disabled={!canApply}
        class="h-8 shrink-0 rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground transition-opacity hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-40"
      >
        Apply
      </button>
    </div>
  </div>

  <div class="my-3 shrink-0 border-t"></div>

  <div class="flex min-h-0 flex-1 flex-col">
    <h2 class="shrink-0 text-sm font-semibold">Sample log entry</h2>
    <p class="mt-1 shrink-0 text-xs leading-5 text-muted-foreground">
      Column paths match this original JSON structure exactly.
    </p>
    {#if sampleFields}
      <pre class="mt-2 min-h-0 flex-1 overflow-auto rounded-md border bg-background p-2 font-mono text-[0.6875rem] leading-5 text-muted-foreground" aria-label="Sample JSON log entry">{sampleJSON}</pre>
    {:else}
      <div class="mt-2 min-h-0 flex-1 rounded-md border bg-background p-3 text-xs leading-5 text-muted-foreground" role="status">
        {#if viewer.session?.inputKind === 'raw'}
          No structured JSON fields are available.
        {:else if viewer.session?.inputStatus === 'eof' && viewer.session?.inputKind === 'records'}
          No structured fields were found in the loaded rows.
        {:else}
          Waiting for a structured log entry…
        {/if}
      </div>
    {/if}
  </div>
</section>
