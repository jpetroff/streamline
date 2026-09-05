import type { JSONValue, RowPage } from './transport/types';

export const DEFAULT_COLUMNS = ['timestamp', 'level', 'msg'] as const;
export const ABSENT_COLUMN_TEXT = '—';

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

/** Produces the single-line, display-safe representation used by a table cell. */
export function formatColumnValue(value: JSONValue | undefined): string {
  if (value === undefined) return ABSENT_COLUMN_TEXT;
  if (typeof value === 'string') return value;
  if (value === null) return 'null';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
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
