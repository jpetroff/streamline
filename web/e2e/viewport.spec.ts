import { expect, test, type Locator, type Page } from '@playwright/test';
import { leftPanel, mockViewer, openDetails, rightPanel, settleLayout, toggle, viewport, wideColumns } from './viewer-fixture';

async function width(panel: Locator) { return panel.evaluate(element => element.getBoundingClientRect().width); }
async function drag(page: Page, handle: Locator, deltaX: number) {
  const box = (await handle.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + deltaX, box.y + box.height / 2, { steps: 5 });
  await page.mouse.up();
  await settleLayout(page);
}
async function tailGap(page: Page) {
  return viewport(page).evaluate(element => element.scrollHeight - element.clientHeight - element.scrollTop);
}

/** Both full-height panels flank one center column containing table and controls. */
async function expectPanelLayout(page: Page) {
  await settleLayout(page);
  const layout = await page.evaluate(() => {
    const rect = (selector: string) => document.querySelector(selector)!.getBoundingClientRect().toJSON();
    const panels = ['#columns-panel', '#row-details-panel'].map(selector => {
      const element = document.querySelector<HTMLElement>(selector)!;
      const style = getComputedStyle(element);
      return { rect: rect(selector), hidden: element.hidden, background: style.backgroundColor, color: style.color };
    });
    return {
      panels, height: innerHeight,
      appToolbar: rect('[aria-label="Application toolbar"]'),
      table: rect('[role="table"]'), toolbar: rect('[aria-label="Table toolbar"]'),
      search: rect('[aria-label="General search"]'), input: rect('#general-search'),
    };
  });
  for (const panel of layout.panels.filter(panel => !panel.hidden)) {
    expect(panel.rect.top).toBe(layout.appToolbar.bottom);
    expect(panel.rect.bottom).toBe(layout.height);
  }
  expect(layout.panels[0].background).toBe(layout.panels[1].background);
  expect(layout.panels[0].color).toBe(layout.panels[1].color);
  for (const control of [layout.toolbar, layout.search]) {
    expect(control.left).toBeCloseTo(layout.table.left, 0);
    expect(control.right).toBeCloseTo(layout.table.right, 0);
  }
  expect(layout.toolbar.top).toBe(layout.table.bottom);
  expect(layout.search.top).toBe(layout.toolbar.bottom);
  expect(layout.search.bottom).toBe(layout.height);
  expect(layout.input.left).toBeGreaterThan(layout.table.left);
  expect(layout.input.right).toBeLessThan(layout.table.right);
}

async function expectContainedScrollbars(page: Page, leftOpen: boolean, rightOpen: boolean) {
  await expectPanelLayout(page);
  const bounds = await viewport(page).evaluate(element => {
    const rect = element.getBoundingClientRect();
    const left = document.querySelector('#columns-panel')!.getBoundingClientRect();
    const right = document.querySelector('#row-details-panel')!.getBoundingClientRect();
    const toolbar = document.querySelector('[aria-label="Table toolbar"]')!.getBoundingClientRect();
    return { x: rect.x, right: rect.right, bottom: rect.bottom, leftEdge: left.right, rightEdge: right.left,
      toolbarTop: toolbar.top, windowWidth: innerWidth, documentWidth: document.documentElement.scrollWidth,
      width: element.clientWidth, totalWidth: element.scrollWidth,
      verticalTrack: element.getBoundingClientRect().width - element.clientWidth,
      horizontalTrack: element.getBoundingClientRect().height - element.clientHeight,
      overflowX: getComputedStyle(element).overflowX, overflowY: getComputedStyle(element).overflowY };
  });
  expect(bounds.x).toBeCloseTo(leftOpen ? bounds.leftEdge : 0, 0);
  expect(bounds.right).toBeCloseTo(rightOpen ? bounds.rightEdge : bounds.windowWidth, 0);
  expect(bounds.bottom).toBeLessThanOrEqual(bounds.toolbarTop + 1);
  expect(bounds.documentWidth).toBe(bounds.windowWidth);
  expect(bounds.totalWidth).toBeGreaterThan(bounds.width);
  expect(bounds.overflowX).toBe('scroll');
  expect(bounds.overflowY).toBe('scroll');
  // Explicit native scrollbar sizing ensures headless tests exercise reserved tracks.
  expect(bounds.verticalTrack).toBeGreaterThanOrEqual(15);
  expect(bounds.horizontalTrack).toBeGreaterThanOrEqual(15);
  await viewport(page).evaluate(element => { element.scrollLeft = element.scrollWidth; });
  await settleLayout(page);
  const alignment = await viewport(page).evaluate(element => {
    const row = element.querySelector('[role="row"][aria-busy="false"]')!;
    const headers = [...document.querySelectorAll('[role="columnheader"]')];
    const cells = [...row.querySelectorAll('[role="cell"]')];
    return {
      offsets: headers.map((header, i) => header.getBoundingClientRect().left - cells[i].getBoundingClientRect().left),
      lastRight: cells.at(-1)!.getBoundingClientRect().right,
      visibleRight: element.getBoundingClientRect().left + element.clientWidth,
      offset: element.scrollLeft, maximum: element.scrollWidth - element.clientWidth,
    };
  });
  expect(alignment.offset).toBe(alignment.maximum);
  for (const offset of alignment.offsets) expect(Math.abs(offset)).toBeLessThan(1);
  expect(alignment.lastRight).toBeCloseTo(alignment.visibleRight, 0);
  expect(await tailGap(page)).toBeLessThanOrEqual(1);
  return bounds.width / bounds.totalWidth;
}

test('both scrollbar tracks and header alignment survive every panel combination', async ({ page }) => {
  await mockViewer(page);
  await page.addStyleTag({ content: '.table-viewport::-webkit-scrollbar { width: 16px; height: 16px; }' });
  await wideColumns(page);
  const original = await viewport(page).elementHandle();
  const leftOnlyRatio = await expectContainedScrollbars(page, true, false);
  await openDetails(page);
  const bothRatio = await expectContainedScrollbars(page, true, true);
  expect(bothRatio).toBeLessThan(leftOnlyRatio);
  await toggle(page).click();
  const rightOnlyRatio = await expectContainedScrollbars(page, false, true);
  expect(rightOnlyRatio).toBeGreaterThan(bothRatio);
  await page.getByRole('button', { name: 'Close row details' }).click();
  const fullRatio = await expectContainedScrollbars(page, false, false);
  expect(fullRatio).toBeGreaterThan(rightOnlyRatio);
  expect(await viewport(page).evaluate((element, original) => element === original, original)).toBe(true);
  const finalRow = viewport(page).getByRole('row').last();
  const rowBottom = (await finalRow.boundingBox())!;
  const viewBottom = await viewport(page).evaluate(element => element.getBoundingClientRect().top + element.clientHeight);
  expect(rowBottom.y + rowBottom.height).toBeCloseTo(viewBottom, 0);
});

test('shared pointer and keyboard resizing enforce both bounds and clean up cancellation', async ({ page }) => {
  await mockViewer(page);
  await wideColumns(page);
  await openDetails(page);
  const leftHandle = page.getByRole('separator', { name: 'Resize columns and filters' });
  const rightHandle = page.getByRole('separator', { name: 'Resize row details' });
  for (const [panel, handle, direction] of [[leftPanel(page), leftHandle, 1], [rightPanel(page), rightHandle, -1]] as const) {
    await handle.press('Home');
    expect(await width(panel)).toBe(240);
    await drag(page, handle, direction * 70);
    expect(await width(panel)).toBeCloseTo(310, 0);
    await expectPanelLayout(page);
    await handle.press(direction === 1 ? 'ArrowRight' : 'ArrowLeft');
    expect(await width(panel)).toBeCloseTo(326, 0);
    await handle.press('End');
    expect(await width(panel)).toBeCloseTo(1280 * 0.33, 0);
    await drag(page, handle, direction * 100);
    expect(await width(panel)).toBeCloseTo(1280 * 0.33, 0);
    await handle.press('Home');
    await drag(page, handle, -direction * 100);
    expect(await width(panel)).toBe(240);
  }
  const box = (await leftHandle.boundingBox())!;
  await page.mouse.move(box.x + 3, box.y + 30);
  await page.mouse.down();
  await page.mouse.move(box.x + 43, box.y + 30);
  await leftHandle.dispatchEvent('pointercancel', { pointerId: 1 });
  await page.mouse.up();
  await settleLayout(page);
  expect(await page.evaluate(() => document.documentElement.style.cursor)).toBe('');
  expect(await page.evaluate(() => document.documentElement.style.userSelect)).toBe('');
});

test('hiding preserves drafts and preferred widths, while reload restores defaults', async ({ page }) => {
  await mockViewer(page);
  await wideColumns(page);
  await openDetails(page);
  await page.getByRole('separator', { name: 'Resize columns and filters' }).press('End');
  await page.getByRole('separator', { name: 'Resize row details' }).press('Home');
  await page.getByRole('separator', { name: 'Resize row details' }).press('ArrowLeft');
  const preferredLeft = await width(leftPanel(page));
  const preferredRight = await width(rightPanel(page));
  await page.getByRole('textbox', { name: 'Column paths' }).fill('message\nunsaved.path');
  await page.getByRole('button', { name: 'Add filter', exact: true }).click();
  await page.getByRole('textbox', { name: 'Field for filter 1' }).fill('request.host');
  await page.getByRole('textbox', { name: 'Value for filter 1' }).fill('unsaved-host');
  await toggle(page).click();
  await expect(toggle(page)).toHaveAttribute('aria-expanded', 'false');
  await expect(leftPanel(page)).toBeHidden();
  expect(await leftPanel(page).evaluate(element => (element as HTMLElement).inert)).toBe(true);
  await page.getByRole('button', { name: 'Close row details' }).click();
  await page.setViewportSize({ width: 600, height: 800 });
  await toggle(page).click();
  await openDetails(page);
  expect(await width(leftPanel(page))).toBeCloseTo(198, 0);
  expect(await width(rightPanel(page))).toBeCloseTo(198, 0);
  await expectPanelLayout(page);
  expect(await viewport(page).evaluate(element => element.getBoundingClientRect().width)).toBeGreaterThanOrEqual(204);
  await page.setViewportSize({ width: 1280, height: 800 });
  await settleLayout(page);
  expect(await width(leftPanel(page))).toBeCloseTo(preferredLeft, 0);
  expect(await width(rightPanel(page))).toBeCloseTo(preferredRight, 0);
  await expect(page.getByRole('textbox', { name: 'Column paths' })).toHaveValue('message\nunsaved.path');
  await expect(page.getByRole('textbox', { name: 'Field for filter 1' })).toHaveValue('request.host');
  await expect(page.getByRole('textbox', { name: 'Value for filter 1' })).toHaveValue('unsaved-host');
  await toggle(page).click();
  await page.reload();
  await expect(leftPanel(page)).toBeVisible();
  await expect(rightPanel(page)).toBeHidden();
  await settleLayout(page);
  expect(await width(leftPanel(page))).toBe(288);
});

for (const lines of [1, 2]) {
  test(`${lines}-line rows preserve paused anchors and live following through layout changes`, async ({ page }) => {
    const fixture = await mockViewer(page);
    await wideColumns(page);
    await page.getByRole('button', { name: lines === 1 ? '1 line' : '2 lines', exact: true }).click();
    await openDetails(page);
    const rowHeight = lines === 1 ? 24 : 40;
    const anchor = 450 * rowHeight + 7;
    await viewport(page).evaluate((element, top) => { element.scrollTop = top; }, anchor);
    await settleLayout(page);
    await expect.poll(() => viewport(page).evaluate(element => element.scrollTop)).toBe(anchor);
    await toggle(page).click();
    await page.getByRole('separator', { name: 'Resize row details' }).press('Home');
    await page.setViewportSize({ width: 1100, height: 700 });
    await settleLayout(page);
    expect(await viewport(page).evaluate(element => element.scrollTop)).toBe(anchor);
    await fixture.append(20);
    await settleLayout(page);
    expect(await viewport(page).evaluate(element => element.scrollTop)).toBe(anchor);
    expect(await viewport(page).evaluate(element => element.scrollHeight)).toBe(1000 * rowHeight);
    await viewport(page).evaluate(element => { element.scrollTop = element.scrollHeight; });
    await expect.poll(() => viewport(page).evaluate(element => element.scrollHeight)).toBe(1020 * rowHeight);
    await expect.poll(() => tailGap(page)).toBeLessThanOrEqual(1);
    await toggle(page).click();
    await page.setViewportSize({ width: 1280, height: 650 });
    await settleLayout(page);
    await fixture.append(20);
    await expect.poll(() => viewport(page).evaluate(element => element.scrollHeight)).toBe(1040 * rowHeight);
    await expect.poll(() => tailGap(page)).toBeLessThanOrEqual(1);
  });
}

test('explicit column widths survive panel changes and horizontal offsets clamp on expansion', async ({ page }) => {
  await mockViewer(page);
  await wideColumns(page);
  await page.getByRole('button', { name: 'Resize timestamp column', exact: true }).press('ArrowRight');
  const initialWidths = await page.getByRole('columnheader').evaluateAll(elements => elements.map(element => element.getBoundingClientRect().width));
  await openDetails(page);
  await viewport(page).evaluate(element => { element.scrollLeft = element.scrollWidth; });
  await settleLayout(page);
  const smallerOffset = await viewport(page).evaluate(element => element.scrollLeft);
  await toggle(page).click();
  await page.getByRole('button', { name: 'Close row details' }).click();
  await settleLayout(page);
  expect(await page.getByRole('columnheader').evaluateAll(elements => elements.map(element => element.getBoundingClientRect().width))).toEqual(initialWidths);
  const result = await viewport(page).evaluate(element => ({ offset: element.scrollLeft, maximum: element.scrollWidth - element.clientWidth }));
  expect(result.offset).toBe(result.maximum);
  expect(result.offset).toBeLessThan(smallerOffset);
  await viewport(page).locator('[role="row"][aria-busy="false"]').last().press('Shift+Enter');
  await expect(rightPanel(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(rightPanel(page)).toBeHidden();
});

test('empty results keep a usable viewport and sidebar toggle', async ({ page }) => {
  await mockViewer(page, { count: 0 });
  const status = page.getByText('No log records.', { exact: true });
  await expect(status).toBeVisible();
  await settleLayout(page);
  const statusBox = (await status.boundingBox())!;
  expect(statusBox.height).toBe(await viewport(page).evaluate(element => element.clientHeight));
  expect(statusBox.width).toBe(await viewport(page).evaluate(element => element.clientWidth));
  await toggle(page).click();
  await expect(leftPanel(page)).toBeHidden();
  await expect(viewport(page)).toBeVisible();
  await toggle(page).click();
  await expect(leftPanel(page)).toBeVisible();
});

for (const kind of ['pending', 'raw'] as const) {
  test(`${kind} input retains the toolbar without row controls`, async ({ page }) => {
    await mockViewer(page, { kind, count: 0 });
    await expect(page.getByRole('group', { name: 'Row display' })).toHaveCount(0);
    await toggle(page).click();
    await expect(leftPanel(page)).toBeHidden();
    await toggle(page).click();
    await expect(leftPanel(page)).toBeVisible();
    if (kind === 'raw') await expect(page.getByRole('region', { name: 'Scrollable raw stdin text' })).toContainText('Raw report');
    else await expect(page.getByText('Waiting for stdin…')).toBeVisible();
  });
}

test('connecting toolbar can hide and restore the sidebar', async ({ page }) => {
  const fixture = await mockViewer(page, { connecting: true });
  await expect(page.getByText('Connecting…', { exact: true })).toBeVisible();
  await toggle(page).click();
  await expect(leftPanel(page)).toBeHidden();
  await toggle(page).click();
  await expect(leftPanel(page)).toBeVisible();
  fixture.releaseSession();
  await expect(page.getByRole('table')).toBeVisible();
});
