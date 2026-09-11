import { DATE_FORMAT_OPTIONS, type ColumnConfig } from './columns';
import { validateFilters } from './filters';
import { EMPTY_SEARCH, validateSearch } from './search';
import type { FilterSpec, SearchSpec } from './transport/types';
import type { SourcePreferences } from './transport/sources';
import { responseJSON } from './transport/api';

export interface Configuration {
  version: 1;
  name: string;
  command: string;
  mode: 'auto' | 'text';
  columns: ColumnConfig[];
  filters: { filter: FilterSpec[]; search: SearchSpec };
}
export interface ConfigurationIssue { field: string; message: string; index?: number; line?: number }
export interface ConfigurationEntry { id: string; document: Configuration }
export interface ConfigurationListing {
  directory: string;
  entries: ConfigurationEntry[];
  errors: { file: string; message: string; issues?: ConfigurationIssue[] }[];
}
export interface ConfigurationDraft { name: string; command: string; mode: 'auto' | 'text'; columns: string; filters: string }
export function configurationFromPreferences(preferences: SourcePreferences, command = '', mode: 'auto' | 'text' = 'auto'): Configuration {
  return { version: 1, name: '', command, mode,
    columns: preferences.columns.map(column => ({ ...column })),
    filters: { filter: preferences.spec.filter.map(filter => ({ ...filter })), search: { ...(preferences.spec.search ?? EMPTY_SEARCH) } } };
}
export function draftFromConfiguration(doc: Configuration): ConfigurationDraft {
  return { name: doc.name, command: doc.command, mode: doc.mode, columns: JSON.stringify(doc.columns, null, 2), filters: JSON.stringify(doc.filters, null, 2) };
}
const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value);
export function parseConfigurationDraft(draft: ConfigurationDraft): { document?: Configuration; errors: ConfigurationIssue[]; warnings: ConfigurationIssue[] } {
  const errors: ConfigurationIssue[] = [], warnings: ConfigurationIssue[] = [];
  const add = (field: string, message: string, index?: number) => errors.push({ field, message, index });
  if (!draft.name.trim()) add('name', 'Enter a name.');
  if (!['auto', 'text'].includes(draft.mode)) add('mode', 'Choose auto or text.');
  let columns: unknown, filters: unknown;
  try { columns = JSON.parse(draft.columns); } catch { add('columns', 'Enter valid JSON.'); }
  try { filters = JSON.parse(draft.filters); } catch { add('filters', 'Enter valid JSON.'); }
  if (!Array.isArray(columns) || !columns.length) add('columns', 'Enter a nonempty JSON array of columns.');
  else columns.forEach((column, index) => {
    if (!object(column) || Object.keys(column).some(key => !['path', 'dateFormat', 'width'].includes(key)) || typeof column.path !== 'string' || !column.path.trim() || !DATE_FORMAT_OPTIONS.some(option => option.value === column.dateFormat)) {
      add('columns', 'Each column requires a nonempty path and supported dateFormat.', index + 1);
    }
    if (object(column) && column.width !== undefined && (typeof column.width !== 'number' || !Number.isFinite(column.width) || column.width < 144)) {
      add('columns', 'width must be a finite number of at least 144 pixels.', index + 1);
    }
  });
  if (!object(filters) || Object.keys(filters).some(key => !['filter', 'search'].includes(key))) add('filters', 'Enter an object with filter and search.');
  else {
    for (const issue of validateFilters(filters.filter, false)) add('filters', `${issue.property}: ${issue.message}`, issue.index);
    const search = filters.search;
    if (!object(search) || Object.keys(search).some(key => !['text', 'mode', 'operator'].includes(key)) || typeof search.text !== 'string' || !['plain', 'regexp'].includes(String(search.mode)) || !['and', 'or'].includes(String(search.operator))) {
      add('filters', 'search requires text, mode (plain/regexp), and operator (and/or).');
    } else {
      warnings.push(...validateSearch(search as unknown as SearchSpec).map(issue => ({ field: 'filters', ...issue })));
    }
    // Browser and Go regex dialects differ; only the server blocks unsupported expressions.
    if (validateFilters(filters.filter, false).length === 0) warnings.push(...validateFilters(filters.filter).map(issue => ({ field: 'filters', index: issue.index, message: issue.message })));
  }
  return { errors, warnings, document: errors.length ? undefined : {
    version: 1, name: draft.name, command: draft.command, mode: draft.mode,
    columns: columns as ColumnConfig[], filters: filters as Configuration['filters'],
  } };
}
export function issueText(issue: ConfigurationIssue) {
  return `${issue.index ? `Item ${issue.index}: ` : ''}${issue.line ? `Search line ${issue.line}: ` : ''}${issue.message}`;
}
export class HTTPConfigurationAPI {
  constructor(private readonly baseURL = '/api/v1/configurations') {}
  list() { return fetch(this.baseURL).then(responseJSON<ConfigurationListing>); }
  get(id: string) { return fetch(`${this.baseURL}/${encodeURIComponent(id)}`).then(responseJSON<ConfigurationEntry>); }
  async remove(id: string): Promise<void> {
    await fetch(`${this.baseURL}/${encodeURIComponent(id)}`, { method: 'DELETE', headers: { 'Content-Type': 'application/json', 'X-Streamline-Request': '1' } }).then(responseJSON);
  }
  validate(document: Configuration) { return this.send<Configuration>('POST', '/validate', document); }
  save(document: Configuration, id?: string) { return this.send<ConfigurationEntry>(id ? 'PUT' : 'POST', id ? `/${encodeURIComponent(id)}` : '', document); }
  private send<T>(method: string, suffix: string, document: Configuration) {
    return fetch(this.baseURL + suffix, { method, headers: { 'Content-Type': 'application/json', 'X-Streamline-Request': '1' }, body: JSON.stringify(document) }).then(responseJSON<T>);
  }
}
