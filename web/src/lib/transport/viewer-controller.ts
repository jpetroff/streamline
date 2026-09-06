import { pageOffsetsForRange, PAGE_SIZE } from '$lib/virtual-window';
import { HTTPQueryAPI, TransportError, type EventConnection, type QueryAPI } from './api';
import { PageCache } from './page-cache';
import type { APIErrorBody, QueryEvent, QuerySort, QuerySpec, SearchSpec, QueryState, RowPage, Session, Snapshot } from './types';
import { reduceViewer, type ViewerState } from './viewer-state';

/** Coordinates query lifecycle, SSE notifications, guarded page loading, and follow state. */
export class ViewerController {
  state: ViewerState = { following: true, needsRefresh: false };
  private intent = 0;
  private rangeRequest = 0;
  private abort?: AbortController;
  private activeEvents?: EventConnection;
  private pendingEvents?: EventConnection;
  private activeRange?: { key: string; promise: Promise<void> };
  private rawLoad?: Promise<void>;
  private lastLoadedRangeKey?: string;
  private cache = new PageCache();
  private inflightPages = new Map<string, Promise<RowPage | undefined>>();
  private listeners = new Set<(state: ViewerState) => void>();
  private requestedRevision = new Map<string, bigint>();
  private disposed = false;

  /** Creates one independent viewer controller, suitable for a single browser tab. */
  constructor(private readonly api: QueryAPI = new HTTPQueryAPI()) {}

  /** Observes complete state snapshots and returns an unsubscribe callback. */
  subscribe(listener: (state: ViewerState) => void) {
    this.listeners.add(listener);
    listener(this.state);
    return () => this.listeners.delete(listener);
  }

  /** Reads session input state and starts either the parsed query or raw-output path. */
  async start() {
    const intent = ++this.intent;
    this.abort?.abort();
    this.abort = new AbortController();
    try {
      const session = await this.api.session(this.abort.signal);
      if (!this.current(intent)) return;
      this.dispatch({ type: 'session', session });
      if (session.inputKind === 'raw') {
        await this.activateRaw(session);
      } else {
        await this.setQuery('');
      }
    } catch (error) {
      if (this.current(intent) && !isAbort(error)) this.dispatch({ type: 'failed', error: errorBody(error) });
    }
  }

  /** Builds a replacement query while preserving the current display until its first page is ready. */
  async setQuery(filter: string, sort: QuerySort = 'input', search?: SearchSpec): Promise<APIErrorBody | undefined> {
    // Copy caller-owned options before asynchronous work; recovery must replay
    // the confirmed query, never whatever the editor currently contains.
    const spec: QuerySpec = { filter, sort, search: search ? { ...search } : undefined };
    const intent = ++this.intent;
    this.rangeRequest++;
    this.activeRange = undefined;
    this.rawLoad = undefined;
    this.lastLoadedRangeKey = undefined;
    this.inflightPages.clear();
    this.abort?.abort();
    this.abort = new AbortController();
    const superseded = this.state.pending?.queryId;
    this.pendingEvents?.close();
    this.pendingEvents = undefined;
    if (superseded) void this.api.delete(superseded);
    const priorEvents = this.activeEvents;
    try {
      const query = await this.api.create(spec, this.abort.signal);
      if (!this.current(intent)) { void this.api.delete(query.queryId); return; }
      this.dispatch({ type: 'pending', queryId: query.queryId, ...spec });
      this.pendingEvents = this.api.events(query.queryId, event => void this.receive(intent, spec, event), () => void this.resync(intent, spec, query.queryId));
      await this.acceptState(intent, spec, query);
      if (this.state.displayed?.queryId === query.queryId && priorEvents) priorEvents.close();
    } catch (error) {
      if (this.current(intent) && !isAbort(error)) {
        const body = errorBody(error);
        this.dispatch({ type: 'failed', error: body });
        this.restoreDisplayed(intent);
        return body;
      }
    }
  }

  /** Ensures the page-aligned windows around a virtual viewport are available. */
  async ensureRange(start: bigint, endExclusive: bigint) {
    const displayed = this.state.displayed;
    if (!displayed) return;
    const snapshot = displayed.snapshot;
    const offsets = pageOffsetsForRange(start, endExclusive, BigInt(snapshot.matchedCount));
    if (offsets.length === 0) return;
    const key = [snapshot.queryId, snapshot.snapshotToken, ...offsets.map(String)].join(':');
    if (this.activeRange?.key === key) return this.activeRange.promise;
    if (this.lastLoadedRangeKey === key) return;

    const request = ++this.rangeRequest;
    const intent = this.intent;
    const promise = (async () => {
      try {
        const pages = await this.fetchPages(snapshot, offsets, intent);
        if (!this.current(intent) || request !== this.rangeRequest) return;
        const current = this.state.displayed;
        if (current?.queryId !== snapshot.queryId || current.snapshot.snapshotToken !== snapshot.snapshotToken) return;
        this.dispatch({ type: 'pagesLoaded', snapshot, pages });
        this.lastLoadedRangeKey = key;
      } catch (error) {
        if (!this.current(intent) || request !== this.rangeRequest || isAbort(error)) return;
        const body = errorBody(error);
        if (error instanceof TransportError && (error.code === 'snapshot_invalid' || error.status === 404)) {
          this.dispatch({ type: 'refreshRequired', error: body });
        } else {
          this.dispatch({ type: 'failed', error: body });
        }
      } finally {
        if (this.activeRange?.key === key) this.activeRange = undefined;
      }
    })();
    this.activeRange = { key, promise };
    return promise;
  }

  /** Loads the next sequential raw stdin page, deduplicating concurrent requests. */
  async loadMoreRaw() {
    const raw = this.state.raw;
    if (!raw || raw.loading) return;
    if (raw.totalChunks !== undefined && BigInt(raw.nextOffset) >= BigInt(raw.totalChunks)) return;
    if (this.rawLoad) return this.rawLoad;
    const intent = this.intent;
    const generationId = raw.generationId;
    const offset = BigInt(raw.nextOffset);
    this.dispatch({ type: 'rawLoading', generationId });
    let pending!: Promise<void>;
    pending = (async () => {
      try {
        const page = await this.api.raw(generationId, offset, 4, this.abort?.signal);
        if (!this.current(intent) || this.state.session?.inputKind !== 'raw') return;
        if (page.generationId !== generationId || page.offset !== offset.toString()) return;
        this.dispatch({ type: 'rawPage', page });
      } catch (error) {
        if (this.current(intent) && !isAbort(error)) this.dispatch({ type: 'failed', error: errorBody(error) });
      } finally {
        if (this.rawLoad === pending) this.rawLoad = undefined;
      }
    })();
    this.rawLoad = pending;
    return pending;
  }

  /** Pins the currently displayed snapshot while ingestion and query evaluation continue. */
  pause() {
    if (this.state.following) this.dispatch({ type: 'pause' });
  }

  /** Resynchronizes and moves the display to the latest matching tail. */
  async resume() {
    const displayed = this.state.displayed;
    if (!displayed || this.state.following) return;
    const intent = this.intent;
    try {
      const latest = await this.api.get(displayed.queryId);
      if (latest.status !== 'ready' || !latest.snapshot || !this.current(intent)) return;
      const pages = await this.initialPages(latest.snapshot, false, intent);
      if (!this.current(intent) || this.state.displayed?.queryId !== displayed.queryId) return;
      this.lastLoadedRangeKey = undefined;
      this.requestedRevision.set(displayed.queryId, BigInt(latest.snapshot.revision));
      this.dispatch({ type: 'resume', snapshot: latest.snapshot, pages });
    } catch (error) {
      if (error instanceof TransportError && error.status === 404) {
        this.dispatch({ type: 'refreshRequired', error: errorBody(error) });
      } else if (!isAbort(error)) {
        this.dispatch({ type: 'failed', error: errorBody(error) });
      }
    }
  }

  /** Cancels requests, closes streams, and releases server and browser query resources. */
  dispose() {
    this.disposed = true;
    this.intent++;
    this.rangeRequest++;
    this.abort?.abort();
    this.activeEvents?.close();
    this.pendingEvents?.close();
    this.rawLoad = undefined;
    const ids = new Set([this.state.displayed?.queryId, this.state.pending?.queryId].filter(Boolean) as string[]);
    for (const id of ids) void this.api.delete(id);
    this.listeners.clear();
    this.inflightPages.clear();
    this.cache.clear();
  }

  /** Handles one SSE event, including generation replacement and recovery. */
  private async receive(intent: number, spec: QuerySpec, event: QueryEvent) {
    if (!this.current(intent)) return;
    this.dispatch({ type: 'session', session: event.session });
    if (event.session.inputKind === 'raw') {
      await this.activateRaw(event.session);
      return;
    }
    const snapshotGeneration = event.state.snapshot?.generationId;
    if (event.type === 'generation' || (snapshotGeneration && snapshotGeneration !== event.session.generationId)) { void this.setQuery(spec.filter, spec.sort, spec.search); return; }
    try { await this.acceptState(intent, spec, event.state); }
    catch (error) { if (!isAbort(error)) void this.resync(intent, spec, event.state.queryId); }
  }

  /** Leaves query mode and begins guarded sequential raw-output paging. */
  private async activateRaw(session: Session) {
    if (this.state.raw?.generationId === session.generationId) {
      await this.loadMoreRaw();
      return;
    }
    ++this.intent;
    this.rangeRequest++;
    this.abort?.abort();
    this.abort = new AbortController();
    this.activeEvents?.close();
    this.pendingEvents?.close();
    this.activeEvents = undefined;
    this.pendingEvents = undefined;
    this.activeRange = undefined;
    this.rawLoad = undefined;
    const ids = new Set([this.state.displayed?.queryId, this.state.pending?.queryId].filter(Boolean) as string[]);
    this.dispatch({ type: 'rawStart', generationId: session.generationId });
    this.inflightPages.clear();
    this.cache.clear();
    this.requestedRevision.clear();
    for (const id of ids) void this.api.delete(id);
    await this.loadMoreRaw();
  }

  /** Converts authoritative query state into guarded page fetches and atomic viewer transitions. */
  private async acceptState(intent: number, spec: QuerySpec, query: QueryState) {
    if (!this.current(intent)) return;
    if (query.status === 'failed') {
      this.dispatch({ type: 'failed', error: query.error ?? { code: 'query_failed', message: 'Query failed' } });
      this.restoreDisplayed(intent);
      return;
    }
    if (query.status === 'building') {
      if (query.progress) this.dispatch({ type: 'progress', queryId: query.queryId, processed: BigInt(query.progress.processed), total: BigInt(query.progress.total) });
      return;
    }
    if (!query.snapshot) return;
    const snapshot = query.snapshot;
    const revision = BigInt(snapshot.revision);
    const replacing = this.state.displayed?.queryId !== query.queryId;
    if (!replacing && !this.state.following) return;
    if (revision <= (this.requestedRevision.get(query.queryId) ?? -1n)) return;
    this.requestedRevision.set(query.queryId, revision);
    const atHead = replacing && !this.state.following;
    let pages;
    try {
      pages = await this.initialPages(snapshot, atHead, intent);
    } catch (error) {
      if (this.requestedRevision.get(query.queryId) === revision) this.requestedRevision.delete(query.queryId);
      throw error;
    }
    if (!this.current(intent) || this.requestedRevision.get(query.queryId) !== revision) return;
    if (replacing) {
      const oldID = this.state.displayed?.queryId;
      this.dispatch({ type: 'replace', query: { queryId: query.queryId, ...spec, snapshot, pages } });
      this.activeEvents?.close();
      this.activeEvents = this.pendingEvents;
      this.pendingEvents = undefined;
      if (oldID && oldID !== query.queryId) {
        this.requestedRevision.delete(oldID);
        this.cache.deleteQuery(oldID);
        void this.api.delete(oldID);
      }
    } else {
      this.dispatch({ type: 'extend', snapshot, pages });
    }
  }

  /** Loads the first visible page for an atomic head or tail transition. */
  private initialPages(snapshot: Snapshot, atHead: boolean, intent: number) {
    const total = BigInt(snapshot.matchedCount);
    if (total === 0n) return Promise.resolve([] as RowPage[]);
    const focus = atHead ? 0n : total - 1n;
    return this.fetchPages(snapshot, pageOffsetsForRange(focus, focus + 1n, total, 0), intent);
  }

  /** Resolves a group of fixed pages while retaining only valid results. */
  private async fetchPages(snapshot: Snapshot, offsets: bigint[], intent: number) {
    const pages = await Promise.all(offsets.map(offset => this.fetchPage(snapshot, offset, PAGE_SIZE, intent)));
    return pages.filter((page): page is RowPage => page !== undefined);
  }

  /** Resolves a page through the bounded cache, request deduplication, and snapshot guards. */
  private fetchPage(snapshot: Snapshot, offset: bigint, limit: number, intent: number) {
    const cached = this.cache.get(snapshot.queryId, offset, limit, BigInt(snapshot.matchedCount));
    if (cached) return Promise.resolve({ ...cached, snapshot });
    const key = [snapshot.queryId, snapshot.snapshotToken, offset, limit].join(':');
    const existing = this.inflightPages.get(key);
    if (existing) return existing;

    const pending = this.api.rows(snapshot.queryId, snapshot.snapshotToken, offset, limit, this.abort?.signal)
      .then(page => {
        if (!this.current(intent) || page.snapshot.queryId !== snapshot.queryId || page.snapshot.snapshotToken !== snapshot.snapshotToken) return undefined;
        this.cache.set(page, limit);
        return page;
      })
      .finally(() => {
        if (this.inflightPages.get(key) === pending) this.inflightPages.delete(key);
      });
    this.inflightPages.set(key, pending);
    return pending;
  }

  /** Reconnects the previous displayed query after a replacement command fails. */
  private restoreDisplayed(intent: number) {
    const displayed = this.state.displayed;
    if (!displayed || !this.current(intent)) return;
    this.activeEvents?.close();
    this.activeEvents = this.api.events(displayed.queryId, event => void this.receive(intent, displayed, event), () => void this.resync(intent, displayed, displayed.queryId));
    void this.resync(intent, displayed, displayed.queryId);
  }

  /** Reads current state after stream failure and rebuilds expired following queries. */
  private async resync(intent: number, spec: QuerySpec, queryId: string) {
    if (!this.current(intent)) return;
    try { await this.acceptState(intent, spec, await this.api.get(queryId)); }
    catch (error) {
      if (error instanceof TransportError && error.status === 404 && this.state.displayed?.queryId === queryId) {
        if (this.state.following) void this.setQuery(spec.filter, spec.sort, spec.search);
        else this.dispatch({ type: 'refreshRequired', error: errorBody(error) });
      }
    }
  }

  /** Reports whether asynchronous work still belongs to the active user intent. */
  private current(intent: number) { return !this.disposed && intent === this.intent; }
  /** Reduces an action and publishes the resulting immutable viewer state. */
  private dispatch(action: Parameters<typeof reduceViewer>[1]) {
    this.state = reduceViewer(this.state, action);
    for (const listener of this.listeners) listener(this.state);
  }
}

/** Distinguishes expected fetch cancellation from actionable transport failures. */
function isAbort(error: unknown) { return error instanceof DOMException && error.name === 'AbortError'; }
/** Converts unknown client failures into the UI error contract. */
function errorBody(error: unknown): APIErrorBody {
  return error instanceof TransportError ? { code: error.code, message: error.message, lineErrors: error.lineErrors } : { code: 'transport_error', message: error instanceof Error ? error.message : 'Transport failed' };
}
