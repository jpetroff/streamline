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
    this.creates.set(String(spec.filter[0]?.value ?? ''), request);
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
  const first = controller.setQuery([{ field: 'level', op: 'eq', value: 'first' }]);
  const second = controller.setQuery([{ field: 'level', op: 'eq', value: 'second' }]);
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
  const initial = controller.setQuery([{ field: 'level', op: 'eq', value: 'old' }]);
  api.creates.get('old')!.resolve(ready('old-query'));
  await initial;
  const replacement = controller.setQuery([{ field: 'level', op: 'eq', value: 'new' }]);
  expect(controller.state.displayed?.queryId).toBe('old-query');
  api.creates.get('new')!.resolve(ready('new-query'));
  await replacement;
  expect(controller.state.displayed?.queryId).toBe('new-query');
  controller.dispose();
});

test('viewport ranges load aligned pages with one page of prefetch', async () => {
  const api = new RangeAPI();
  const controller = new ViewerController(api);
  await controller.setQuery([]);
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
  await controller.setQuery([]);
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
  await controller.setQuery([]);
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
  const pending = controller.setQuery([{ field: 'level', op: 'eq', value: 'permanent' }], 'input', search);
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
  const initial = controller.setQuery([{ field: 'level', op: 'eq', value: 'old' }], 'input', { text: 'old', mode: 'plain', operator: 'or' });
  api.creates.get('old')!.resolve(ready('old-query'));
  await initial;
  const replacement = controller.setQuery([{ field: 'level', op: 'eq', value: 'new' }], 'input', { text: '(?=x)', mode: 'regexp', operator: 'or' });
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
  const first = controller.setQuery([{ field: 'level', op: 'eq', value: 'first' }], 'input', { text: '(?=x)', mode: 'regexp', operator: 'or' });
  const second = controller.setQuery([{ field: 'level', op: 'eq', value: 'second' }], 'input', { text: 'ok', mode: 'plain', operator: 'and' });
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
    const initial = controller.setQuery([{ field: 'level', op: 'eq', value: 'permanent' }], 'input', search);
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
    expect(api.specs[1]).toEqual({ filter: [{ field: 'level', op: 'eq' as const, value: 'permanent' }], sort: 'input', search });
    api.creates.get('permanent')!.resolve(ready('replacement', '0'));
    await settle();
    controller.dispose();
  }
});

test('submitted filters own their array and tuples', async () => {
  const api = new FakeAPI();
  const controller = new ViewerController(api);
  const filters: QuerySpec['filter'] = [{ field: 'level', op: 'eq', value: 'error' }];
  const submission = controller.setFilters(filters);
  filters[0].field = 'changed';
  filters.push({ field: 'other', op: 'gt', value: 10 });
  expect(api.specs[0].filter).toEqual([{ field: 'level', op: 'eq', value: 'error' }]);
  api.creates.get('error')!.resolve(ready('filters'));
  await submission;
  expect(controller.state.displayed?.filter).toEqual(api.specs[0].filter);
  controller.dispose();
});

test('overlapping search and filter submissions retain the latest counterpart in either order', async () => {
  for (const firstEditor of ['filters', 'search']) {
    const api = new FakeAPI();
    const controller = new ViewerController(api);
    const filter: QuerySpec['filter'] = [{ field: 'level', op: 'eq', value: 'error' }];
    const search = { text: 'timeout', mode: 'plain' as const, operator: 'or' as const };
    const first = firstEditor === 'filters' ? controller.setFilters(filter) : controller.setSearch(search);
    const firstRequest = api.creates.get(firstEditor === 'filters' ? 'error' : '')!;
    const second = firstEditor === 'filters' ? controller.setSearch(search) : controller.setFilters(filter);
    expect(api.specs[1]).toEqual({ filter, search, sort: 'input' });
    api.creates.get('error')!.resolve(ready('combined'));
    await second;
    firstRequest.resolve(ready('superseded'));
    await first;
    expect(controller.state.displayed?.filter).toEqual(filter);
    expect(controller.state.displayed?.search).toEqual(search);
    controller.dispose();
  }
});

test('rejected filters preserve displayed results and roll back options used by the next search', async () => {
  const api = new FakeAPI();
  const controller = new ViewerController(api);
  const initial = controller.setFilters([{ field: 'level', op: 'eq', value: 'old' }]);
  api.creates.get('old')!.resolve(ready('old-query'));
  await initial;
  const replacement = controller.setFilters([{ field: 'name', op: 'regex', value: '(?=x)' }]);
  const filterErrors = [{ index: 1, property: 'value', message: 'Unsupported expression' }];
  api.creates.get('(?=x)')!.reject(new TransportError('invalid_filter', 'Invalid filter', 400, undefined, filterErrors));
  expect((await replacement)?.filterErrors).toEqual(filterErrors);
  expect(controller.state.displayed?.queryId).toBe('old-query');
  const search = controller.setSearch({ text: 'timeout', mode: 'plain', operator: 'or' });
  expect(api.specs.at(-1)?.filter).toEqual([{ field: 'level', op: 'eq', value: 'old' }]);
  api.creates.get('old')!.resolve(ready('new-search'));
  await search;
  controller.dispose();
});

test('pausing invalidates a resume response already in flight', async () => {
  const api = new RangeAPI();
  const controller = new ViewerController(api);
  await controller.start();
  controller.pause();
  const pending = deferred<QueryState>();
  api.get = () => pending.promise;
  const resumed = controller.resume();
  controller.pause();
  pending.resolve(ready('range-query', '1020', '2'));
  await resumed;
  expect(controller.state.following).toBe(false);
  expect(controller.state.displayed?.snapshot.matchedCount).toBe('1000');
  controller.dispose();
});

test('errors from an obsolete resume do not contaminate the current display', async () => {
  const api = new RangeAPI();
  const controller = new ViewerController(api);
  await controller.start();
  controller.pause();
  const pending = deferred<QueryState>();
  api.get = () => pending.promise;
  const resumed = controller.resume();
  controller.pause();
  pending.reject(new TransportError('snapshot_invalid', 'Expired', 404));
  await resumed;
  expect(controller.state.error).toBeUndefined();
  expect(controller.state.needsRefresh).toBe(false);
  controller.dispose();
});

test('replacement queries load their tail and restore following after navigation paused it', async () => {
  const api = new RangeAPI();
  const controller = new ViewerController(api);
  await controller.start();
  controller.pause();
  api.calls = [];
  api.state.queryId = 'replacement-query';
  api.state.snapshot = ready('replacement-query', '5000').snapshot;
  await controller.setSearch({ text: 'info', mode: 'plain', operator: 'or' });
  expect(controller.state.following).toBe(true);
  expect(api.calls).toContain(4800n);
  expect(controller.state.displayed?.queryId).toBe('replacement-query');
  controller.dispose();
});

test('a recreated source viewer starts with its saved query and releases late source responses', async () => {
  const api = new FakeAPI();
  const controller = new ViewerController(api);
  const spec: QuerySpec = { filter: [{ field: 'level', op: 'eq', value: 'saved' }], sort: 'input', search: { text: 'failure', mode: 'plain', operator: 'or' } };
  const started = controller.start(spec);
  await settle();
  expect(api.specs[0]).toEqual(spec);
  controller.dispose();
  api.creates.get('saved')!.resolve(ready('old-source-query'));
  await started;
  expect(controller.state.displayed).toBeUndefined();
  expect(api.deleted).toContain('old-source-query');
});

test('configuration replacement waits for building query and its first page', async () => {
  class BuildingAPI extends FakeAPI {
    receive?: (event: QueryEvent) => void;
    page = deferred<RowPage>();
    events(_id: string, onEvent: (event: QueryEvent) => void) { this.receive = onEvent; return { close() {} }; }
    rows() { return this.page.promise; }
  }
  const api = new BuildingAPI();
  const controller = new ViewerController(api);
  let completed = false;
  const applying = controller.setQueryAndWait({ filter: [], sort: 'input', search: { text: 'saved', mode: 'plain', operator: 'or' } }).then(error => { completed = true; return error; });
  api.creates.get('')!.resolve({ queryId: 'saved', status: 'building' });
  await settle(); expect(completed).toBe(false);
  api.receive!({ type: 'state', state: ready('saved'), session: await api.session() });
  await settle(); expect(completed).toBe(false);
  api.page.resolve({ snapshot: ready('saved').snapshot!, offset: '0', rows: [{ id: '1', message: 'saved', sourceFormat: 'text' }] });
  expect(await applying).toBeUndefined();
  expect(controller.state.displayed?.search?.text).toBe('saved');
  controller.dispose();
});

test('failed configuration replacement reports failure and retains the previous display', async () => {
  const api = new FakeAPI(), controller = new ViewerController(api);
  const initial = controller.setQuery([]); api.creates.get('')!.resolve(ready('old')); await initial;
  const applying = controller.setQueryAndWait({ filter: [{ field: 'level', op: 'eq', value: 'new' }], sort: 'input' });
  api.creates.get('new')!.reject(new TransportError('invalid_filter', 'Rejected', 400));
  expect((await applying)?.code).toBe('invalid_filter');
  expect(controller.state.displayed?.queryId).toBe('old');
  controller.dispose();
});

test('superseding or disposing a loading configuration settles its caller and ignores late responses', async () => {
  const api = new FakeAPI(), controller = new ViewerController(api);
  const first = controller.setQueryAndWait({ filter: [{ field: 'level', op: 'eq', value: 'first' }], sort: 'input' });
  const second = controller.setQueryAndWait({ filter: [{ field: 'level', op: 'eq', value: 'second' }], sort: 'input' });
  expect((await first)?.code).toBe('configuration_superseded');
  controller.dispose();
  expect((await second)?.code).toBe('configuration_superseded');
  api.creates.get('first')!.resolve(ready('first')); api.creates.get('second')!.resolve(ready('second'));
  await settle(); expect(controller.state.displayed).toBeUndefined();
});
