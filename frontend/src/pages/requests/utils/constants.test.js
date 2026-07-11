import test from 'node:test';
import assert from 'node:assert/strict';

import { DEFAULT_VISIBLE_COLUMNS, TABLE_COLUMNS } from './constants.js';

test('request type column exists but stays hidden by default', () => {
  const requestTypeColumn = TABLE_COLUMNS.find((column) => column.id === 'requestType');

  assert.ok(requestTypeColumn, 'requestType column should be registered');
  assert.equal(requestTypeColumn.label, '请求类型');
  assert.equal(requestTypeColumn.alwaysVisible, false);
  assert.equal(DEFAULT_VISIBLE_COLUMNS.includes('requestType'), false);
});

test('model column keeps its natural width for complete single-line model names', () => {
  const modelColumn = TABLE_COLUMNS.find((column) => column.id === 'model');

  assert.ok(modelColumn, 'model column should be registered');
  assert.equal(modelColumn.width, 'auto');
});
