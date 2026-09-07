import { expect, type Page } from '@playwright/test';
import type { APIErrorBody, InputKind, QuerySpec, QueryEvent, QueryState, Session, Snapshot } from '../src/lib/transport/types';

/** Deterministic HTTP/SSE boundary; production components and controller run unchanged. */
export async function mockViewer(page: Page, options: { kind?: InputKind; count?: number | bigint; connecting?: boolean; idStride?: number } = {}) {
  let count = BigInt(options.count ?? 1000);
  let queryNumber = 0;
  let queryId = 'query-1';
  let rejection: APIErrorBody | undefined;
  let nextCount: bigint | undefined;
  const specs: QuerySpec[] = [];
  const held: { start: bigint; end: bigint; wait: Promise<void>; requested: () => void }[] = [];
  let rowFailure: bigint | undefined;
  let revision = 1;
  const session: Session = { sessionId: 'test-session', generationId: '1', inputKind: options.kind ?? 'records', inputStatus: 'streaming' };
  const snapshots = new Map<string, Snapshot>();
  function state(): QueryState {
    const snapshot: Snapshot = {
      sessionId: session.sessionId, generationId: session.generationId, queryId,
      revision: String(revision), processedThrough: String(count), matchedCount: String(count), snapshotToken: `${queryId}-snapshot-${revision}`,
    };
    snapshots.set(snapshot.snapshotToken, snapshot);
    return { queryId: snapshot.queryId, status: 'ready', snapshot };
  }
  await page.addInitScript(() => {
    class FixtureEventSource extends EventTarget {
      constructor(_url: string) {
        super();
        window.addEventListener('fixture-query-event', this.receive);
      }
      receive = (event: Event) => {
        const detail = (event as CustomEvent).detail;
        this.dispatchEvent(new MessageEvent(detail.type, { data: JSON.stringify(detail) }));
      };
      close() { window.removeEventListener('fixture-query-event', this.receive); }
    }
    Object.defineProperty(window, 'EventSource', { value: FixtureEventSource });
  });
  let releaseSession!: () => void;
  const sessionReady = new Promise<void>(resolve => { releaseSession = resolve; });
  if (!options.connecting) releaseSession();
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url());
    if (url.pathname.endsWith('/session')) {
      await sessionReady;
      return route.fulfill({ json: session });
    }
    if (url.pathname.endsWith('/input/raw')) {
      return route.fulfill({ json: { generationId: '1', offset: '0', totalChunks: '1', chunks: ['Raw report\n' + 'long output '.repeat(150)] } });
    }
    if (route.request().method() === 'DELETE') return route.fulfill({ status: 204 });
    if (url.pathname.endsWith('/rows')) {
      const offset = BigInt(url.searchParams.get('offset')!);
      const hold = held.find(item => offset >= item.start && offset < item.end);
      if (hold) { hold.requested(); await hold.wait; }
      if (rowFailure === offset) return route.fulfill({ status: 500, json: { error: { code: 'fixture_failure', message: 'Could not load rows.' } } });
      const limit = Number(url.searchParams.get('limit'));
      const snapshot = snapshots.get(url.searchParams.get('snapshot')!)!;
      const length = Math.max(0, Number(BigInt(limit) < BigInt(snapshot.matchedCount) - offset ? BigInt(limit) : BigInt(snapshot.matchedCount) - offset));
      return route.fulfill({ json: { snapshot, offset: String(offset), rows: Array.from({ length }, (_, i) => ({
        id: String((offset + BigInt(i + 1)) * BigInt(options.idStride ?? 1)), timestamp: '2026-09-07T12:00:00Z', severity: 'info',
        message: `Log entry ${offset + BigInt(i + 1)}: ` + 'message text '.repeat(12), sourceFormat: 'json',
        fields: { extra1: 'one', extra2: 'two', extra3: 'three', extra4: 'four', extra5: 'final column' },
      })) } });
    }
    if (route.request().method() === 'POST') {
      specs.push(route.request().postDataJSON());
      if (rejection) { const error = rejection; rejection = undefined; return route.fulfill({ status: 400, json: { error } }); }
      queryId = `query-${++queryNumber}`;
      if (nextCount !== undefined) { count = nextCount; nextCount = undefined; }
    }
    return route.fulfill({ json: state() });
  });
  await page.goto('/');
  return {
    releaseSession,
    specs,
    rejectNextQuery(error: APIErrorBody) { rejection = error; },
    setNextQueryCount(value: bigint) { nextCount = value; },
    failRowsAt(offset?: bigint) { rowFailure = offset; },
    holdRows(start: bigint, end: bigint) {
      let release!: () => void;
      let requested!: () => void;
      const wait = new Promise<void>(resolve => { release = resolve; });
      const seen = new Promise<void>(resolve => { requested = resolve; });
      const hold = { start, end, wait, requested };
      held.push(hold);
      return { seen, release() { held.splice(held.indexOf(hold), 1); release(); } };
    },
    async append(amount: number) {
      count += BigInt(amount);
      revision++;
      const event: QueryEvent = { type: 'snapshot', state: state(), session };
      await page.evaluate(event => window.dispatchEvent(new CustomEvent('fixture-query-event', { detail: event })), event);
    },
  };
}

export const viewport = (page: Page) => page.getByRole('rowgroup', { name: 'Scrollable log records' });
export const toggle = (page: Page) => page.getByRole('button', { name: 'Toggle columns and filters' });
export const leftPanel = (page: Page) => page.locator('#columns-panel');
export const rightPanel = (page: Page) => page.locator('#row-details-panel');

export async function settleLayout(page: Page) {
  await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))));
}

export async function wideColumns(page: Page) {
  await expect(page.getByRole('table')).toHaveAttribute('aria-busy', 'false');
  await page.getByRole('textbox', { name: 'Column paths' }).fill('timestamp\nseverity\nmessage\nextra1\nextra2\nextra3\nextra4\nextra5');
  await page.getByRole('region', { name: 'Column configuration' }).getByRole('button', { name: 'Apply', exact: true }).first().click();
  await expect(page.getByRole('columnheader')).toHaveCount(8);
  await settleLayout(page);
}

export async function openDetails(page: Page) {
  const row = viewport(page).locator('[role="row"][aria-busy="false"]').last();
  await expect(row).toBeVisible();
  await row.click({ modifiers: ['Shift'], position: { x: 30, y: 10 } });
  await expect(rightPanel(page)).toBeVisible();
  await settleLayout(page);
}
