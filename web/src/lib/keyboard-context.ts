import { getContext, onMount, setContext } from 'svelte';
import { CommandRegistry, type Command, type KeyboardOverlay } from './keyboard';
const KEY = Symbol('keyboard');
export function provideKeyboard() { return setContext(KEY, new CommandRegistry(undefined, import.meta.env.DEV)); }
export function useKeyboard() { return getContext<CommandRegistry>(KEY); }
export function registerCommand(command: Command) {
  const keyboard = useKeyboard();
  onMount(() => keyboard.register(command));
  return keyboard;
}
export function registerOverlay(overlay: KeyboardOverlay) {
  const keyboard = useKeyboard();
  onMount(() => keyboard.overlay(overlay));
}
