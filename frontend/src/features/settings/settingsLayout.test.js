import assert from 'node:assert/strict';
import test from 'node:test';
import { sharedRuntimeSettingSections } from '../../shared/lib/settingsSections.js';
import { settingsCardLayouts, settingsInOrder } from './settingsLayout.js';

test('shared settings stay reachable exactly once within their original category', () => {
  for (const section of sharedRuntimeSettingSections) {
    const layout = settingsCardLayouts[section.category];
    const keys = [...layout.primary, ...layout.pages.flatMap(page => page.keys)];
    assert.equal(new Set(keys).size, keys.length, `${section.category} duplicates a control`);
    assert.deepEqual([...keys].sort(), [...section.keys].sort(), `${section.category} loses a control`);
  }
});

test('detail grouping preserves stored identity, false and zero values, and tolerates older settings', () => {
  const settings = [
    { category: 'request', key: 'codex_history_persistence', value: 'false' },
    { category: 'request', key: 'global_timeout', value: '0s' },
    { category: 'request', key: 'response_header_timeout', value: '90s' },
  ];
  const before = structuredClone(settings);
  const result = settingsInOrder(settings, ['global_timeout', 'codex_history_directory', 'codex_history_persistence']);
  assert.deepEqual(result.map(setting => setting.value), ['0s', 'false']);
  assert.equal(result[0], settings[1]);
  assert.equal(result[1], settings[0]);
  assert.deepEqual(settings, before);
});
