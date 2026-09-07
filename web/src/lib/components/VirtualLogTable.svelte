<script lang="ts">
  import { get } from 'svelte/store';
  import { tick, untrack } from 'svelte';
  import { createVirtualizer } from '@tanstack/svelte-virtual';
  import {
    DATE_FORMAT_OPTIONS,
    formatColumnValue,
    isDateColumnPath,
    resolveRowColumnValue,
    type ColumnConfig,
    type DateDisplayFormat,
  } from '$lib/columns';
  import type { ViewerController } from '$lib/transport/viewer-controller';
  import type { LogRow, RowPage } from '$lib/transport/types';
  import type { ViewerState } from '$lib/transport/viewer-state';
  import {
    OVERSCAN_ROWS,
    ROW_HEIGHT,
    WRAPPED_ROW_HEIGHT,
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
    rowLines = 1,
    onDateFormatChange,
    selectedRowId,
    onOpenDetails,
  }: {
    viewer: ViewerState;
    controller: ViewerController;
    columns: readonly ColumnConfig[];
    rowLines?: 1 | 2;
    onDateFormatChange: (index: number, format: DateDisplayFormat) => void;
    selectedRowId?: string;
    onOpenDetails: (row: LogRow) => void;
  } = $props();
  let scrollElement = $state<HTMLDivElement>();
  let headerElement: HTMLDivElement;
  const minimumColumnWidth = 144;
  let columnWidths = $state<Record<string, number>>({});
  let resize = $state<{ key: string; pointerId: number; startX: number; startWidth: number }>();
  let segmentBase = $state(0n);
  let segmentCount = $state(0);
  let programmaticScroll = $state(false);
  let resumePending = $state(false);
  let activeSnapshot = '';
  let moveSequence = 0;
  let measuredRowHeight = ROW_HEIGHT;
  let rowHeight = $derived(rowLines === 2 ? WRAPPED_ROW_HEIGHT : ROW_HEIGHT);

  let total = $derived(viewer.displayed ? BigInt(viewer.displayed.snapshot.matchedCount) : 0n);
  let rowsByOffset = $derived(indexPages(viewer.displayed?.pages ?? []));
  let widths = $derived(columns.map((column, index) => columnWidths[`${index}:${column.path}`]));
  let gridTemplate = $derived(widths.map(width => width === undefined ? 'minmax(12rem, 1fr)' : `${width}px`).join(' '));
  let minimumTableWidth = $derived(`calc(${widths.map(width => width === undefined ? '12rem' : `${width}px`).join(' + ') || '0px'})`);
  let tableWidth = $derived(widths.length > 0 && widths.every(width => width !== undefined)
    ? minimumTableWidth
    : `max(100%, ${minimumTableWidth})`);

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
    const height = rowHeight;
    get(virtualizer).setOptions({
      count,
      getScrollElement: () => element ?? null,
      estimateSize: () => height,
      overscan: OVERSCAN_ROWS,
      getItemKey: index => (base + BigInt(index)).toString(),
    });
    untrack(() => {
      if (height === measuredRowHeight) return;
      const previousHeight = measuredRowHeight;
      measuredRowHeight = height;
      void updateRowHeight(previousHeight, height);
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

  /** Rebuilds fixed row measurements while preserving the visible row or live tail. */
  async function updateRowHeight(previousHeight: number, height: number) {
    const sequence = ++moveSequence;
    const offset = scrollElement?.scrollTop ?? 0;
    const visibleIndex = Math.floor(offset / previousHeight);
    const intraRowOffset = Math.min(height - 1, offset % previousHeight);
    const following = viewer.following;
    programmaticScroll = true;
    const instance = get(virtualizer);
    instance.measure();
    await tick();
    if (sequence !== moveSequence) return;
    if (following && segmentCount > 0) {
      instance.scrollToIndex(segmentCount - 1, { align: 'end' });
    } else {
      scrollElement?.scrollTo({ top: visibleIndex * height + intraRowOffset });
    }
    finishProgrammaticScroll(sequence);
  }

  /** Replaces the browser-sized segment while preserving the visible logical anchor. */
  async function rebase(segment: VirtualSegment, visibleStart: number) {
    const sequence = ++moveSequence;
    const logicalAnchor = segmentBase + BigInt(visibleStart);
    const rawIntraRowOffset = (scrollElement?.scrollTop ?? 0) - visibleStart * rowHeight;
    const intraRowOffset = Math.max(0, Math.min(rowHeight - 1, rawIntraRowOffset));
    programmaticScroll = true;
    segmentBase = segment.base;
    segmentCount = segment.count;
    await tick();
    if (sequence !== moveSequence) return;
    scrollElement?.scrollTo({ top: scrollOffsetForAnchor(logicalAnchor, segment.base, intraRowOffset, rowHeight) });
    finishProgrammaticScroll(sequence);
  }

  /** Re-enables user-scroll behavior after the virtualizer observes a programmatic move. */
  function finishProgrammaticScroll(sequence: number) {
    requestAnimationFrame(() => {
      if (sequence === moveSequence) programmaticScroll = false;
    });
  }

  function handleRowClick(event: MouseEvent, row: LogRow | undefined) {
    if (!row || !event.shiftKey || event.button !== 0) return;
    event.preventDefault();
    onOpenDetails(row);
  }

  function handleRowKeydown(event: KeyboardEvent, row: LogRow | undefined) {
    if (!row || !event.shiftKey || event.key !== 'Enter') return;
    event.preventDefault();
    onOpenDetails(row);
  }

  function preventShiftSelection(event: MouseEvent, row: LogRow | undefined) {
    if (row && event.shiftKey && event.button === 0) event.preventDefault();
  }

  // Freeze the rendered widths so dragging one column leaves its neighbors unchanged.
  function measureColumnWidths() {
    const headers = headerElement.querySelectorAll<HTMLElement>('[role="columnheader"]');
    columnWidths = Object.fromEntries(columns.map((column, index) => [
      `${index}:${column.path}`,
      headers[index].getBoundingClientRect().width,
    ]));
  }

  function startResize(event: PointerEvent, key: string) {
    if (event.button !== 0 || resize) return;
    event.preventDefault();
    measureColumnWidths();
    (event.currentTarget as HTMLButtonElement).setPointerCapture(event.pointerId);
    resize = { key, pointerId: event.pointerId, startX: event.clientX, startWidth: columnWidths[key] };
  }

  function moveResize(event: PointerEvent) {
    if (!resize || event.pointerId !== resize.pointerId) return;
    columnWidths[resize.key] = Math.max(minimumColumnWidth, resize.startWidth + event.clientX - resize.startX);
  }

  function endResize(event: PointerEvent) {
    if (event.pointerId !== resize?.pointerId) return;
    resize = undefined;
    const handle = event.currentTarget as HTMLButtonElement;
    if (handle.hasPointerCapture(event.pointerId)) handle.releasePointerCapture(event.pointerId);
  }

  function resizeWithKeyboard(event: KeyboardEvent, key: string) {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    measureColumnWidths();
    columnWidths[key] = Math.max(minimumColumnWidth, columnWidths[key] + (event.key === 'ArrowRight' ? 16 : -16));
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

<section class="flex h-full min-h-0 flex-col bg-background" class:resizing={resize !== undefined} aria-label="Log output">
  <div class="min-h-0 flex-1 overflow-x-auto">
    <div
      class="flex h-full flex-col"
      style={`width: ${tableWidth};`}
      role="table"
      aria-label="Log records"
      aria-busy={viewer.pending !== undefined}
      aria-colcount={columns.length}
    >
      <div
        bind:this={headerElement}
        class="grid h-9 shrink-0 border-b bg-table-header font-mono text-xs font-medium tracking-[0.04em] text-muted-foreground"
        style={`grid-template-columns: ${gridTemplate};`}
        role="rowgroup"
      >
        <div role="row" class="contents">
          {#each columns as column, index (`${index}:${column.path}`)}
            <div class="relative flex min-w-0 items-center gap-2 border-r px-3" role="columnheader">
              <span class="min-w-0 flex-1 truncate" title={column.path}>{column.path}</span>
              {#if isDateColumnPath(column.path)}
                <label class="shrink-0">
                  <span class="sr-only">Date display for {column.path}</span>
                  <select
                    value={column.dateFormat}
                    onchange={event => onDateFormatChange(index, event.currentTarget.value as DateDisplayFormat)}
                    aria-label={`Date display for ${column.path}`}
                    title="Date display format"
                    class="h-6 max-w-24 rounded border border-input bg-background px-1 font-sans text-[0.6875rem] font-normal tracking-normal text-foreground outline-none focus:ring-2 focus:ring-ring"
                  >
                    {#each DATE_FORMAT_OPTIONS as option}
                      <option value={option.value}>{option.label}</option>
                    {/each}
                  </select>
                </label>
              {/if}
              <button
                type="button"
                class="resize-handle"
                class:active={resize?.key === `${index}:${column.path}`}
                aria-label={`Resize ${column.path} column`}
                onpointerdown={event => startResize(event, `${index}:${column.path}`)}
                onpointermove={moveResize}
                onpointerup={endResize}
                onpointercancel={endResize}
                onlostpointercapture={endResize}
                onkeydown={event => resizeWithKeyboard(event, `${index}:${column.path}`)}
              ></button>
            </div>
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
              {@const isSelected = row !== undefined && row.id === selectedRowId}
              <div
                class={`absolute left-0 top-0 grid w-full border-b border-border/70 font-mono text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring ${isSelected ? 'bg-accent' : 'hover:bg-row-hover'}`}
                style={`height: ${item.size}px; transform: translateY(${item.start}px); grid-template-columns: ${gridTemplate};`}
                role="row"
                aria-busy={row === undefined}
                aria-selected={row ? isSelected : undefined}
                aria-keyshortcuts={row ? 'Shift+Enter' : undefined}
                tabindex={row ? 0 : undefined}
                onmousedown={event => preventShiftSelection(event, row)}
                onclick={event => handleRowClick(event, row)}
                onkeydown={event => handleRowKeydown(event, row)}
              >
                {#if row}
                  {#each columns as column, index (`${index}:${column.path}`)}
                    {@const value = resolveRowColumnValue(row, column.path)}
                    {@const formatted = formatColumnValue(value, column.dateFormat)}
                    <span
                      class={`flex min-w-0 items-center border-r border-border/70 px-3 ${value === undefined ? 'text-muted-foreground/70' : 'text-foreground'}`}
                      role="cell"
                      title={value === undefined ? 'Not present' : formatted}
                      aria-label={value === undefined ? `${column.path}: not present` : undefined}
                    >
                      <span class={rowLines === 2 ? 'line-clamp-2 whitespace-pre-wrap leading-4 [overflow-wrap:anywhere]' : 'truncate leading-4'}>{formatted}</span>
                    </span>
                  {/each}
                {:else}
                  {#each columns as _, index (index)}
                    <span class="flex min-w-0 items-center border-r border-border/70 px-3" role="cell" aria-hidden="true">
                      <span class="mr-5 h-2.5 w-full animate-pulse rounded-sm bg-placeholder"></span>
                    </span>
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

<style>
  .resize-handle {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    width: 6px;
    cursor: col-resize;
    touch-action: none;
  }

  .resize-handle:hover,
  .resize-handle:focus-visible,
  .resize-handle.active {
    background: var(--color-ring);
    outline: none;
  }

  .resizing,
  .resizing :global(*) {
    cursor: col-resize !important;
    user-select: none;
  }
</style>
