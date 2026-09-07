import type { LogRow } from './transport/types';
export interface ActiveRow {
  queryId: string;
  generationId: string;
  offset: bigint;
  row?: LogRow;
}
export interface RowNavigator { navigate: (offset: bigint, focus?: boolean) => Promise<void>; focus: () => Promise<void>; }
export function clampRow(offset: bigint, total: bigint): bigint | undefined {
  if (total <= 0n) return undefined;
  return offset < 0n ? 0n : offset >= total ? total - 1n : offset;
}
export function parseRowJump(value: string, total: bigint): bigint | undefined {
  if (!/^\d+$/.test(value.trim())) return undefined;
  const position = BigInt(value.trim());
  return position > 0n && position <= total ? position - 1n : undefined;
}
