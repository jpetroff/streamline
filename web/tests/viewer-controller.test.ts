import { expect, test } from 'bun:test';
import { TransportError, type EventConnection, type QueryAPI } from '../src/lib/transport/api';
import { ViewerController } from '../src/lib/transport/viewer-controller';
import type { QueryEvent, QuerySpec, QueryState, RawChunkPage, RowPage, Session, Snapshot } from '../src/lib/transport/types';

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail; });
  return { promise, resolve, reject };
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
  specs: QuerySpec[] = [];
  session(): Promise<Session> { return Promise.resolve({ sessionId: 'session', generationId: '1', inputStatus: 'streaming', inputKind: 'records' }); }
  create(spec: QuerySpec): Promise<QueryState> {
    this.specs.push(spec);
    const request = deferred<QueryState>();
    this.creates.set(spec.filter, request);
    return request.promise;
  }
  get(queryId: string): Promise<QueryState> { return Promise.resolve(ready(queryId)); }
  rows(queryId: string, _snapshot: string, offset: bigint): Promise<RowPage> {
    return Promise.resolve({ snapshot: ready(queryId).snapshot!, offset: offset.toString(), rows: [{ id: '1', message: queryId, sourceFormat: 'text' }] });
  }
  raw(): Promise<RawChunkPage> { throw new Error('raw output was not expected'); }
  events(): EventConnection { return { close() {} }; }
  delete(queryId: string): Promise<void> { this.deleted.push(queryId); return Promise.resolve(); }
}

class RangeAPI implements QueryAPI {
  readonly state = ready('range-query', '1000');
  calls: bigint[] = [];
  deferredRows = false;
  pending = new Map<bigint, ReturnType<typeof deferred<RowPage>>>();

  session(): Promise<Session> { return Promise.resolve({ sessionId: 'session', generationId: '1', inputStatus: 'streaming', inputKind: 'records' }); }
  create(): Promise<QueryState> { return Promise.resolve(this.state); }
  get(): Promise<QueryState> { return Promise.resolve(this.state); }
  raw(): Promise<RawChunkPage> { throw new Error('raw output was not expected'); }
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


class StartupAPI implements QueryAPI {
  readonly state = ready('startup-query', '0');
  readonly deleted: string[] = [];
  readonly rawCalls: bigint[] = [];
  created = 0;
  onEvent?: (event: QueryEvent) => void;

  constructor(public sessionState: Session, private rawChunks: string[] = []) {}

  session(): Promise<Session> { return Promise.resolve(this.sessionState); }
  create(): Promise<QueryState> { this.created++; return Promise.resolve(this.state); }
  get(): Promise<QueryState> { return Promise.resolve(this.state); }
  rows(): Promise<RowPage> { throw new Error('row output was not expected'); }
  raw(generationId: string, offset: bigint, limit: number): Promise<RawChunkPage> {
    this.rawCalls.push(offset);
    return Promise.resolve({
      generationId,
      offset: offset.toString(),
      totalChunks: this.rawChunks.length.toString(),
      chunks: this.rawChunks.slice(Number(offset), Number(offset) + limit),
    });
  }
  events(_queryId: string, onEvent: (event: QueryEvent) => void): EventConnection {
    this.onEvent = onEvent;
    return { close() {} };
  }
  delete(queryId: string): Promise<void> { this.deleted.push(queryId); return Promise.resolve(); }

  emitRaw() {
    this.sessionState = { ...this.sessionState, inputKind: 'raw', inputStatus: 'eof' };
    this.onEvent?.({ type: 'input', state: this.state, session: this.sessionState });
  }
}

test('startup routes terminal raw stdin directly to bounded chunk loading', async () => {
  const api = new StartupAPI(
    { sessionId: 'session', generationId: '1', inputStatus: 'eof', inputKind: 'raw' },
    ['one', 'two', 'three', 'four', 'five'],
  );
  const controller = new ViewerController(api);
  await controller.start();
  expect(api.created).toBe(0);
  expect(api.rawCalls).toEqual([0n]);
  expect(controller.state.raw?.chunks).toEqual(['one', 'two', 'three', 'four']);
  await controller.loadMoreRaw();
  expect(api.rawCalls).toEqual([0n, 4n]);
  expect(controller.state.raw?.chunks.join('')).toBe('onetwothreefourfive');
  controller.dispose();
});

test('an input event atomically releases an empty parsed query for raw mode', async () => {
  const api = new StartupAPI({ sessionId: 'session', generationId: '1', inputStatus: 'streaming', inputKind: 'pending' }, ['report']);
  const controller = new ViewerController(api);
  await controller.start();
  expect(controller.state.displayed?.queryId).toBe('startup-query');

  api.emitRaw();
  await settle();
  expect(api.deleted).toContain('startup-query');
  expect(controller.state.displayed).toBeUndefined();
  expect(controller.state.raw?.chunks).toEqual(['report']);
  controller.dispose();
});

test('confirmed search is copied into pending and displayed query state', async () => {
  const api = new FakeAPI();
  const controller = new ViewerController(api);
  const search = { text: 'timeout\napi', mode: 'plain' as const, operator: 'and' as const };
  const pending = controller.setQuery('permanent', 'input', search);
  search.text = 'unconfirmed edit';
  expect(api.specs[0].search?.text).toBe('timeout\napi');
  api.creates.get('permanent')!.resolve({ queryId: 'search-query', status: 'building' });
  await pending;
  expect(controller.state.pending?.search?.text).toBe('timeout\napi');
  controller.dispose();
});

test('backend line errors preserve the old display and return diagnostics to the editor', async () => {
  const api = new FakeAPI();
  const controller = new ViewerController(api);
  const initial = controller.setQuery('old', 'input', { text: 'old', mode: 'plain', operator: 'or' });
  api.creates.get('old')!.resolve(ready('old-query'));
  await initial;
  const replacement = controller.setQuery('new', 'input', { text: '(?=x)', mode: 'regexp', operator: 'or' });
  api.creates.get('new')!.reject(new TransportError('invalid_search', 'Unsupported expression', 400, [{ line: 1, message: 'lookaround unsupported' }]));
  const error = await replacement;
  expect(error?.lineErrors).toEqual([{ line: 1, message: 'lookaround unsupported' }]);
  expect(controller.state.displayed?.queryId).toBe('old-query');
  expect(controller.state.displayed?.search?.text).toBe('old');
  expect(controller.state.error?.lineErrors).toEqual(error?.lineErrors);
  controller.dispose();
});

test('a superseded rejection cannot annotate or replace the latest search', async () => {
  const api = new FakeAPI();
  const controller = new ViewerController(api);
  const first = controller.setQuery('first', 'input', { text: '(?=x)', mode: 'regexp', operator: 'or' });
  const second = controller.setQuery('second', 'input', { text: 'ok', mode: 'plain', operator: 'and' });
  api.creates.get('second')!.resolve(ready('second-query'));
  await second;
  api.creates.get('first')!.reject(new TransportError('invalid_search', 'Unsupported', 400, [{ line: 1, message: 'unsupported' }]));
  expect(await first).toBeUndefined();
  expect(controller.state.error).toBeUndefined();
  expect(controller.state.displayed?.search).toEqual({ text: 'ok', mode: 'plain', operator: 'and' });
  controller.dispose();
});

class SearchRecoveryAPI extends FakeAPI {
  listeners = new Map<string, { event: (event: QueryEvent) => void; error: () => void }>();
  expired = false;
  override events(queryId: string, event: (event: QueryEvent) => void, error: () => void): EventConnection {
    this.listeners.set(queryId, { event, error });
    return { close: () => { this.listeners.delete(queryId); } };
  }
  override get(queryId: string): Promise<QueryState> {
    return this.expired ? Promise.reject(new TransportError('query_not_found', 'Expired', 404)) : super.get(queryId);
  }
}

test('generation changes and expired reconnects preserve every applied search option', async () => {
  for (const recovery of ['generation', 'expired']) {
    const api = new SearchRecoveryAPI();
    const controller = new ViewerController(api);
    const search = { text: 'timeout\napi', mode: 'regexp' as const, operator: 'and' as const };
    const initial = controller.setQuery('permanent', 'input', search);
    api.creates.get('permanent')!.resolve(ready('old-query'));
    await initial;
    if (recovery === 'generation') {
      api.listeners.get('old-query')!.event({ type: 'generation', state: ready('old-query'), session: { sessionId: 'session', generationId: '2', inputStatus: 'streaming', inputKind: 'records' } });
    } else {
      api.expired = true;
      api.listeners.get('old-query')!.error();
    }
    await settle();
    expect(api.specs).toHaveLength(2);
    expect(api.specs[1]).toEqual({ filter: 'permanent', sort: 'input', search });
    api.creates.get('permanent')!.resolve(ready('replacement', '0'));
    await settle();
    controller.dispose();
  }
});
