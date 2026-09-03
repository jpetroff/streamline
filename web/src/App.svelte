<script lang="ts">
  import { onMount } from 'svelte';
  import { ViewerController } from '$lib/transport/viewer-controller';
  import type { ViewerState } from '$lib/transport/viewer-state';

  const controller = new ViewerController();
  let viewer = $state<ViewerState>(controller.state);
  let filter = $state('');
  let submitting = $state(false);

  onMount(() => {
    const unsubscribe = controller.subscribe(next => { viewer = next; });
    void controller.setQuery('');
    return () => { unsubscribe(); controller.dispose(); };
  });

  /** Applies the form value as a new atomic query replacement. */
  async function applyFilter(event: SubmitEvent) {
    event.preventDefault();
    submitting = true;
    await controller.setQuery(filter);
    submitting = false;
  }

  /** Loads the preceding result window and leaves the view paused. */
  function previous() {
    const displayed = viewer.displayed;
    if (!displayed) return;
    const offset = displayed.offset > 200n ? displayed.offset - 200n : 0n;
    void controller.loadWindow(offset);
  }

  /** Loads the next available result window without exceeding the snapshot count. */
  function next() {
    const displayed = viewer.displayed;
    if (!displayed) return;
    const count = BigInt(displayed.snapshot.matchedCount);
    const offset = displayed.offset + BigInt(displayed.rows.length);
    if (offset < count) void controller.loadWindow(offset);
  }
</script>

<svelte:head><title>Streamline</title></svelte:head>

<main aria-label="Streamline" class="mx-auto flex min-h-screen max-w-6xl flex-col gap-4 p-6">
  <header class="flex flex-wrap items-center justify-between gap-3">
    <div>
      <h1 class="text-xl font-semibold">Streamline</h1>
      <p class="text-sm text-muted-foreground">Local streaming log viewer{viewer.session ? ` · ${viewer.session.inputStatus}` : ''}</p>
    </div>
    <button class="rounded-md border px-3 py-2 text-sm" onclick={() => viewer.following ? controller.pause() : void controller.resume()}>
      {viewer.following ? 'Pause view' : 'Resume follow'}
    </button>
  </header>

  <form class="flex gap-2" onsubmit={applyFilter}>
    <input class="min-w-0 flex-1 rounded-md border bg-background px-3 py-2" bind:value={filter} placeholder="Filter expression" aria-label="Filter expression" />
    <button class="rounded-md bg-primary px-4 py-2 text-primary-foreground disabled:opacity-50" disabled={submitting}>Apply</button>
  </form>

  {#if viewer.pending}
    <div class="rounded-md border bg-muted px-3 py-2 text-sm" role="status">
      Applying filter… {viewer.pending.progress === undefined ? '' : `${Math.round(viewer.pending.progress * 100)}%`}
    </div>
  {/if}
  {#if viewer.error}
    <div class="rounded-md border border-destructive/50 px-3 py-2 text-sm text-destructive" role="alert">{viewer.error.message}</div>
  {/if}
  {#if viewer.needsRefresh}
    <button class="self-start rounded-md border px-3 py-2 text-sm" onclick={() => void controller.setQuery(viewer.displayed?.filter ?? '')}>Refresh query</button>
  {/if}

  <section class="min-h-0 flex-1 overflow-auto rounded-md border" aria-label="Log records">
    {#if viewer.displayed?.rows.length}
      {#each viewer.displayed.rows as row (row.id)}
        <article class="grid grid-cols-[8rem_6rem_1fr] gap-3 border-b px-3 py-2 font-mono text-xs">
          <time class="truncate text-muted-foreground">{row.timestamp ?? ''}</time>
          <span class="truncate">{row.severity ?? ''}</span>
          <span class="whitespace-pre-wrap break-words">{row.message}</span>
        </article>
      {/each}
    {:else if viewer.displayed}
      <p class="p-8 text-center text-sm text-muted-foreground">No matching records.</p>
    {:else}
      <p class="p-8 text-center text-sm text-muted-foreground">Connecting…</p>
    {/if}
  </section>

  {#if viewer.displayed}
    <footer class="flex items-center justify-between text-sm text-muted-foreground">
      <span>{viewer.displayed.snapshot.matchedCount} matching records · revision {viewer.displayed.snapshot.revision}</span>
      <div class="flex gap-2">
        <button class="rounded-md border px-3 py-1.5 text-foreground" onclick={previous} disabled={viewer.displayed.offset === 0n}>Previous</button>
        <button class="rounded-md border px-3 py-1.5 text-foreground" onclick={next} disabled={viewer.displayed.offset + BigInt(viewer.displayed.rows.length) >= BigInt(viewer.displayed.snapshot.matchedCount)}>Next</button>
      </div>
    </footer>
  {/if}
</main>
