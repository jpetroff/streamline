<script lang="ts">
  import { onMount } from 'svelte';
  import { configureColumns, DEFAULT_COLUMNS, type DateDisplayFormat } from '$lib/columns';
  import ColumnSidebar from '$lib/components/ColumnSidebar.svelte';
  import RawOutput from '$lib/components/RawOutput.svelte';
  import SearchEditor from '$lib/components/SearchEditor.svelte';
  import RowDetailPanel from '$lib/components/RowDetailPanel.svelte';
  import TableToolbar from '$lib/components/TableToolbar.svelte';
  import VirtualLogTable from '$lib/components/VirtualLogTable.svelte';
  import { ViewerController } from '$lib/transport/viewer-controller';
  import type { LogRow } from '$lib/transport/types';
  import type { ViewerState } from '$lib/transport/viewer-state';

  interface SelectedRow {
    row: LogRow;
    queryId: string;
    generationId: string;
  }

  const controller = new ViewerController();
  let viewer = $state<ViewerState>(controller.state);
  let source = $state('stdin');
  let columns = $state(configureColumns(DEFAULT_COLUMNS));
  let columnPaths = $derived(columns.map(column => column.path));
  let selectedRow = $state<SelectedRow>();
  let rowLines = $state<1 | 2>(1);

  // Own the controller for exactly the lifetime of the root Svelte component.
  onMount(() => {
    const unsubscribe = controller.subscribe(next => { viewer = next; });
    void controller.start();
    return () => {
      unsubscribe();
      controller.dispose();
    };
  });

  // Details survive virtual page rotation, but never cross query or input generations.
  $effect(() => {
    const selection = selectedRow;
    const displayed = viewer.displayed;
    if (!selection) return;
    if (
      viewer.session?.inputKind !== 'records' ||
      !displayed ||
      displayed.queryId !== selection.queryId ||
      displayed.snapshot.generationId !== selection.generationId
    ) selectedRow = undefined;
  });

  function openDetails(row: LogRow) {
    const displayed = viewer.displayed;
    if (!displayed) return;
    selectedRow = { row, queryId: displayed.queryId, generationId: displayed.snapshot.generationId };
  }

  function applyColumns(paths: string[]) {
    columns = configureColumns(paths, columns);
  }

  function setDateFormat(index: number, dateFormat: DateDisplayFormat) {
    columns = columns.map((column, columnIndex) => (
      columnIndex === index ? { ...column, dateFormat } : column
    ));
  }
</script>

<svelte:head>
  <title>Streamline</title>
  <meta name="description" content="Local streaming log viewer" />
</svelte:head>

<div class="grid h-full grid-cols-[18rem_minmax(0,1fr)] grid-rows-[3rem_minmax(0,1fr)] bg-background text-foreground">
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
  <aside class="min-h-0 border-r bg-sidebar" aria-label="Sidebar">
    <ColumnSidebar {viewer} appliedColumns={columnPaths} onApply={applyColumns} onApplyFilters={filters => controller.setFilters(filters)} />
  </aside>
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

    <div class="min-h-0 min-w-0 flex-1">
      {#if !viewer.session}
        <div class="grid h-full place-items-center px-6 text-sm text-muted-foreground" role="status">Connecting…</div>
      {:else if viewer.session.inputKind === 'pending'}
        <div class="grid h-full place-items-center px-6 text-sm text-muted-foreground" role="status">Waiting for stdin…</div>
      {:else if viewer.session.inputKind === 'raw'}
        <RawOutput {viewer} {controller} />
      {:else}
        <div class="flex h-full min-h-0 min-w-0 overflow-hidden">
          <div class="min-h-0 min-w-0 flex-1">
            <VirtualLogTable
              {viewer}
              {controller}
              {columns}
              {rowLines}
              onDateFormatChange={setDateFormat}
              selectedRowId={selectedRow?.row.id}
              onOpenDetails={openDetails}
            />
          </div>
          {#if selectedRow}
            <RowDetailPanel row={selectedRow.row} {columns} onClose={() => { selectedRow = undefined; }} />
          {/if}
        </div>
      {/if}
    </div>
    {#if viewer.session?.inputKind === 'records'}
      <TableToolbar bind:rowLines />
      <SearchEditor
        applied={viewer.displayed?.search}
        pending={viewer.pending !== undefined}
        onApply={search => controller.setSearch(search)}
      />
    {/if}
  </main>
</div>
