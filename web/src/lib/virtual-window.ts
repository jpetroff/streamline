/** Fixed summary-row height in CSS pixels. */
export const ROW_HEIGHT = 32;
/** Number of rows mounted before and after the visible TanStack range. */
export const OVERSCAN_ROWS = 12;
/** Stable server and browser row-page size. */
export const PAGE_SIZE = 200;
/** Maximum logical rows represented by one browser scroll surface. */
export const SEGMENT_ROWS = 100_000n;
/** Logical rows added on either side during a segment rebase. */
export const SEGMENT_SHIFT = 50_000n;
/** Distance from a segment edge that triggers a rebase. */
export const SEGMENT_EDGE_ROWS = 1_000;

/** Browser-sized slice of a potentially larger bigint result space. */
export interface VirtualSegment {
  /** Global logical offset represented by local virtual index zero. */
  base: bigint;
  /** Segment-local item count; guaranteed small enough for number-based DOM APIs. */
  count: number;
}

/** Returns a browser-sized virtual segment anchored to the newest result. */
export function tailSegment(total: bigint): VirtualSegment {
  const safeTotal = total > 0n ? total : 0n;
  const base = safeTotal > SEGMENT_ROWS ? safeTotal - SEGMENT_ROWS : 0n;
  return { base, count: Number(safeTotal - base) };
}

/** Returns the first virtual segment for a paused replacement query. */
export function headSegment(total: bigint): VirtualSegment {
  const safeTotal = total > 0n ? total : 0n;
  return { base: 0n, count: Number(safeTotal > SEGMENT_ROWS ? SEGMENT_ROWS : safeTotal) };
}

/** Chooses a neighboring segment when the visible range approaches an edge. */
export function rebasedSegment(total: bigint, base: bigint, startIndex: number, endIndex: number): VirtualSegment {
  const currentCount = Number(minBigInt(SEGMENT_ROWS, maxBigInt(0n, total - base)));
  let nextBase = base;
  if (startIndex < SEGMENT_EDGE_ROWS && base > 0n) {
    nextBase = maxBigInt(0n, base - SEGMENT_SHIFT);
  } else if (endIndex >= currentCount - SEGMENT_EDGE_ROWS && base + BigInt(currentCount) < total) {
    nextBase = minBigInt(base + SEGMENT_SHIFT, maxBigInt(0n, total - SEGMENT_ROWS));
  }
  return { base: nextBase, count: Number(minBigInt(SEGMENT_ROWS, maxBigInt(0n, total - nextBase))) };
}

/** Maps one logical row back into a newly rebased browser segment. */
export function scrollOffsetForAnchor(logicalIndex: bigint, base: bigint, intraRowOffset = 0): number {
  const localIndex = maxBigInt(0n, logicalIndex - base);
  return Number(localIndex) * ROW_HEIGHT + intraRowOffset;
}

/** Lists fixed page offsets covering a range plus neighboring prefetch pages. */
export function pageOffsetsForRange(start: bigint, endExclusive: bigint, total: bigint, prefetchPages = 1): bigint[] {
  if (total <= 0n) return [];
  const pageSize = BigInt(PAGE_SIZE);
  const clampedStart = minBigInt(maxBigInt(0n, start), total);
  const clampedEnd = minBigInt(maxBigInt(clampedStart, endExclusive), total);
  if (clampedStart === clampedEnd) return [];

  let first = clampedStart / pageSize * pageSize;
  const last = (clampedEnd - 1n) / pageSize * pageSize;
  first = maxBigInt(0n, first - BigInt(prefetchPages) * pageSize);
  const final = minBigInt(last + BigInt(prefetchPages) * pageSize, (total - 1n) / pageSize * pageSize);
  const offsets: bigint[] = [];
  for (let offset = first; offset <= final; offset += pageSize) offsets.push(offset);
  return offsets;
}

function minBigInt(left: bigint, right: bigint) { return left < right ? left : right; }
function maxBigInt(left: bigint, right: bigint) { return left > right ? left : right; }
