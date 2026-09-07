import { describe, expect, test } from 'bun:test';
import { CommandRegistry, matchesBinding } from '../src/lib/keyboard';

function key(key: string, options: Record<string, unknown> = {}) {
  const target = { closest: () => null };
  return { key, code: '', ctrlKey: false, metaKey: false, shiftKey: false, altKey: false, repeat: false,
    defaultPrevented: false, target, composedPath: () => [target], getModifierState: () => false,
    preventDefault() { this.defaultPrevented = true; }, stopImmediatePropagation() {}, ...options } as unknown as KeyboardEvent;
}

describe('command registry', () => {
  test('matches exact platform modifiers, including Option-generated letter keys', () => {
    expect(matchesBinding(key('f', { ctrlKey: true }), 'Mod+F', false)).toBe(true);
    expect(matchesBinding(key('u', { ctrlKey: true, code: 'KeyF' }), 'Mod+F', false)).toBe(false);
    expect(matchesBinding(key('f', { ctrlKey: true, shiftKey: true }), 'Mod+F', false)).toBe(false);
    expect(matchesBinding(key('f', { metaKey: true }), 'Mod+F', true)).toBe(true);
    expect(matchesBinding(key('ƒ', { metaKey: true, altKey: true, code: 'KeyF' }), 'Mod+Alt+F', true)).toBe(true);
    expect(matchesBinding(key('g', { ctrlKey: true }), 'Ctrl+G', true)).toBe(true);
    const registry = new CommandRegistry(true);
    expect(registry.label('Mod+Alt+F')).toBe('Cmd+Option+F');
    expect(registry.aria('Mod+Enter')).toBe('Meta+Enter');
  });
  test('deepest scope wins, availability is live, and unregister removes handlers', () => {
    const registry = new CommandRegistry(false);
    const outer = {} as HTMLElement, inner = {} as HTMLElement;
    const calls: string[] = [];
    let available = true;
    registry.register({ id: 'global', label: '', bindings: ['Enter'], handler: () => { calls.push('global'); } });
    registry.register({ id: 'outer', label: '', bindings: ['Enter'], scope: () => outer, handler: () => { calls.push('outer'); } });
    const remove = registry.register({ id: 'inner', label: '', bindings: ['Enter'], scope: () => inner, when: () => available, handler: () => { calls.push('inner'); } });
    const event = () => key('Enter', { composedPath: () => [inner, outer] });
    registry.handle(event()); available = false; registry.handle(event()); available = true; remove(); registry.handle(event());
    expect(calls).toEqual(['inner', 'outer', 'outer']);
  });
  test('editable policy, composition and repeats preserve ordinary editing', () => {
    const registry = new CommandRegistry(false);
    let calls = 0;
    registry.register({ id: 'row', label: '', bindings: ['ArrowUp'], repeat: true, handler: () => { calls++; } });
    registry.register({ id: 'focus', label: '', bindings: ['Ctrl+F'], allowInInput: true, handler: () => { calls++; } });
    const target = { closest: () => ({}) };
    registry.handle(key('ArrowUp', { target }));
    registry.handle(key('f', { target, ctrlKey: true, isComposing: true }));
    registry.handle(key('f', { target, ctrlKey: true, getModifierState: () => true }));
    registry.handle(key('f', { target, ctrlKey: true, repeat: true }));
    expect(calls).toBe(0);
    registry.handle(key('f', { target, ctrlKey: true }));
    registry.handle(key('ArrowUp', { repeat: true }));
    expect(calls).toBe(2);
  });
  test('modal scope blocks background commands and leaves Escape to the overlay', () => {
    const registry = new CommandRegistry(false);
    const scope = {} as HTMLElement;
    const calls: string[] = [];
    registry.overlay({ open: () => true, modal: true, contains: target => target === scope, close() {} });
    registry.register({ id: 'global', label: '', bindings: ['Ctrl+F', 'Escape'], handler: () => { calls.push('global'); } });
    registry.register({ id: 'import', label: '', bindings: ['Ctrl+Enter'], scope: () => scope, handler: () => { calls.push('import'); } });
    registry.handle(key('f', { ctrlKey: true })); registry.handle(key('Escape'));
    registry.handle(key('Enter', { ctrlKey: true, composedPath: () => [scope] }));
    expect(calls).toEqual(['import']);
  });
  test('focus commands dismiss popovers before execution and IDs must be unique', async () => {
    const registry = new CommandRegistry(false);
    const calls: string[] = [];
    registry.overlay({ open: () => true, modal: false, contains: () => false, close: async () => { calls.push('close'); } });
    const command = { id: 'focus', label: '', changesFocus: true, handler: () => { calls.push('focus'); } };
    registry.register(command);
    expect(() => registry.register(command)).toThrow('Duplicate command');
    await registry.execute('focus');
    expect(calls).toEqual(['close', 'focus']);
  });
});

test('a recognized event dispatches once and window listener cleanup uses capture', () => {
  const registry = new CommandRegistry(false);
  let calls = 0;
  let stopped = 0;
  registry.register({ id: 'low', label: '', bindings: ['Escape'], handler: () => { calls += 100; } });
  registry.register({ id: 'high', label: '', bindings: ['Escape'], priority: 10, handler: () => { calls++; } });
  const event = key('Escape', { stopImmediatePropagation: () => { stopped++; } });
  registry.handle(event);
  registry.handle(event); // A later listener sees defaultPrevented.
  expect(calls).toBe(1);
  expect(stopped).toBe(1);
  const listeners: unknown[][] = [];
  const target = {
    addEventListener: (...args: unknown[]) => { listeners.push(args); },
    removeEventListener: (...args: unknown[]) => { listeners.push(args); },
  } as unknown as Window;
  const detach = registry.attach(target);
  detach();
  expect(listeners).toEqual([['keydown', registry.handle, true], ['keydown', registry.handle, true]]);
});
