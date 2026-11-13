import assert from 'node:assert/strict';
import test from 'node:test';
import { isSystemSetting, runtimeSettingSections, settingsForRuntimeSection, sharedRuntimeSettingSections } from './settingsSections.js';

test('system settings contain application controls and exclude provider-specific and legacy controls', () => {
  for (const category of ['server', 'auth', 'retention', 'logging', 'cache']) {
    assert.equal(isSystemSetting({ provider: 'global', category }), true);
    assert.equal(isSystemSetting({ provider: 'claude', category }), false);
    assert.equal(isSystemSetting({ provider: 'codex', category }), false);
  }
  for (const category of ['token_counting', 'failover', 'strategy', 'streaming', 'search', 'unknown']) {
    assert.equal(isSystemSetting({ provider: 'global', category }), false);
  }
  assert.equal(isSystemSetting({ category: 'auth' }), true);
});

test('shared runtime controls have one system settings entry and token estimation stays under Claude', () => {
  assert.deepEqual(sharedRuntimeSettingSections.map(section => section.category), ['request', 'health', 'retry']);
  for (const section of sharedRuntimeSettingSections) {
    for (const key of section.keys) {
      assert.equal(isSystemSetting({ provider: 'global', category: section.category, key }), true);
      assert.equal(isSystemSetting({ provider: 'claude', category: section.category, key }), false);
      assert.equal(isSystemSetting({ provider: 'codex', category: section.category, key }), false);
    }
    assert.equal(isSystemSetting({ provider: 'global', category: section.category, key: 'legacy_key' }), false);
  }
  assert.deepEqual(runtimeSettingSections('claude').map(section => section.category), ['token_counting']);
  assert.deepEqual(runtimeSettingSections('codex'), []);
});

test('runtime sections retain stored values and reject legacy keys or provider overrides', () => {
  const section = sharedRuntimeSettingSections.find(section => section.category === 'request');
  const settings = [
    { provider: 'global', category: 'request', key: 'default_response_header_timeout', value: '77s' },
    { provider: 'codex', category: 'request', key: 'global_timeout', value: '2s' },
    { provider: 'global', category: 'request', key: 'global_timeout', value: '420s' },
    { category: 'request', key: 'response_header_timeout', value: '330s' },
    { provider: 'global', category: 'request', key: 'suspend_wait_timeout', value: '120s' },
    { provider: 'global', category: 'health', key: 'timeout', value: '9s' },
  ];
  const before = structuredClone(settings);
  assert.deepEqual(settingsForRuntimeSection(settings, section).map(setting => [setting.key, setting.value]), [
    ['global_timeout', '420s'], ['response_header_timeout', '330s'], ['default_response_header_timeout', '77s'],
  ]);
  assert.deepEqual(settings, before);
});
