<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import type { Configuration } from '$lib/configurations';
  import { registerCommand } from '$lib/keyboard-context';
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

  import { HTTPQueryAPI } from '$lib/transport/api';
  import type { LogSource, QuerySpec } from '$lib/transport/types';
  import type { SourcePreferences } from '$lib/transport/sources';

  let { sourceId, info, preferences, onSave }: {
    sourceId: string; info?: LogSource; preferences?: SourcePreferences;
    onSave: (preferences: SourcePreferences) => void;
  } = $props();
  const initial = untrack(() => preferences);
  const controller = new ViewerController(new HTTPQueryAPI(untrack(() => sourceId === 'stdin' ? '/api/v1' : `/api/v1/sources/${encodeURIComponent(sourceId)}`)));
  let activeRow = $state.raw<ActiveRow>();
  let navigator = $state<RowNavigator>();
  let previewOpen = $state(false);
  let viewer = $state<ViewerState>(controller.state);
  let columns = $state(initial?.columns ?? configureColumns(untrack(() => sourceId === 'stdin' ? DEFAULT_COLUMNS : ['timestamp', 'severity', 'message'])));
  let columnPaths = $derived(columns.map(column => column.path));

  let retainedSpec = $state<QuerySpec>(initial?.spec ?? { filter: [], sort: 'input' });
  let editorRevision = $state(0);
  let configurationIntent = 0;
  let inheritOnRun = $state(initial?.inheritOnRun ?? false);

  export function snapshot(): SourcePreferences {
    return { spec: { filter: retainedSpec.filter.map(filter => ({ ...filter })), sort: retainedSpec.sort, search: retainedSpec.search ? { ...retainedSpec.search } : undefined },
      columns: columns.map(column => ({ ...column })), following: viewer.following, offset: activeRow?.offset, rowLines, inheritOnRun };
  }

  export async function applyConfiguration(doc: Configuration) {
    if (!viewer.session) throw new Error('Wait for the source to connect, then load again.');
    const request = ++configurationIntent;
    const spec: QuerySpec = { filter: doc.filters.filter.map(filter => ({ ...filter })), search: { ...doc.filters.search }, sort: 'input' };
    if (viewer.session.inputKind !== 'raw') {
      const error = await controller.setQueryAndWait(spec);
      if (error) throw new Error(error.message);
    }
    if (!mounted || request !== configurationIntent) throw new Error('The active tab changed.');
    retainedSpec = spec;
    inheritOnRun = true;
    columns = doc.columns.map(column => ({ ...column }));
    editorRevision++;
  }

  let rowLines = $state<1 | 2>(initial?.rowLines ?? 1);
  let columnsOpen = $state(COLUMNS_PANEL.initiallyOpen);
  let columnsPanelWidth = $state<number>();
  let detailsPanelWidth = $state<number>();

  // Own the controller for exactly the lifetime of the root Svelte component.
  onMount(() => {
    const unsubscribe = controller.subscribe(next => {
      viewer = next;
      if (next.displayed) retainedSpec = { filter: next.displayed.filter, sort: next.displayed.sort, search: next.displayed.search };
    });
    void controller.start(initial?.spec);
    return () => {
      onSave(snapshot());
      unsubscribe();
      controller.dispose();
    };
  });

  let mounted = true;
  onMount(() => () => { mounted = false; });
  let restore = initial?.following === false;
  $effect(() => {
    if (!restore || !viewer.displayed || !navigator) return;
    restore = false;
    const navigation = navigator;
    controller.pause();
    void tick().then(async () => {
      if (!mounted) return;
      if (initial?.offset !== undefined) await navigation.navigate(initial.offset);
      controller.pause();
    });
  });

  const keyboard = registerCommand({ id: 'columns.show', label: 'Show columns', handler: async () => { columnsOpen = true; await tick(); } });
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
    inheritOnRun = true;
    columns = configureColumns(paths, columns);
  }

  function setDateFormat(index: number, dateFormat: DateDisplayFormat) {
    inheritOnRun = true;
    columns = columns.map((column, columnIndex) => (
      columnIndex === index ? { ...column, dateFormat } : column
    ));
  }
</script>

  <div class="flex min-h-0 min-w-0 flex-1 overflow-hidden">
    <SidePanel definition={COLUMNS_PANEL} open={columnsOpen} bind:preferredWidth={columnsPanelWidth}>
      {#key editorRevision}
      <ColumnSidebar initialFilter={retainedSpec.filter} {viewer} appliedColumns={columnPaths} onApply={applyColumns} onApplyFilters={filters => { inheritOnRun = true; return controller.setFilters(filters); }} />
      {/key}
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
          <div class="grid h-full place-items-center px-6 text-sm text-muted-foreground" role="status">{sourceId === 'stdin' ? 'Waiting for stdin…' : info?.state === 'stopped' ? 'Command stopped without output.' : info?.state === 'failed' ? 'Command failed without output.' : 'Waiting for command output…'}</div>
        {:else if viewer.session.inputKind === 'raw'}
          <RawOutput {viewer} {controller} label={sourceId === 'stdin' ? 'stdin' : 'command'} stopped={info?.state === 'stopped'} />
        {:else if sourceId !== 'stdin' && viewer.session.inputStatus !== 'streaming' && viewer.displayed?.snapshot.processedThrough === '0'}
          <div class="grid h-full place-items-center px-6 text-sm text-muted-foreground" role="status">{info?.state === 'stopped' ? 'Command stopped without output.' : info?.state === 'failed' ? 'Command failed without output.' : 'Command completed without output.'}</div>
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
        {#key editorRevision}
        <SearchEditor
          applied={retainedSpec.search}
          pending={viewer.pending !== undefined}
          onApply={search => { inheritOnRun = true; return controller.setSearch(search); }}
        />
        {/key}
      {/if}
    </main>
    <SidePanel definition={ROW_DETAILS_PANEL} open={previewOpen} bind:preferredWidth={detailsPanelWidth}>
      {#if previewOpen}
        <RowDetailPanel row={activeRow?.row} error={viewer.error?.message} {columns} onClose={() => { void keyboard.execute('preview.close'); }} />

      {/if}
    </SidePanel>
  </div>
