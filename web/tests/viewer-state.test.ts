import { describe, expect, test } from 'bun:test';
import { PageCache } from '../src/lib/transport/page-cache';
import { reduceViewer, type ViewerState } from '../src/lib/transport/viewer-state';
import type { JSONValue, LogRow, RowPage, Snapshot } from '../src/lib/transport/types';

const snapshot = (queryId: string, revision: string, count: string): Snapshot => ({
  sessionId: 'session', generationId: '1', queryId, revision,
  processedThrough: count, matchedCount: count, snapshotToken: `${queryId}.${revision}`,
});

const page = (descriptor: Snapshot, offset: string, rows: LogRow[]): RowPage => ({
  snapshot: descriptor,
  offset,
  rows,
});

describe('viewer state', () => {
  test('keeps displayed rows while a replacement query is pending or fails', () => {
    const descriptor = snapshot('old', '1', '1');
    const initial: ViewerState = {
      following: true, needsRefresh: false,
      displayed: {
        queryId: 'old', filter: '', sort: 'input', snapshot: descriptor,
        pages: [page(descriptor, '0', [{ id: '1', message: 'old', sourceFormat: 'text' }])],
      },
    };
    const pending = reduceViewer(initial, { type: 'pending', queryId: 'new', filter: 'error' });
    expect(pending.displayed?.pages[0].rows[0].message).toBe('old');
    const failed = reduceViewer(pending, { type: 'failed', error: { code: 'invalid_filter', message: 'bad filter' } });
    expect(failed.displayed?.queryId).toBe('old');
    expect(failed.pending).toBeUndefined();
  });

  test('ignores live extension while paused', () => {
    const first = snapshot('q', '1', '1');
    const ready = reduceViewer({ following: true, needsRefresh: false }, {
      type: 'replace',
      query: {
        queryId: 'q', filter: '', sort: 'input', snapshot: first,
        pages: [page(first, '0', [{ id: '1', message: 'first', sourceFormat: 'text' }])],
      },
    });
    const paused = reduceViewer(ready, { type: 'pause' });
    const second = snapshot('q', '2', '2');
    const unchanged = reduceViewer(paused, {
      type: 'extend',
      snapshot: second,
      pages: [page(second, '0', [{ id: '2', message: 'new', sourceFormat: 'text' }])],
    });
    expect(unchanged.displayed?.snapshot.revision).toBe('1');
    expect(unchanged.displayed?.pages[0].rows[0].message).toBe('first');
  });

  test('ignores pages from a different snapshot token', () => {
    const current = snapshot('q', '2', '2');
    const state: ViewerState = {
      following: false,
      needsRefresh: false,
      displayed: { queryId: 'q', filter: '', sort: 'input', snapshot: current, pages: [] },
    };
    const stale = snapshot('q', '1', '1');
    const unchanged = reduceViewer(state, {
      type: 'pagesLoaded',
      snapshot: stale,
      pages: [page(stale, '0', [{ id: '1', message: 'stale', sourceFormat: 'text' }])],
    });
    expect(unchanged).toBe(state);
  });
});

describe('page cache', () => {
  test('reuses full prefix pages and invalidates a partial tail when results grow', () => {
    const cache = new PageCache(4096);
    cache.set({ snapshot: snapshot('q', '1', '2'), offset: '0', rows: [{ id: '1', message: 'a', sourceFormat: 'text' }, { id: '2', message: 'b', sourceFormat: 'text' }] }, 2);
    expect(cache.get('q', 0n, 2, 3n)?.rows).toHaveLength(2);
    cache.set({ snapshot: snapshot('q', '1', '1'), offset: '2', rows: [{ id: '3', message: 'c', sourceFormat: 'text' }] }, 2);
    expect(cache.get('q', 2n, 2, 4n)).toBeUndefined();
  });
});

test('shared event fixture preserves 64-bit values as strings', async () => {
  const fixture = await Bun.file(new URL('../../testdata/transport/query-event.json', import.meta.url)).json();
  expect(fixture.state.snapshot.processedThrough).toBe('9007199254740993');
  expect(typeof fixture.state.snapshot.processedThrough).toBe('string');
});

test('structured log row contract carries typed fields and parser metadata', () => {
  const fields: Record<string, JSONValue> = {
    nested: { ok: true },
    values: ['one', 2, null],
  };
  const row: LogRow = {
    id: '1',
    message: 'handled request',
    sourceFormat: 'json',
    fields,
    diagnostics: [{ code: 'normalized', message: 'normalized' }],
  };
  const decoded = JSON.parse(JSON.stringify(row)) as LogRow;
  expect(decoded.fields?.nested).toEqual({ ok: true });
  expect(decoded.fields?.values).toEqual(['one', 2, null]);
  expect(decoded.sourceFormat).toBe('json');
  expect(decoded.diagnostics?.[0].code).toBe('normalized');
});


test('raw pages append only at the current generation cursor', () => {
  const started = reduceViewer(
    { following: true, needsRefresh: false },
    { type: 'rawStart', generationId: '2' },
  );
  const stale = reduceViewer(started, {
    type: 'rawPage',
    page: { generationId: '1', offset: '0', totalChunks: '1', chunks: ['stale'] },
  });
  expect(stale).toBe(started);

  const loaded = reduceViewer(started, {
    type: 'rawPage',
    page: { generationId: '2', offset: '0', totalChunks: '2', chunks: ['one'] },
  });
  expect(loaded.raw?.chunks).toEqual(['one']);
  expect(loaded.raw?.nextOffset).toBe('1');

  const outOfOrder = reduceViewer(loaded, {
    type: 'rawPage',
    page: { generationId: '2', offset: '0', totalChunks: '2', chunks: ['duplicate'] },
  });
  expect(outOfOrder).toBe(loaded);
});
