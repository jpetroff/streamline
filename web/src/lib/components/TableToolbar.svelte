<script lang="ts">
  import { PanelLeft } from '@lucide/svelte';
  import { COLUMNS_PANEL } from '$lib/side-panels';

  let { rowLines = $bindable(1), columnsOpen = $bindable(true), showRowControls = true }: {
    rowLines?: 1 | 2;
    columnsOpen?: boolean;
    showRowControls?: boolean;
  } = $props();
</script>

<section class="flex h-8 shrink-0 items-center gap-2 border-t bg-shell px-3 text-xs" aria-label="Table toolbar">
  <button
    type="button"
    aria-label="Toggle columns and filters"
    aria-expanded={columnsOpen}
    aria-controls={COLUMNS_PANEL.id}
    title={columnsOpen ? 'Hide columns and filters' : 'Show columns and filters'}
    class="grid size-6 shrink-0 place-items-center rounded border border-input hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    onclick={() => { columnsOpen = !columnsOpen; }}
  ><PanelLeft size={16} aria-hidden="true" /></button>
  {#if showRowControls}
    <span class="text-muted-foreground">Rows</span>
    <div class="inline-flex overflow-hidden rounded border border-input" role="group" aria-label="Row display">
      {#each [1, 2] as lines}
        <button
          type="button"
          aria-pressed={rowLines === lines}
          title={lines === 1 ? 'Show one line per row' : 'Wrap up to two lines per row'}
          class="h-6 px-2 aria-pressed:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
          onclick={() => { rowLines = lines as 1 | 2; }}
        >{lines === 1 ? '1 line' : '2 lines'}</button>
      {/each}
    </div>
  {/if}
</section>
