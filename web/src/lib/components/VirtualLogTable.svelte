<script lang="ts">
  import { registerCommand } from '$lib/keyboard-context';
  import { clampRow, type ActiveRow, type RowNavigator } from '$lib/row-navigation';
  import { get } from 'svelte/store';
  import { tick, untrack } from 'svelte';
  import { createVirtualizer, type Virtualizer, type Rect } from '@tanstack/svelte-virtual';
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
    segmentForRow,
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
    onColumnWidthsChange,
    activeRow = $bindable(),
    navigator = $bindable(),
    onReset,
    onOpenDetails,
  }: {
    viewer: ViewerState;
    controller: ViewerController;
    columns: readonly ColumnConfig[];
    rowLines?: 1 | 2;
    onColumnWidthsChange: (widths: number[]) => void;
    onDateFormatChange: (index: number, format: DateDisplayFormat) => void;
    activeRow?: ActiveRow;
    navigator?: RowNavigator;
    onReset: () => void;
    onOpenDetails: () => void;
  } = $props();
  let scrollElement = $state<HTMLDivElement>();
  let headerElement: HTMLDivElement;
  let viewportWidth = $state(0);
  let viewportHeight = $state(0);
  let horizontalOffset = $state(0);
  let lastVerticalOffset = 0;
  let viewportChanging = $state(false);
  const minimumColumnWidth = 144;

  let resize = $state<{ index: number; pointerId: number; startX: number; startWidth: number }>();
  let segmentBase = $state(0n);
  let segmentCount = $state(0);
  let programmaticScroll = $state(false);
  let resumePending = $state(false);
  let activeSnapshot = '';
  let identity = '';
  let wasFollowing = false;
  let navigationSequence = 0;
  let selectionPaused = $state(false);
  let pendingFocus = $state<{ sequence: number; origin: Element | null }>();
  let moveSequence = 0;
  let measuredRowHeight = ROW_HEIGHT;
  let rowHeight = $derived(rowLines === 2 ? WRAPPED_ROW_HEIGHT : ROW_HEIGHT);

  let total = $derived(viewer.displayed ? BigInt(viewer.displayed.snapshot.matchedCount) : 0n);
  let rowsByOffset = $derived(indexPages(viewer.displayed?.pages ?? []));
  let widths = $derived(columns.map(column => column.width));
  let gridTemplate = $derived(widths.map(width => width === undefined ? 'minmax(12rem, 1fr)' : `${width}px`).join(' '));
  let minimumTableWidth = $derived(`calc(${widths.map(width => width === undefined ? '12rem' : `${width}px`).join(' + ') || '0px'})`);
  let tableWidth = $derived(widths.length > 0 && widths.every(width => width !== undefined)
    ? minimumTableWidth
    : `max(${viewportWidth}px, ${minimumTableWidth})`);

  const virtualizer = createVirtualizer<HTMLDivElement, HTMLDivElement>({
    count: 0,
    getScrollElement: () => scrollElement ?? null,
    estimateSize: () => ROW_HEIGHT,
    overscan: OVERSCAN_ROWS,
    observeElementRect: observeViewport,
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

  const keyboard = registerCommand({ id: 'rows.preview', label: 'Open row preview', bindings: ['Shift+Enter'],
    scope: () => scrollElement, when: () => !!activeRow, handler: () => onOpenDetails() });
  for (const [suffix, key, delta] of [['up', 'ArrowUp', -1n], ['down', 'ArrowDown', 1n], ['pageUp', 'PageUp', -10n], ['pageDown', 'PageDown', 10n]] as const) {
    registerCommand({ id: `rows.${suffix}`, label: `Move ${delta} rows`, bindings: [key], scope: () => scrollElement,
      repeat: true, when: () => total > 0n, handler: () => navigate((activeRow?.offset ?? total - 1n) + delta, true) });
    registerCommand({ id: `rows.global.${suffix}`, label: `Move ${delta} rows globally`,
      bindings: [`Ctrl+Alt+${delta === -10n || delta === 10n ? 'Shift+' : ''}${delta < 0n ? 'ArrowUp' : 'ArrowDown'}`],
      repeat: true, allowInInput: true, when: () => total > 0n,
      handler: () => navigate((activeRow?.offset ?? total - 1n) + delta, false) });
  }
  $effect(() => {
    navigator = { navigate, focus: async () => { if (activeRow) await navigate(activeRow.offset, true); } };
    return () => { navigator = undefined; navigationSequence++; };
  });

  // A replacement query starts at its newest result. Ordinary tail updates retain editor focus.
  $effect(() => {
    const displayed = viewer.displayed;
    const token = displayed?.snapshot.snapshotToken ?? '';
    const following = viewer.following;
    const count = total;
    const resumed = following && !wasFollowing;
    wasFollowing = following;
    if (token === activeSnapshot && !resumed) return;
    activeSnapshot = token;
    untrack(() => {
      const sequence = ++navigationSequence;
      const nextIdentity = displayed ? `${displayed.queryId}:${displayed.snapshot.generationId}` : '';
      if (nextIdentity !== identity) { identity = nextIdentity; selectionPaused = false; onReset(); }
      if (!displayed || count === 0n) {
        activeRow = undefined;
        void moveSegment({ base: 0n, count: 0 }, 'start');
        return;
      }
      const origin = document.activeElement;
      const ownedFocus = !!scrollElement?.contains(origin);
      const offset = following ? count - 1n : clampRow(activeRow?.offset ?? count - 1n, count)!;
      activeRow = { queryId: displayed.queryId, generationId: displayed.snapshot.generationId, offset, row: rowsByOffset.get(String(offset)) };
      if (ownedFocus) pendingFocus = { sequence, origin };
      void moveSegment(following ? tailSegment(count) : segmentForRow(count, offset), following ? 'end' : 'start');
    });
  });

  // Retain the active payload when its page rotates out of the bounded cache.
  $effect(() => {
    const active = activeRow;
    const row = active && rowsByOffset.get(String(active.offset));
    if (active && row && row.id !== active.row?.id) activeRow = { ...active, row };
  });
  const activeMounted = $derived(activeRow !== undefined && $virtualizer.getVirtualItems().some(item => segmentBase + BigInt(item.index) === activeRow!.offset));
  $effect.pre(() => {
    const keys = $virtualizer.getVirtualItems().map(item => String(segmentBase + BigInt(item.index)));
    const focused = document.activeElement as HTMLElement | null;
    if (focused && scrollElement?.contains(focused) && focused.dataset.offset && !keys.includes(focused.dataset.offset)) scrollElement.focus({ preventScroll: true });
  });

  // Scroll observers may publish the new range after tick(). Wait until the target
  // actually mounts, and abandon restoration if the user has focused another editor.
  $effect(() => {
    const request = pendingFocus;
    const offset = activeRow?.offset;
    if (!request || offset === undefined || !activeMounted) return;
    void tick().then(() => {
      if (request.sequence !== navigationSequence || pendingFocus?.sequence !== request.sequence || activeRow?.offset !== offset) return;
      const row = scrollElement?.querySelector<HTMLElement>(`[data-offset="${offset}"]`);
      if (!row) return;
      const focused = document.activeElement;
      if (focused === request.origin || scrollElement?.contains(focused) || (focused === document.body && request.origin && !request.origin.isConnected)) row.focus({ preventScroll: true });
      pendingFocus = undefined;
    });
  });

  async function navigate(requested: bigint, focus = false) {
    const displayed = viewer.displayed;
    const offset = clampRow(requested, total);
    if (!displayed || offset === undefined) return;
    const sequence = ++navigationSequence;
    const originalFocus = document.activeElement;
    const token = displayed.snapshot.snapshotToken;
    const current = () => sequence === navigationSequence && viewer.displayed?.snapshot.snapshotToken === token;
    selectionPaused = offset < total - 1n;
    // Also invalidate an in-flight resume when another navigation supersedes it.
    controller.pause();
    activeRow = { queryId: displayed.queryId, generationId: displayed.snapshot.generationId, offset, row: rowsByOffset.get(String(offset)) };
    const move = ++moveSequence;
    programmaticScroll = true;
    if (offset < segmentBase || offset >= segmentBase + BigInt(segmentCount)) {
      const segment = segmentForRow(total, offset);
      segmentBase = segment.base;
      segmentCount = segment.count;
    }
    await tick();
    if (!current()) return;
    get(virtualizer).scrollToIndex(Number(offset - segmentBase), { align: 'auto' });
    await controller.ensureRange(offset, offset + 1n);
    if (!current()) return;
    await tick();
    if (!current()) return;
    if (focus) pendingFocus = { sequence, origin: originalFocus };
    finishProgrammaticScroll(move);
    if (!selectionPaused) await controller.resume();
  }

  function resumeFromScroll() {
    if (!scrollElement || programmaticScroll || viewportChanging || !selectionPaused) return;
    if (segmentBase + BigInt(segmentCount) >= total && scrollElement.scrollTop + scrollElement.clientHeight >= scrollElement.scrollHeight - 1) {
      selectionPaused = false;
      void controller.resume();
    }
  }
  function handleScroll() {
    if (!scrollElement) return;
    horizontalOffset = scrollElement.scrollLeft;
    const movedDown = scrollElement.scrollTop > lastVerticalOffset;
    lastVerticalOffset = scrollElement.scrollTop;
    if (movedDown) resumeFromScroll();
  }

  // Translate the rendered range into data requests, rebases, and follow transitions.
  $effect(() => {
    const instance = $virtualizer;
    const range = instance.range;
    const items = instance.getVirtualItems();
    const base = segmentBase;
    const count = segmentCount;
    const matchedCount = total;
    const moving = programmaticScroll || viewportChanging;
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
    } else if (!following && atTail && !selectionPaused && !resumePending) {
      resumePending = true;
      void controller.resume().finally(() => { resumePending = false; });
    }

    if (count === 0) scrollElement?.scrollTo({ top: 0 });
  });

  /** Measure usable space, excluding native scrollbars, for both header and virtualizer. */
  function observeViewport(instance: Virtualizer<HTMLDivElement, HTMLDivElement>, callback: (rect: Rect) => void) {
    const element = instance.scrollElement;
    if (!element) return;
    let previousWidth = -1;
    let previousHeight = -1;
    let frame = 0;
    const measure = () => untrack(() => {
      const width = element.clientWidth;
      const height = element.clientHeight;
      horizontalOffset = element.scrollLeft;
      if (width === previousWidth && height === previousHeight) return;
      previousWidth = width;
      previousHeight = height;
      // A geometry change must not be mistaken for scrolling away from the live tail.
      viewportChanging = true;
      viewportWidth = width;
      viewportHeight = height;
      callback({ width, height });
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        if (viewer.following && segmentCount > 0 && !programmaticScroll) {
          instance.scrollToIndex(segmentCount - 1, { align: 'end' });
        }
        horizontalOffset = element.scrollLeft;
        frame = requestAnimationFrame(() => { viewportChanging = false; });
      });
    });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame);
    };
  }

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

  function handleRowClick(event: MouseEvent, row: LogRow | undefined, offset: bigint) {
    if (!row || event.button !== 0) return;
    void navigate(offset, true);
    if (event.shiftKey) { event.preventDefault(); onOpenDetails(); }
  }

  function preventShiftSelection(event: MouseEvent, row: LogRow | undefined) {
    if (row && event.shiftKey && event.button === 0) event.preventDefault();
  }

  // Capture actual flexible widths as well as explicitly resized columns.
  export function snapshotColumns(): ColumnConfig[] {
    const headers = headerElement?.querySelectorAll<HTMLElement>('[role="columnheader"]');
    return columns.map((column, index) => {
      const width = headers?.[index]?.getBoundingClientRect().width;
      return width && width >= minimumColumnWidth ? { ...column, width } : { ...column };
    });
  }

  // Freeze the rendered widths so dragging one column leaves its neighbors unchanged.
  function measureColumnWidths() {
    const measured = snapshotColumns().map(column => column.width ?? minimumColumnWidth);
    onColumnWidthsChange(measured);
    return measured;
  }

  function startResize(event: PointerEvent, index: number) {
    if (event.button !== 0 || resize) return;
    event.preventDefault();
    const measured = measureColumnWidths();
    (event.currentTarget as HTMLButtonElement).setPointerCapture(event.pointerId);
    resize = { index, pointerId: event.pointerId, startX: event.clientX, startWidth: measured[index] };
  }

  function moveResize(event: PointerEvent) {
    if (!resize || event.pointerId !== resize.pointerId) return;
    const width = Math.max(minimumColumnWidth, resize.startWidth + event.clientX - resize.startX);
    onColumnWidthsChange(columns.map((column, index) => index === resize!.index ? width : column.width ?? minimumColumnWidth));
  }

  function endResize(event: PointerEvent) {
    if (event.pointerId !== resize?.pointerId) return;
    resize = undefined;
    const handle = event.currentTarget as HTMLButtonElement;
    if (handle.hasPointerCapture(event.pointerId)) handle.releasePointerCapture(event.pointerId);
  }

  function resizeWithKeyboard(event: KeyboardEvent, index: number) {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    const measured = measureColumnWidths();
    measured[index] = Math.max(minimumColumnWidth, measured[index] + (event.key === 'ArrowRight' ? 16 : -16));
    onColumnWidthsChange(measured);
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

<section class="flex h-full min-h-0 min-w-0 flex-col bg-background" class:resizing={resize !== undefined} aria-label="Log output">
  <div
    class="flex min-h-0 min-w-0 flex-1 flex-col"
    role="table"
    aria-label="Log records"
    aria-busy={viewer.pending !== undefined}
    aria-colcount={columns.length}
  >
    <div class="shrink-0 overflow-clip" style:width={`${viewportWidth}px`}>
      <div
        bind:this={headerElement}
        class="grid h-9 shrink-0 border-b bg-table-header font-mono text-xs font-medium tracking-[0.04em] text-muted-foreground"
        style={`width: ${tableWidth}; transform: translateX(${-horizontalOffset}px); grid-template-columns: ${gridTemplate};`}
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
                class:active={resize?.index === index}
                aria-label={`Resize ${column.path} column`}
                onpointerdown={event => startResize(event, index)}
                onpointermove={moveResize}
                onpointerup={endResize}
                onpointercancel={endResize}
                onlostpointercapture={endResize}
                onkeydown={event => resizeWithKeyboard(event, index)}
              ></button>
            </div>
          {/each}
        </div>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_noninteractive_tabindex (Stable keyboard focus fallback for virtualized rows.) -->
    <div
      bind:this={scrollElement}
      class="table-viewport relative min-h-0 min-w-0 flex-1"
      role="rowgroup"
      aria-label="Scrollable log records"
      tabindex={activeMounted ? -1 : 0}
      onscroll={handleScroll}
      onwheel={event => { if (event.deltaY > 0) resumeFromScroll(); }}
    >
      <div class="min-h-full" style={`width: ${tableWidth};`}>
        {#if !viewer.displayed}
          <div class="sticky left-0 grid place-items-center px-6 text-sm text-muted-foreground" style:width={`${viewportWidth}px`} style:min-height={`${viewportHeight}px`} role="status">
            {#if viewer.pending}
              Preparing logs{viewer.pending.progress === undefined ? '…' : `… ${Math.round(viewer.pending.progress * 100)}%`}
            {:else}
              Connecting…
            {/if}
          </div>
        {:else if total === 0n}
          <div class="sticky left-0 grid place-items-center px-6 text-sm text-muted-foreground" style:width={`${viewportWidth}px`} style:min-height={`${viewportHeight}px`} role="status">
            No log records.
          </div>
        {:else}
          <div class="relative w-full" style={`height: ${$virtualizer.getTotalSize()}px;`}>
            {#each $virtualizer.getVirtualItems() as item (item.key)}
              {@const logicalIndex = segmentBase + BigInt(item.index)}
              {@const row = rowsByOffset.get(logicalIndex.toString())}
              {@const isSelected = logicalIndex === activeRow?.offset}
              <!-- svelte-ignore a11y_click_events_have_key_events (Row keyboard commands are handled by the shared window capture registry.) -->
              <div
                class={`absolute left-0 top-0 grid w-full border-b border-border/70 font-mono text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring ${isSelected ? 'bg-accent' : 'hover:bg-row-hover'}`}
                style={`height: ${item.size}px; transform: translateY(${item.start}px); grid-template-columns: ${gridTemplate};`}
                role="row"
                aria-busy={row === undefined}
                aria-current={isSelected ? 'true' : undefined}
                data-offset={String(logicalIndex)}
                aria-keyshortcuts={keyboard.aria('Shift+Enter')}
                tabindex={isSelected ? 0 : -1}
                onmousedown={event => preventShiftSelection(event, row)}
                onclick={event => handleRowClick(event, row, logicalIndex)}
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
  .table-viewport {
    overflow: scroll;
    scrollbar-gutter: stable;
    overflow-anchor: none;
  }

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
