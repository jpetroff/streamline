import { expect, test, type Page } from '@playwright/test';
import { mockViewer, viewport, leftPanel, rightPanel, toggle, settleLayout } from './viewer-fixture';

const active = (page: Page) => viewport(page).locator('[aria-current="true"]');
const search = (page: Page) => page.getByRole('textbox', { name: 'Search', exact: true });
async function jump(page: Page, position: string) {
  await page.keyboard.press('Control+g');
  const field = page.getByRole('textbox', { name: 'Jump to row' });
  await expect(field).toBeFocused();
  await field.fill(position);
  await field.press('Enter');
}
async function expectActive(page: Page, offset: bigint) {
  await expect(active(page)).toHaveAttribute('data-offset', String(offset));
  await expect(active(page)).toHaveAttribute('aria-busy', 'false');
}

test('global focus commands cross active inputs and preserve drafts; apply is scoped', async ({ page }) => {
  const fixture = await mockViewer(page);
  await expectActive(page, 999n);
  await toggle(page).click();
  await page.keyboard.press('Control+f');
  await expect(search(page)).toBeFocused();
  await search(page).fill('timeout\nrefused');
  // A widget that stops bubbling must not block a designated global shortcut.
  await search(page).evaluate(element => element.addEventListener('keydown', event => event.stopPropagation()));
  await page.keyboard.press('Control+b');
  await expect(leftPanel(page)).toBeVisible();
  const columns = page.getByRole('textbox', { name: 'Column paths' });
  await expect(columns).toBeFocused();
  await columns.fill('message\nseverity');
  await columns.press('Control+Enter');
  await expect(page.getByRole('columnheader')).toHaveCount(2);
  expect(fixture.specs).toHaveLength(1);
  await page.keyboard.press('Control+f');
  await expect(search(page)).toHaveValue('timeout\nrefused');
  expect(await search(page).evaluate((el: HTMLTextAreaElement) => [el.selectionStart, el.selectionEnd])).toEqual([0, 15]);
  await search(page).evaluate((el: HTMLTextAreaElement) => el.setSelectionRange(2, 4));
  await page.keyboard.press('Control+f');
  expect(await search(page).evaluate((el: HTMLTextAreaElement) => [el.selectionStart, el.selectionEnd])).toEqual([2, 4]);
  await page.keyboard.press('Control+Enter');
  await expect.poll(() => fixture.specs.length).toBe(2);
  await expect(search(page)).toBeFocused();
  expect(fixture.specs[1].search?.text).toBe('timeout\nrefused');
});

test('filter controls support continuous Tab addition, deletion focus and scoped submission', async ({ page }) => {
  const fixture = await mockViewer(page);
  await expectActive(page, 999n);
  await toggle(page).click();
  await page.keyboard.press('Control+Alt+f');
  const field = page.getByRole('textbox', { name: 'Field for filter 1' });
  await expect(field).toBeFocused();
  await field.fill('severity');
  await page.keyboard.press('Tab');
  await expect(page.getByRole('combobox', { name: 'Operator for filter 1' })).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(page.getByRole('textbox', { name: 'Value for filter 1' })).toBeFocused();
  await page.keyboard.type('info');
  await page.keyboard.press('Tab');
  await expect(page.getByRole('button', { name: 'Remove filter 1' })).toBeFocused();
  const add = page.getByRole('button', { name: 'Add filter', exact: true });
  for (let i = 0; i < 8 && !(await add.evaluate(el => el === document.activeElement)); i++) await page.keyboard.press('Tab');
  await expect(add).toBeFocused();
  await page.keyboard.press('Enter');
  const second = page.getByRole('textbox', { name: 'Field for filter 2' });
  await expect(second).toBeFocused();
  await page.getByRole('button', { name: 'Remove filter 2' }).click();
  await expect(field).toBeFocused();
  await field.press('Control+Enter');
  await expect.poll(() => fixture.specs.length).toBe(2);
  expect(fixture.specs[1].filter).toEqual([{ field: 'severity', op: 'eq', value: 'info' }]);
  for (let i = 0; i < 8; i++) await page.keyboard.press('Control+Alt+f');
  await expect(page.getByRole('textbox', { name: 'Field for filter 9' })).toBeFocused();
  await expect(page.getByRole('textbox', { name: 'Field for filter 9' })).toBeInViewport();
});

test('rows move by one and ten; global movement retains input selection and preview follows', async ({ page }) => {
  const fixture = await mockViewer(page);
  await expectActive(page, 999n);
  await page.keyboard.press('Control+f');
  await search(page).fill('draft');
  await search(page).evaluate((el: HTMLTextAreaElement) => el.setSelectionRange(1, 3));
  await page.keyboard.press('Control+Alt+ArrowUp');
  await expectActive(page, 998n);
  await expect(search(page)).toBeFocused();
  expect(await search(page).evaluate((el: HTMLTextAreaElement) => [el.selectionStart, el.selectionEnd])).toEqual([1, 3]);
  await fixture.append(20);
  await settleLayout(page);
  await expectActive(page, 998n); // Tail is visible, but keyboard selection pins this snapshot.
  await page.keyboard.press('Control+Alt+Shift+ArrowUp');
  await expectActive(page, 988n);
  await page.keyboard.press('Escape');
  await expect(active(page)).toBeFocused();
  await page.keyboard.press('Shift+Enter');
  await expect(rightPanel(page)).toBeVisible();
  await expect(rightPanel(page)).toContainText('Row 989');
  await page.keyboard.press('PageUp');
  await expectActive(page, 978n);
  await expect(rightPanel(page)).toContainText('Row 979');
  await page.keyboard.press('ArrowDown');
  await expectActive(page, 979n);
  await page.keyboard.press('Escape');
  await expect(rightPanel(page)).toBeHidden();
  await jump(page, '1000');
  await expectActive(page, 1019n);
  await fixture.append(1);
  await expectActive(page, 1020n);
});

test('jump validates positions, crosses virtual segments, and distinguishes result position from source ID', async ({ page }) => {
  await mockViewer(page, { count: 9007199254740993n, idStride: 3 });
  await expectActive(page, 9007199254740992n);
  await jump(page, '400001');
  await expectActive(page, 400000n);
  await expect(active(page)).toBeFocused();
  await page.keyboard.press('Shift+Enter');
  await expect(rightPanel(page)).toContainText('Row 1200003');
  await jump(page, '1');
  await expectActive(page, 0n);
  await page.keyboard.press('PageUp');
  await expectActive(page, 0n);
  for (const value of ['', '0', '-1', '1.2', '1e3', '9007199254740994']) {
    await jump(page, value);
    await expect(page.getByRole('textbox', { name: 'Jump to row' })).toHaveAttribute('aria-invalid', 'true');
    await expect(page.getByRole('textbox', { name: 'Jump to row' })).toBeFocused();
    await expectActive(page, 0n);
  }
});

test('new queries resume at their newest result, retain editor focus, and failed queries preserve selection', async ({ page }) => {
  const fixture = await mockViewer(page);
  await expectActive(page, 999n);
  await jump(page, '100');
  await expectActive(page, 99n);
  await page.keyboard.press('Shift+Enter');
  fixture.setNextQueryCount(40n);
  await page.keyboard.press('Control+f');
  await search(page).fill('new query');
  await search(page).press('Control+Enter');
  await expectActive(page, 39n);
  await expect(search(page)).toBeFocused();
  await expect(rightPanel(page)).toBeHidden();
  await fixture.append(5);
  await expectActive(page, 44n);
  fixture.rejectNextQuery({ code: 'invalid_search', message: 'Rejected search' });
  await search(page).fill('bad query');
  await search(page).press('Control+Enter');
  await expect(page.getByRole('alert')).toContainText('Rejected search');
  await expectActive(page, 44n);
  await expect(search(page)).toBeFocused();
});

test('slow navigation cannot replace a newer target or steal editor focus', async ({ page }) => {
  const fixture = await mockViewer(page, { count: 10000 });
  await expectActive(page, 9999n);
  const held = fixture.holdRows(1800n, 2400n);
  await jump(page, '2001');
  await held.seen;
  await jump(page, '6001');
  await expectActive(page, 6000n);
  await page.keyboard.press('Control+f');
  held.release();
  await settleLayout(page);
  await expectActive(page, 6000n);
  await expect(search(page)).toBeFocused();
});

test('modal import owns shortcuts and popovers dismiss before global focus changes', async ({ page }) => {
  await mockViewer(page);
  await expectActive(page, 999n);
  await page.getByRole('button', { name: 'Sample log entry for columns' }).click();
  await expect(page.getByRole('dialog', { name: 'Sample log entry' })).toBeVisible();
  await page.keyboard.press('Control+f');
  await expect(page.getByRole('dialog', { name: 'Sample log entry' })).toBeHidden();
  await expect(search(page)).toBeFocused();
  await page.getByRole('button', { name: 'Import JSON' }).click();
  const dialog = page.getByRole('dialog', { name: 'Import filters' });
  await expect(dialog).toBeVisible();
  const json = page.getByRole('textbox', { name: 'Filter JSON' });
  await json.fill('[{"field":"severity","op":"eq","value":"info"}]');
  await json.press('Control+Alt+f');
  await expect(json).toBeFocused();
  await json.press('Control+Enter');
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('textbox', { name: 'Field for filter 1' })).toHaveValue('severity');
});

test('active-row focus survives streaming and virtual eviction without taking focus from inputs', async ({ page }) => {
  const fixture = await mockViewer(page);
  await expectActive(page, 999n);
  await page.keyboard.press('Escape');
  await expect(active(page)).toBeFocused();
  await fixture.append(1);
  await expectActive(page, 1000n);
  await expect(active(page)).toBeFocused();
  await fixture.append(1000);
  await expectActive(page, 2000n);
  await expect(active(page)).toBeFocused();
  await viewport(page).evaluate(element => { element.scrollTop = 24 * 300; });
  await settleLayout(page);
  await expect(viewport(page)).toBeFocused();
  await expect(viewport(page)).toHaveAttribute('tabindex', '0');
  await page.keyboard.press('ArrowUp');
  await expectActive(page, 1999n);
  await expect(active(page)).toBeFocused();
  await page.keyboard.press('Control+f');
  await fixture.append(1000);
  await expect(search(page)).toBeFocused();
  await expectActive(page, 1999n);
});

test('native input navigation and multiline Enter are untouched; empty results disable row commands', async ({ page }) => {
  await mockViewer(page, { count: 0 });
  await page.keyboard.press('Control+f');
  await search(page).fill('one');
  await search(page).press('Enter');
  await search(page).press('Shift+Enter');
  await expect(search(page)).toHaveValue('one\n\n');
  await search(page).press('ArrowUp');
  await search(page).press('Control+Alt+ArrowUp');
  await expect(search(page)).toBeFocused();
  await expect(rightPanel(page)).toBeHidden();
  await expect(page.getByRole('textbox', { name: 'Jump to row' })).toBeDisabled();
});

test('loading preview never shows a stale row and failed pages can be retried', async ({ page }) => {
  const fixture = await mockViewer(page, { count: 10000 });
  await expectActive(page, 9999n);
  await page.keyboard.press('Escape');
  await page.keyboard.press('Shift+Enter');
  const held = fixture.holdRows(1800n, 2400n);
  await jump(page, '2001');
  await held.seen;
  await expect(rightPanel(page)).toContainText('Loading row…');
  await expect(page.getByRole('button', { name: 'Close row details' })).toBeVisible();
  await expect(rightPanel(page)).not.toContainText('Row 10000');
  fixture.failRowsAt(2000n);
  held.release();
  await expect(rightPanel(page)).toContainText('Could not load rows.');
  fixture.failRowsAt();
  await jump(page, '2001');
  await expectActive(page, 2000n);
  await expect(rightPanel(page)).toContainText('Row 2001');
  await expect(page.getByRole('alert')).toHaveCount(0);
});

for (const platform of ['MacIntel', 'Win32']) {
  test(`${platform} shortcut mapping and hints work in the browser`, async ({ page }) => {
    await page.addInitScript(platform => Object.defineProperty(navigator, 'platform', { value: platform }), platform);
    await mockViewer(page);
    await expectActive(page, 999n);
    const mod = platform === 'MacIntel' ? 'Meta' : 'Control';
    await page.keyboard.press(`${mod}+f`);
    await expect(search(page)).toBeFocused();
    await expect(search(page)).toHaveAttribute('aria-keyshortcuts', `${mod}+F`);
    await page.keyboard.press(`${mod}+b`);
    await expect(page.getByRole('textbox', { name: 'Column paths' })).toBeFocused();
    await page.keyboard.press(`${mod}+Alt+f`);
    await expect(page.getByRole('textbox', { name: 'Field for filter 1' })).toBeFocused();
    await page.keyboard.press('Control+g');
    await expect(page.getByRole('textbox', { name: 'Jump to row' })).toBeFocused();
  });
}
