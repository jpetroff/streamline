<script lang="ts">
  import { onMount } from 'svelte';
  import RawOutput from '$lib/components/RawOutput.svelte';
  import VirtualLogTable from '$lib/components/VirtualLogTable.svelte';
  import { ViewerController } from '$lib/transport/viewer-controller';
  import type { ViewerState } from '$lib/transport/viewer-state';

  const controller = new ViewerController();
  let viewer = $state<ViewerState>(controller.state);
  let source = $state('stdin');

  // Own the controller for exactly the lifetime of the root Svelte component.
  onMount(() => {
    const unsubscribe = controller.subscribe(next => { viewer = next; });
    void controller.start();
    return () => {
      unsubscribe();
      controller.dispose();
    };
  });
</script>

<svelte:head>
  <title>Streamline</title>
  <meta name="description" content="Local streaming log viewer" />
</svelte:head>

<div class="grid h-full grid-cols-[14rem_minmax(0,1fr)] grid-rows-[3rem_minmax(0,1fr)] bg-background text-foreground">
  <header class="col-span-2 flex items-center border-b bg-shell px-3" aria-label="Application toolbar">
    <label for="input-source" class="sr-only">Input source</label>
    <select
      id="input-source"
      bind:value={source}
      class="h-8 min-w-32 rounded-md border border-input bg-background px-2 text-sm text-foreground outline-none focus:ring-2 focus:ring-ring"
      aria-label="Input source"
    >
      <option value="stdin">stdin</option>
      <option value="file" disabled>file</option>
      <option value="command" disabled>command</option>
    </select>
  </header>
  <aside class="border-r bg-sidebar" aria-label="Sidebar"></aside>
  <main class="flex min-h-0 min-w-0 flex-col overflow-hidden" aria-label="Streamline">
    {#if viewer.error}
      <div class="shrink-0 border-b border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive" role="alert">
        {viewer.error.message}
      </div>
    {/if}
    {#if viewer.session?.error && viewer.session.error.code !== viewer.error?.code}
      <div class="shrink-0 border-b border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive" role="alert">
        {viewer.session.error.message}
      </div>
    {/if}

    <div class="min-h-0 flex-1">
      {#if !viewer.session}
        <div class="grid h-full place-items-center px-6 text-sm text-muted-foreground" role="status">Connecting…</div>
      {:else if viewer.session.inputKind === 'pending'}
        <div class="grid h-full place-items-center px-6 text-sm text-muted-foreground" role="status">Waiting for stdin…</div>
      {:else if viewer.session.inputKind === 'raw'}
        <RawOutput {viewer} {controller} />
      {:else}
        <VirtualLogTable {viewer} {controller} />
      {/if}
    </div>
  </main>
</div>
