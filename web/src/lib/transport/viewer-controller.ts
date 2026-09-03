import { HTTPQueryAPI, TransportError, type EventConnection, type QueryAPI } from './api';
import { PageCache } from './page-cache';
import type { APIErrorBody, QueryEvent, QuerySort, QueryState, Snapshot } from './types';
import { reduceViewer, type ViewerState } from './viewer-state';

const PAGE_SIZE = 200;

export class ViewerController {
  state: ViewerState = { following: true, needsRefresh: false };
  private intent = 0;
  private abort?: AbortController;
  private activeEvents?: EventConnection;
  private pendingEvents?: EventConnection;
  private cache = new PageCache();
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

  /** Builds a replacement query while preserving the current display until its first page is ready. */
  async setQuery(filter: string, sort: QuerySort = 'input') {
    const intent = ++this.intent;
    this.abort?.abort();
    this.abort = new AbortController();
    const superseded = this.state.pending?.queryId;
    this.pendingEvents?.close();
    this.pendingEvents = undefined;
    if (superseded) void this.api.delete(superseded);
    const priorEvents = this.activeEvents;
    try {
      const query = await this.api.create(filter, sort, this.abort.signal);
      if (!this.current(intent)) { void this.api.delete(query.queryId); return; }
      this.dispatch({ type: 'pending', queryId: query.queryId, filter });
      this.pendingEvents = this.api.events(query.queryId, event => void this.receive(intent, filter, sort, event), () => void this.resync(intent, filter, sort, query.queryId));
      await this.acceptState(intent, filter, sort, query);
      if (this.state.displayed?.queryId === query.queryId && priorEvents) priorEvents.close();
    } catch (error) {
      if (this.current(intent) && !isAbort(error)) {
        this.dispatch({ type: 'failed', error: errorBody(error) });
        this.restoreDisplayed(intent);
      }
    }
  }

  /** Loads an arbitrary viewport window and pauses when navigating away from the tail. */
  async loadWindow(offset: bigint, limit = PAGE_SIZE) {
    const displayed = this.state.displayed;
    if (!displayed) return;
    const count = BigInt(displayed.snapshot.matchedCount);
    if (offset + BigInt(limit) < count) this.pause();
    try {
      await this.loadPage(this.intent, displayed.snapshot, offset, limit, 'pageLoaded');
    } catch (error) {
      const body = errorBody(error);
      if (error instanceof TransportError && error.code === 'snapshot_invalid') this.dispatch({ type: 'refreshRequired', error: body });
      else if (!isAbort(error)) this.dispatch({ type: 'failed', error: body });
    }
  }

  /** Pins the currently displayed snapshot while ingestion and query evaluation continue. */
  pause() { this.dispatch({ type: 'pause' }); }

  /** Resynchronizes and moves the display to the latest matching tail. */
  async resume() {
    const displayed = this.state.displayed;
    if (!displayed) return;
    try {
      const latest = await this.api.get(displayed.queryId);
      if (latest.status !== 'ready' || !latest.snapshot) return;
      await this.loadPage(this.intent, latest.snapshot, tailOffset(latest.snapshot), PAGE_SIZE, 'pageLoaded');
      this.dispatch({ type: 'resume' });
    } catch (error) {
      if (error instanceof TransportError && error.status === 404) {
        this.dispatch({ type: 'refreshRequired', error: errorBody(error) });
      } else this.dispatch({ type: 'failed', error: errorBody(error) });
    }
  }

  /** Cancels requests, closes streams, and releases server and browser query resources. */
  dispose() {
    this.disposed = true;
    this.intent++;
    this.abort?.abort();
    this.activeEvents?.close();
    this.pendingEvents?.close();
    const ids = new Set([this.state.displayed?.queryId, this.state.pending?.queryId].filter(Boolean) as string[]);
    for (const id of ids) void this.api.delete(id);
    this.listeners.clear();
    this.cache.clear();
  }

  /** Handles one SSE event, including generation replacement and recovery. */
  private async receive(intent: number, filter: string, sort: QuerySort, event: QueryEvent) {
    if (!this.current(intent)) return;
    this.dispatch({ type: 'session', session: event.session });
    const snapshotGeneration = event.state.snapshot?.generationId;
    if (event.type === 'generation' || (snapshotGeneration && snapshotGeneration !== event.session.generationId)) { void this.setQuery(filter, sort); return; }
    try { await this.acceptState(intent, filter, sort, event.state); }
    catch (error) { if (!isAbort(error)) void this.resync(intent, filter, sort, event.state.queryId); }
  }

  /** Converts authoritative query state into guarded page fetches and atomic viewer transitions. */
  private async acceptState(intent: number, filter: string, sort: QuerySort, query: QueryState) {
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
    const revision = BigInt(query.snapshot.revision);
    const replacing = this.state.displayed?.queryId !== query.queryId;
    if (!replacing && !this.state.following) return;
    if (revision <= (this.requestedRevision.get(query.queryId) ?? -1n)) return;
    this.requestedRevision.set(query.queryId, revision);
    const offset = replacing && !this.state.following ? 0n : tailOffset(query.snapshot);
    let page;
    try { page = await this.fetchPage(query.snapshot, offset, PAGE_SIZE, intent); }
    catch (error) {
      if (this.requestedRevision.get(query.queryId) === revision) this.requestedRevision.delete(query.queryId);
      throw error;
    }
    if (!page || !this.current(intent) || this.requestedRevision.get(query.queryId) !== revision) return;
    if (replacing) {
      const oldID = this.state.displayed?.queryId;
      this.dispatch({ type: 'replace', query: { queryId: query.queryId, filter, sort, snapshot: page.snapshot, offset, rows: page.rows } });
      this.activeEvents?.close();
      this.activeEvents = this.pendingEvents;
      this.pendingEvents = undefined;
      if (oldID && oldID !== query.queryId) { this.cache.deleteQuery(oldID); void this.api.delete(oldID); }
    } else {
      this.dispatch({ type: 'extend', snapshot: page.snapshot, offset, rows: page.rows });
    }
  }

  /** Fetches and applies a page only while its originating intent remains current. */
  private async loadPage(intent: number, snapshot: Snapshot, offset: bigint, limit: number, type: 'extend' | 'pageLoaded') {
    const page = await this.fetchPage(snapshot, offset, limit, intent);
    if (page && this.current(intent)) this.dispatch({ type, snapshot: page.snapshot, offset, rows: page.rows });
  }

  /** Resolves a page through the bounded cache and verifies its snapshot identity. */
  private async fetchPage(snapshot: Snapshot, offset: bigint, limit: number, intent: number) {
    const cached = this.cache.get(snapshot.queryId, offset, limit, BigInt(snapshot.matchedCount));
    if (cached) return { ...cached, snapshot };
    const page = await this.api.rows(snapshot.queryId, snapshot.snapshotToken, offset, limit, this.abort?.signal);
    if (!this.current(intent) || page.snapshot.queryId !== snapshot.queryId || page.snapshot.snapshotToken !== snapshot.snapshotToken) return undefined;
    this.cache.set(page, limit);
    return page;
  }

  /** Reconnects the previous displayed query after a replacement command fails. */
  private restoreDisplayed(intent: number) {
    const displayed = this.state.displayed;
    if (!displayed || !this.current(intent)) return;
    this.activeEvents?.close();
    this.activeEvents = this.api.events(displayed.queryId, event => void this.receive(intent, displayed.filter, displayed.sort, event), () => void this.resync(intent, displayed.filter, displayed.sort, displayed.queryId));
    void this.resync(intent, displayed.filter, displayed.sort, displayed.queryId);
  }

  /** Reads current state after stream failure and rebuilds expired following queries. */
  private async resync(intent: number, filter: string, sort: QuerySort, queryId: string) {
    if (!this.current(intent)) return;
    try { await this.acceptState(intent, filter, sort, await this.api.get(queryId)); }
    catch (error) {
      if (error instanceof TransportError && error.status === 404 && this.state.displayed?.queryId === queryId) {
        if (this.state.following) void this.setQuery(filter, sort);
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

/** Calculates the first offset of the default tail window. */
function tailOffset(snapshot: Snapshot) {
  const count = BigInt(snapshot.matchedCount);
  return count > BigInt(PAGE_SIZE) ? count - BigInt(PAGE_SIZE) : 0n;
}
/** Distinguishes expected fetch cancellation from actionable transport failures. */
function isAbort(error: unknown) { return error instanceof DOMException && error.name === 'AbortError'; }
/** Converts unknown client failures into the UI error contract. */
function errorBody(error: unknown): APIErrorBody {
  return error instanceof TransportError ? { code: error.code, message: error.message } : { code: 'transport_error', message: error instanceof Error ? error.message : 'Transport failed' };
}
