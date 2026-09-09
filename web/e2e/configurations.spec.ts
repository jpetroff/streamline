import { test, expect, type Page } from '@playwright/test';
import { spawn, type ChildProcess } from 'node:child_process';
import { once } from 'node:events';
import { createServer } from 'node:net';
import { mkdtemp, mkdir, readFile, writeFile, readdir, copyFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { Configuration } from '../src/lib/configurations';

let child: ChildProcess;
let apiURL: string;
let directory: string;
const command = `printf '%s\\n' '{"timestamp":"2026-09-09T12:00:00Z","level":"error","msg":"timeout"}' '{"timestamp":"2026-09-09T12:01:00Z","level":"info","msg":"healthy"}'`;
const bundle = (): Configuration => ({ version: 1, name: 'Errors', command, mode: 'auto', columns: [{ path: 'timestamp', dateFormat: 'iso' }, { path: 'message', dateFormat: 'original' }], filters: { filter: [{ field: 'level', op: 'eq', value: 'error' }], search: { text: 'timeout', mode: 'plain', operator: 'or' } } });
async function start() {
  const server = createServer(); server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const port = (server.address() as { port: number }).port;
  await new Promise<void>(resolve => server.close(() => resolve()));
  child = spawn('../bin/streamline', ['-port', String(port), '-config-dir', directory], { stdio: ['pipe', 'ignore', 'pipe'] });
  apiURL = `http://127.0.0.1:${port}`;
  await expect.poll(() => fetch(`${apiURL}/api/v1/health`).then(r => r.status).catch(() => 0)).toBe(200);
}
async function stop() {
  if (child?.exitCode === null) {
    const exited = once(child, 'exit'); child.kill('SIGTERM');
    const timeout = setTimeout(() => child.kill('SIGKILL'), 8000);
    await exited; clearTimeout(timeout);
  }
}
test.beforeEach(async () => { directory = await mkdtemp(join(tmpdir(), 'streamline-configurations-')); await start(); });
test.afterEach(async () => { await stop(); await rm(directory, { recursive: true, force: true }); });
async function settings(page: Page) {
  await page.getByRole('button', { name: 'Saved configurations', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Saved configurations', exact: true });
  await expect(dialog).toBeVisible(); return dialog;
}
async function run(page: Page, text = command, mode = 'auto') {
  await page.getByLabel('Input source', { exact: true }).selectOption('command');
  await page.getByLabel('Command', { exact: true }).fill(text);
  await page.getByLabel('Output mode').selectOption(mode);
  await page.getByRole('button', { name: 'Run', exact: true }).click();
}
async function putFile(doc = bundle()) {
  await mkdir(join(directory, 'configs'), { recursive: true });
  await writeFile(join(directory, 'configs', 'imported.json'), JSON.stringify(doc));
}

test('captures applied active settings, edits and clones, persists through restart, then loads and runs', async ({ page }) => {
  await page.goto(apiURL); await run(page);
  await expect(page.getByRole('cell', { name: 'healthy', exact: true })).toBeVisible();
  await page.getByLabel('Column paths', { exact: true }).fill('timestamp\nmessage');
  await page.getByLabel('Column paths', { exact: true }).press('Control+Enter');
  await page.getByLabel('Date display for timestamp').selectOption('iso');
  await page.getByRole('button', { name: 'Import JSON', exact: true }).click();
  await page.getByLabel('Filter JSON', { exact: true }).fill(JSON.stringify(bundle().filters.filter));
  await page.getByRole('button', { name: 'Import', exact: true }).click();
  await page.getByRole('region', { name: 'Filters', exact: true }).getByRole('button', { name: 'Apply', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'healthy', exact: true })).toHaveCount(0);
  await page.getByLabel('Search', { exact: true }).fill('timeout');
  await page.getByLabel('Search', { exact: true }).press('Control+Enter');
  await expect(page.getByRole('region', { name: 'General search' }).getByRole('button', { name: 'Apply', exact: true })).toBeDisabled();
  // Unapplied drafts and an unrun command must not leak into the captured bundle.
  await page.getByLabel('Column paths', { exact: true }).fill('unapplied');
  await page.getByLabel('Search', { exact: true }).fill('unapplied');
  await page.getByLabel('Command', { exact: true }).fill('unrun command');
  let dialog = await settings(page);
  await dialog.getByRole('button', { name: 'Save current as new' }).click();
  await expect(dialog.locator('textarea')).toHaveCount(3);
  await expect(dialog.getByLabel('Command', { exact: true })).toHaveValue(command);
  expect(JSON.parse(await dialog.getByLabel('Column setup (JSON)').inputValue())).toEqual(bundle().columns);
  expect(JSON.parse(await dialog.getByLabel('Filters and search (JSON)').inputValue())).toEqual(bundle().filters);
  await dialog.getByLabel('Name', { exact: true }).fill('Errors');
  await dialog.getByLabel('Name', { exact: true }).press('Control+Enter');
  await expect(dialog.getByRole('status')).toHaveText('Configuration saved.');
  await dialog.getByRole('article', { name: 'Errors', exact: true }).getByRole('button', { name: 'Clone and edit' }).click();
  await expect(dialog.getByLabel('Name', { exact: true })).toHaveValue('Errors copy');
  await dialog.getByLabel('Command', { exact: true }).fill('printf copy');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog.getByRole('article')).toHaveCount(2);
  await dialog.getByRole('article', { name: 'Errors', exact: true }).getByRole('button', { name: 'Edit', exact: true }).click();
  await dialog.getByLabel('Name', { exact: true }).fill('Saved errors');
  await dialog.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(dialog.getByRole('article', { name: 'Saved errors', exact: true })).toBeVisible();
  await page.reload();
  dialog = await settings(page);
  await expect(dialog.getByRole('article')).toHaveCount(2);
  const files = await readdir(join(directory, 'configs')); expect(files).toHaveLength(2);
  const docs = await Promise.all(files.map(file => readFile(join(directory, 'configs', file), 'utf8').then(JSON.parse)));
  expect(docs.find(doc => doc.name === 'Saved errors').command).toBe(command);
  await stop(); await start(); await page.goto(apiURL);
  dialog = await settings(page);
  await dialog.getByRole('article', { name: 'Saved errors', exact: true }).getByRole('button', { name: 'Load', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('navigation', { name: 'Log sources' }).getByRole('button')).toHaveCount(1);
  await expect(page.getByLabel('Command', { exact: true })).toHaveValue(command);
  await expect(page.getByLabel('Column paths', { exact: true })).toHaveValue('timestamp\nmessage');
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'timeout', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: 'healthy', exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Date display for timestamp')).toHaveValue('iso');
  await expect(page.getByLabel('Search', { exact: true })).toHaveValue('timeout');
});

test('loading resets equal-valued editor drafts and retains current results on query failure', async ({ page }) => {
  await putFile(); await page.goto(apiURL); await run(page);
  await expect(page.getByRole('cell', { name: 'healthy', exact: true })).toBeVisible();
  let dialog = await settings(page);
  await dialog.getByRole('article', { name: 'Errors' }).getByRole('button', { name: 'Load', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await page.getByLabel('Column paths', { exact: true }).fill('draft');
  await page.getByLabel('Search', { exact: true }).fill('draft');
  dialog = await settings(page);
  await dialog.getByRole('article', { name: 'Errors' }).getByRole('button', { name: 'Load', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByLabel('Column paths', { exact: true })).toHaveValue('timestamp\nmessage');
  await expect(page.getByLabel('Search', { exact: true })).toHaveValue('timeout');
  const changed = bundle(); changed.columns = [{ path: 'level', dateFormat: 'original' }]; changed.command = 'unrun'; changed.filters.search.text = 'healthy'; await putFile(changed);
  await page.route('**/sources/*/queries', route => route.request().method() === 'POST' ? route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: { code: 'test_failure', message: 'Query replacement failed' } }) }) : route.continue());
  dialog = await settings(page);
  await dialog.getByRole('article', { name: 'Errors' }).getByRole('button', { name: 'Load', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('Query replacement failed');
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'timeout', exact: true })).toBeVisible();
  await expect(page.getByLabel('Column paths', { exact: true })).toHaveValue('timestamp\nmessage');
  await expect(page.getByLabel('Command', { exact: true })).toHaveValue(command);
});

test('validates regex on server, protects unsaved edits, and owns keyboard focus', async ({ page }) => {
  await putFile(); await page.goto(apiURL);
  const dialog = await settings(page);
  await dialog.getByRole('article', { name: 'Errors' }).getByRole('button', { name: 'Edit', exact: true }).click();
  const bad = bundle().filters; bad.search = { text: '(?=lookahead)', mode: 'regexp', operator: 'or' };
  await dialog.getByLabel('Filters and search (JSON)').fill(JSON.stringify(bad));
  await dialog.getByLabel('Filters and search (JSON)').press('Control+Enter');
  await expect(dialog.getByText(/Search line 1:/)).toBeVisible();
  await dialog.getByLabel('Filters and search (JSON)').fill('{');
  await expect(dialog.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  await page.keyboard.press('Escape');
  await expect(dialog.getByText('Save your changes or discard them before continuing.')).toBeVisible();
  await dialog.getByRole('button', { name: 'Keep editing' }).click();
  await page.keyboard.press('Control+b');
  expect(await page.evaluate(() => !!document.activeElement?.closest('[role="dialog"]'))).toBe(true);
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await dialog.getByRole('button', { name: 'Discard changes' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Saved configurations', exact: true })).toBeFocused();
  expect(JSON.parse(await readFile(join(directory, 'configs', 'imported.json'), 'utf8')).filters.search.mode).toBe('plain');
});

test('copies text files between directories and retains loaded settings through raw output and stdin', async ({ page }) => {
  const external = await mkdtemp(join(tmpdir(), 'streamline-transfer-'));
  try {
    await writeFile(join(external, 'portable.json'), JSON.stringify(bundle()));
    await mkdir(join(directory, 'configs'));
    await copyFile(join(external, 'portable.json'), join(directory, 'configs', 'portable.json'));
  } finally { await rm(external, { recursive: true, force: true }); }
  await page.goto(apiURL); await run(page, 'printf raw-output');
  await expect(page.getByLabel('Raw command output')).toContainText('raw-output');
  let dialog = await settings(page);
  await dialog.getByRole('article', { name: 'Errors' }).getByRole('button', { name: 'Load', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByLabel('Raw command output')).toContainText('raw-output');
  await page.getByRole('navigation', { name: 'Log sources' }).getByRole('button', { name: 'stdin', exact: true }).click();
  dialog = await settings(page);
  await dialog.getByRole('button', { name: 'Save current as new' }).click();
  await expect(dialog.getByLabel('Command', { exact: true })).toHaveValue('');
  expect(JSON.parse(await dialog.getByLabel('Filters and search (JSON)').inputValue()).filter).toEqual([]);
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await dialog.getByRole('button', { name: 'Discard changes' }).click();
  await page.getByRole('navigation', { name: 'Log sources' }).getByRole('button', { name: /raw-output/ }).click();
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'timeout', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: 'healthy', exact: true })).toHaveCount(0);
  await writeFile(join(directory, 'configs', 'bad.json'), '{');
  dialog = await settings(page);
  await expect(dialog.getByRole('alert')).toContainText('bad.json');
  await expect(dialog.getByRole('article', { name: 'Errors' })).toBeVisible();
});

test('source removal while a load is in flight cannot apply settings to stdin', async ({ page, request }) => {
  await putFile(); await page.goto(apiURL); await run(page);
  await expect(page.getByRole('cell', { name: 'healthy', exact: true })).toBeVisible();
  const sources = await request.get(`${apiURL}/api/v1/sources`).then(response => response.json());
  const id = sources.find((item: { kind: string }) => item.kind === 'command').id;
  let release!: () => void, observed!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  const received = new Promise<void>(resolve => { observed = resolve; });
  await page.route('**/configurations/imported', async route => { const response = await route.fetch(); observed(); await held; await route.fulfill({ response }).catch(() => {}); });
  const dialog = await settings(page);
  await dialog.getByRole('article', { name: 'Errors' }).getByRole('button', { name: 'Load', exact: true }).click();
  await received;
  const removed = await request.delete(`${apiURL}/api/v1/sources/${id}`, { headers: { 'Content-Type': 'application/json', 'X-Streamline-Request': '1' }, data: {} });
  expect(removed.status()).toBe(204);
  await expect(page.getByRole('navigation', { name: 'Log sources', includeHidden: true }).getByRole('button', { name: 'stdin', exact: true, includeHidden: true })).toHaveAttribute('aria-pressed', 'true');
  release();
  await expect(dialog.getByRole('alert')).toContainText('The active tab changed.');
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await expect(page.getByLabel('Column paths', { exact: true })).toHaveValue('timestamp\nlevel\nmsg');
});
