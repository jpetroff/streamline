import { expect, test } from 'bun:test';
import { TabController, commandPreferences, type TabSourceAPI } from '../src/lib/tabs';
import type { CommandRequest, LogSource } from '../src/lib/transport/types';

function source(id: string, command = id): LogSource {
  return { id, kind: 'command', command, mode: 'text', state: 'running', createdAt: '',
    session: { sessionId: id, generationId: id, inputStatus: 'streaming', inputKind: 'records' } };
}
function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function setup(overrides: Partial<TabSourceAPI> = {}) {
  const calls: string[] = [];
  let number = 0;
  const api: TabSourceAPI = {
    create: async (request: CommandRequest) => { calls.push(`create:${request.command}`); return source(`source-${++number}`, request.command); },
    remove: async id => { calls.push(`remove:${id}`); },
    stop: async id => { calls.push(`stop:${id}`); }, ...overrides,
  };
  return { tabs: new TabController(api), calls };
}

test('blank tabs have independent drafts/defaults and stdin is immutable', async () => {
  const { tabs, calls } = setup();
  const first = tabs.open(); tabs.patch(first, { command: 'first', mode: 'text' });
  const second = tabs.open();
  expect(tabs.get(second)?.command).toBe(''); expect(tabs.get(second)?.mode).toBe('auto');
  expect(tabs.get(second)?.preferences?.columns.map(column => column.path)).toEqual(['timestamp', 'severity', 'message']);
  tabs.select(first); expect(tabs.get(first)?.command).toBe('first');
  await tabs.close('stdin'); await tabs.run('stdin'); await tabs.stop('stdin');
  expect(tabs.state.tabs[0].id).toBe('stdin'); expect(calls).toEqual([]);
});

test('closing selects the right neighbor then the left; background close preserves selection', async () => {
  const { tabs } = setup();
  const first = tabs.open(), second = tabs.open(), third = tabs.open();
  tabs.select(second); await tabs.close(first); expect(tabs.state.selected).toBe(second);
  await tabs.close(second); expect(tabs.state.selected).toBe(third);
  await tabs.close(third); expect(tabs.state.selected).toBe('stdin');
});

test('replacement waits for discard, preserves settings and ignores old viewer saves', async () => {
  const removed = deferred<void>();
  const { tabs, calls } = setup({ remove: () => removed.promise });
  const id = tabs.open(); tabs.patch(id, { command: 'old' }); await tabs.run(id);
  const settings = commandPreferences(); settings.following = false; settings.offset = 30n; settings.rowLines = 2;
  settings.spec.search = { text: 'retained', mode: 'plain', operator: 'or' };
  tabs.patch(id, { command: 'new' }); const replacing = tabs.run(id, settings);
  expect(tabs.get(id)?.sourceId).toBe('source-1'); expect(calls).toEqual(['create:old']);
  await tabs.close(id); await tabs.run(id); await tabs.stop(id);
  const other = tabs.open();
  removed.resolve(); await replacing;
  expect(tabs.state.selected).toBe(other); expect(tabs.state.tabs.map(tab => tab.id)).toEqual(['stdin', id, other]);
  expect(tabs.get(id)?.sourceId).toBe('source-2');
  expect(tabs.get(id)?.preferences).toEqual({ ...settings, following: true, offset: undefined });
  tabs.save(id, 'source-1', commandPreferences());
  expect(tabs.get(id)?.preferences?.spec.search?.text).toBe('retained');
});

test('Run again uses the executed command/mode without overwriting a newer draft', async () => {
  const { tabs, calls } = setup(); const id = tabs.open();
  tabs.patch(id, { command: 'executed', mode: 'text' }); await tabs.run(id);
  tabs.patch(id, { command: 'draft', mode: 'auto' }); await tabs.run(id, undefined, true);
  expect(calls).toEqual(['create:executed', 'remove:source-1', 'create:executed']);
  expect(tabs.get(id)?.command).toBe('draft'); expect(tabs.get(id)?.mode).toBe('auto');
  expect(tabs.state.tabs).toHaveLength(2);
});

test('failed discard retains the source; failed creation retains an empty editable tab', async () => {
  let failDelete = true;
  const { tabs } = setup({ remove: async () => { if (failDelete) throw new Error('discard failed'); }, create: async () => { throw new Error('start failed'); } });
  tabs.receive([source('old')]); const id = tabs.state.tabs[1].id;
  tabs.patch(id, { command: 'replacement' }); await tabs.run(id);
  expect(tabs.get(id)?.sourceId).toBe('old'); expect(tabs.get(id)?.error).toBe('discard failed');
  failDelete = false; await tabs.run(id);
  expect(tabs.get(id)?.sourceId).toBeUndefined(); expect(tabs.get(id)?.info).toBeUndefined();
  expect(tabs.get(id)?.command).toBe('replacement'); expect(tabs.get(id)?.error).toBe('start failed');
  expect(tabs.get(id)?.pending).toBeUndefined(); expect(tabs.get(id)?.preferences).toBeDefined();
});

test('empty and NUL commands cannot discard a source', async () => {
  const { tabs, calls } = setup(); tabs.receive([source('old')]); const id = tabs.state.tabs[1].id;
  for (const command of ['   ', 'bad\0command']) { tabs.patch(id, { command }); await tabs.run(id); }
  expect(calls).toEqual([]); expect(tabs.get(id)?.sourceId).toBe('old');
});

test('events preceding concurrent create responses do not duplicate tabs or steal selection', async () => {
  const first = deferred<LogSource>(), second = deferred<LogSource>(); let count = 0;
  const { tabs } = setup({ create: () => ++count === 1 ? first.promise : second.promise });
  const a = tabs.open(); tabs.patch(a, { command: 'a' }); const runA = tabs.run(a);
  const b = tabs.open(); tabs.patch(b, { command: 'b' }); const runB = tabs.run(b);
  tabs.receive([source('a'), source('b'), source('external')]);
  expect(tabs.state.tabs).toHaveLength(3);
  second.resolve({ ...source('b'), state: 'starting' }); await runB;
  tabs.select('stdin'); first.resolve(source('a')); await runA;
  expect(tabs.state.tabs.map(tab => tab.sourceId)).toEqual(['stdin', 'a', 'b', 'external']);
  expect(tabs.get(b)?.info?.state).toBe('running'); expect(tabs.state.selected).toBe('stdin');
});

test('reconciliation retains blank/replacing tabs and removes externally deleted sources', async () => {
  const removed = deferred<void>(); const { tabs } = setup({ remove: () => removed.promise });
  tabs.receive([source('a'), source('b')]); const a = tabs.state.tabs[1].id, b = tabs.state.tabs[2].id;
  const blank = tabs.open(); tabs.select(a); const replacing = tabs.run(a);
  tabs.receive([source('b')]); expect(tabs.get(a)).toBeDefined(); expect(tabs.get(blank)).toBeDefined();
  removed.resolve(); await replacing;
  tabs.select(b); tabs.receive([source('source-1')]);
  expect(tabs.state.selected).toBe(blank); expect(tabs.get(b)).toBeUndefined();
});

test('close failure is scoped to the originating tab and stop retains output', async () => {
  const { tabs, calls } = setup({ remove: async () => { throw new Error('offline'); } });
  tabs.receive([source('a')]); const id = tabs.state.tabs[1].id;
  tabs.select(id); await tabs.stop(id); expect(tabs.get(id)?.sourceId).toBe('a');
  const other = tabs.open(); await tabs.close(id);
  expect(tabs.state.selected).toBe(other); expect(tabs.get(id)?.error).toBe('offline');
  expect(tabs.get(other)?.error).toBe(''); expect(calls).toEqual(['stop:a']);
});

test('external deletion during a delayed create response cannot resurrect the source', async () => {
  const created = deferred<LogSource>(); const { tabs } = setup({ create: () => created.promise });
  const id = tabs.open(); tabs.patch(id, { command: 'a' }); const running = tabs.run(id);
  tabs.receive([source('a')]); tabs.receive([]);
  created.resolve(source('a')); await running;
  expect(tabs.get(id)).toBeUndefined(); expect(tabs.state.selected).toBe('stdin');
});

test('external deletion during Stop removes the tab after the pending response settles', async () => {
  const stopped = deferred<void>(); const { tabs } = setup({ stop: () => stopped.promise });
  tabs.receive([source('a')]); const id = tabs.state.tabs[1].id; tabs.select(id);
  const stopping = tabs.stop(id); tabs.receive([]);
  stopped.resolve(); await stopping;
  expect(tabs.get(id)).toBeUndefined(); expect(tabs.state.selected).toBe('stdin');
});
