/** Base-10 integer serialized as a string to preserve the Go service's 64-bit range. */
export type Decimal = string;
/** Current state of the binary's input producer. */
export type InputStatus = 'streaming' | 'eof' | 'error';
/** Mutually exclusive stdin representation currently exposed by the binary. */
export type InputKind = 'pending' | 'records' | 'raw';
/** Lifecycle state of an immutable server-side query. */
export type QueryStatus = 'building' | 'ready' | 'failed';
/** Ordering applied by the query service; input order is the only current option. */
export type QuerySort = 'input';
/** Parser family that produced a normalized log record. */
export type SourceFormat = 'journald-json' | 'json' | 'text';
/** Recursive value domain allowed in normalized structured log fields. */
export type JSONValue = string | number | boolean | null | JSONValue[] | { [key: string]: JSONValue };

/** Non-fatal parser or normalization issue attached to a record. */
export interface ParseDiagnostic {
  code: string;
  message: string;
}

/** Confirmed general search over nested scalar values in each log entry. */
export interface SearchSpec {
  text: string;
  mode: 'plain' | 'regexp';
  operator: 'or' | 'and';
}

/** Complete immutable query command, retained through reconnect and recovery. */
export interface QuerySpec {
  filter: FilterSpec[];
  sort: QuerySort;
  search?: SearchSpec;
}

/** An expression failure at its original one-based editor line. */
export interface LineError {
  line: number;
  message: string;
}

/** Stable machine-readable error envelope returned by the binary. */
export interface APIErrorBody {
  code: string;
  message: string;
  lineErrors?: LineError[];
  filterErrors?: FilterError[];
}

/** Identity and input state for one in-memory binary session. */
export interface Session {
  sessionId: string;
  generationId: string;
  inputStatus: InputStatus;
  inputKind: InputKind;
  error?: APIErrorBody;
}

/** Immutable query result boundary used to keep notifications and row pages consistent. */
export interface Snapshot {
  sessionId: string;
  generationId: string;
  queryId: string;
  revision: Decimal;
  processedThrough: Decimal;
  matchedCount: Decimal;
  /** Opaque capability required when reading rows from this exact boundary. */
  snapshotToken: string;
}

/** Authoritative server-side query lifecycle state delivered by HTTP and SSE. */
export interface QueryState {
  queryId: string;
  status: QueryStatus;
  progress?: { processed: Decimal; total: Decimal };
  snapshot?: Snapshot;
  error?: APIErrorBody;
}

/** Normalized row projected by the binary for summary-table display and later details. */
export interface LogRow {
  id: Decimal;
  timestamp?: string;
  severity?: string;
  message: string;
  fields?: Record<string, JSONValue>;
  sourceFormat: SourceFormat;
  diagnostics?: ParseDiagnostic[];
}

/** Bounded row window read from one immutable query snapshot. */
export interface RowPage {
  snapshot: Snapshot;
  offset: Decimal;
  rows: LogRow[];
}

/** Bounded immutable page of display-safe raw stdin text chunks. */
export interface RawChunkPage {
  generationId: string;
  offset: Decimal;
  totalChunks: Decimal;
  chunks: string[];
}

/** Latest-state notification; row payloads are intentionally excluded from SSE. */
export interface QueryEvent {
  type: 'state' | 'progress' | 'snapshot' | 'input' | 'generation';
  state: QueryState;
  session: Session;
}

/** One ordered condition on an original JSON field. */
export type FilterSpec =
  | { field: string; op: 'eq' | 'contains' | 'regex'; value: string }
  | { field: string; op: 'gt' | 'gte' | 'lt' | 'lte'; value: number };

export interface FilterError {
  index: number;
  property: string;
  message: string;
}
