<script lang="ts">
  import { onMount, type Snippet } from 'svelte';
  import { clampPanelWidth, draggedPanelWidth, keyboardPanelWidth, panelWidthLimits, type SidePanelDefinition } from '$lib/side-panels';

  let { definition, open, preferredWidth = $bindable(), children, class: className = '' }: {
    definition: SidePanelDefinition;
    open: boolean;
    preferredWidth?: number;
    children: Snippet;
    class?: string;
  } = $props();

  let viewportWidth = $state(0);
  let rem = $state(16);
  let handle: HTMLDivElement;
  let drag = $state<{ pointerId: number; startX: number; startWidth: number }>();
  const limits = $derived(panelWidthLimits(viewportWidth));
  const width = $derived(clampPanelWidth(preferredWidth ?? definition.defaultWidth(viewportWidth, rem), viewportWidth));

  onMount(() => { rem = parseFloat(getComputedStyle(document.documentElement).fontSize); });

  $effect(() => {
    if (!open) stopResize();
  });

  // Restore document styles on pointer cancellation, hiding, and unmounting.
  $effect(() => {
    if (!drag) return;
    const previousCursor = document.documentElement.style.cursor;
    const previousSelection = document.documentElement.style.userSelect;
    document.documentElement.style.cursor = 'col-resize';
    document.documentElement.style.userSelect = 'none';
    return () => {
      document.documentElement.style.cursor = previousCursor;
      document.documentElement.style.userSelect = previousSelection;
    };
  });

  function startResize(event: PointerEvent) {
    if (event.button !== 0 || drag) return;
    event.preventDefault();
    handle.focus();
    handle.setPointerCapture(event.pointerId);
    drag = { pointerId: event.pointerId, startX: event.clientX, startWidth: width };
  }

  function moveResize(event: PointerEvent) {
    if (!drag || drag.pointerId !== event.pointerId) return;
    preferredWidth = draggedPanelWidth(definition.side, drag.startWidth, event.clientX - drag.startX, viewportWidth);
  }

  function stopResize(event?: PointerEvent) {
    if (!drag || (event && event.pointerId !== drag.pointerId)) return;
    const pointerId = drag.pointerId;
    drag = undefined;
    if (handle?.hasPointerCapture(pointerId)) handle.releasePointerCapture(pointerId);
  }

  function resizeWithKeyboard(event: KeyboardEvent) {
    const nextWidth = keyboardPanelWidth(definition.side, width, event.key, viewportWidth);
    if (nextWidth === undefined) return;
    event.preventDefault();
    preferredWidth = nextWidth;
  }
</script>

<svelte:window bind:innerWidth={viewportWidth} />

<aside
  id={definition.id}
  aria-label={definition.label}
  hidden={!open}
  inert={!open}
  class={`side-panel bg-sidebar text-sidebar-foreground ${className}`}
  class:right={definition.side === 'right'}
  style:width={`${width}px`}
>
  <div class="panel-content">{@render children()}</div>
  <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (a focusable ARIA window splitter is an interactive separator) -->
  <div
    bind:this={handle}
    class="panel-divider"
    class:active={drag !== undefined}
    role="separator"
    tabindex={open ? 0 : -1}
    aria-label={`Resize ${definition.label.toLowerCase()}`}
    aria-orientation="vertical"
    aria-controls={definition.id}
    aria-valuemin={limits.minimum}
    aria-valuemax={limits.maximum}
    aria-valuenow={width}
    aria-valuetext={`${Math.round(width)} pixels`}
    onpointerdown={startResize}
    onpointermove={moveResize}
    onpointerup={stopResize}
    onpointercancel={stopResize}
    onlostpointercapture={stopResize}
    onkeydown={resizeWithKeyboard}
  ></div>
</aside>

<style>
  .side-panel { display: flex; min-width: 0; min-height: 0; flex-shrink: 0; overflow: hidden; }
  .side-panel[hidden] { display: none; }
  .right { flex-direction: row-reverse; }
  .panel-content { flex: 1; min-width: 0; min-height: 0; overflow: hidden; }
  .panel-divider { width: 6px; flex-shrink: 0; cursor: col-resize; touch-action: none; border-right: 1px solid var(--color-border); }
  .right .panel-divider { border-right: 0; border-left: 1px solid var(--color-border); }
  .panel-divider:hover, .panel-divider:focus-visible, .panel-divider.active { background: var(--color-ring); outline: none; }
</style>
