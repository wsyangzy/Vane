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
  assert.match(settingsSource, /\{categorySettings\.map\(setting =>/);
});

test('settings cards use grouped alignment around the tall cache card', () => {
  assert.match(settingsSource, /const BALANCED_CATEGORY_ORDER = \[[\s\S]*'token_counting',[\s\S]*'strategy',[\s\S]*'failover',[\s\S]*'cache',[\s\S]*'retention'/);
  assert.doesNotMatch(settingsSource, /settingCategoryColumns/);
  assert.match(settingsSource, /leadingSettingCategories\.map\(category => renderSettingCategory\(category\)\)/);
  assert.match(settingsSource, /className="contents lg:flex lg:h-full lg:min-w-0 lg:flex-col lg:gap-6"/);
  assert.match(settingsSource, /className="order-2 min-w-0 lg:flex-1"[\s\S]*get\('retention'\), 'lg:h-full'/);
  assert.match(settingsSource, /className="order-1 min-w-0 lg:h-full lg:order-none"[\s\S]*get\('cache'\), 'lg:h-full'/);
  assert.match(settingsSource, /trailingSettingCategories\.map\(category => renderSettingCategory\(category\)\)/);
});

test('failover and retention stretch to the cache card height on desktop', () => {
  assert.match(settingsSource, /const renderSettingCategory = \(category, className = ''\)/);
  assert.match(settingsSource, /className=\{className\}/);
  assert.match(settingsSource, /lg:grid-cols-2 lg:items-stretch/);
  assert.equal((settingsSource.match(/lg:flex-1/g) || []).length, 2);
  assert.equal((settingsSource.match(/renderSettingCategory\(settingCategoriesByName\.get\('(failover|retention|cache)'\), 'lg:h-full'\)/g) || []).length, 3);
});

test('timezone icon does not shadow the native Map constructor', () => {
  assert.match(settingsSource, /Map as MapIcon/);
  assert.match(settingsSource, /timezone: MapIcon/);
  assert.match(settingsSource, /const settingCategoriesByName = new Map\(/);
  assert.doesNotMatch(settingsSource, /timezone: Map,/);
});
