<script lang="ts">
  import { X } from '@lucide/svelte';
  import { formatColumnValue, formatDetailColumnValue, resolveRowColumnValue, type ColumnConfig } from '$lib/columns';
  import type { LogRow } from '$lib/transport/types';

  let {
    row,
    error,
    columns,
    onClose,
  }: {
    row?: LogRow;
    error?: string;
    columns: readonly ColumnConfig[];
    onClose: () => void;
  } = $props();

  let fullEntry = $derived(formatDetailColumnValue(row?.fields ?? row?.message));

</script>

<div class="flex h-full min-h-0 min-w-0 flex-col">
  <header class="flex h-12 shrink-0 items-center justify-between gap-3 border-b px-3">
    <div class="min-w-0">
      <h2 class="text-sm font-semibold">Row details</h2>
      <p class="truncate font-mono text-[0.6875rem] text-muted-foreground" title={row ? `Row ${row.id}` : undefined}>{row ? `Row ${row.id}` : 'Loading row…'}</p>
    </div>
    <button
      type="button"
      onclick={onClose}
      class="grid size-8 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      aria-label="Close row details"
      title="Close row details (Escape)"
    >
      <X size={16} aria-hidden="true" />
    </button>
  </header>

  <div class="min-h-0 flex-1 overflow-y-auto p-3">
    {#if row}
    <dl class="space-y-3">
      {#each columns as column, index (`${index}:${column.path}`)}
        {@const value = resolveRowColumnValue(row, column.path)}
        {@const formatted = column.dateFormat === 'original'
          ? formatDetailColumnValue(value)
          : { text: formatColumnValue(value, column.dateFormat), kind: value === undefined ? 'absent' : 'scalar' }}
        <div class="overflow-hidden rounded-md border bg-background">
          <dt class="border-b bg-muted/30 px-3 py-2 font-mono text-xs font-medium text-muted-foreground" title={column.path}>
            {column.path}
          </dt>
          <dd class="m-0">
            {#if formatted.kind === 'json'}
              <pre class="max-h-80 overflow-auto p-3 font-mono text-xs leading-5 text-foreground">{formatted.text}</pre>
            {:else}
              <div
                class={`whitespace-pre-wrap break-words px-3 py-2 font-mono text-xs leading-5 ${formatted.kind === 'absent' ? 'text-muted-foreground/70' : 'text-foreground'}`}
                aria-label={formatted.kind === 'absent' ? `${column.path}: not present` : undefined}
              >{formatted.text}</div>
            {/if}
          </dd>
        </div>
      {/each}
    </dl>

    <section class="mt-5 border-t pt-4" aria-labelledby="full-entry-heading">
      <h3 id="full-entry-heading" class="text-sm font-semibold">Full log entry</h3>
      <div class="mt-2 overflow-hidden rounded-md border bg-background">
        {#if fullEntry.kind === 'json'}
          <pre class="overflow-x-auto p-3 font-mono text-xs leading-5 text-foreground">{fullEntry.text}</pre>
        {:else}
          <div class="whitespace-pre-wrap break-words px-3 py-2 font-mono text-xs leading-5 text-foreground">
            {fullEntry.text}
          </div>
        {/if}
      </div>
    </section>
    {:else}
      <p class="text-sm" role="status">{error ?? 'Loading row…'}</p>
    {/if}
  </div>
</div>
