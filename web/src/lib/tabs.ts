import { configureColumns } from './columns';
import type { SourcePreferences } from './transport/sources';
import type { CommandRequest, LogSource } from './transport/types';

export interface SourceTab {
  id: string;
  sourceId?: string;
  info?: LogSource;
  command: string;
  mode: 'auto' | 'text';
  preferences?: SourcePreferences;
  pending?: 'run' | 'stop' | 'close';
  error: string;
}
export interface TabState { tabs: SourceTab[]; selected: string }
export interface TabSourceAPI {
  create(request: CommandRequest): Promise<LogSource>;
  remove(id: string): Promise<void>;
  stop(id: string): Promise<void>;
}
export const tabTitle = (tab: SourceTab) => tab.id === 'stdin' ? 'stdin' : tab.info?.command || 'New command';
export function commandPreferences(): SourcePreferences {
  return { spec: { filter: [], sort: 'input' }, columns: configureColumns(['timestamp', 'severity', 'message']), following: true, rowLines: 1 };
}

/** UI tabs outlive individual backend captures. All mutations are scoped to their originating tab. */
export class TabController {
  state: TabState = { tabs: [{ id: 'stdin', sourceId: 'stdin', command: '', mode: 'auto', error: '' }], selected: 'stdin' };
  private nextId = 0;
  private sources = new Map<string, LogSource>();
  private listeners = new Set<(state: TabState) => void>();
  private creates = 0;
  private removedDuringCreates = new Set<string>();
  revision = 0;
  selectionRevision = 0;
  constructor(private api: TabSourceAPI) {}
  subscribe(listener: (state: TabState) => void) {
    this.listeners.add(listener); listener(this.state);
    return () => { this.listeners.delete(listener); };
  }
  get(id: string) { return this.state.tabs.find(tab => tab.id === id); }
  private publish() { for (const listener of this.listeners) listener(this.state); }
  patch(id: string, patch: Partial<SourceTab>) {
    this.state = { ...this.state, tabs: this.state.tabs.map(tab => tab.id === id ? { ...tab, ...patch } : tab) };
    this.publish();
  }
  select(id: string) {
    if (!this.get(id)) return;
    this.selectionRevision++;
    this.state = { ...this.state, selected: id }; this.publish();
  }
  open() {
    const id = `tab-${++this.nextId}`;
    this.state = { ...this.state, tabs: [...this.state.tabs, { id, command: '', mode: 'auto', preferences: commandPreferences(), error: '' }] };
    this.select(id);
    return id;
  }
  private forget(id: string) {
    const index = this.state.tabs.findIndex(tab => tab.id === id);
    if (index < 0 || id === 'stdin') return;
    const selected = this.state.selected === id
      ? (this.state.tabs[index + 1] ?? this.state.tabs[index - 1]).id : this.state.selected;
    if (selected !== this.state.selected) this.selectionRevision++;
    this.state = { tabs: this.state.tabs.filter(tab => tab.id !== id), selected };
    this.publish();
  }
  receive(sources: LogSource[]) {
    this.revision++;
    const next = new Map(sources.map(source => [source.id, source]));
    if (this.creates) for (const id of this.sources.keys()) {
      if (!next.has(id)) this.removedDuringCreates.add(id);
    }
    this.sources = next;
    for (const tab of this.state.tabs) {
      if (!tab.sourceId) continue;
      const info = this.sources.get(tab.sourceId);
      if (info) this.patch(tab.id, { info });
      else if (!tab.pending && tab.id !== 'stdin') this.forget(tab.id);
    }
    this.discover();
  }
  private discover() {
    // A source event can precede its create response. Wait for IDs before adopting unknown sources.
    if (this.creates) return;
    const known = new Set(this.state.tabs.map(tab => tab.sourceId));
    const additions = [...this.sources.values()].filter(info => info.kind === 'command' && !known.has(info.id))
      .map(info => ({ id: `tab-${++this.nextId}`, sourceId: info.id, info, command: info.command ?? '', mode: info.mode, error: '' }));
    if (additions.length) { this.state = { ...this.state, tabs: [...this.state.tabs, ...additions] }; this.publish(); }
  }
  save(id: string, sourceId: string, preferences: SourcePreferences) {
    const tab = this.get(id);
    if (tab?.sourceId === sourceId) this.patch(id, { preferences });
  }
  async run(id: string, preferences?: SourcePreferences, again = false) {
    const tab = this.get(id);
    if (!tab || id === 'stdin' || tab.pending) return;
    const request = { command: again ? tab.info?.command ?? tab.command : tab.command, mode: again ? tab.info?.mode ?? tab.mode : tab.mode };
    if (!request.command.trim() || request.command.includes('\0')) {
      this.patch(id, { error: 'Enter a nonempty command without NUL characters.' }); return;
    }
    this.revision++; // Invalidate a startup list fetched before this mutation.
    this.creates++;
    this.patch(id, { pending: 'run', error: '' });
    try {
      if (tab.sourceId) {
        await this.api.remove(tab.sourceId);
        this.sources.delete(tab.sourceId);
      }
      const retained = preferences ?? this.get(id)?.preferences ?? commandPreferences();
      this.patch(id, { sourceId: undefined, info: undefined, preferences: { ...retained, following: true, offset: undefined } });
      const created = await this.api.create(request);
      // Another client can delete a capture while its create response is still in flight.
      if (this.removedDuringCreates.has(created.id)) { this.forget(id); return; }
      const info = this.sources.get(created.id) ?? created;
      this.sources.set(info.id, info);
      this.patch(id, { sourceId: info.id, info });
    } catch (error) {
      this.patch(id, { error: error instanceof Error ? error.message : String(error) });
    } finally {
      this.patch(id, { pending: undefined });
      this.creates--;
      if (!this.creates) this.removedDuringCreates.clear();
      this.discover();
    }
  }
  async close(id: string) {
    const tab = this.get(id);
    if (!tab || id === 'stdin' || tab.pending) return;
    if (tab.sourceId) this.revision++;
    this.patch(id, { pending: 'close', error: '' });
    try {
      if (tab.sourceId) { await this.api.remove(tab.sourceId); this.sources.delete(tab.sourceId); }
      this.forget(id);
    } catch (error) {
      this.patch(id, { pending: undefined, error: error instanceof Error ? error.message : String(error) });
    }
  }
  async stop(id: string) {
    const tab = this.get(id);
    if (!tab?.sourceId || id === 'stdin' || tab.pending) return;
    this.revision++;
    this.patch(id, { pending: 'stop', error: '' });
    try { await this.api.stop(tab.sourceId); }
    catch (error) { this.patch(id, { error: error instanceof Error ? error.message : String(error) }); }
    finally {
      this.patch(id, { pending: undefined });
      if (!this.sources.has(tab.sourceId)) this.forget(id);
    }
  }
}
