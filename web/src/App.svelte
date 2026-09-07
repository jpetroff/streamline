<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { provideKeyboard, registerCommand } from '$lib/keyboard-context';
  import type { ActiveRow, RowNavigator } from '$lib/row-navigation';
  import { configureColumns, DEFAULT_COLUMNS, type DateDisplayFormat } from '$lib/columns';
  import ColumnSidebar from '$lib/components/ColumnSidebar.svelte';
  import RawOutput from '$lib/components/RawOutput.svelte';
  import SearchEditor from '$lib/components/SearchEditor.svelte';
  import RowDetailPanel from '$lib/components/RowDetailPanel.svelte';
  import TableToolbar from '$lib/components/TableToolbar.svelte';
  import VirtualLogTable from '$lib/components/VirtualLogTable.svelte';
  import SidePanel from '$lib/components/SidePanel.svelte';
  import { COLUMNS_PANEL, ROW_DETAILS_PANEL } from '$lib/side-panels';
  import { ViewerController } from '$lib/transport/viewer-controller';
  import type { ViewerState } from '$lib/transport/viewer-state';

  const keyboard = provideKeyboard();
  let activeRow = $state.raw<ActiveRow>();
  let navigator = $state<RowNavigator>();
  let previewOpen = $state(false);
  const controller = new ViewerController();
  let viewer = $state<ViewerState>(controller.state);
  let source = $state('stdin');
  let columns = $state(configureColumns(DEFAULT_COLUMNS));
  let columnPaths = $derived(columns.map(column => column.path));

  let rowLines = $state<1 | 2>(1);
  let columnsOpen = $state(COLUMNS_PANEL.initiallyOpen);
  let columnsPanelWidth = $state<number>();
  let detailsPanelWidth = $state<number>();

  // Own the controller for exactly the lifetime of the root Svelte component.
  onMount(() => {
    const unsubscribe = controller.subscribe(next => { viewer = next; });
    void controller.start();
    return () => {
      unsubscribe();
      controller.dispose();
    };
  });

  onMount(() => keyboard.attach(window));
  registerCommand({ id: 'columns.show', label: 'Show columns', handler: async () => { columnsOpen = true; await tick(); } });
  registerCommand({ id: 'preview.close', label: 'Close preview', bindings: ['Escape'], allowInInput: true,
    priority: 10, when: () => previewOpen, handler: () => {
      const ownedFocus = document.activeElement?.closest('#row-details-panel');
      previewOpen = false;
      if (ownedFocus) void navigator?.focus();
    } });
  registerCommand({ id: 'rows.focus', label: 'Return to active row', bindings: ['Escape'], allowInInput: true,
    when: () => !!activeRow && !previewOpen, changesFocus: true, handler: async () => { await navigator?.focus(); } });

  $effect(() => {
    const displayed = viewer.displayed;
    if (viewer.session?.inputKind !== 'records' || !displayed) activeRow = undefined;
    if (!displayed || activeRow?.queryId !== displayed.queryId || activeRow?.generationId !== displayed.snapshot.generationId) previewOpen = false;
  });

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

<div class="flex h-full min-w-0 flex-col bg-background text-foreground">
  <header class="flex h-12 shrink-0 items-center border-b bg-shell px-3" aria-label="Application toolbar">
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
  <div class="flex min-h-0 min-w-0 flex-1 overflow-hidden">
    <SidePanel definition={COLUMNS_PANEL} open={columnsOpen} bind:preferredWidth={columnsPanelWidth}>
      <ColumnSidebar {viewer} appliedColumns={columnPaths} onApply={applyColumns} onApplyFilters={filters => controller.setFilters(filters)} />
    </SidePanel>
    <main class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden" aria-label="Streamline">
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
          <VirtualLogTable
            {viewer}
            {controller}
            {columns}
            {rowLines}
            onDateFormatChange={setDateFormat}
            bind:activeRow
            bind:navigator
            onReset={() => { previewOpen = false; }}
            onOpenDetails={() => { previewOpen = true; }}
          />
        {/if}
      </div>
      <TableToolbar bind:rowLines bind:columnsOpen showRowControls={viewer.session?.inputKind === 'records'} total={BigInt(viewer.displayed?.snapshot.matchedCount ?? 0)} {activeRow} {navigator} />
      {#if viewer.session?.inputKind === 'records'}
        <SearchEditor
          applied={viewer.displayed?.search}
          pending={viewer.pending !== undefined}
          onApply={search => controller.setSearch(search)}
        />
      {/if}
    </main>
    <SidePanel definition={ROW_DETAILS_PANEL} open={previewOpen} bind:preferredWidth={detailsPanelWidth}>
      {#if previewOpen}
        <RowDetailPanel row={activeRow?.row} error={viewer.error?.message} {columns} onClose={() => { void keyboard.execute('preview.close'); }} />

      {/if}
    </SidePanel>
  </div>
</div>
