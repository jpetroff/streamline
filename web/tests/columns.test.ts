import { describe, expect, test } from 'bun:test';
import {
  ABSENT_COLUMN_TEXT,
  findSampleFields,
  formatColumnValue,
  parseColumnDraft,
  resolveColumnValue,
} from '../src/lib/columns';
import type { JSONValue, RowPage, Snapshot } from '../src/lib/transport/types';

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
