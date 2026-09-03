import type { APIErrorBody, LogRow, QuerySort, Session, Snapshot } from './types';

export interface DisplayedQuery {
  queryId: string;
  filter: string;
  sort: QuerySort;
  snapshot: Snapshot;
  offset: bigint;
  rows: LogRow[];
}

export interface ViewerState {
  following: boolean;
  displayed?: DisplayedQuery;
  pending?: { queryId: string; filter: string; progress?: number };
  error?: APIErrorBody;
  needsRefresh: boolean;
  session?: Session;
}

export type ViewerAction =
  | { type: 'session'; session: Session }
  | { type: 'pending'; queryId: string; filter: string }
  | { type: 'progress'; queryId: string; processed: bigint; total: bigint }
  | { type: 'replace'; query: DisplayedQuery }
  | { type: 'extend'; snapshot: Snapshot; offset: bigint; rows: LogRow[] }
  | { type: 'pageLoaded'; snapshot: Snapshot; offset: bigint; rows: LogRow[] }
  | { type: 'pause' }
  | { type: 'resume' }
  | { type: 'failed'; error: APIErrorBody }
  | { type: 'refreshRequired'; error: APIErrorBody };

/** Applies one deterministic transport or viewer transition without side effects. */
export function reduceViewer(state: ViewerState, action: ViewerAction): ViewerState {
  switch (action.type) {
    case 'session': return { ...state, session: action.session };
    case 'pending': return { ...state, pending: { queryId: action.queryId, filter: action.filter }, error: undefined };
    case 'progress': {
      if (state.pending?.queryId !== action.queryId) return state;
      const progress = action.total === 0n ? 1 : Number(action.processed * 1000n / action.total) / 1000;
      return { ...state, pending: { ...state.pending, progress } };
    }
    case 'replace': return { ...state, displayed: action.query, pending: undefined, error: undefined, needsRefresh: false };
    case 'extend':
      if (!state.following || state.displayed?.queryId !== action.snapshot.queryId) return state;
      return { ...state, displayed: { ...state.displayed, snapshot: action.snapshot, offset: action.offset, rows: action.rows } };
    case 'pageLoaded':
      if (state.displayed?.queryId !== action.snapshot.queryId) return state;
      return { ...state, displayed: { ...state.displayed, snapshot: action.snapshot, offset: action.offset, rows: action.rows } };
    case 'pause': return { ...state, following: false };
    case 'resume': return { ...state, following: true, needsRefresh: false };
    case 'failed': return { ...state, pending: undefined, error: action.error };
    case 'refreshRequired': return { ...state, following: false, needsRefresh: true, error: action.error };
  }
}
