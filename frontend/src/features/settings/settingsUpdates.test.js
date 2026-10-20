import assert from 'node:assert/strict';
import test from 'node:test';

import { buildSettingsUpdates } from './settingsUpdates.js';

test('settings changes keep the provider separate from category and key', () => {
  assert.deepEqual(buildSettingsUpdates({
    'global.auth.enabled': false,
    'global.server.preferred_port': 8087,
    'global.logging.level': 'warn',
    'global.cache.usage_stats.enabled': true,
  }), [
    { provider: 'global', category: 'auth', key: 'enabled', value: 'false' },
    { provider: 'global', category: 'server', key: 'preferred_port', value: '8087' },
    { provider: 'global', category: 'logging', key: 'level', value: 'warn' },
    { provider: 'global', category: 'cache', key: 'usage_stats.enabled', value: 'true' },
  ]);
});

test('one save keeps Claude and Codex failover changes in their own scopes', () => {
  assert.deepEqual(buildSettingsUpdates({
    'claude.failover.enabled': 'true',
    'codex.failover.enabled': 'false',
    'codex.failover.max_loops': '3',
  }), [
    { provider: 'claude', category: 'failover', key: 'enabled', value: 'true' },
    { provider: 'codex', category: 'failover', key: 'enabled', value: 'false' },
    { provider: 'codex', category: 'failover', key: 'max_loops', value: '3' },
  ]);
  assert.deepEqual(buildSettingsUpdates({}), []);
});
