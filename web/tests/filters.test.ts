import { expect, test } from 'bun:test';
import { cloneFilters, FILTER_OPERATORS, filtersEqual, isNumericOperator, parseFilterJSON, validateFilters, visibleFilterErrors } from '../src/lib/filters';
import type { FilterSpec } from '../src/lib/transport/types';

test('filter JSON round trips preserve order, duplicates, value types, and embedded newlines', () => {
  const filters: FilterSpec[] = [{ field: 'name', op: 'regex', value: '^a\nb$' }, { field: 'n', op: 'gte', value: 1.5 }, { field: 'name', op: 'eq', value: '' }, { field: 'n', op: 'gte', value: 1.5 }];
  expect(parseFilterJSON(JSON.stringify(filters, null, 2))).toEqual({ filters, errors: [] });
  expect(parseFilterJSON('[]')).toEqual({ filters: [], errors: [] });
  expect(filtersEqual(filters, cloneFilters(filters))).toBe(true);
  expect(filtersEqual(filters, [...filters].reverse())).toBe(false);
});

test('invalid imports never produce a replacement draft', () => {
  for (const json of ['[', 'null', '{}', '[null]', '[[]]', '[1]', '[{}]', '[{"field":"a","op":"eq"}]', '[{"field":"a","op":"eq","value":"x","extra":1}]']) {
    const result = parseFilterJSON(json);
    expect(result.filters).toBeUndefined();
    expect(result.errors.length).toBeGreaterThan(0);
  }
});

test('validation checks specification types without requiring known fields', () => {
  expect(validateFilters([{ field: 'unknown.path', op: 'gt', value: 10 }])).toEqual([]);
  expect(validateFilters([{ field: 'a', op: 'eq', value: 10 }, { field: 'n', op: 'gt', value: '10' }, { field: 'a..b', op: 'bad', value: '' }]).map(error => [error.index, error.property])).toEqual([[1, 'value'], [2, 'value'], [3, 'field'], [3, 'op']]);
  for (const value of [NaN, Infinity, -Infinity, null, false]) expect(validateFilters([{ field: 'n', op: 'lt', value }])).toHaveLength(1);
  for (const field of ['', ' ', '.a', 'a.', 'a..b']) expect(validateFilters([{ field, op: 'eq', value: '' }])).toHaveLength(1);
});

test('filter regex uses search syntax validation, leaving JS-only syntax to the server', () => {
  expect(validateFilters([{ field: 'a', op: 'regex', value: '[' }])[0]).toMatchObject({ index: 1, property: 'value' });
  expect(validateFilters([{ field: 'a', op: 'regex', value: '(?=x)' }])).toEqual([]);
  expect(validateFilters([{ field: 'a', op: 'contains', value: '[' }])).toEqual([]);
});

test('server errors disappear for relevant edits while retaining unrelated errors', () => {
  const filters: FilterSpec[] = [{ field: 'a', op: 'regex', value: '(?=x)' }, { field: 'b', op: 'eq', value: 'x' }];
  const rejection = { filters: cloneFilters(filters), errors: [{ index: 1, property: 'value', message: 'Unsupported' }] };
  filters[1].value = 'other';
  expect(visibleFilterErrors(filters, rejection)).toEqual(rejection.errors);
  filters[0].value = 'x';
  expect(visibleFilterErrors(filters, rejection)).toEqual([]);
  expect(visibleFilterErrors([], rejection)).toEqual([]);
});

test('negative filters round trip and retain their operand types', () => {
  const filters: FilterSpec[] = [
    { field: 'level', op: 'neq', value: 'error' },
    { field: 'message', op: 'not_contains', value: '[' },
    { field: 'message', op: 'not_regex', value: '^a\nb$' },
    { field: 'n', op: 'not_gt', value: 1.5 },
    { field: 'n', op: 'not_gte', value: 1.5 },
    { field: 'n', op: 'not_lt', value: 1.5 },
    { field: 'n', op: 'not_lte', value: 1.5 },
  ];
  expect(parseFilterJSON(JSON.stringify(filters))).toEqual({ filters, errors: [] });
  for (const filter of filters) {
    expect(FILTER_OPERATORS.some(operator => operator.value === filter.op)).toBe(true);
    expect(isNumericOperator(filter.op)).toBe(typeof filter.value === 'number');
    for (const value of [null, false, {}, [], typeof filter.value === 'number' ? '1.5' : 1.5]) {
      expect(validateFilters([{ ...filter, value }])).toMatchObject([{ index: 1, property: 'value' }]);
    }
  }
});

test('negative regex is validated while negative literal operators allow punctuation', () => {
  expect(parseFilterJSON(JSON.stringify([{ field: 'a', op: 'not_regex', value: '[' }])).filters).toBeUndefined();
  expect(validateFilters([{ field: 'a', op: 'not_regex', value: '[' }])).toMatchObject([{ index: 1, property: 'value' }]);
  for (const op of ['neq', 'not_contains']) expect(validateFilters([{ field: 'a', op, value: '[' }])).toEqual([]);
  for (const value of ['', '(?=x)']) expect(validateFilters([{ field: 'a', op: 'not_regex', value }])).toEqual([]);
});
