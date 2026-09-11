import type { JSONValue, LogRow, RowPage } from './transport/types';

export const DEFAULT_COLUMNS = ['timestamp', 'level', 'msg'] as const;
export const ABSENT_COLUMN_TEXT = '—';

export const DATE_FORMAT_OPTIONS = [
  { value: 'original', label: 'Original' },
  { value: 'iso', label: 'ISO (UTC)' },
  { value: 'local', label: 'Local date & time' },
  { value: 'date', label: 'Date only' },
  { value: 'time', label: 'Time only' },
] as const;

export type DateDisplayFormat = typeof DATE_FORMAT_OPTIONS[number]['value'];

export interface ColumnConfig {
  path: string;
  dateFormat: DateDisplayFormat;
  width?: number;
}

export interface DateFormatContext {
  locales?: string | string[];
  timeZone?: string;
}

export interface FormattedDetailValue {
  text: string;
  kind: 'absent' | 'scalar' | 'json';
}

/** Converts the line-oriented editor value into the ordered table columns. */
export function parseColumnDraft(draft: string): string[] {
  return draft
    .split(/\r?\n/)
    .map(column => column.trim())
    .filter(column => column.length > 0);
}

/** Resolves a case-sensitive dotted object path against an original JSON record. */
export function resolveColumnValue(
  fields: Record<string, JSONValue> | undefined,
  path: string,
): JSONValue | undefined {
  if (!fields) return undefined;
  const segments = path.split('.');
  if (segments.some(segment => segment.length === 0)) return undefined;

  let value: JSONValue = fields;
  for (const segment of segments) {
    if (value === null || Array.isArray(value) || typeof value !== 'object') return undefined;
    if (!Object.prototype.hasOwnProperty.call(value, segment)) return undefined;
    value = value[segment];
  }
  return value;
}

/** Resolves normalized row columns before falling back to the original JSON. */
export function resolveRowColumnValue(row: LogRow, path: string): JSONValue | undefined {
  if (path === 'timestamp' && row.timestamp !== undefined) return row.timestamp;
  if (path === 'severity' && row.severity !== undefined) return row.severity;
  if (path === 'message') return row.message;
  return resolveColumnValue(row.fields, path);
}

/** Indicates column names that should expose date display controls in the header. */
export function isDateColumnPath(path: string): boolean {
  const name = path.split('.').at(-1) ?? path;
  const normalized = name.toLowerCase();
  if (['timestamp', 'datetime', 'date', 'time', 'created_at', 'updated_at', 'deleted_at'].includes(normalized)) {
    return true;
  }
  return /(?:Timestamp|DateTime|Datetime|Date|Time|[cC]reatedAt|[uU]pdatedAt|[dD]eletedAt)$/.test(name)
    || /(?:^|[_-])(?:timestamp|datetime|date|time|created_at|updated_at|deleted_at)$/i.test(name);
}

/** Produces the single-line, display-safe representation used by a table cell. */
export function formatColumnValue(
  value: JSONValue | undefined,
  dateFormat: DateDisplayFormat = 'original',
  context: DateFormatContext = {},
): string {
  if (value === undefined) return ABSENT_COLUMN_TEXT;
  if (dateFormat !== 'original') {
    const date = parseColumnDate(value);
    if (date) return formatDate(date, dateFormat, context);
  }
  if (typeof value === 'string') return value;
  if (value === null) return 'null';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

/** Parses ISO-style strings and Unix timestamps without coercing other scalar values. */
function parseColumnDate(value: JSONValue): Date | undefined {
  let date: Date;
  if (typeof value === 'number' && Number.isFinite(value)) {
    // Unix seconds are still common in log payloads; larger values are milliseconds.
    date = new Date(Math.abs(value) < 1_000_000_000_000 ? value * 1_000 : value);
  } else if (typeof value === 'string' && value.trim() !== '') {
    date = new Date(value);
  } else {
    return undefined;
  }
  return Number.isNaN(date.getTime()) ? undefined : date;
}

function formatDate(date: Date, format: Exclude<DateDisplayFormat, 'original'>, context: DateFormatContext): string {
  if (format === 'iso') return date.toISOString();

  const common: Intl.DateTimeFormatOptions = { timeZone: context.timeZone };
  const options: Intl.DateTimeFormatOptions = format === 'local'
    ? { ...common, dateStyle: 'medium', timeStyle: 'medium' }
    : format === 'date'
      ? { ...common, dateStyle: 'medium' }
      : { ...common, timeStyle: 'medium' };
  return new Intl.DateTimeFormat(context.locales, options).format(date);
}

/** Produces the expanded representation used by the selected-row detail panel. */
export function formatDetailColumnValue(value: JSONValue | undefined): FormattedDetailValue {
  if (value === undefined) return { text: ABSENT_COLUMN_TEXT, kind: 'absent' };
  if (value !== null && typeof value === 'object') {
    return { text: JSON.stringify(value, null, 2), kind: 'json' };
  }
  if (typeof value === 'string') {
    const parsed = parseStructuredJSONString(value);
    if (parsed !== undefined) return { text: JSON.stringify(parsed, null, 2), kind: 'json' };
  }
  return { text: formatColumnValue(value), kind: 'scalar' };
}

/** Recognizes encoded JSON containers without reinterpreting ordinary scalar strings. */
function parseStructuredJSONString(value: string): JSONValue[] | { [key: string]: JSONValue } | undefined {
  const trimmed = value.trim();
  if (!(trimmed.startsWith('{') || trimmed.startsWith('['))) return undefined;
  try {
    const parsed: unknown = JSON.parse(trimmed);
    if (parsed !== null && typeof parsed === 'object') {
      return parsed as JSONValue[] | { [key: string]: JSONValue };
    }
  } catch {
    // Malformed JSON-looking strings remain exactly as logged.
  }
  return undefined;
}

/** Finds a structured example from the current input generation. */
export function findSampleFields(
  pages: readonly RowPage[],
  generationId: string,
): Record<string, JSONValue> | undefined {
  for (const page of pages) {
    if (page.snapshot.generationId !== generationId) continue;
    for (const row of page.rows) {
      if (row.fields && Object.keys(row.fields).length > 0) return row.fields;
    }
  }
  return undefined;
}

/** Builds column settings while retaining the display choice for matching paths. */
export function configureColumns(
  paths: readonly string[],
  current: readonly ColumnConfig[] = [],
): ColumnConfig[] {
  const available = new Map<string, ColumnConfig[]>();
  for (const column of current) {
    const matches = available.get(column.path) ?? [];
    matches.push(column);
    available.set(column.path, matches);
  }

  return paths.map(path => {
    const previous = available.get(path)?.shift();
    return previous ? { ...previous } : { path, dateFormat: 'original' };
  });
}
