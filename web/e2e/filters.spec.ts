import { expect, test } from '@playwright/test';
import { mockViewer } from './viewer-fixture';

test('negative operators validate and submit text and numeric values from the builder', async ({ page }) => {
  const fixture = await mockViewer(page);
  await expect(page.getByRole('table')).toHaveAttribute('aria-busy', 'false');
  await page.keyboard.press('Control+Alt+f');
  await page.getByRole('textbox', { name: 'Field for filter 1' }).fill('severity');
  const operator = page.getByRole('combobox', { name: 'Operator for filter 1' });
  const value = page.getByRole('textbox', { name: 'Value for filter 1' });
  const apply = page.getByRole('region', { name: 'Filters', exact: true }).getByRole('button', { name: 'Apply', exact: true });

  await operator.selectOption({ label: 'Does not match regex' });
  await expect(value).toHaveAttribute('placeholder', '^error|timeout$');
  await value.fill('[');
  await expect(value).toHaveAttribute('aria-invalid', 'true');
  await expect(apply).toBeDisabled();
  await operator.selectOption({ label: 'Does not contain' });
  await expect(value).toHaveAttribute('aria-invalid', 'false');

  const cases = [
    { op: 'not_contains', value: '[' },
    { op: 'neq', value: 'error' },
    { op: 'not_regex', value: '^debug|trace$' },
    { op: 'not_gt', value: 10.5 },
    { op: 'not_gte', value: 10.5 },
    { op: 'not_lt', value: 10.5 },
    { op: 'not_lte', value: 10.5 },
  ];
  for (const condition of cases) {
    await operator.selectOption(condition.op);
    if (typeof condition.value === 'number') {
      await expect(value).toHaveAttribute('inputmode', 'decimal');
      await value.fill('');
      await expect(apply).toBeDisabled();
    }
    await value.fill(String(condition.value));
    const previous = fixture.specs.length;
    await apply.click();
    await expect.poll(() => fixture.specs.length).toBe(previous + 1);
    expect(fixture.specs.at(-1)?.filter).toEqual([{ field: 'severity', ...condition }]);
    await expect(apply).toBeDisabled();
  }
});
