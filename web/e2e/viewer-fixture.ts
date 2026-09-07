import { expect, type Page } from '@playwright/test';
import type { InputKind, QueryEvent, QueryState, Session, Snapshot } from '../src/lib/transport/types';

/** Deterministic HTTP/SSE boundary; production components and controller run unchanged. */
export async function mockViewer(page: Page, options: { kind?: InputKind; count?: number; connecting?: boolean } = {}) {
  let count = options.count ?? 1000;
  let revision = 1;
  const session: Session = { sessionId: 'test-session', generationId: '1', inputKind: options.kind ?? 'records', inputStatus: 'streaming' };
  const snapshots = new Map<string, Snapshot>();
  function state(): QueryState {
    const snapshot: Snapshot = {
      sessionId: session.sessionId, generationId: session.generationId, queryId: 'query-1',
      revision: String(revision), processedThrough: String(count), matchedCount: String(count), snapshotToken: `snapshot-${revision}`,
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
      const offset = Number(url.searchParams.get('offset'));
      const limit = Number(url.searchParams.get('limit'));
      const snapshot = snapshots.get(url.searchParams.get('snapshot')!)!;
      const length = Math.max(0, Math.min(limit, Number(snapshot.matchedCount) - offset));
      return route.fulfill({ json: { snapshot, offset: String(offset), rows: Array.from({ length }, (_, i) => ({
        id: String(offset + i + 1), timestamp: '2026-09-07T12:00:00Z', severity: 'info',
        message: `Log entry ${offset + i + 1}: ` + 'message text '.repeat(12), sourceFormat: 'json',
        fields: { extra1: 'one', extra2: 'two', extra3: 'three', extra4: 'four', extra5: 'final column' },
      })) } });
    }
    return route.fulfill({ json: state() });
  });
  await page.goto('/');
  return {
    releaseSession,
    async append(amount: number) {
      count += amount;
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
