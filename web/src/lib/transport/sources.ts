import { responseJSON, type EventConnection } from './api';
import type { CommandRequest, LogSource, QuerySpec } from './types';
import type { ColumnConfig } from '$lib/columns';

/** Preferences belong to a source, while its query and page cache belong to a mounted viewer. */
export interface SourcePreferences {
  spec: QuerySpec;
  columns: ColumnConfig[];
  following: boolean;
  offset?: bigint;
  rowLines: 1 | 2;
  /** Explicit settings should carry from stdin into a command, including a loaded default-looking bundle. */
  inheritOnRun?: boolean;
}

export class HTTPSourceAPI {
  constructor(private readonly baseURL = '/api/v1/sources') {}
  list(signal?: AbortSignal): Promise<LogSource[]> {
    return fetch(this.baseURL, { signal }).then(responseJSON<LogSource[]>);
  }
  create(request: CommandRequest): Promise<LogSource> {
    return fetch(this.baseURL, { method: 'POST', headers: this.headers(), body: JSON.stringify(request) }).then(responseJSON<LogSource>);
  }
  async stop(id: string) { await this.mutate(id, 'POST', '/stop'); }
  async remove(id: string) { await this.mutate(id, 'DELETE'); }
  events(onSources: (sources: LogSource[]) => void, onError: () => void): EventConnection {
    const events = new EventSource(`${this.baseURL}/events`);
    events.addEventListener('sources', (event: MessageEvent<string>) => {
      try { onSources(JSON.parse(event.data)); } catch { onError(); }
    });
    events.onerror = onError;
    return { close: () => events.close() };
  }
  private headers() { return { 'Content-Type': 'application/json', 'X-Streamline-Request': '1' }; }
  private async mutate(id: string, method: string, suffix = '') {
    const response = await fetch(`${this.baseURL}/${encodeURIComponent(id)}${suffix}`, { method, headers: this.headers(), body: '{}' });
    if (!response.ok && response.status !== 404) await responseJSON(response);
  }
}
