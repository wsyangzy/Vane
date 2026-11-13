import test from 'node:test';
import assert from 'node:assert/strict';
import { mapSettingRecord } from './settings.js';

test('search settings preserve selectable routes across the Wails mapping boundary', () => {
  const setting = mapSettingRecord({
    provider: 'global', category: 'search', key: 'mode', value: 'native', value_type: 'enum',
    enum_options: [
      { value: 'native', label: '原生协议转发', description: '保留原生搜索协议' },
      { value: 'service', label: '搜索服务适配', description: '使用独立搜索服务' }
    ],
    requires_restart: false
  });
  assert.equal(setting.value_type, 'enum');
  assert.deepEqual(setting.enum_options, [
    { value: 'native', label: '原生协议转发', description: '保留原生搜索协议' },
    { value: 'service', label: '搜索服务适配', description: '使用独立搜索服务' }
  ]);
  assert.equal(setting.provider, 'global');
  assert.equal(setting.requires_restart, false);
  assert.deepEqual(mapSettingRecord({ value_type: 'password', value: '' }).enum_options, []);
});
