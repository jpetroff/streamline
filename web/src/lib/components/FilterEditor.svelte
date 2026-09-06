<script lang="ts">
  import { untrack, type Snippet } from 'svelte';
  import { Dialog } from 'bits-ui';
  import { cloneFilters, FILTER_OPERATORS, filtersEqual, isNumericOperator, parseFilterJSON, validateFilters, visibleFilterErrors, type FilterRejection } from '$lib/filters';
  import type { APIErrorBody, FilterError, FilterSpec } from '$lib/transport/types';

  let { applied = [], pending = false, disabled = false, helper, onApply }: {
    applied?: FilterSpec[]; pending?: boolean; disabled?: boolean; helper: Snippet;
    onApply: (filters: FilterSpec[]) => Promise<APIErrorBody | undefined>;
  } = $props();
  type DraftRow = { id: number; field: string; op: FilterSpec['op']; value: string };
  let nextID = 0;
  function rowsFrom(filters: FilterSpec[]): DraftRow[] { return filters.map(filter => ({ ...filter, id: nextID++, value: String(filter.value) })); }
  let rows = $state<DraftRow[]>(untrack(() => rowsFrom(applied)));
  let rejection = $state<FilterRejection>();
  let requesting = $state(false);
  let importOpen = $state(false);
  let importText = $state('');
  let importErrors = $state<FilterError[]>([]);
  let copyStatus = $state('');
  let submission = 0;
  const draft = $derived(rows.map(({ field, op, value }) => ({ field, op, value: isNumericOperator(op) ? (value.trim() === '' ? NaN : Number(value)) : value })) as FilterSpec[]);
  const errors = $derived([...validateFilters(draft), ...visibleFilterErrors(draft, rejection)]);
  const valid = $derived(errors.length === 0);
  const canApply = $derived(valid && !disabled && !requesting && !filtersEqual(draft, applied));
  const button = 'rounded border px-2 py-1.5 text-xs hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-40';
  const input = 'w-full min-w-0 rounded border border-input bg-background px-2 py-1.5 text-xs outline-none focus:ring-2 focus:ring-ring';

  $effect(() => {
    const previous = rejection;
    if (!previous) return;
    const remaining = visibleFilterErrors(draft, previous);
    if (remaining.length !== previous.errors.length) rejection = { ...previous, errors: remaining };
  });

  function clearFeedback() { copyStatus = ''; }
  function removeRow(id: number) { rows = rows.filter(row => row.id !== id); rejection = undefined; clearFeedback(); }
  function importFilters() {
    const result = parseFilterJSON(importText);
    importErrors = result.errors;
    if (!result.filters) return;
    rows = rowsFrom(result.filters);
    rejection = undefined;
    clearFeedback();
    importOpen = false;
  }
  async function copyJSON() {
    if (!valid) return;
    try { await navigator.clipboard.writeText(JSON.stringify(draft, null, 2)); copyStatus = 'JSON copied.'; }
    catch { copyStatus = 'Could not copy JSON. Check clipboard access and try again.'; }
  }
  async function apply() {
    if (!canApply) return;
    const confirmed = cloneFilters(draft);
    const confirmedIDs = rows.map(row => row.id);
    const currentSubmission = ++submission;
    requesting = true;
    rejection = undefined;
    try {
      const error = await onApply(confirmed);
      if (currentSubmission === submission && error?.filterErrors) {
        // Keep diagnostics attached to the submitted row even if other rows were edited or removed.
        const currentErrors = error.filterErrors.flatMap(issue => {
          const index = rows.findIndex(row => row.id === confirmedIDs[issue.index - 1]);
          return index >= 0 && filtersEqual([draft[index]], [confirmed[issue.index - 1]])
            ? [{ ...issue, index: index + 1 }] : [];
        });
        rejection = { filters: cloneFilters(draft), errors: currentErrors };
      }
    } finally { if (currentSubmission === submission) requesting = false; }
  }
</script>

<section class="border-t pt-3" aria-label="Filters">
  <div class="flex items-center gap-2"><h2 class="text-sm font-semibold">Filters</h2>{@render helper()}</div>
  <p class="mt-1 text-xs leading-5 text-muted-foreground">All conditions must match, in order. Paths use original JSON fields. Text ignores case.</p>
  {#if rows.length === 0}<p class="my-3 text-xs text-muted-foreground">No filters. Add a condition or import JSON.</p>{/if}
  <div class="mt-3 space-y-3">
    {#each rows as row, index (row.id)}
      {@const rowErrors = errors.filter(error => error.index === index + 1)}
      <fieldset class="min-w-0 space-y-2 rounded-md border p-2" aria-describedby={`filter-errors-${row.id}`}>
        <legend class="px-1 text-xs text-muted-foreground">Filter {index + 1}</legend>
        <div class="flex gap-2">
          <label class="min-w-0 flex-1"><span class="sr-only">Field for filter {index + 1}</span><input class={input} bind:value={row.field} oninput={clearFeedback} placeholder="request.host" spellcheck={false} aria-invalid={rowErrors.some(error => error.property === 'field')} /></label>
          <button type="button" class={button} onclick={() => removeRow(row.id)} aria-label={`Remove filter ${index + 1}`}>×</button>
        </div>
        <label class="block"><span class="sr-only">Operator for filter {index + 1}</span><select class={input} bind:value={row.op} onchange={clearFeedback}>{#each FILTER_OPERATORS as operator}<option value={operator.value}>{operator.label}</option>{/each}</select></label>
        <label class="block"><span class="sr-only">Value for filter {index + 1}</span>
          {#if isNumericOperator(row.op)}
            <input class={input} type="text" inputmode="decimal" bind:value={row.value} oninput={clearFeedback} placeholder="100" aria-invalid={rowErrors.some(error => error.property === 'value')} />
          {:else}
            <textarea class={`${input} min-h-14 resize-y font-mono`} rows="2" bind:value={row.value} oninput={clearFeedback} placeholder={row.op === 'regex' ? '^error|timeout$' : 'Text value'} spellcheck={false} aria-invalid={rowErrors.some(error => error.property === 'value')}></textarea>
          {/if}
        </label>
        <div id={`filter-errors-${row.id}`} class="text-xs text-destructive" aria-live="polite">{#each rowErrors as error}<p>{error.message}</p>{/each}</div>
      </fieldset>
    {/each}
  </div>
  <div class="mt-3 flex flex-wrap gap-2">
    <button type="button" class={button} onclick={() => { rows = [...rows, { id: nextID++, field: '', op: 'eq', value: '' }]; clearFeedback(); }}>Add filter</button>
    <button type="button" class={button} disabled={rows.length === 0} onclick={() => { rows = []; rejection = undefined; clearFeedback(); }}>Clear all</button>
    <Dialog.Root bind:open={importOpen} onOpenChange={open => { if (open) { importText = ''; importErrors = []; } }}>
      <Dialog.Trigger class={button}>Import JSON</Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay class="fixed inset-0 z-50 bg-black/60" />
        <Dialog.Content class="fixed left-1/2 top-1/2 z-50 flex max-h-[90vh] w-[min(36rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 flex-col gap-3 overflow-auto rounded-lg border bg-popover p-4 text-popover-foreground shadow-xl">
          <Dialog.Title class="text-sm font-semibold">Import filters</Dialog.Title>
          <Dialog.Description class="text-xs leading-5 text-muted-foreground">Paste an array of field, op, and value objects. Import replaces your draft; Apply updates the results.</Dialog.Description>
          <label for="filter-json" class="text-xs">Filter JSON</label>
          <textarea id="filter-json" class={`${input} min-h-56 resize-y font-mono leading-5`} bind:value={importText} oninput={() => { importErrors = []; }} spellcheck={false} aria-invalid={importErrors.length > 0} aria-describedby="filter-import-errors" placeholder={'[\n  { "field": "level", "op": "eq", "value": "error" }\n]'}></textarea>
          <div id="filter-import-errors" class="text-xs text-destructive" role="alert">{#each importErrors as error}<p>{error.index ? `Filter ${error.index}${error.property ? ` (${error.property})` : ''}: ` : ''}{error.message}</p>{/each}</div>
          <div class="flex justify-end gap-2"><Dialog.Close class={button}>Cancel</Dialog.Close><button type="button" class={button} onclick={importFilters}>Import</button></div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
    <button type="button" class={button} disabled={!valid} onclick={() => void copyJSON()}>Copy JSON</button>
  </div>
  <div class="mt-3 flex items-center justify-between gap-2">
    <p class="text-xs text-muted-foreground" role="status">{disabled ? 'Filters apply to structured log entries.' : filtersEqual(draft, applied) ? `${applied.length} active` : 'Unapplied changes'}</p>
    <button type="button" class="rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-40" disabled={!canApply} onclick={() => void apply()}>{requesting || pending ? 'Applying…' : 'Apply'}</button>
  </div>
  <p class="mt-1 text-xs text-muted-foreground" role="status">{copyStatus}</p>
</section>
