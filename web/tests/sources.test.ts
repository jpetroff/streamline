import { expect, spyOn, test } from 'bun:test';
import { HTTPSourceAPI } from '../src/lib/transport/sources';
import { HTTPQueryAPI, TransportError } from '../src/lib/transport/api';

test('command requests preserve shell text and require explicit JSON mutation headers', async () => {
  const request = { command: 'value="a b"; printf "%s\\n" "$value" | cat', mode: 'text' as const };
  const fetch = spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ id: 'source-1' }), { status: 202 }));
  try {
    await new HTTPSourceAPI().create(request);
    const [url, options] = fetch.mock.calls[0];
    expect(url).toBe('/api/v1/sources');
    expect(options?.headers).toEqual({ 'Content-Type': 'application/json', 'X-Streamline-Request': '1' });
    expect(JSON.parse(options?.body as string)).toEqual(request);
    fetch.mockResolvedValue(new Response(null, { status: 204 }));
    await new HTTPSourceAPI().stop('source-1');
    expect(fetch.mock.calls[1][0]).toBe('/api/v1/sources/source-1/stop');
    await new HTTPSourceAPI().remove('source-1');
    expect(fetch.mock.calls[2][1]?.method).toBe('DELETE');
  } finally { fetch.mockRestore(); }
});

test('source errors stay structured and query requests remain scoped to their source', async () => {
  const fetch = spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ error: { code: 'invalid_command', message: 'Empty command' } }), { status: 400 }));
  try {
    try { await new HTTPSourceAPI().create({ command: '', mode: 'auto' }); throw new Error('expected rejection'); }
    catch (error) { expect(error).toBeInstanceOf(TransportError); expect((error as TransportError).code).toBe('invalid_command'); }
    fetch.mockResolvedValue(new Response(JSON.stringify({}), { status: 200 }));
    await new HTTPQueryAPI('/api/v1/sources/source-2').rows('query-1', 'snapshot-1', 0n, 200);
    expect(fetch.mock.calls[1][0]).toBe('/api/v1/sources/source-2/queries/query-1/rows?snapshot=snapshot-1&offset=0&limit=200');
  } finally { fetch.mockRestore(); }
});
