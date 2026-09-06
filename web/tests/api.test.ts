import { expect, spyOn, test } from 'bun:test';
import { HTTPQueryAPI, TransportError } from '../src/lib/transport/api';

test('HTTP query commands carry search and preserve structured backend diagnostics', async () => {
  const lineErrors = [{ line: 3, message: 'unsupported lookaround' }];
  const fetch = spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ error: { code: 'invalid_search', message: 'Invalid search', lineErrors } }), { status: 400 }));
  try {
    const spec = { filter: [{ field: 'level', op: 'eq' as const, value: 'permanent' }], sort: 'input' as const, search: { text: 'ok\n\n(?=x)', mode: 'regexp' as const, operator: 'and' as const } };
    let caught: unknown;
    try { await new HTTPQueryAPI().create(spec); } catch (error) { caught = error; }
    expect(caught).toBeInstanceOf(TransportError);
    expect((caught as TransportError).lineErrors).toEqual(lineErrors);
    expect(JSON.parse(fetch.mock.calls[0][1]!.body as string)).toEqual(spec);
  } finally { fetch.mockRestore(); }
});

test('filter tuples and indexed property errors survive the HTTP transport', async () => {
  const filterErrors = [{ index: 2, property: 'value', message: 'Unsupported expression' }];
  const fetch = spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ error: { code: 'invalid_filter', message: 'Invalid filters', filterErrors } }), { status: 400 }));
  try {
    const spec = { filter: [{ field: 'n', op: 'gt' as const, value: 100 }, { field: 'name', op: 'regex' as const, value: '(?=x)' }], sort: 'input' as const };
    try { await new HTTPQueryAPI().create(spec); throw new Error('Expected rejection'); }
    catch (error) { expect(error).toBeInstanceOf(TransportError); expect((error as TransportError).filterErrors).toEqual(filterErrors); }
    expect(JSON.parse(fetch.mock.calls[0][1]!.body as string)).toEqual(spec);
  } finally { fetch.mockRestore(); }
});
