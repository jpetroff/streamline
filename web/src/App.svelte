<script lang="ts">
  import { onMount } from 'svelte';
  import VirtualLogTable from '$lib/components/VirtualLogTable.svelte';
  import { ViewerController } from '$lib/transport/viewer-controller';
  import type { ViewerState } from '$lib/transport/viewer-state';

  const controller = new ViewerController();
  let viewer = $state<ViewerState>(controller.state);

  // Own the controller for exactly the lifetime of the root Svelte component.
  onMount(() => {
    const unsubscribe = controller.subscribe(next => { viewer = next; });
    void controller.setQuery('');
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
  <header class="col-span-2 border-b bg-shell" aria-label="Application toolbar"></header>
  <aside class="border-r bg-sidebar" aria-label="Sidebar"></aside>
  <main class="min-h-0 min-w-0 overflow-hidden" aria-label="Streamline">
    <VirtualLogTable {viewer} {controller} />
  </main>
</div>
