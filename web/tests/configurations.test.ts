import { expect, test } from 'bun:test';
import { configurationFromPreferences, draftFromConfiguration, parseConfigurationDraft } from '../src/lib/configurations';
import type { SourcePreferences } from '../src/lib/transport/sources';

function preferences(): SourcePreferences {
  return { spec: { filter: [{ field: 'level', op: 'regex', value: 'error' }], sort: 'input', search: { text: 'retry\n\ntimeout ', mode: 'regexp', operator: 'and' } },
    columns: [{ path: 'timestamp', dateFormat: 'iso', width: 280.5 }, { path: 'message', dateFormat: 'original' }], following: false, offset: 44n, rowLines: 2 };
}
test('configuration snapshots round trip text, columns, formats and search without session state or shared references', () => {
  const source = preferences();
  const doc = configurationFromPreferences(source, "  printf 'hello\\n'\n", 'text');
  doc.name = 'Saved';
  const draft = draftFromConfiguration(doc);
  expect(parseConfigurationDraft(draft)).toEqual({ document: doc, errors: [], warnings: [] });
  expect(JSON.stringify(doc)).not.toContain('offset');
  source.columns[0].dateFormat = 'date';
  source.columns[0].width = 600;
  source.spec.filter[0].value = 'changed';
  source.spec.search!.text = 'changed';
  expect(doc.columns[0].dateFormat).toBe('iso');
  expect(doc.columns[0].width).toBe(280.5);
  expect(doc.filters.filter[0].value).toBe('error');
  expect(doc.filters.search.text).toBe('retry\n\ntimeout ');
});
test('stdin snapshot supplies empty command and complete search defaults', () => {
  const source = preferences(); source.spec.search = undefined;
  const doc = configurationFromPreferences(source); doc.name = 'stdin';
  expect(doc.command).toBe(''); expect(doc.mode).toBe('auto');
  expect(doc.filters.search).toEqual({ text: '', mode: 'plain', operator: 'or' });
  expect(parseConfigurationDraft(draftFromConfiguration(doc)).errors).toEqual([]);
});
test('structural failures block save while browser regex errors are advisory', () => {
  const doc = configurationFromPreferences(preferences()); doc.name = 'Valid';
  const draft = draftFromConfiguration(doc);
  expect(parseConfigurationDraft({ ...draft, columns: '{}' }).document).toBeUndefined();
  expect(parseConfigurationDraft({ ...draft, columns: '[{"path":"message","dateFormat":"unsupported"}]' }).errors[0].index).toBe(1);
  expect(parseConfigurationDraft({ ...draft, filters: '[]' }).document).toBeUndefined();
  expect(parseConfigurationDraft({ ...draft, filters: '{"filter":[],"search":{"mode":"plain","operator":"or"}}' }).document).toBeUndefined();
  doc.filters.search.text = '[\n(?i)Go-only';
  doc.filters.filter[0].value = '[';
  const result = parseConfigurationDraft(draftFromConfiguration(doc));
  expect(result.document).toBeDefined();
  expect(result.errors).toEqual([]);
  expect(result.warnings.some(issue => issue.line === 1)).toBe(true);
  expect(result.warnings.some(issue => issue.index === 1)).toBe(true);
});

test('column widths are optional but must be finite numbers of at least 144 pixels', () => {
  const doc = configurationFromPreferences(preferences()); doc.name = 'Widths';
  const draft = draftFromConfiguration(doc);
  for (const width of ['null', '"200"', 'false', '0', '-1', '143', '1e999']) {
    const result = parseConfigurationDraft({ ...draft, columns: `[{"path":"message","dateFormat":"original","width":${width}}]` });
    expect(result.document).toBeUndefined();
    expect(result.errors[0].index).toBe(1);
  }
  expect(parseConfigurationDraft({ ...draft, columns: '[{"path":"message","dateFormat":"original","width":144}]' }).errors).toEqual([]);
});
