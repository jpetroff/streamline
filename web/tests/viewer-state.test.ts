import { describe, expect, test } from 'bun:test';
import { PageCache } from '../src/lib/transport/page-cache';
import { reduceViewer, type ViewerState } from '../src/lib/transport/viewer-state';
import type { Snapshot } from '../src/lib/transport/types';

const snapshot = (queryId: string, revision: string, count: string): Snapshot => ({
  sessionId: 'session', generationId: '1', queryId, revision,
  processedThrough: count, matchedCount: count, snapshotToken: `${queryId}.${revision}`,
});

describe('viewer state', () => {
  test('keeps displayed rows while a replacement query is pending or fails', () => {
    const initial: ViewerState = {
      following: true, needsRefresh: false,
      displayed: { queryId: 'old', filter: '', sort: 'input', snapshot: snapshot('old', '1', '1'), offset: 0n, rows: [{ id: '1', message: 'old' }] },
    };
    const pending = reduceViewer(initial, { type: 'pending', queryId: 'new', filter: 'error' });
    expect(pending.displayed?.rows[0].message).toBe('old');
    const failed = reduceViewer(pending, { type: 'failed', error: { code: 'invalid_filter', message: 'bad filter' } });
    expect(failed.displayed?.queryId).toBe('old');
    expect(failed.pending).toBeUndefined();
  });

  test('ignores live extension while paused', () => {
    const ready = reduceViewer({ following: true, needsRefresh: false }, {
      type: 'replace', query: { queryId: 'q', filter: '', sort: 'input', snapshot: snapshot('q', '1', '1'), offset: 0n, rows: [{ id: '1', message: 'first' }] },
    });
    const paused = reduceViewer(ready, { type: 'pause' });
    const unchanged = reduceViewer(paused, { type: 'extend', snapshot: snapshot('q', '2', '2'), offset: 0n, rows: [{ id: '2', message: 'new' }] });
    expect(unchanged.displayed?.snapshot.revision).toBe('1');
    expect(unchanged.displayed?.rows[0].message).toBe('first');
  });
});

describe('page cache', () => {
  test('reuses full prefix pages and invalidates a partial tail when results grow', () => {
    const cache = new PageCache(4096);
    cache.set({ snapshot: snapshot('q', '1', '2'), offset: '0', rows: [{ id: '1', message: 'a' }, { id: '2', message: 'b' }] }, 2);
    expect(cache.get('q', 0n, 2, 3n)?.rows).toHaveLength(2);
    cache.set({ snapshot: snapshot('q', '1', '1'), offset: '2', rows: [{ id: '3', message: 'c' }] }, 2);
    expect(cache.get('q', 2n, 2, 4n)).toBeUndefined();
  });
});

test('shared event fixture preserves 64-bit values as strings', async () => {
  const fixture = await Bun.file(new URL('../../testdata/transport/query-event.json', import.meta.url)).json();
  expect(fixture.state.snapshot.processedThrough).toBe('9007199254740993');
  expect(typeof fixture.state.snapshot.processedThrough).toBe('string');
});
