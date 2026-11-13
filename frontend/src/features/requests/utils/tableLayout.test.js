import test from 'node:test';
import assert from 'node:assert/strict';
import { TABLE_COLUMNS, DEFAULT_VISIBLE_COLUMNS } from './constants.js';
import { auxiliaryColumnLayout, requestColumnLayout, requestRowHeight } from './tableLayout.js';

test('auxiliary descriptions span only adjacent model fields and preserve shared column coordinates', () => {
  const selections = [
    TABLE_COLUMNS,
    TABLE_COLUMNS.filter(column => DEFAULT_VISIBLE_COLUMNS.includes(column.id)),
    TABLE_COLUMNS.filter(column => ['requestId', 'timestamp', 'model', 'duration'].includes(column.id)),
    ['model', 'status', 'reasoningEffort', 'duration', 'serviceTier'].map(id => TABLE_COLUMNS.find(column => column.id === id)),
    TABLE_COLUMNS.filter(column => !['model', 'reasoningEffort'].includes(column.id)),
  ];
  for (const selection of selections) {
    const original = requestColumnLayout(selection, 1200).columns;
    const before = structuredClone(original);
    const cells = auxiliaryColumnLayout(original);
    let index = 0;
    for (const cell of cells) {
      assert.equal(cell.id, original[index].id);
      assert.equal(cell.layoutWidth, original.slice(index, index + cell.colSpan).reduce((sum, column) => sum + column.layoutWidth, 0));
      if (cell.colSpan > 1) {
        assert.equal(cell.id, 'model');
        assert.ok(original.slice(index + 1, index + cell.colSpan).every(column => ['reasoningEffort', 'serviceTier'].includes(column.id)));
      }
      index += cell.colSpan;
    }
    assert.equal(index, original.length);
    assert.deepEqual(original, before);
  }
  const cells = auxiliaryColumnLayout(requestColumnLayout(TABLE_COLUMNS, 1200).columns);
  assert.equal(cells.find(column => column.id === 'model').colSpan, 3);
  assert.equal(cells.find(column => column.id === 'status').colSpan, 1);
  assert.equal(cells.find(column => column.id === 'duration').colSpan, 1);
});

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

test('date ranges reserve a full timestamp while today gives that space to labels and icons', () => {
  const selected = TABLE_COLUMNS.filter((column) => DEFAULT_VISIBLE_COLUMNS.includes(column.id));
  for (const width of [900, 1440, 1780]) {
    const today = requestColumnLayout(selected, width, false);
    const dated = requestColumnLayout(selected, width, true);
    const column = (layout, id) => layout.columns.find((item) => item.id === id);
    assert.ok(column(dated, 'timestamp').layoutWidth > column(today, 'timestamp').layoutWidth);
    assert.ok(column(today, 'model').layoutWidth > column(dated, 'model').layoutWidth);
    for (const layout of [today, dated]) {
      assert.ok(column(layout, 'model').layoutWidth > column(layout, 'serviceTier').layoutWidth);
      assert.ok(Math.abs(layout.columns.reduce((sum, item) => sum + item.layoutWidth, 0) - width) < 0.001);
    }
  }
});

test('complete request pages fill the viewport while short pages preserve density', () => {
  for (const height of [480, 640, 900]) {
    const rowHeight = requestRowHeight(height, 36, 10);
    assert.ok(rowHeight * 10 + 36 <= height);
    assert.ok(height - (rowHeight * 10 + 36) <= 1.001, 'only the table border and rounding allowance may remain');
    assert.ok(rowHeight * 3 + 36 < height, 'a short last page must not enlarge its remaining rows');
  }
  assert.equal(requestRowHeight(300, 36, 10), 40);
  assert.equal(requestRowHeight(700, 36, 50), 40);
  assert.equal(requestRowHeight(0, 0, 10), 40);
});
