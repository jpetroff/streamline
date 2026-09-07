/** One application command, with live scope/availability supplied by its owner. */
export interface Command {
  id: string;
  label: string;
  bindings?: readonly string[];
  scope?: () => HTMLElement | undefined;
  when?: () => boolean;
  allowInInput?: boolean;
  repeat?: boolean;
  priority?: number;
  changesFocus?: boolean;
  handler: (event?: KeyboardEvent) => void | Promise<void>;
}
export interface KeyboardOverlay {
  open: () => boolean;
  modal: boolean;
  contains: (target: EventTarget | null) => boolean;
  close: () => void | Promise<void>;
}

export function matchesBinding(event: KeyboardEvent, binding: string, mac: boolean): boolean {
  const parts = binding.split('+');
  const key = parts.pop()!;
  const modifiers = new Set(parts.map(part => part === 'Mod' ? (mac ? 'Meta' : 'Ctrl') : part));
  return event.ctrlKey === modifiers.has('Ctrl') && event.metaKey === modifiers.has('Meta')
    && event.altKey === modifiers.has('Alt') && event.shiftKey === modifiers.has('Shift')
    && (event.key.toLowerCase() === key.toLowerCase() || (mac && event.altKey && !/^[a-z]$/i.test(event.key) && /^[a-z]$/i.test(key) && event.code === `Key${key.toUpperCase()}`));
}
export function isEditable(target: EventTarget | null): boolean {
  return !!(target as HTMLElement | null)?.closest?.('input, textarea, select, [contenteditable]:not([contenteditable="false"])');
}

export class CommandRegistry {
  private commands = new Map<string, Command>();
  private overlays: KeyboardOverlay[] = [];
  constructor(readonly mac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform), private warn = false) {}

  register(command: Command) {
    if (this.commands.has(command.id)) throw new Error(`Duplicate command: ${command.id}`);
    if (this.warn) for (const existing of this.commands.values()) {
      const sameScope = existing.scope
        ? command.scope && existing.scope() && existing.scope() === command.scope()
        : !command.scope;
      const canonical = (binding: string) => this.aria(binding).toLowerCase().split('+').sort().join('+');
      if (sameScope && (existing.priority ?? 0) === (command.priority ?? 0)
        && command.bindings?.some(binding => existing.bindings?.some(other => canonical(binding) === canonical(other)))) {
        console.warn(`Keyboard binding shared by ${existing.id} and ${command.id}; use availability or priority to disambiguate.`);
      }
    }
    this.commands.set(command.id, command);
    return () => { if (this.commands.get(command.id) === command) this.commands.delete(command.id); };
  }
  overlay(overlay: KeyboardOverlay) {
    this.overlays.push(overlay);
    return () => { this.overlays = this.overlays.filter(item => item !== overlay); };
  }
  label(binding: string) {
    return binding.split('+').map(key => ({ Mod: this.mac ? 'Cmd' : 'Ctrl', Meta: 'Cmd', Alt: this.mac ? 'Option' : 'Alt', ArrowUp: '↑', ArrowDown: '↓', PageUp: 'Page Up', PageDown: 'Page Down' }[key] ?? key)).join('+');
  }
  aria(binding: string) { return binding.replaceAll('Mod', this.mac ? 'Meta' : 'Control').replaceAll('Ctrl', 'Control'); }
  async execute(id: string, event?: KeyboardEvent) {
    const command = this.commands.get(id);
    if (!command || command.when?.() === false) return;
    if (command.changesFocus) for (const overlay of [...this.overlays].reverse()) {
      if (overlay.open() && !overlay.modal) await overlay.close();
    }
    await command.handler(event);
  }
  handle = (event: KeyboardEvent) => {
    if (event.defaultPrevented || event.isComposing || event.keyCode === 229 || event.getModifierState?.('AltGraph')) return;
    const openOverlays = [...this.overlays].reverse().filter(item => item.open());
    const overlay = openOverlays.find(item => item.modal) ?? openOverlays[0];
    const path = event.composedPath();
    const candidates = [...this.commands.values()].filter(command => {
      if (!command.bindings?.some(binding => matchesBinding(event, binding, this.mac))) return false;
      const scope = command.scope?.();
      if (command.scope && (!scope || !path.includes(scope))) return false;
      if (overlay?.modal && (!scope || !overlay.contains(scope))) return false;
      if (overlay && event.key === 'Escape') return false; // The overlay's library owns dismissal.
      return command.when?.() !== false && (command.allowInInput || !isEditable(event.target));
    }).sort((a, b) => {
      const aDepth = a.scope ? path.indexOf(a.scope()!) : Infinity;
      const bDepth = b.scope ? path.indexOf(b.scope()!) : Infinity;
      return aDepth - bDepth || (b.priority ?? 0) - (a.priority ?? 0);
    });
    const command = candidates[0];
    if (!command) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    if (!event.repeat || command.repeat) void this.execute(command.id, event);
  };
  attach(target: Window) {
    target.addEventListener('keydown', this.handle, true);
    return () => target.removeEventListener('keydown', this.handle, true);
  }
}
