import test from 'node:test';
import assert from 'node:assert/strict';
import { buildUsageCalendar, heatmapRange } from './usageHeatmap.js';

const today = new Date(2026, 9, 27, 12);
test('usage heatmap uses actual Monday-based calendar days and future placeholders', () => {
  const calendar = buildUsageCalendar([{ date: '2026-10-27', total: 120, requestCount: 2 }], 'tokens', today);
  assert.equal(calendar.days.length, 371);
  assert.equal(calendar.days[0].date, '2025-10-27');
  const day = calendar.days.find((day) => day.date === '2026-10-27');
  assert.equal(day.week, 52);
  assert.equal(day.weekday, 1);
  assert.equal(day.value, 120);
  assert.equal(calendar.days.filter((day) => day.future).length, 5);
  assert.equal(calendar.activeDays, 1);
  assert.equal(calendar.days.find((day) => day.date === '2026-10-26').value, 0);
});

test('request metric and zero days use their own levels without stale token intensity', () => {
  const data = [{ date: '2026-10-25', total: 99999, requestCount: 1 }, { date: '2026-10-26', total: 10, requestCount: 100 }, { date: '2026-10-27', total: 0, requestCount: 0 }];
  const calendar = buildUsageCalendar(data, 'requests', today);
  assert.equal(calendar.total, 101);
  assert.equal(calendar.peak, 100);
  assert.equal(calendar.activeDays, 2);
  assert.equal(calendar.days.find((day) => day.date === '2026-10-27').level, 0);
  assert.ok(calendar.days.find((day) => day.date === '2026-10-26').level > calendar.days.find((day) => day.date === '2026-10-25').level);
});

test('calendar handles leap days and year boundaries without stretching sample positions', () => {
  const now = new Date(2024, 2, 1);
  const calendar = buildUsageCalendar([], 'tokens', now);
  const leap = calendar.days.find((day) => day.date === '2024-02-29');
  assert.equal(leap.weekday, 3);
  assert.equal(calendar.days.filter((day) => day.date === '2024-02-29').length, 1);
  assert.equal(heatmapRange(new Date(2026, 0, 1)).start.getDay(), 1);
  assert.ok(calendar.months.every((item, index) => !index || item.week - calendar.months[index - 1].week >= 3));
});

test('activity days remain stable when requests have no reported tokens', () => {
  const data = [{ date: '2026-10-27', total: 0, requestCount: 2 }];
  assert.equal(buildUsageCalendar(data, 'tokens', today).activeDays, 1);
  assert.equal(buildUsageCalendar(data, 'requests', today).activeDays, 1);
});
