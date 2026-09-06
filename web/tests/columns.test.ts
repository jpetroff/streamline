import { describe, expect, test } from 'bun:test';
import {
  ABSENT_COLUMN_TEXT,
  configureColumns,
  findSampleFields,
  formatColumnValue,
  formatDetailColumnValue,
  isDateColumnPath,
  parseColumnDraft,
  resolveColumnValue,
  resolveRowColumnValue,
} from '../src/lib/columns';
import type { JSONValue, LogRow, RowPage, Snapshot } from '../src/lib/transport/types';

const snapshot = (generationId: string): Snapshot => ({
  sessionId: 'session', generationId, queryId: 'query', revision: '1',
  processedThrough: '1', matchedCount: '1', snapshotToken: `query.${generationId}`,
});

describe('column draft parsing', () => {
  test('trims paths, ignores blank lines, and preserves order and duplicates', () => {
    expect(parseColumnDraft(' timestamp \n\n request.host\r\nlevel\nlevel ')).toEqual([
      'timestamp', 'request.host', 'level', 'level',
    ]);
  });

  test('returns no usable columns for whitespace-only input', () => {
    expect(parseColumnDraft(' \n\t\n')).toEqual([]);
  });

  test('retains date formats by path when configured columns are reordered', () => {
    const configured = configureColumns(['timestamp', 'timestamp', 'message']);
    configured[0].dateFormat = 'iso';
    configured[1].dateFormat = 'time';
    expect(configureColumns(['message', 'timestamp', 'timestamp', 'request.at'], configured)).toEqual([
      { path: 'message', dateFormat: 'original' },
      { path: 'timestamp', dateFormat: 'iso' },
      { path: 'timestamp', dateFormat: 'time' },
      { path: 'request.at', dateFormat: 'original' },
    ]);
  });

  test('recognizes conventional date paths without matching unrelated suffixes', () => {
    expect(isDateColumnPath('timestamp')).toBe(true);
    expect(isDateColumnPath('request.created_at')).toBe(true);
    expect(isDateColumnPath('eventTime')).toBe(true);
    expect(isDateColumnPath('update')).toBe(false);
  });
});

describe('column value lookup and formatting', () => {
  const fields: Record<string, JSONValue> = {
    request: { host: 'example.test', details: { secure: true }, missing: null },
    'request.host': 'literal dotted key',
    Level: 'INFO',
    values: ['one', 2, null],
  };

  test('resolves case-sensitive top-level and nested object paths', () => {
    expect(resolveColumnValue(fields, 'request.host')).toBe('example.test');
    expect(resolveColumnValue(fields, 'request.details')).toEqual({ secure: true });
    expect(resolveColumnValue(fields, 'Level')).toBe('INFO');
    expect(resolveColumnValue(fields, 'level')).toBeUndefined();
  });

  test('distinguishes explicit null from absent or invalid traversal', () => {
    expect(resolveColumnValue(fields, 'request.missing')).toBeNull();
    expect(resolveColumnValue(fields, 'request.unknown')).toBeUndefined();
    expect(resolveColumnValue(fields, 'request..host')).toBeUndefined();
    expect(resolveColumnValue(fields, 'values.0')).toBeUndefined();
    expect(resolveColumnValue(undefined, 'request')).toBeUndefined();
  });

  test('renders scalars and compact JSON while marking absent values', () => {
    expect(formatColumnValue('message')).toBe('message');
    expect(formatColumnValue(42)).toBe('42');
    expect(formatColumnValue(false)).toBe('false');
    expect(formatColumnValue(null)).toBe('null');
    expect(formatColumnValue({ host: 'example.test' })).toBe('{"host":"example.test"}');
    expect(formatColumnValue(['one', 2, null])).toBe('["one",2,null]');
    expect(formatColumnValue(undefined)).toBe(ABSENT_COLUMN_TEXT);
  });

  test('resolves normalized row fields before the original JSON', () => {
    const row: LogRow = {
      id: '1',
      timestamp: '2026-09-05T12:34:56Z',
      severity: 'warning',
      message: 'normalized message',
      fields: { timestamp: 1788611696, severity: 'WARN', message: 'source message' },
      sourceFormat: 'json',
    };
    expect(resolveRowColumnValue(row, 'timestamp')).toBe('2026-09-05T12:34:56Z');
    expect(resolveRowColumnValue(row, 'severity')).toBe('warning');
    expect(resolveRowColumnValue(row, 'message')).toBe('normalized message');
    expect(resolveRowColumnValue(row, 'missing')).toBeUndefined();
  });

  test('falls back to the original timestamp when normalization did not recognize it', () => {
    const row: LogRow = {
      id: '1', message: 'invalid timestamp', fields: { timestamp: 'last Tuesday' }, sourceFormat: 'json',
    };
    expect(resolveRowColumnValue(row, 'timestamp')).toBe('last Tuesday');
  });

  test('formats ISO strings and Unix timestamps as dates with safe invalid-value fallback', () => {
    expect(formatColumnValue('2026-09-05T12:34:56-04:00', 'iso')).toBe('2026-09-05T16:34:56.000Z');
    expect(formatColumnValue(1788611696, 'iso')).toBe('2026-09-05T12:34:56.000Z');
    expect(formatColumnValue('2026-09-05T12:34:56Z', 'date', { locales: 'en-US', timeZone: 'UTC' }))
      .toBe('Sep 5, 2026');
    expect(formatColumnValue('not-a-date', 'local')).toBe('not-a-date');
  });

  test('renders expanded structured values for row details', () => {
    expect(formatDetailColumnValue({ host: 'example.test', secure: true })).toEqual({
      text: '{\n  "host": "example.test",\n  "secure": true\n}',
      kind: 'json',
    });
    expect(formatDetailColumnValue(['one', { nested: 2 }])).toEqual({
      text: '[\n  "one",\n  {\n    "nested": 2\n  }\n]',
      kind: 'json',
    });
  });

  test('prettifies encoded JSON containers without reinterpreting other strings', () => {
    expect(formatDetailColumnValue(' {"host":"example.test","ports":[80,443]} ')).toEqual({
      text: '{\n  "host": "example.test",\n  "ports": [\n    80,\n    443\n  ]\n}',
      kind: 'json',
    });
    expect(formatDetailColumnValue('[broken')).toEqual({ text: '[broken', kind: 'scalar' });
    expect(formatDetailColumnValue('42')).toEqual({ text: '42', kind: 'scalar' });
    expect(formatDetailColumnValue('null')).toEqual({ text: 'null', kind: 'scalar' });
  });

  test('keeps absent, null, and scalar detail values distinct', () => {
    expect(formatDetailColumnValue(undefined)).toEqual({ text: ABSENT_COLUMN_TEXT, kind: 'absent' });
    expect(formatDetailColumnValue(null)).toEqual({ text: 'null', kind: 'scalar' });
    expect(formatDetailColumnValue(false)).toEqual({ text: 'false', kind: 'scalar' });
  });

  test('supports the configured Caddy column example', () => {
    const caddy: Record<string, JSONValue> = {
      timestamp: 1788566400,
      level: 'info',
      msg: 'handled request',
      request: { host: 'example.test', method: 'GET' },
    };
    const columns = parseColumnDraft('timestamp\nlevel\nmsg\nrequest.host\nrequest');
    expect(columns.map(column => formatColumnValue(resolveColumnValue(caddy, column)))).toEqual([
      '1788566400',
      'info',
      'handled request',
      'example.test',
      '{"host":"example.test","method":"GET"}',
    ]);
  });
});

test('sample selection uses only structured rows from the current generation', () => {
  const oldPage: RowPage = {
    snapshot: snapshot('old'), offset: '0',
    rows: [{ id: '1', message: 'old', fields: { old: true }, sourceFormat: 'json' }],
  };
  const currentPage: RowPage = {
    snapshot: snapshot('current'), offset: '0',
    rows: [
      { id: '2', message: 'text', sourceFormat: 'text' },
      { id: '3', message: 'current', fields: { request: { host: 'example.test' } }, sourceFormat: 'json' },
    ],
  };
  expect(findSampleFields([oldPage, currentPage], 'current')).toEqual({ request: { host: 'example.test' } });
});
