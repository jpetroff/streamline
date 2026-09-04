import { expect, test } from 'bun:test';
import type { EventConnection, QueryAPI } from '../src/lib/transport/api';
import { ViewerController } from '../src/lib/transport/viewer-controller';
import type { QuerySort, QueryState, RowPage, Session } from '../src/lib/transport/types';

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}

function ready(queryId: string): QueryState {
  return { queryId, status: 'ready', snapshot: {
    sessionId: 'session', generationId: '1', queryId, revision: '1', processedThrough: '1', matchedCount: '1', snapshotToken: `${queryId}.1`,
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
