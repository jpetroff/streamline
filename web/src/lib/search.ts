import type { LineError, SearchSpec } from './transport/types';

export const EMPTY_SEARCH: SearchSpec = { text: '', mode: 'plain', operator: 'or' };

/** Keeps physical line numbers stable across pasted platform line endings. */
export function normalizeSearchText(text: string): string {
  return text.replace(/\r\n?/g, '\n');
}

/** Blank lines are inactive; significant whitespace is part of the expression. */
export function searchLines(text: string) {
  return normalizeSearchText(text).split('\n')
    .map((text, index) => ({ text, line: index + 1 }))
    .filter(({ text }) => text.trim() !== '');
}

/** Syntax checking only: never run a user expression against browser log data.
 * Go independently compiles confirmed expressions and may reject JS-only syntax.
 */
export function validateSearch(search: SearchSpec): LineError[] {
  if (search.mode === 'plain') return [];
  const errors: LineError[] = [];
  for (const { text, line } of searchLines(search.text)) {
    const message = validateRegex(text);
    if (message) errors.push({ line, message });
  }
  return errors;
}

export function searchesEqual(left: SearchSpec, right: SearchSpec): boolean {
  return normalizeSearchText(left.text) === normalizeSearchText(right.text)
    && left.mode === right.mode && left.operator === right.operator;
}

export interface SearchRejection {
  search: SearchSpec;
  errors: LineError[];
}

/** Retain server errors only while the expression at that line is unchanged.
 * Operator changes cannot fix a syntax error; switching to Plain can.
 */
export function visibleServerErrors(draft: SearchSpec, rejection?: SearchRejection): LineError[] {
  if (!rejection || draft.mode !== 'regexp' || rejection.search.mode !== draft.mode) return [];
  const current = normalizeSearchText(draft.text).split('\n');
  const submitted = normalizeSearchText(rejection.search.text).split('\n');
  return rejection.errors.filter(error => current[error.line - 1] === submitted[error.line - 1]);
}

/** Shared syntax-only check; Go remains authoritative for supported expressions. */
export function validateRegex(text: string): string | undefined {
  try { new RegExp(text, 'iu'); }
  catch (error) { return error instanceof Error ? error.message : 'Invalid regular expression'; }
}
