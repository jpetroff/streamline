import { validateRegex } from './search';
import type { FilterError, FilterSpec } from './transport/types';

export const FILTER_OPERATORS = [
  { value: 'eq', label: 'Equals' }, { value: 'neq', label: 'Is not equal' },
  { value: 'contains', label: 'Contains' }, { value: 'not_contains', label: 'Does not contain' },
  { value: 'regex', label: 'Matches regex' }, { value: 'not_regex', label: 'Does not match regex' },
  { value: 'gt', label: 'Greater than' }, { value: 'not_gt', label: 'Not greater than' },
  { value: 'gte', label: 'Greater or equal' }, { value: 'not_gte', label: 'Not greater or equal' },
  { value: 'lt', label: 'Less than' }, { value: 'not_lt', label: 'Not less than' },
  { value: 'lte', label: 'Less or equal' }, { value: 'not_lte', label: 'Not less or equal' },
] as const;

export function isNumericOperator(op: string) { return ['gt', 'gte', 'lt', 'lte', 'not_gt', 'not_gte', 'not_lt', 'not_lte'].includes(op); }
export function isRegexOperator(op: string) { return op === 'regex' || op === 'not_regex'; }
export function cloneFilters(filters: readonly FilterSpec[]): FilterSpec[] { return filters.map(filter => ({ ...filter })); }
export function filtersEqual(left: readonly FilterSpec[], right: readonly FilterSpec[]) {
  return left.length === right.length && left.every((filter, index) => {
    const other = right[index];
    return filter.field === other.field && filter.op === other.op && filter.value === other.value;
  });
}

/** Validates arbitrary imported JSON as well as builder drafts without sampling log types. */
export function validateFilters(value: unknown): FilterError[] {
  if (!Array.isArray(value)) return [{ index: 0, property: '', message: 'Filters must be a JSON array.' }];
  const errors: FilterError[] = [];
  value.forEach((filter: unknown, position) => {
    const index = position + 1;
    const report = (property: string, message: string) => errors.push({ index, property, message });
    if (!filter || typeof filter !== 'object' || Array.isArray(filter)) { report('', 'Each filter must be an object.'); return; }
    const item = filter as Record<string, unknown>;
    for (const property of Object.keys(item)) {
      if (!['field', 'op', 'value'].includes(property)) report(property, 'Unknown filter property.');
    }
    if (typeof item.field !== 'string' || !item.field.trim() || item.field.split('.').some(segment => !segment)) report('field', 'Enter a nonempty dotted object path.');
    if (!FILTER_OPERATORS.some(operator => operator.value === item.op)) { report('op', 'Choose a supported operator.'); return; }
    if (isNumericOperator(item.op as string)) {
      if (typeof item.value !== 'number' || !Number.isFinite(item.value)) report('value', 'Numeric operators require a finite number value.');
    } else if (typeof item.value !== 'string') report('value', 'Text operators require a string value.');
    else if (isRegexOperator(item.op as string)) {
      const message = validateRegex(item.value);
      if (message) report('value', message);
    }
  });
  return errors;
}

export function parseFilterJSON(text: string): { filters?: FilterSpec[]; errors: FilterError[] } {
  let value: unknown;
  try { value = JSON.parse(text); }
  catch { return { errors: [{ index: 0, property: '', message: 'Enter valid JSON.' }] }; }
  const errors = validateFilters(value);
  return errors.length ? { errors } : { filters: cloneFilters(value as FilterSpec[]), errors: [] };
}

export interface FilterRejection { filters: FilterSpec[]; errors: FilterError[] }
export function visibleFilterErrors(draft: readonly FilterSpec[], rejection?: FilterRejection): FilterError[] {
  return rejection?.errors.filter(error => {
    const before = rejection.filters[error.index - 1];
    const now = draft[error.index - 1];
    return before && now && filtersEqual([before], [now]);
  }) ?? [];
}
