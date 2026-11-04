const { test } = require('node:test');
const assert = require('node:assert/strict');
const { duplicateHeadings, layoutDrift } = require('./test-support/parity-checks.cjs');
test('duplicate visible h1 names survive set comparison and whitespace differences', () => {
  assert.deepEqual(duplicateHeadings([' Calendar ', 'Calendar', 'Other']), ['Calendar']);
  assert.deepEqual(duplicateHeadings(['Calendar', 'Other']), []);
});
test('frame geometry tolerates rounding and rejects a moved or missing region', () => {
  const a = { sidebar: { x: 0, y: 0, width: 224, height: 900 }, page: { x: 224, y: 8, width: 1208, height: 884 } };
  assert.deepEqual(layoutDrift(a, { ...a, page: { ...a.page, width: 1207.5 } }), []);
  assert.match(layoutDrift(a, { ...a, page: { ...a.page, x: 240 } })[0], /page.x/);
  assert.deepEqual(layoutDrift(a, { sidebar: a.sidebar }), ['page missing']);
});
