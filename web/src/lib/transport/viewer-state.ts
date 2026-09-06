import type { APIErrorBody, QuerySpec, RawChunkPage, RowPage, Session, Snapshot } from './types';

/** Query snapshot and small page set currently presented by the virtual table. */
export interface DisplayedQuery extends QuerySpec {
  queryId: string;
  snapshot: Snapshot;
  /** Visible and prefetched pages only; the larger reusable set remains in PageCache. */
  pages: RowPage[];
}

/** Loaded prefix and cursor for terminal display-safe raw stdin output. */
export interface RawDisplay {
  generationId: string;
  chunks: string[];
  nextOffset: string;
  totalChunks?: string;
  loading: boolean;
}

/** Immutable UI projection published to every controller subscriber. */
export interface ViewerState {
  /** True only while snapshot notifications should move the table to the latest tail. */
  following: boolean;
  displayed?: DisplayedQuery;
  pending?: QuerySpec & { queryId: string; progress?: number };
  error?: APIErrorBody;
  /** Signals that a paused snapshot expired and must not be replaced silently. */
  needsRefresh: boolean;
  session?: Session;
  raw?: RawDisplay;
}

/** Exhaustive events accepted by the pure viewer reducer. */
export type ViewerAction =
  | { type: 'session'; session: Session }
  | ({ type: 'pending'; queryId: string } & QuerySpec)
  | { type: 'progress'; queryId: string; processed: bigint; total: bigint }
  | { type: 'replace'; query: DisplayedQuery }
  | { type: 'extend'; snapshot: Snapshot; pages: RowPage[] }
  | { type: 'pagesLoaded'; snapshot: Snapshot; pages: RowPage[] }
  | { type: 'rawStart'; generationId: string }
  | { type: 'rawLoading'; generationId: string }
  | { type: 'rawPage'; page: RawChunkPage }
  | { type: 'pause' }
  | { type: 'resume'; snapshot: Snapshot; pages: RowPage[] }
  | { type: 'failed'; error: APIErrorBody }
  | { type: 'refreshRequired'; error: APIErrorBody };

/** Applies one deterministic transport or viewer transition without side effects. */
export function reduceViewer(state: ViewerState, action: ViewerAction): ViewerState {
  switch (action.type) {
    case 'session': return { ...state, session: action.session };
    case 'pending': return { ...state, pending: { queryId: action.queryId, filter: action.filter, sort: action.sort, search: action.search }, raw: undefined, error: undefined };
    case 'progress': {
      if (state.pending?.queryId !== action.queryId) return state;
      const progress = action.total === 0n ? 1 : Number(action.processed * 1000n / action.total) / 1000;
      return { ...state, pending: { ...state.pending, progress } };
    }
    case 'replace': return { ...state, displayed: action.query, raw: undefined, pending: undefined, error: undefined, needsRefresh: false };
    case 'extend':
      if (!state.following || state.displayed?.queryId !== action.snapshot.queryId) return state;
      return { ...state, displayed: { ...state.displayed, snapshot: action.snapshot, pages: action.pages } };
    case 'pagesLoaded':
      if (state.displayed?.queryId !== action.snapshot.queryId || state.displayed.snapshot.snapshotToken !== action.snapshot.snapshotToken) return state;
      return { ...state, displayed: { ...state.displayed, pages: action.pages } };
    case 'rawStart':
      return { ...state, displayed: undefined, pending: undefined, error: undefined, needsRefresh: false,
        raw: { generationId: action.generationId, chunks: [], nextOffset: '0', loading: false } };
    case 'rawLoading':
      if (state.raw?.generationId !== action.generationId) return state;
      return { ...state, raw: { ...state.raw, loading: true } };
    case 'rawPage': {
      const raw = state.raw;
      if (!raw || raw.generationId !== action.page.generationId || raw.nextOffset !== action.page.offset) return state;
      return { ...state, raw: {
        ...raw,
        chunks: [...raw.chunks, ...action.page.chunks],
        nextOffset: (BigInt(action.page.offset) + BigInt(action.page.chunks.length)).toString(),
        totalChunks: action.page.totalChunks,
        loading: false,
      } };
    }
    case 'pause': return { ...state, following: false };
    case 'resume':
      if (state.displayed?.queryId !== action.snapshot.queryId) return state;
      return { ...state, following: true, needsRefresh: false, error: undefined, displayed: { ...state.displayed, snapshot: action.snapshot, pages: action.pages } };
    case 'failed': return { ...state, pending: undefined, raw: state.raw ? { ...state.raw, loading: false } : undefined, error: action.error };
    case 'refreshRequired': return { ...state, following: false, needsRefresh: true, error: action.error };
  }
}
