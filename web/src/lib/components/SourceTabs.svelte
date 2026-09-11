<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { Plus, X } from '@lucide/svelte';
  import { tabTitle, type SourceTab } from '$lib/tabs';

  let { tabs, selected, onSelect, onOpen, onClose }: {
    tabs: SourceTab[]; selected: string;
    onSelect: (id: string) => void; onOpen: () => void; onClose: (id: string) => Promise<void>;
  } = $props();
  let strip: HTMLDivElement;
  function revealSelected() {
    strip?.querySelector('[aria-selected="true"]')?.parentElement?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }
  onMount(() => {
    const resize = new ResizeObserver(revealSelected);
    resize.observe(strip);
    return () => resize.disconnect();
  });
  async function focusSelected() {
    await tick();
    const button = strip.querySelector<HTMLButtonElement>('[role="tab"][aria-selected="true"]');
    button?.focus(); revealSelected();
  }
  async function close(id: string) {
    const restoreFocus = strip.contains(document.activeElement);
    await onClose(id);
    if (restoreFocus && (!document.activeElement || document.activeElement === document.body || strip.contains(document.activeElement))) await focusSelected();
  }
  function navigate(event: KeyboardEvent, id: string) {
    const index = tabs.findIndex(tab => tab.id === id);
    let next: number;
    switch (event.key) {
      case 'ArrowLeft': next = (index + tabs.length - 1) % tabs.length; break;
      case 'ArrowRight': next = (index + 1) % tabs.length; break;
      case 'Home': next = 0; break;
      case 'End': next = tabs.length - 1; break;
      case 'Delete': event.preventDefault(); void close(id); return;
      default: return;
    }
    event.preventDefault(); onSelect(tabs[next].id); void focusSelected();
  }
  $effect(() => {
    selected;
    void tick().then(revealSelected);
  });
</script>

<div class="flex min-w-0 flex-1 items-end gap-1">
  <div bind:this={strip} role="tablist" aria-label="Log sources" class="flex min-w-0 items-end overflow-x-auto overflow-y-hidden pt-1">
    {#each tabs as tab (tab.id)}
      <div role="presentation" class="group relative -mb-px flex max-w-64 shrink-0 items-center rounded-t-md border border-b-0 {selected === tab.id ? 'z-10 border-border bg-background text-foreground' : 'border-transparent bg-shell text-muted-foreground hover:bg-accent/50'}">
        <button type="button" role="tab" id={`source-tab-${tab.id}`} aria-controls={`source-panel-${tab.id}`}
          aria-selected={selected === tab.id} tabindex={selected === tab.id ? 0 : -1} title={tabTitle(tab)}
          onclick={() => onSelect(tab.id)} onkeydown={event => navigate(event, tab.id)}
          class="flex h-10 min-w-0 items-center gap-2 rounded-t-md px-3 text-xs outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
          {#if tab.id !== 'stdin'}<span aria-hidden="true" title={tab.pending ?? tab.info?.state ?? 'Not started'} class="size-1.5 shrink-0 rounded-full {tab.error || tab.info?.state === 'failed' ? 'bg-destructive' : tab.pending || ['starting', 'running', 'stopping'].includes(tab.info?.state ?? '') ? 'bg-primary' : 'bg-muted-foreground/50'}"></span>{/if}
          <span class="truncate font-mono">{tabTitle(tab)}</span>
        </button>
        {#if tab.id !== 'stdin'}
          <button type="button" aria-label={`Close ${tabTitle(tab)}`} title="Close tab and discard output" disabled={!!tab.pending}
            onclick={() => void close(tab.id)} class="mr-1 flex size-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-40">
            <X size={13} />
          </button>
        {/if}
      </div>
    {/each}
  </div>
  <button type="button" aria-label="New command tab" title="New command tab" onclick={onOpen}
    class="mb-1 flex size-8 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"><Plus size={17} /></button>
</div>
