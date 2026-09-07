<script lang="ts">
  import { tick } from 'svelte';
  import { registerOverlay } from '$lib/keyboard-context';
  import { Popover } from 'bits-ui';
  import type { JSONValue } from '$lib/transport/types';
  import type { ViewerState } from '$lib/transport/viewer-state';
  let { fields, viewer, context }: { fields?: Record<string, JSONValue>; viewer: ViewerState; context: string } = $props();
  let open = $state(false);
  registerOverlay({ open: () => open, modal: false, contains: () => false, close: async () => { open = false; await tick(); } });
</script>

<Popover.Root bind:open>
  <Popover.Trigger aria-label={`Sample log entry for ${context}`} class="inline-flex h-5 w-5 items-center justify-center rounded-full border text-xs text-muted-foreground hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">?</Popover.Trigger>
  <Popover.Portal>
    <Popover.Content role="dialog" side="right" align="start" sideOffset={8} class="z-50 w-[min(32rem,calc(100vw-2rem))] rounded-md border bg-popover p-3 text-popover-foreground shadow-lg outline-none" aria-label="Sample log entry">
      <div class="flex items-center justify-between gap-3">
        <h2 class="text-sm font-semibold">Sample log entry</h2>
        <Popover.Close class="rounded px-2 py-1 text-xs hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label="Close sample log entry">Close</Popover.Close>
      </div>
      <p class="mt-1 text-xs leading-5 text-muted-foreground">Original JSON fields. Use dot notation for nested objects.</p>
      {#if fields}
        <!-- svelte-ignore a11y_no_noninteractive_tabindex (Keyboard users must be able to scroll the sample.) -->
        <pre role="region" tabindex="0" class="mt-2 max-h-[min(28rem,60vh)] overflow-auto rounded border bg-background p-2 font-mono text-xs leading-5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" aria-label="Sample JSON log entry">{JSON.stringify(fields, null, 2)}</pre>
      {:else}
        <p class="mt-2 text-xs leading-5 text-muted-foreground" role="status">
          {#if viewer.session?.inputKind === 'raw'}No structured JSON fields are available.
          {:else if viewer.session?.inputStatus === 'eof' && viewer.session?.inputKind === 'records'}No structured fields were found in the loaded rows.
          {:else}Waiting for a structured log entry…{/if}
        </p>
      {/if}
    </Popover.Content>
  </Popover.Portal>
</Popover.Root>
