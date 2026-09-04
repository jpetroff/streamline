import type { QueryEvent, QuerySort, QueryState, RowPage, Session } from './types';

/** Structured transport failure carrying the server's stable error code and HTTP status. */
export class TransportError extends Error {
  /** Creates an error carrying the stable server code and HTTP status. */
  constructor(public readonly code: string, message: string, public readonly status: number) {
    super(message);
  }
}

/** Decodes a JSON response and normalizes structured API failures. */
async function responseJSON<T>(response: Response): Promise<T> {
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = body?.error;
    throw new TransportError(error?.code ?? 'http_error', error?.message ?? response.statusText, response.status);
  }
  return body as T;
}

/** Close-only handle for the browser's active query event stream. */
export interface EventConnection {
  /** Closes the underlying browser event stream. */
  close(): void;
}

/** Transport boundary consumed by the viewer controller and replaced by fakes in tests. */
export interface QueryAPI {
  /** Reads current session identity and input state. */
  session(signal?: AbortSignal): Promise<Session>;
  /** Starts a new immutable filter and sort query. */
  create(filter: string, sort: QuerySort, signal?: AbortSignal): Promise<QueryState>;
  /** Resynchronizes the latest authoritative state for a query. */
  get(queryId: string, signal?: AbortSignal): Promise<QueryState>;
  /** Fetches a bounded row window from an exact snapshot token. */
  rows(queryId: string, snapshot: string, offset: bigint, limit: number, signal?: AbortSignal): Promise<RowPage>;
  /** Subscribes to small state notifications for a query. */
  events(queryId: string, onEvent: (event: QueryEvent) => void, onError: () => void): EventConnection;
  /** Releases a query and its result indexes. */
  delete(queryId: string): Promise<void>;
}

/** Same-origin HTTP and EventSource implementation of the versioned query protocol. */
export class HTTPQueryAPI implements QueryAPI {
  /** Creates a same-origin client rooted at the versioned API prefix. */
  constructor(private readonly baseURL = '/api/v1') {}

  /** Implements the session lookup with fetch cancellation. */
  session(signal?: AbortSignal) {
    return fetch(`${this.baseURL}/session`, { signal }).then(responseJSON<Session>);
  }

  /** Sends a query command and returns its initial building or ready state. */
  create(filter: string, sort: QuerySort, signal?: AbortSignal) {
    return fetch(`${this.baseURL}/queries`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ filter, sort }),
      signal,
    }).then(responseJSON<QueryState>);
  }

  /** Reads query state after reconnects or before resuming follow mode. */
  get(queryId: string, signal?: AbortSignal) {
    return fetch(`${this.baseURL}/queries/${encodeURIComponent(queryId)}`, { signal }).then(responseJSON<QueryState>);
  }

  /** Reads rows while preserving 64-bit offsets as decimal strings. */
  rows(queryId: string, snapshot: string, offset: bigint, limit: number, signal?: AbortSignal) {
    const params = new URLSearchParams({ snapshot, offset: offset.toString(), limit: String(limit) });
    return fetch(`${this.baseURL}/queries/${encodeURIComponent(queryId)}/rows?${params}`, { signal }).then(responseJSON<RowPage>);
  }

  /** Opens EventSource and routes every named transport event through one decoder. */
  events(queryId: string, onEvent: (event: QueryEvent) => void, onError: () => void): EventConnection {
    const source = new EventSource(`${this.baseURL}/queries/${encodeURIComponent(queryId)}/events`);
    const receive = (raw: MessageEvent<string>) => {
      try { onEvent(JSON.parse(raw.data) as QueryEvent); } catch { onError(); }
    };
    for (const name of ['state', 'progress', 'snapshot', 'input', 'generation']) source.addEventListener(name, receive as EventListener);
    source.onerror = onError;
    return source;
  }

  /** Deletes a query, treating an already-expired query as successfully released. */
  async delete(queryId: string) {
    const response = await fetch(`${this.baseURL}/queries/${encodeURIComponent(queryId)}`, { method: 'DELETE' });
    if (!response.ok && response.status !== 404) await responseJSON(response);
  }
}
