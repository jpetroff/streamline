import { expect, test } from 'bun:test';
import type { EventConnection, QueryAPI } from '../src/lib/transport/api';
import { ViewerController } from '../src/lib/transport/viewer-controller';
import type { QuerySort, QueryState, RowPage, Session, Snapshot } from '../src/lib/transport/types';

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}

function ready(queryId: string, matchedCount = '1', revision = '1'): QueryState {
  return { queryId, status: 'ready', snapshot: {
    sessionId: 'session', generationId: '1', queryId, revision, processedThrough: matchedCount,
    matchedCount, snapshotToken: `${queryId}.${revision}`,
  } };
}

class FakeAPI implements QueryAPI {
  creates = new Map<string, ReturnType<typeof deferred<QueryState>>>();
  deleted: string[] = [];
  session(): Promise<Session> { return Promise.resolve({ sessionId: 'session', generationId: '1', inputStatus: 'streaming' }); }
  create(filter: string, _sort: QuerySort): Promise<QueryState> {
    const request = deferred<QueryState>();
    this.creates.set(filter, request);
    return request.promise;
  }
  get(queryId: string): Promise<QueryState> { return Promise.resolve(ready(queryId)); }
  rows(queryId: string, _snapshot: string, offset: bigint): Promise<RowPage> {
    return Promise.resolve({ snapshot: ready(queryId).snapshot!, offset: offset.toString(), rows: [{ id: '1', message: queryId, sourceFormat: 'text' }] });
  }
  events(): EventConnection { return { close() {} }; }
  delete(queryId: string): Promise<void> { this.deleted.push(queryId); return Promise.resolve(); }
}

class RangeAPI implements QueryAPI {
  readonly state = ready('range-query', '1000');
  calls: bigint[] = [];
  deferredRows = false;
  pending = new Map<bigint, ReturnType<typeof deferred<RowPage>>>();

  session(): Promise<Session> { return Promise.resolve({ sessionId: 'session', generationId: '1', inputStatus: 'streaming' }); }
  create(): Promise<QueryState> { return Promise.resolve(this.state); }
  get(): Promise<QueryState> { return Promise.resolve(this.state); }
  events(): EventConnection { return { close() {} }; }
  delete(): Promise<void> { return Promise.resolve(); }

  rows(_queryId: string, _snapshot: string, offset: bigint, limit: number): Promise<RowPage> {
    this.calls.push(offset);
    const page = this.page(offset, limit);
    if (!this.deferredRows) return Promise.resolve(page);
    const request = deferred<RowPage>();
    this.pending.set(offset, request);
    return request.promise;
  }

  resolve(offsets: bigint[]) {
    for (const offset of offsets) {
      const request = this.pending.get(offset);
      if (request) {
        request.resolve(this.page(offset, 200));
        this.pending.delete(offset);
      }
    }
  }

  private page(offset: bigint, limit: number): RowPage {
    const snapshot = this.state.snapshot as Snapshot;
    const count = Math.min(limit, Number(BigInt(snapshot.matchedCount) - offset));
    return {
      snapshot,
      offset: offset.toString(),
      rows: Array.from({ length: count }, (_, index) => ({
        id: (offset + BigInt(index + 1)).toString(),
        message: `row ${offset + BigInt(index)}`,
        sourceFormat: 'text' as const,
      })),
    };
  }
}

async function settle() { await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); }

test('a late response from a superseded filter cannot replace the current query', async () => {
  const api = new FakeAPI();
  const controller = new ViewerController(api);
  const first = controller.setQuery('first');
  const second = controller.setQuery('second');
  api.creates.get('second')!.resolve(ready('query-2'));
  await second;
  expect(controller.state.displayed?.queryId).toBe('query-2');
  api.creates.get('first')!.resolve(ready('query-1'));
  await first;
  await settle();
  expect(controller.state.displayed?.queryId).toBe('query-2');
  expect(api.deleted).toContain('query-1');
  controller.dispose();
});

test('the old display remains visible until a new query page is ready', async () => {
  const api = new FakeAPI();
  const controller = new ViewerController(api);
  const initial = controller.setQuery('old');
  api.creates.get('old')!.resolve(ready('old-query'));
  await initial;
  const replacement = controller.setQuery('new');
  expect(controller.state.displayed?.queryId).toBe('old-query');
  api.creates.get('new')!.resolve(ready('new-query'));
  await replacement;
  expect(controller.state.displayed?.queryId).toBe('new-query');
  controller.dispose();
});

test('viewport ranges load aligned pages with one page of prefetch', async () => {
  const api = new RangeAPI();
  const controller = new ViewerController(api);
  await controller.setQuery('');
  expect(api.calls).toEqual([800n]);

  api.calls = [];
  await controller.ensureRange(410n, 430n);
  expect(api.calls).toEqual([200n, 400n, 600n]);
  expect(controller.state.displayed?.pages.map(page => page.offset)).toEqual(['200', '400', '600']);
  const settled = controller.state;
  await controller.ensureRange(410n, 430n);
  expect(controller.state).toBe(settled);
  controller.dispose();
});

test('identical viewport requests share the same in-flight page loads', async () => {
  const api = new RangeAPI();
  const controller = new ViewerController(api);
  await controller.setQuery('');
  api.calls = [];
  api.deferredRows = true;

  const first = controller.ensureRange(410n, 430n);
  const second = controller.ensureRange(410n, 430n);
  expect(api.calls).toEqual([200n, 400n, 600n]);
  api.resolve([200n, 400n, 600n]);
  await Promise.all([first, second]);
  expect(controller.state.displayed?.pages.map(page => page.offset)).toEqual(['200', '400', '600']);
  controller.dispose();
});

test('a late viewport response cannot replace a newer visible range', async () => {
  const api = new RangeAPI();
  const controller = new ViewerController(api);
  await controller.setQuery('');
  api.deferredRows = true;

  const older = controller.ensureRange(10n, 20n);
  const newer = controller.ensureRange(610n, 620n);
  api.resolve([400n, 600n]);
  await newer;
  expect(controller.state.displayed?.pages.map(page => page.offset)).toEqual(['400', '600', '800']);

  api.resolve([0n, 200n]);
  await older;
  expect(controller.state.displayed?.pages.map(page => page.offset)).toEqual(['400', '600', '800']);
  controller.dispose();
});
