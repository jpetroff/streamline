export type Decimal = string;
export type InputStatus = 'streaming' | 'eof' | 'error';
export type QueryStatus = 'building' | 'ready' | 'failed';
export type QuerySort = 'input';

export interface APIErrorBody {
  code: string;
  message: string;
}

export interface Session {
  sessionId: string;
  generationId: string;
  inputStatus: InputStatus;
  error?: APIErrorBody;
}

export interface Snapshot {
  sessionId: string;
  generationId: string;
  queryId: string;
  revision: Decimal;
  processedThrough: Decimal;
  matchedCount: Decimal;
  snapshotToken: string;
}

export interface QueryState {
  queryId: string;
  status: QueryStatus;
  progress?: { processed: Decimal; total: Decimal };
  snapshot?: Snapshot;
  error?: APIErrorBody;
}

export interface LogRow {
  id: Decimal;
  timestamp?: string;
  severity?: string;
  message: string;
  fields?: Record<string, string>;
}

export interface RowPage {
  snapshot: Snapshot;
  offset: Decimal;
  rows: LogRow[];
}

export interface QueryEvent {
  type: 'state' | 'progress' | 'snapshot' | 'input' | 'generation';
  state: QueryState;
  session: Session;
}
