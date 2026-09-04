import type { RowPage } from './types';

interface Entry { page: RowPage; bytes: number; countAtFetch: bigint }

/** Approximate-size LRU for immutable row pages, keyed by query and page coordinates. */
export class PageCache {
  private entries = new Map<string, Entry>();
  private bytes = 0;

  /** Creates an LRU cache with an approximate UTF-16 memory budget. */
  constructor(private readonly maxBytes = 32 * 1024 * 1024) {}

  /** Returns a reusable full page, or a partial tail only when the result count is unchanged. */
  get(queryId: string, offset: bigint, limit: number, snapshotCount: bigint): RowPage | undefined {
    const key = `${queryId}:${offset}:${limit}`;
    const entry = this.entries.get(key);
    if (!entry || (entry.page.rows.length < limit && entry.countAtFetch !== snapshotCount)) return undefined;
    this.entries.delete(key);
    this.entries.set(key, entry);
    return { ...entry.page, rows: [...entry.page.rows] };
  }

  /** Stores a page and evicts least-recently-used entries until it fits the budget. */
  set(page: RowPage, limit: number) {
    const key = `${page.snapshot.queryId}:${page.offset}:${limit}`;
    const bytes = JSON.stringify(page).length * 2;
    const old = this.entries.get(key);
    if (old) this.bytes -= old.bytes;
    this.entries.delete(key);
    this.entries.set(key, { page, bytes, countAtFetch: BigInt(page.snapshot.matchedCount) });
    this.bytes += bytes;
    while (this.bytes > this.maxBytes && this.entries.size > 1) {
      const oldest = this.entries.keys().next().value as string;
      const removed = this.entries.get(oldest)!;
      this.entries.delete(oldest);
      this.bytes -= removed.bytes;
    }
  }

  /** Removes every cached window owned by a released query. */
  deleteQuery(queryId: string) {
    for (const [key, entry] of this.entries) {
      if (key.startsWith(`${queryId}:`)) { this.entries.delete(key); this.bytes -= entry.bytes; }
    }
  }

  /** Releases the entire client-side row cache. */
  clear() { this.entries.clear(); this.bytes = 0; }
}
