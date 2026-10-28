import test from 'node:test';
import assert from 'node:assert/strict';
import { normalizePrivacySettings, denormalizePrivacySettings, normalizePrivacyActivity, mapPrivacyRequestPreview } from './privacy.js';

test('privacy settings map runtime options without reintroducing legacy matching overrides', () => {
  const dto = { mode: 'redact', scan_max_bytes: 4194304, over_limit_action: 'scan_prefix', on_error: 'fail_open', unsupported_path_action: 'warn', restore_responses: true, keep_key_format: true };
  const mapped = normalizePrivacySettings(dto);
  assert.equal(mapped.restoreResponses, true);
  Object.assign(mapped, { categories: { personal: false }, words: ['private-term'], wordsAnyCase: true, allow: ['private@example.com'], actions: { api_key: 'block' } });
  mapped.rawMapping = 'never-forward';
  assert.deepEqual(denormalizePrivacySettings(mapped), dto);
  assert.equal(normalizePrivacySettings({}).restoreResponses, false);
  assert.equal('categories' in normalizePrivacySettings(mapped), false);
  assert.equal('actions' in normalizePrivacySettings(mapped), false);
});

test('privacy activity exposes a metadata allowlist and preserves partial-scan and restore status', () => {
  const activity = normalizePrivacyActivity({ id: 2, request_id: 'r1', provider: 'claude', state: 'redacted', hit_count: 3, scan_duration_ms: 1.2, truncated: true, restored_count: 2, restore_skipped: 'mapping_capacity', raw_body: 'never-return', rule_hits: [{ rule_id: 1, kind: 'email', count: 3, matches: ['private@example.com'] }] });
  assert.equal(activity.truncated, true);
  assert.equal(activity.restoredCount, 2);
  assert.equal(activity.ruleHits[0].kind, 'email');
  assert.equal(JSON.stringify(activity).includes('private@example.com'), false);
  assert.equal(JSON.stringify(activity).includes('never-return'), false);
  assert.equal(mapPrivacyRequestPreview({ blocked: true, error: '拒发' }).blocked, true);
});
