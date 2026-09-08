import { test, expect, type Page } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { once } from 'node:events';
import { createServer } from 'node:net';

// Run make build before this suite. Each test owns a standalone binary so real
// shell execution, HTTP/SSE, and embedded frontend behavior are tested together.
let child: ChildProcess;
let apiURL: string;
test.beforeEach(async () => {
  const server = createServer(); server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const port = (server.address() as { port: number }).port;
  await new Promise<void>(resolve => server.close(() => resolve()));
  child = spawn('../bin/streamline', ['-port', String(port)], { stdio: ['pipe', 'ignore', 'pipe'] });
  apiURL = `http://127.0.0.1:${port}`;
  await expect.poll(async () => fetch(`${apiURL}/api/v1/health`).then(r => r.status).catch(() => 0)).toBe(200);

});
test.afterEach(async () => {
  if (child.exitCode === null) {
    const exited = once(child, 'exit');
    child.kill('SIGTERM');
    const timeout = setTimeout(() => child.kill('SIGKILL'), 8000);
    const [code] = await exited; clearTimeout(timeout);
    expect(code, 'server must shut down gracefully with stdin still open').toBe(0);
  }
});

async function run(page: Page, command: string, mode = 'text') {
  await page.getByLabel('Input source', { exact: true }).selectOption('command');
  await page.getByLabel('Command', { exact: true }).fill(command);
  await page.getByLabel('Output mode').selectOption(mode);
  await page.getByRole('button', { name: 'Run', exact: true }).click();
}

test('concurrent commands retain isolated output, reruns, and stopped tabs across reload', async ({ page }) => {
  await page.goto(apiURL);
  await run(page, "printf 'first log\\n'; sleep 30");
  await expect(page.getByRole('cell', { name: 'first log', exact: true })).toBeVisible();
  await run(page, "printf 'second log\\n'");
  await expect(page.getByRole('cell', { name: 'second log', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: 'first log', exact: true })).toHaveCount(0);
  await page.getByRole('navigation', { name: 'Log sources' }).getByRole('button', { name: /first log/ }).click();
  await expect(page.getByRole('cell', { name: 'first log', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Stop', exact: true }).click();
  await expect(page.getByLabel('Command controls').getByRole('status')).toContainText('stopped');
  await expect(page.getByRole('cell', { name: 'first log', exact: true })).toBeVisible();
  await page.getByRole('navigation', { name: 'Log sources' }).getByRole('button', { name: /second log/ }).click();
  await page.getByRole('button', { name: 'Run again' }).click();
  await expect(page.getByRole('navigation', { name: 'Log sources' }).getByRole('button', { name: /second log/ })).toHaveCount(2);
  await page.reload();
  await expect(page.getByRole('navigation', { name: 'Log sources' }).getByRole('button')).toHaveCount(4);
  await page.getByRole('navigation', { name: 'Log sources' }).getByRole('button', { name: /first log/ }).click();
  await page.getByRole('button', { name: 'Close and discard' }).click();
  await expect(page.getByRole('navigation', { name: 'Log sources' }).getByRole('button')).toHaveCount(3);
});

test('auto raw failures and empty commands expose completion', async ({ page }) => {
  await page.goto(apiURL);
  await run(page, "printf 'failure details'; exit 7", 'auto');
  await expect(page.getByLabel('Raw command output')).toContainText('failure details');
  await expect(page.getByRole('alert')).toContainText('exit status 7');
  await run(page, 'true');
  await expect(page.getByText('Command completed without output.', { exact: true })).toBeVisible();
});

test('source switching restores search, columns, and paused row position', async ({ page }) => {
  await page.goto(apiURL);
  await run(page, "i=0; while [ $i -lt 100 ]; do printf 'record %s\\n' $i; i=$((i+1)); done");
  await expect(page.getByRole('cell', { name: 'record 99', exact: true })).toBeVisible();
  await page.getByLabel('Column paths', { exact: true }).fill('message');
  await page.getByLabel('Column paths', { exact: true }).press('Control+Enter');
  await expect(page.getByRole('columnheader')).toHaveCount(1);
  await page.getByLabel('Search', { exact: true }).fill('record');
  await page.getByRole('region', { name: 'General search' }).getByRole('button', { name: 'Apply', exact: true }).click();
  await page.getByLabel('Jump to row', { exact: true }).fill('20');
  await page.getByRole('button', { name: 'Go', exact: true }).click();
  await expect(page.locator('[data-offset="19"][aria-current="true"]')).toBeVisible();
  await run(page, "printf 'other source\\n'");
  await expect(page.getByRole('cell', { name: 'other source', exact: true })).toBeVisible();
  await page.getByRole('navigation', { name: 'Log sources' }).getByRole('button', { name: /while/ }).click();
  await expect(page.getByLabel('Search', { exact: true })).toHaveValue('record');
  await expect(page.getByLabel('Column paths', { exact: true })).toHaveValue('message');
  await expect(page.getByRole('columnheader')).toHaveCount(1);
  await expect(page.locator('[data-offset="19"][aria-current="true"]')).toBeVisible();
});

test('deletion from another client returns to stdin and stale responses cannot replace a new source', async ({ page, request }) => {
  await page.goto(apiURL);
  await run(page, "printf 'old output\\n'");
  await expect(page.getByRole('cell', { name: 'old output', exact: true })).toBeVisible();
  const sources = await request.get(`${apiURL}/api/v1/sources`).then(response => response.json());
  const id = sources.find((source: { kind: string }) => source.kind === 'command').id;
  // Hold a replacement query response while switching to a different source.
  let release!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  let observed!: () => void;
  const received = new Promise<void>(resolve => { observed = resolve; });
  await page.route(`**/sources/${id}/queries`, async route => {
    if (route.request().method() !== 'POST') return route.continue();
    const response = await route.fetch(); observed(); await held;
    await route.fulfill({ response }).catch(() => {});
  });
  await page.getByLabel('Search', { exact: true }).fill('old');
  await page.getByLabel('Search', { exact: true }).press('Control+Enter');
  await received;
  await run(page, "printf 'new output\\n'");
  await expect(page.getByRole('cell', { name: 'new output', exact: true })).toBeVisible();
  release();
  await expect(page.getByRole('cell', { name: 'old output', exact: true })).toHaveCount(0);
  const current = await request.get(`${apiURL}/api/v1/sources`).then(response => response.json());
  const selected = current.find((source: { command?: string }) => source.command?.includes('new output')).id;
  const deleted = await request.delete(`${apiURL}/api/v1/sources/${selected}`, { headers: { 'Content-Type': 'application/json', 'X-Streamline-Request': '1' }, data: {} });
  expect(deleted.status()).toBe(204);
  await expect(page.getByRole('navigation', { name: 'Log sources' }).getByRole('button', { name: 'stdin', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await expect(page.getByText('Waiting for stdin…', { exact: true })).toBeVisible();
});

test('applied field filters survive switching command sources', async ({ page }) => {
  await page.goto(apiURL);
  await run(page, `printf '{"message":"keep","level":"info"}\\n{"message":"drop","level":"error"}\\n'`, 'auto');
  await expect(page.getByRole('cell', { name: 'drop', exact: true })).toBeVisible();
  await page.keyboard.press('Control+Alt+f');
  await page.getByRole('textbox', { name: 'Field for filter 1' }).fill('level');
  await page.getByRole('textbox', { name: 'Value for filter 1' }).fill('info');
  await page.getByRole('region', { name: 'Filters', exact: true }).getByRole('button', { name: 'Apply', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'drop', exact: true })).toHaveCount(0);
  await run(page, "printf 'another source\\n'");
  await expect(page.getByRole('cell', { name: 'another source', exact: true })).toBeVisible();
  await page.getByRole('navigation', { name: 'Log sources' }).getByRole('button', { name: /keep/ }).click();
  await expect(page.getByRole('textbox', { name: 'Field for filter 1' })).toHaveValue('level');
  await expect(page.getByRole('textbox', { name: 'Value for filter 1' })).toHaveValue('info');
  await expect(page.getByRole('cell', { name: 'keep', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: 'drop', exact: true })).toHaveCount(0);
});
