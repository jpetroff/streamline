import { expect, test } from 'bun:test';
import { EMPTY_SEARCH, normalizeSearchText, searchesEqual, searchLines, validateSearch, visibleServerErrors } from '../src/lib/search';

test('line parsing preserves significant whitespace and physical numbering', () => {
  expect(searchLines('\r\n first \r\n \t\rsecond')).toEqual([{ line: 2, text: ' first ' }, { line: 4, text: 'second' }]);
  expect(normalizeSearchText('one\rtwo\r\nthree')).toBe('one\ntwo\nthree');
});

test('native validation reports every invalid physical line and treats plain text literally', () => {
  const draft = { ...EMPTY_SEARCH, text: 'ok\n\n[\n(' };
  expect(validateSearch(draft)).toEqual([]);
  const errors = validateSearch({ ...draft, mode: 'regexp' });
  expect(errors.map(error => error.line)).toEqual([3, 4]);
  expect(errors.every(error => error.message.length > 0)).toBe(true);
  expect(validateSearch({ ...draft, mode: 'regexp', text: '\n \t\n' })).toEqual([]);
});

test('native validation accepts JS-only expressions for authoritative backend validation', () => {
  expect(validateSearch({ ...EMPTY_SEARCH, mode: 'regexp', text: '(?=x)\n(x)\\1' })).toEqual([]);
  expect(validateSearch({ ...EMPTY_SEARCH, mode: 'regexp', text: 'timeout|refused\nstatus=[45][0-9]{2}' })).toEqual([]);
});

test('server errors survive unrelated edits but disappear for edited expressions or plain mode', () => {
  const search = { ...EMPTY_SEARCH, mode: 'regexp' as const, text: 'ok\n(?=x)' };
  const rejection = { search, errors: [{ line: 2, message: 'unsupported' }] };
  expect(visibleServerErrors({ ...search, text: 'changed\n(?=x)', operator: 'and' }, rejection)).toEqual(rejection.errors);
  expect(visibleServerErrors({ ...search, text: 'ok\nx' }, rejection)).toEqual([]);
  expect(visibleServerErrors({ ...search, mode: 'plain' }, rejection)).toEqual([]);
  expect(searchesEqual(search, { ...search, text: 'ok\r\n(?=x)' })).toBe(true);
  expect(searchesEqual(search, { ...search, operator: 'and' })).toBe(false);
});
