import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const settingsSource = readFileSync(new URL('./index.jsx', import.meta.url), 'utf8');
const settingItemSource = readFileSync(new URL('./components/SettingItem.jsx', import.meta.url), 'utf8');

test('retention UI keeps only retention days as a user setting', () => {
  assert.doesNotMatch(settingsSource, /max_active_database_size_mb|cleanup_interval/);
  assert.doesNotMatch(settingItemSource, /max_active_database_size_mb|活动数据库大小上限/);
  assert.match(settingItemSource, /setting\.key === 'retention_days' \? 0 : undefined/);
});

test('conditional settings do not change category cards to full-width rows', () => {
  assert.doesNotMatch(settingsSource, /categorySettings\.length > 4/);
  assert.doesNotMatch(settingsSource, /isWideCategory \? 'lg:col-span-2'/);
  assert.match(settingsSource, /categorySettings\.map\(setting =>/);
});
