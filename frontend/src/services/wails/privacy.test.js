import test from 'node:test';
import assert from 'node:assert/strict';
import { normalizePrivacySettings, denormalizePrivacySettings, normalizePrivacyRule, denormalizePrivacyRule, normalizePrivacyActivity, mapPrivacyRequestPreview } from './privacy.js';

test('privacy settings map runtime options without reintroducing legacy matching overrides', () => {
  const dto = { mode: 'redact', restore_responses: true, scan_max_bytes: 4194304, over_limit_action: 'scan_prefix', on_error: 'fail_open', unsupported_path_action: 'warn' };
  const mapped = normalizePrivacySettings({ ...dto, restore_responses: true, keep_key_format: true });
  assert.equal(mapped.restoreResponses, true);
  Object.assign(mapped, { categories: { personal: false }, words: ['private-term'], wordsAnyCase: true, allow: ['private@example.com'], actions: { api_key: 'block' } });
  mapped.rawMapping = 'never-forward';
  mapped.restoreResponses = true;
  mapped.keepKeyFormat = true;
  assert.deepEqual(denormalizePrivacySettings(mapped), dto);
  assert.equal(normalizePrivacySettings({}).restoreResponses, true);
  assert.equal('categories' in normalizePrivacySettings(mapped), false);
  assert.equal('actions' in normalizePrivacySettings(mapped), false);
});

test('restoration toggle preserves false and retired rejection policies cannot return through the bridge', () => {
  const settings = normalizePrivacySettings({ restore_responses: false, mode: 'redact', on_error: 'fail_closed', over_limit_action: 'fail_closed', unsupported_path_action: 'fail_closed' });
  const dto = denormalizePrivacySettings(settings);
  assert.equal(dto.restore_responses, false);
  assert.equal(dto.mode, 'redact');
  assert.equal(dto.on_error, 'fail_open');
  assert.equal(dto.over_limit_action, 'scan_prefix');
  assert.equal(dto.unsupported_path_action, 'warn');
});

test('rules only keep ordinary matching or extra allowance when reading and saving legacy actions', () => {
  for (const action of ['detect', 'block', 'redact', 'allow']) {
    const rule = normalizePrivacyRule({ action, replacement_mode: 'placeholder', enabled: true, pattern: 'sample-value' });
    const dto = denormalizePrivacyRule(rule);
    assert.equal(rule.action, action === 'allow' ? 'allow' : 'redact');
    assert.equal(dto.action, rule.action);
    assert.equal(dto.replacement_mode, action === 'allow' ? 'mask_only' : 'reversible');
    assert.equal(dto.pattern, 'sample-value');
  }
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

test('per-rule restoration round-trips and allowance always clears restoration', () => {
  for (const replacementMode of ['reversible', 'reversible_key', 'mask_only', 'mask_only_key']) {
    const rule = normalizePrivacyRule({ replacement_mode: replacementMode, action: 'redact', pattern: 'sample-value' });
    assert.equal(rule.restoreResponse, !replacementMode.startsWith('mask_only'));
    assert.equal(denormalizePrivacyRule(rule).replacement_mode, replacementMode);
    const allowed = normalizePrivacyRule(denormalizePrivacyRule({ ...rule, action: 'allow' }));
    assert.equal(allowed.restoreResponse, false);
    assert.equal(allowed.replacementMode, `mask_only${replacementMode.endsWith('_key') ? '_key' : ''}`);
    const changed = denormalizePrivacyRule({ ...allowed, action: 'redact', restoreResponse: !rule.restoreResponse });
    assert.equal(changed.replacement_mode, `${rule.restoreResponse ? 'mask_only' : 'reversible'}${replacementMode.endsWith('_key') ? '_key' : ''}`);
    assert.equal('restoreResponse' in changed, false);
  }
});

test('scanning limit remains numeric when saved alongside the restoration toggle', () => {
  const settings = normalizePrivacySettings({ scan_max_bytes: 1572864, restore_responses: false });
  const saved = denormalizePrivacySettings(settings);
  assert.equal(saved.scan_max_bytes, 1572864);
  assert.equal(saved.restore_responses, false);
});

test('scanning choices round-trip independently through snake_case and camelCase settings', () => {
  for (const onError of ['fail_open', 'passthrough']) {
    for (const unsupportedPathAction of ['warn', 'passthrough']) {
      for (const overLimitAction of ['scan_prefix', 'skip_scan']) {
        const camel = { onError, unsupportedPathAction, overLimitAction, scanMaxBytes: 1572864, restoreResponses: false };
        const snake = { on_error: onError, unsupported_path_action: unsupportedPathAction, over_limit_action: overLimitAction, scan_max_bytes: 1572864, restore_responses: false, mode: 'detect' };
        assert.deepEqual(denormalizePrivacySettings(normalizePrivacySettings(snake)), snake);
        assert.deepEqual(denormalizePrivacySettings(normalizePrivacySettings(camel)), snake);
      }
    }
  }
});
