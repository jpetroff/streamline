import { describe, expect, test } from 'bun:test';
import {
  PAGE_SIZE,
  SEGMENT_ROWS,
  headSegment,
  pageOffsetsForRange,
  rebasedSegment,
  scrollOffsetForAnchor,
  tailSegment,
} from '../src/lib/virtual-window';

describe('virtual segments', () => {
  test('uses the complete result when it fits in one segment', () => {
    expect(headSegment(42n)).toEqual({ base: 0n, count: 42 });
    expect(tailSegment(42n)).toEqual({ base: 0n, count: 42 });
  });

  test('anchors large and unsafe-integer result counts to a bounded tail segment', () => {
    const total = 9_007_199_254_740_993n;
    const segment = tailSegment(total);
    expect(segment.base).toBe(total - SEGMENT_ROWS);
    expect(segment.count).toBe(Number(SEGMENT_ROWS));
  });

  test('rebases in either direction without changing segment size', () => {
    expect(rebasedSegment(200_000n, 50_000n, 999, 1020)).toEqual({ base: 0n, count: 100_000 });
    expect(rebasedSegment(200_000n, 0n, 98_990, 99_010)).toEqual({ base: 50_000n, count: 100_000 });
  });

  test('maps a bigint logical anchor to a safe local pixel offset', () => {
    const base = 9_007_199_254_700_000n;
    expect(scrollOffsetForAnchor(base + 25_000n, base, 7)).toBe(25_000 * 24 + 7);
  });

  test('preserves a logical anchor when rebasing wrapped rows', () => {
    const base = 9_007_199_254_700_000n;
    expect(scrollOffsetForAnchor(base + 25_000n, base, 17, 40)).toBe(25_000 * 40 + 17);
  });
});

describe('viewport pages', () => {
  test('aligns a range and prefetches one neighboring page on each side', () => {
    expect(pageOffsetsForRange(410n, 430n, 1000n)).toEqual([200n, 400n, 600n]);
  });

  test('covers a page boundary and clamps prefetch to the result', () => {
    expect(pageOffsetsForRange(BigInt(PAGE_SIZE - 1), BigInt(PAGE_SIZE + 1), 450n)).toEqual([0n, 200n, 400n]);
    expect(pageOffsetsForRange(0n, 1n, 50n)).toEqual([0n]);
    expect(pageOffsetsForRange(0n, 0n, 0n)).toEqual([]);
  });
});
