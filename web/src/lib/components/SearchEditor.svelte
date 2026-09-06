<script lang="ts">
  import { untrack } from 'svelte';
  import { EMPTY_SEARCH, normalizeSearchText, searchesEqual, validateSearch, visibleServerErrors, type SearchRejection } from '$lib/search';
  import type { APIErrorBody, LineError, SearchSpec } from '$lib/transport/types';

  let { applied = EMPTY_SEARCH, pending = false, onApply }: {
    applied?: SearchSpec;
    pending?: boolean;
    onApply: (search: SearchSpec) => Promise<APIErrorBody | undefined>;
  } = $props();

  let draft = $state<SearchSpec>(untrack(() => ({ ...applied })));
  let rejection = $state<SearchRejection>();
  let requesting = $state(false);
  let gutter = $state<HTMLDivElement>();
  let textarea = $state<HTMLTextAreaElement>();
  let tooltip = $state<{ message: string; x: number; y: number }>();
  let submission = 0;
  const frontendErrors = $derived(validateSearch(draft));
  const errors = $derived([...visibleServerErrors(draft, rejection), ...frontendErrors]);
  const errorsByLine = $derived(new Map(errors.map(error => [error.line, error])));
  const lineCount = $derived(normalizeSearchText(draft.text).split('\n').length);
  const canApply = $derived(errors.length === 0 && !requesting && !searchesEqual(draft, applied));

  // Once an expression is edited, discard its server error permanently rather
  // than reviving it if the user later types the old text again.
  $effect(() => {
    const previous = rejection;
    if (!previous) return;
    const remaining = visibleServerErrors(draft, previous);
    if (remaining.length !== previous.errors.length) rejection = { ...previous, errors: remaining };
  });

  async function apply() {
    const confirmed = { ...draft, text: normalizeSearchText(draft.text) };
    if (!canApply || validateSearch(confirmed).length !== 0) return;
    const currentSubmission = ++submission;
    requesting = true;
    rejection = undefined;
    tooltip = undefined;
    try {
      const error = await onApply(confirmed);
      // Responses belong to the confirmed draft, never to edits made in flight.
      if (currentSubmission === submission && searchesEqual(draft, confirmed) && error?.lineErrors) {
        rejection = { search: confirmed, errors: error.lineErrors };
      }
    } finally {
      if (currentSubmission === submission) requesting = false;
    }
  }

  function confirmWithKeyboard(event: KeyboardEvent) {
    if (event.key === 'Enter' && (event.ctrlKey || event.metaKey) && !event.isComposing) {
      event.preventDefault();
      void apply();
    }
  }

  function syncGutter() {
    if (gutter && textarea) gutter.scrollTop = textarea.scrollTop;
    tooltip = undefined;
  }

  function showError(event: MouseEvent | FocusEvent, error: LineError) {
    if (event.type === 'focus' && textarea && gutter) textarea.scrollTop = gutter.scrollTop;
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
    tooltip = { message: error.message, x: rect.right + 8, y: rect.top };
  }
</script>

<section class="shrink-0 border-t bg-shell p-3" aria-label="General search">
  <div class="mb-2 flex flex-wrap items-center gap-3 text-xs">
    <label for="general-search" class="font-semibold">Search</label>
    <div class="inline-flex rounded border border-input" role="group" aria-label="Search mode">
      {#each ['plain', 'regexp'] as mode}
        <button type="button" aria-pressed={draft.mode === mode}
          class="px-2 py-1 aria-pressed:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          onclick={() => { draft = { ...draft, mode: mode as SearchSpec['mode'] }; tooltip = undefined; }}
        >{mode === 'plain' ? 'Plain' : 'Regexp'}</button>
      {/each}
    </div>
    <div class="inline-flex rounded border border-input" role="group" aria-label="Combine search lines">
      {#each ['or', 'and'] as operator}
        <button type="button" aria-pressed={draft.operator === operator}
          class="px-2 py-1 aria-pressed:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          onclick={() => { draft = { ...draft, operator: operator as SearchSpec['operator'] }; }}
        >{operator.toUpperCase()}</button>
      {/each}
    </div>
    <span class="text-muted-foreground">Ignore case · {draft.operator === 'or' ? 'Any line' : 'All lines in one entry'}</span>
    <button type="button" onclick={() => void apply()} disabled={!canApply}
      class="ml-auto rounded bg-primary px-3 py-1.5 font-medium text-primary-foreground hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-40"
      aria-keyshortcuts="Control+Enter Meta+Enter"
    >{requesting || pending ? 'Applying…' : 'Apply'}</button>
  </div>

  <div class="flex h-24 overflow-hidden rounded border border-input bg-background font-mono text-xs leading-5 focus-within:ring-2 focus-within:ring-ring">
    <div bind:this={gutter} class="w-9 shrink-0 overflow-hidden border-r bg-muted/40 py-2 text-center text-muted-foreground">
      {#each Array(lineCount) as _, index}
        {@const error = errorsByLine.get(index + 1)}
        <div class="h-5">
          {#if error}
            <button type="button" class="h-5 w-full text-destructive focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
              aria-label={`Error on line ${error.line}`} aria-describedby={`search-error-${error.line}`}
              onmouseenter={event => showError(event, error)} onmouseleave={() => { tooltip = undefined; }}
              onfocus={event => showError(event, error)} onblur={() => { tooltip = undefined; }}
            >●</button>
          {:else}
            <span aria-hidden="true">{index + 1}</span>
          {/if}
        </div>
      {/each}
    </div>
    <textarea id="general-search" bind:this={textarea} bind:value={draft.text}
      onscroll={syncGutter} onkeydown={confirmWithKeyboard} oninput={() => { tooltip = undefined; }}
      wrap="off" spellcheck={false} autocapitalize="off" aria-invalid={errors.length > 0}
      aria-describedby="search-help search-validation"
      placeholder={draft.mode === 'plain' ? 'timeout\nconnection refused' : 'timeout|refused\nstatus=[45][0-9]{2}'}
      class="min-w-0 flex-1 resize-none overflow-auto bg-transparent px-2 py-2 text-foreground outline-none"
    ></textarea>
  </div>
  <div class="mt-1 flex flex-wrap gap-x-3 text-xs leading-5">
    <p id="search-help" class="text-muted-foreground">One filter per line. Ctrl/Cmd+Enter to apply. Empty search shows all entries allowed by other filters.</p>
    <p id="search-validation" class="text-destructive" aria-live="polite">
      {errorsByLine.size > 0 ? `${errorsByLine.size} invalid ${errorsByLine.size === 1 ? 'line' : 'lines'}. Hover or focus a bullet for details.` : ''}
    </p>
  </div>
  {#each [...errorsByLine.values()] as error}
    <span class="sr-only" id={`search-error-${error.line}`}>Line {error.line}: {error.message}</span>
  {/each}
</section>

{#if tooltip}
  <div role="tooltip" class="pointer-events-none fixed z-50 max-w-[min(28rem,calc(100vw-4rem))] break-words rounded border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-lg"
    style={`left: ${tooltip.x}px; top: ${tooltip.y}px; transform: translateY(-100%);`}
  >{tooltip.message}</div>
{/if}
