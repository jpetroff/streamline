<script lang="ts">
  import { get } from 'svelte/store';
  import { tick } from 'svelte';
  import { createVirtualizer } from '@tanstack/svelte-virtual';
  import { formatColumnValue, resolveColumnValue } from '$lib/columns';
  import type { ViewerController } from '$lib/transport/viewer-controller';
  import type { LogRow, RowPage } from '$lib/transport/types';
  import type { ViewerState } from '$lib/transport/viewer-state';
  import {
    OVERSCAN_ROWS,
    ROW_HEIGHT,
    headSegment,
    rebasedSegment,
    scrollOffsetForAnchor,
    tailSegment,
    type VirtualSegment,
  } from '$lib/virtual-window';

  let {
    viewer,
    controller,
    columns,
  }: {
    viewer: ViewerState;
    controller: ViewerController;
    columns: readonly string[];
  } = $props();
  let scrollElement = $state<HTMLDivElement>();
  let segmentBase = $state(0n);
  let segmentCount = $state(0);
  let programmaticScroll = $state(false);
  let resumePending = $state(false);
  let activeSnapshot = '';
  let moveSequence = 0;

  let total = $derived(viewer.displayed ? BigInt(viewer.displayed.snapshot.matchedCount) : 0n);
  let rowsByOffset = $derived(indexPages(viewer.displayed?.pages ?? []));
  let gridTemplate = $derived(columns.map(() => 'minmax(12rem, 1fr)').join(' '));
  let minimumTableWidth = $derived(`${columns.length * 12}rem`);

  const virtualizer = createVirtualizer<HTMLDivElement, HTMLDivElement>({
    count: 0,
    getScrollElement: () => scrollElement ?? null,
    estimateSize: () => ROW_HEIGHT,
    overscan: OVERSCAN_ROWS,
  });

  // Keep the headless virtualizer synchronized with Svelte-owned segment state.
  $effect(() => {
    const count = segmentCount;
    const base = segmentBase;
    const element = scrollElement;
    get(virtualizer).setOptions({
      count,
      getScrollElement: () => element ?? null,
      estimateSize: () => ROW_HEIGHT,
      overscan: OVERSCAN_ROWS,
      getItemKey: index => (base + BigInt(index)).toString(),
    });
  });

  // A new immutable snapshot selects either its live tail or paused head segment.
  $effect(() => {
    const displayed = viewer.displayed;
    const token = displayed?.snapshot.snapshotToken ?? '';
    const following = viewer.following;
    const count = total;
    if (!displayed) {
      activeSnapshot = '';
      if (segmentCount !== 0 || segmentBase !== 0n) void moveSegment({ base: 0n, count: 0 }, 'start');
      return;
    }
    if (token === activeSnapshot) return;
    activeSnapshot = token;
    void moveSegment(following ? tailSegment(count) : headSegment(count), following ? 'end' : 'start');
  });

  // Translate the rendered range into data requests, rebases, and follow transitions.
  $effect(() => {
    const instance = $virtualizer;
    const range = instance.range;
    const items = instance.getVirtualItems();
    const base = segmentBase;
    const count = segmentCount;
    const matchedCount = total;
    const moving = programmaticScroll;
    const following = viewer.following;
    if (moving || !viewer.displayed || !range || items.length === 0) return;

    const nextSegment = rebasedSegment(matchedCount, base, range.startIndex, range.endIndex);
    if (nextSegment.base !== base) {
      void rebase(nextSegment, range.startIndex);
      return;
    }

    const first = items[0];
    const last = items[items.length - 1];
    const start = base + BigInt(first.index);
    const endExclusive = minBigInt(matchedCount, base + BigInt(last.index + 1));
    void controller.ensureRange(start, endExclusive);

    const atTail = base + BigInt(range.endIndex + 1) >= matchedCount;
    if (following && !atTail) {
      controller.pause();
    } else if (!following && atTail && !resumePending) {
      resumePending = true;
      void controller.resume().finally(() => { resumePending = false; });
    }

    if (count === 0) scrollElement?.scrollTo({ top: 0 });
  });

  /** Installs a segment and moves to its first or final logical row after DOM update. */
  async function moveSegment(segment: VirtualSegment, align: 'start' | 'end') {
    const sequence = ++moveSequence;
    programmaticScroll = true;
    segmentBase = segment.base;
    segmentCount = segment.count;
    await tick();
    if (sequence !== moveSequence) return;
    const instance = get(virtualizer);
    if (segment.count === 0) {
      scrollElement?.scrollTo({ top: 0 });
    } else {
      instance.scrollToIndex(align === 'end' ? segment.count - 1 : 0, { align });
    }
    finishProgrammaticScroll(sequence);
  }

  /** Replaces the browser-sized segment while preserving the visible logical anchor. */
  async function rebase(segment: VirtualSegment, visibleStart: number) {
    const sequence = ++moveSequence;
    const logicalAnchor = segmentBase + BigInt(visibleStart);
    const rawIntraRowOffset = (scrollElement?.scrollTop ?? 0) - visibleStart * ROW_HEIGHT;
    const intraRowOffset = Math.max(0, Math.min(ROW_HEIGHT - 1, rawIntraRowOffset));
    programmaticScroll = true;
    segmentBase = segment.base;
    segmentCount = segment.count;
    await tick();
    if (sequence !== moveSequence) return;
    scrollElement?.scrollTo({ top: scrollOffsetForAnchor(logicalAnchor, segment.base, intraRowOffset) });
    finishProgrammaticScroll(sequence);
  }

  /** Re-enables user-scroll behavior after the virtualizer observes a programmatic move. */
  function finishProgrammaticScroll(sequence: number) {
    requestAnimationFrame(() => {
      if (sequence === moveSequence) programmaticScroll = false;
    });
  }

  /** Builds the sparse logical-offset lookup used by currently mounted virtual rows. */
  function indexPages(pages: RowPage[]) {
    const rows = new Map<string, LogRow>();
    for (const page of pages) {
      const offset = BigInt(page.offset);
      for (let index = 0; index < page.rows.length; index++) {
        rows.set((offset + BigInt(index)).toString(), page.rows[index]);
      }
    }
    return rows;
  }

  function minBigInt(left: bigint, right: bigint) { return left < right ? left : right; }
</script>

<section class="flex h-full min-h-0 flex-col bg-background" aria-label="Log output">
  <div class="min-h-0 flex-1 overflow-x-auto">
    <div
      class="flex h-full min-w-full flex-col"
      style={`width: max(100%, ${minimumTableWidth});`}
      role="table"
      aria-label="Log records"
      aria-busy={viewer.pending !== undefined}
      aria-colcount={columns.length}
    >
      <div
        class="grid h-9 shrink-0 items-center border-b bg-table-header px-3 font-mono text-xs font-medium tracking-[0.04em] text-muted-foreground"
        style={`grid-template-columns: ${gridTemplate};`}
        role="rowgroup"
      >
        <div role="row" class="contents">
          {#each columns as column, index (`${index}:${column}`)}
            <div class="truncate pr-4" role="columnheader" title={column}>{column}</div>
          {/each}
        </div>
      </div>

      <div bind:this={scrollElement} class="relative min-h-0 flex-1 overflow-y-auto overflow-x-hidden" role="rowgroup">
        {#if !viewer.displayed}
          <div class="grid h-full place-items-center px-6 text-sm text-muted-foreground" role="status">
            {#if viewer.pending}
              Preparing logs{viewer.pending.progress === undefined ? '…' : `… ${Math.round(viewer.pending.progress * 100)}%`}
            {:else}
              Connecting…
            {/if}
          </div>
        {:else if total === 0n}
          <div class="grid h-full place-items-center px-6 text-sm text-muted-foreground" role="status">
            No log records.
          </div>
        {:else}
          <div class="relative w-full" style={`height: ${$virtualizer.getTotalSize()}px;`}>
            {#each $virtualizer.getVirtualItems() as item (item.key)}
              {@const logicalIndex = segmentBase + BigInt(item.index)}
              {@const row = rowsByOffset.get(logicalIndex.toString())}
              <div
                class="absolute left-0 top-0 grid w-full items-center border-b border-border/70 px-3 font-mono text-xs hover:bg-row-hover"
                style={`height: ${item.size}px; transform: translateY(${item.start}px); grid-template-columns: ${gridTemplate};`}
                role="row"
                aria-busy={row === undefined}
              >
                {#if row}
                  {#each columns as column, index (`${index}:${column}`)}
                    {@const value = resolveColumnValue(row.fields, column)}
                    {@const formatted = formatColumnValue(value)}
                    <span
                      class={`truncate pr-4 ${value === undefined ? 'text-muted-foreground/70' : 'text-foreground'}`}
                      role="cell"
                      title={value === undefined ? 'Not present' : formatted}
                      aria-label={value === undefined ? `${column}: not present` : undefined}
                    >{formatted}</span>
                  {/each}
                {:else}
                  {#each columns as _, index (index)}
                    <span class="mr-8 h-2.5 animate-pulse rounded-sm bg-placeholder" role="cell" aria-hidden="true"></span>
                  {/each}
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  </div>
</section>
