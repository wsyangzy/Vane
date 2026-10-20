import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

import {
  formatDateTimeLocal,
  isDayWithinRange,
  isValidDateTimeRange,
  parseDateTimeLocal
} from './dateTimeRange.js';

const filtersPanelSource = readFileSync(
  new URL('../components/FiltersPanel.jsx', import.meta.url),
  'utf8'
);

test('date-time range values round-trip without changing local time', () => {
  const value = '2026-07-31T23:59';
  const parsed = parseDateTimeLocal(value);

  assert.ok(parsed);
  assert.equal(formatDateTimeLocal(parsed), value);
});

test('date-time range parsing rejects normalized invalid dates', () => {
  assert.equal(parseDateTimeLocal('2026-02-30T12:00'), null);
  assert.equal(parseDateTimeLocal('2026-07-31T24:00'), null);
  assert.equal(parseDateTimeLocal('not-a-date'), null);
});

test('range validation and day highlighting respect both boundaries', () => {
  const start = parseDateTimeLocal('2026-07-08T00:00');
  const middle = parseDateTimeLocal('2026-07-16T12:00');
  const end = parseDateTimeLocal('2026-07-24T23:59');

  assert.equal(isValidDateTimeRange(start, end), true);
  assert.equal(isValidDateTimeRange(end, start), false);
  assert.equal(isDayWithinRange(middle, start, end), true);
  assert.equal(isDayWithinRange(start, start, end), false);
  assert.equal(isDayWithinRange(end, start, end), false);
});

test('advanced filters clamp their anchored panel within the viewport', () => {
  assert.match(filtersPanelSource, /const viewportMargin = 16/);
  assert.match(filtersPanelSource, /style=\{\{ translate: `\$\{horizontalOffset\}px 0` \}\}/);
});
