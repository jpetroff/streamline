import { expect, test } from 'bun:test';
import { clampRow, parseRowJump } from '../src/lib/row-navigation';
import { segmentForRow, SEGMENT_ROWS } from '../src/lib/virtual-window';

test('row navigation clamps one and ten row steps without wrapping', () => {
  expect(clampRow(-1n, 20n)).toBe(0n);
  expect(clampRow(19n + 10n, 20n)).toBe(19n);
  expect(clampRow(19n - 10n, 20n)).toBe(9n);
  expect(clampRow(0n, 0n)).toBeUndefined();
});
test('jump positions are one-based decimal integers and preserve 64-bit precision', () => {
  const total = 9007199254740993n;
  expect(parseRowJump(String(total), total)).toBe(total - 1n);
  expect(parseRowJump(' 01 ', total)).toBe(0n);
  for (const input of ['', '0', '-1', '1.5', '1e2', 'NaN', '21']) expect(parseRowJump(input, 20n)).toBeUndefined();
});
test('arbitrary targets fit in bounded virtual segments', () => {
  const total = 9007199254740993n;
  for (const target of [0n, 100000n, total / 2n, total - 1n]) {
    const segment = segmentForRow(total, target);
    expect(segment.base <= target).toBe(true);
    expect(segment.base + BigInt(segment.count) > target).toBe(true);
    expect(segment.count).toBeLessThanOrEqual(Number(SEGMENT_ROWS));
  }
  expect(segmentForRow(0n, 0n)).toEqual({ base: 0n, count: 0 });
});
