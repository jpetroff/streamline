<script lang="ts">
  import { registerCommand } from '$lib/keyboard-context';
  import { parseRowJump, type ActiveRow, type RowNavigator } from '$lib/row-navigation';
  import { PanelLeft } from '@lucide/svelte';
  import { COLUMNS_PANEL } from '$lib/side-panels';

  let { rowLines = $bindable(1), columnsOpen = $bindable(true), showRowControls = true, total = 0n, activeRow, navigator }: {
    total?: bigint;
    activeRow?: ActiveRow;
    navigator?: RowNavigator;
    rowLines?: 1 | 2;
    columnsOpen?: boolean;
    showRowControls?: boolean;
  } = $props();
  let jumpInput = $state<HTMLInputElement>();
  let jumpScope = $state<HTMLDivElement>();
  let jump = $state('');
  let error = $state('');
  const keyboard = registerCommand({ id: 'rows.jump.focus', label: 'Jump to row', bindings: ['Ctrl+G'], allowInInput: true, changesFocus: true,
    when: () => showRowControls && total > 0n, handler: () => { jump = String((activeRow?.offset ?? 0n) + 1n); error = ''; jumpInput?.focus(); jumpInput?.select(); } });
  registerCommand({ id: 'rows.jump', label: 'Go to row', bindings: ['Enter'], scope: () => jumpScope, allowInInput: true,
    when: () => total > 0n, handler: go });
  async function go() {
    const offset = parseRowJump(jump, total);
    if (offset === undefined) { error = `Enter a whole row number from 1 to ${total}.`; jumpInput?.focus(); return; }
    error = '';
    await navigator?.navigate(offset, true);
  }
</script>

<section class="relative flex min-h-8 shrink-0 flex-wrap items-center gap-2 border-t bg-shell px-3 text-xs" aria-label="Table toolbar">
  <button
    type="button"
    aria-label="Toggle columns and filters"
    aria-expanded={columnsOpen}
    aria-controls={COLUMNS_PANEL.id}
    title={`${columnsOpen ? 'Hide' : 'Show'} columns and filters (${keyboard.label('Mod+B')} focuses columns)`}
    class="grid size-6 shrink-0 place-items-center rounded border border-input hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    onclick={() => { columnsOpen = !columnsOpen; }}
  ><PanelLeft size={16} aria-hidden="true" /></button>
  {#if showRowControls}
    <span class="text-muted-foreground">Rows</span>
    <div class="inline-flex overflow-hidden rounded border border-input" role="group" aria-label="Row display">
      {#each [1, 2] as lines}
        <button
          type="button"
          aria-pressed={rowLines === lines}
          title={lines === 1 ? 'Show one line per row' : 'Wrap up to two lines per row'}
          class="h-6 px-2 aria-pressed:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
          onclick={() => { rowLines = lines as 1 | 2; }}
        >{lines === 1 ? '1 line' : '2 lines'}</button>
      {/each}
    </div>
    <div bind:this={jumpScope} class="ml-auto flex items-center gap-2 py-1">
      <label for="jump-row">Jump to row</label>
      <input bind:this={jumpInput} id="jump-row" type="text" inputmode="numeric" bind:value={jump} disabled={total === 0n}
        oninput={() => { error = ''; }} aria-invalid={!!error} aria-describedby="jump-row-error" aria-keyshortcuts={keyboard.aria('Ctrl+G')}
        title={keyboard.label('Ctrl+G')} placeholder={activeRow ? String(activeRow.offset + 1n) : '—'}
        class="h-6 w-24 rounded border border-input bg-background px-2 outline-none focus:ring-2 focus:ring-ring" />
      <span class="text-muted-foreground">of {String(total)}</span>
      <button type="button" disabled={total === 0n} onclick={() => void go()} class="h-6 rounded border border-input px-2 hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring">Go</button>
    </div>
    <p id="jump-row-error" class="w-full text-xs text-destructive" aria-live="polite">{error}</p>
  {/if}
</section>
