import test from 'node:test';
import assert from 'node:assert/strict';
import { mapClientConfigBackups } from './client-config-backups.js';

test('backup bridge maps metadata and keeps configuration contents outside the UI', () => {
  const result = mapClientConfigBackups({
    directory: 'C:\\Vane\\backup', default_directory: 'C:\\Vane\\backup',
    revision: 'current', config_path: 'C:\\client\\config.toml',
    records: [{ id: 'snapshot', name: '首次接入', directory: 'C:\\Vane\\backup\\20261123-090000_codex_snapshot', created_at: '2026-11-23T01:00:00Z', provider: 'codex',
      operation: 'model_settings', config_path: 'C:\\client\\config.toml', exists: true, size: 123,
      snapshot: { data: 'secret' }, content: 'private', revision: 'not-for-ui' }],
  });
  assert.equal(result.configPath, 'C:\\client\\config.toml');
  assert.equal(result.revision, 'current');
  assert.deepEqual(result.records[0], { id: 'snapshot', name: '首次接入', directory: 'C:\\Vane\\backup\\20261123-090000_codex_snapshot', createdAt: '2026-11-23T01:00:00Z',
    provider: 'codex', operation: 'model_settings', configPath: 'C:\\client\\config.toml', exists: true, size: 123 });
});

test('empty history maps to an empty record list', () => {
  assert.deepEqual(mapClientConfigBackups().records, []);
});
