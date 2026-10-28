import test from 'node:test';
import assert from 'node:assert/strict';
import { TABLE_COLUMNS, DEFAULT_VISIBLE_COLUMNS } from './constants.js';
import { requestColumnLayout } from './tableLayout.js';

test('resizing fits every selected field into one row without horizontal scrolling', () => {
  for (const selected of [TABLE_COLUMNS.filter((column) => DEFAULT_VISIBLE_COLUMNS.includes(column.id)), TABLE_COLUMNS]) {
    for (const width of [360, 620, 768, 900, 1100, 1200, 1600, 2200]) {
      const { columns, tableWidth } = requestColumnLayout(selected, width);
      const ids = columns.map(({ id }) => id);
      assert.deepEqual(ids, selected.map(({ id }) => id));
      assert.equal(new Set(ids).size, ids.length);
      assert.ok(Math.abs(columns.reduce((sum, column) => sum + column.layoutWidth, 0) - tableWidth) < 0.001);
      assert.ok(columns.every(column => column.layoutWidth > 0));
      assert.equal(tableWidth, width);
      assert.ok(columns.find(column => column.id === 'model').layoutWidth > columns.find(column => column.id === 'provider').layoutWidth);
    }
  }
});

test('responsive layout respects hidden fields and does not mutate selection or configured widths', () => {
  const selected = TABLE_COLUMNS.filter(({ id }) => ['requestId', 'timestamp', 'cost'].includes(id));
  const before = structuredClone(selected);
  assert.deepEqual(requestColumnLayout(selected, 700).columns.map(({ id }) => id), ['requestId', 'timestamp', 'cost']);
  assert.deepEqual(selected, before);
  assert.deepEqual(requestColumnLayout([], 900), { columns: [], tableWidth: 0 });
});
