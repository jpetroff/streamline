<script lang="ts">
  import { tick } from 'svelte';
  import type { ViewerController } from '$lib/transport/viewer-controller';
  import type { ViewerState } from '$lib/transport/viewer-state';

  let { viewer, controller, label = 'stdin', stopped = false }: { viewer: ViewerState; controller: ViewerController; label?: string; stopped?: boolean } = $props();
  let scrollElement = $state<HTMLDivElement>();
  let content = $derived(viewer.raw?.chunks.join('') ?? '');
  let complete = $derived(
    viewer.raw?.totalChunks !== undefined &&
    BigInt(viewer.raw.nextOffset) >= BigInt(viewer.raw.totalChunks),
  );

  $effect(() => {
    const raw = viewer.raw;
    const element = scrollElement;
    const done = complete;
    if (!raw || viewer.error || raw.loading || done || !element) return;
    void tick().then(requestMore);
  });

  function requestMore() {
    const element = scrollElement;
    if (!element || complete || viewer.error || viewer.raw?.loading) return;
    if (element.scrollHeight-element.scrollTop-element.clientHeight < 384) void controller.loadMoreRaw();
  }
</script>

<section class="flex h-full min-h-0 flex-col bg-background" aria-label={`Raw ${label} output`}>
  <!-- svelte-ignore a11y_no_noninteractive_tabindex (scrollable output must be keyboard-focusable) -->
  <div
    bind:this={scrollElement}
    onscroll={requestMore}
    class="min-h-0 flex-1 overflow-auto"
    role="region"
    aria-label={`Scrollable raw ${label} text`}
    tabindex="0"
  >
    {#if viewer.raw?.totalChunks === '0'}
      <div class="grid h-full place-items-center px-6 text-sm text-muted-foreground" role="status">
        {label === 'stdin' ? 'No stdin output.' : stopped ? 'Command stopped without output.' : viewer.session?.inputStatus === 'error' ? 'Command failed without output.' : 'Command completed without output.'}
      </div>
    {:else if content === '' && viewer.raw?.loading}
      <div class="grid h-full place-items-center px-6 text-sm text-muted-foreground" role="status">
        Loading raw output…
      </div>
    {:else}
      <pre class="m-0 min-w-max p-4 font-mono text-xs leading-5 text-foreground">{content}</pre>
      {#if viewer.raw?.loading}
        <div class="px-4 pb-3 font-mono text-xs text-muted-foreground" role="status">Loading more…</div>
      {/if}
    {/if}
  </div>
</section>
