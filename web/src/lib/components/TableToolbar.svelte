<script lang="ts">
  let { rowLines = $bindable(1) }: { rowLines?: 1 | 2 } = $props();
</script>

<section class="flex h-8 shrink-0 items-center gap-2 border-t bg-shell px-3 text-xs" aria-label="Table toolbar">
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
</section>
