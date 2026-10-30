import test from 'node:test';
import assert from 'node:assert/strict';
import {
  formatReasoningFieldValue,
  getReasoningEffortMeta,
  getServiceTierMeta,
  getStreamOutcomeMeta,
  isThinkingTypeActive,
  getPrivacyDisplay,
  parseReasoningConfig
} from './requestMetadata.js';

test('request protection uses the correlated activity instead of guessing from a missing hit summary', () => {
  for (const state of ['clean', 'disabled', 'skipped', 'truncated', 'blocked', 'redacted', 'detected']) {
    const activity = { state, hit_count: state === 'redacted' ? 3 : 0, rule_hits: [], scan_duration_ms: 2.3, restored_count: 1, truncated: state === 'truncated' };
    const result = getPrivacyDisplay({ privacy_redaction_summary: JSON.stringify({ activity }) });
    assert.equal(result.state, state);
    assert.equal(result.hitCount, activity.hit_count);
    assert.deepEqual(result.activity, activity);
  }
  assert.equal(getPrivacyDisplay({}).state, 'none');
  assert.equal(getPrivacyDisplay({ privacy_redaction_summary: 'broken-json' }).state, 'none');
  const legacy = getPrivacyDisplay({ privacy_redacted: true, privacy_redaction_summary: JSON.stringify({ changed: true, hits: [{ rule_name: 'old-rule', count: 2 }] }) });
  assert.equal(legacy.state, 'redacted');
  assert.equal(legacy.hitCount, 2);
});

test('stream outcome labels separate an incomplete stream from a completed one', () => {
  assert.equal(getStreamOutcomeMeta('completed').label, '完整');
  assert.equal(getStreamOutcomeMeta('incomplete').label, '不完整');
  assert.equal(getStreamOutcomeMeta('not_streaming').label, '非流式完成');
  assert.equal(getStreamOutcomeMeta('canceled').label, '已取消');
  // 历史行没有终态
  assert.equal(getStreamOutcomeMeta(''), null);
  assert.equal(getStreamOutcomeMeta(null), null);
  assert.equal(getStreamOutcomeMeta('weird').label, 'weird');
});

test('request metadata safely parses reasoning and keeps product modes distinct', () => {
  assert.deepEqual(parseReasoningConfig('null'), {});
  assert.deepEqual(parseReasoningConfig('"high"'), { effort: 'high' });
  assert.deepEqual(parseReasoningConfig('{"effort":"xhigh","summary":"concise"}'), { effort: 'xhigh', summary: 'concise' });
  assert.match(getReasoningEffortMeta('max').description, /不是标准单请求/);
  for (const effort of ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra']) {
    assert.equal(getReasoningEffortMeta(effort).label, effort);
  }
  assert.equal(getReasoningEffortMeta('custom').label, 'custom');
  assert.equal(formatReasoningFieldValue('enabled', false), 'false · 否');
});

test('request metadata maps service-tier aliases without guessing auto', () => {
  assert.equal(getServiceTierMeta('priority').value, 'fast');
  assert.equal(getServiceTierMeta('priority').label, 'Fast mode');
  assert.equal(getServiceTierMeta('default').value, 'standard');
  assert.equal(getServiceTierMeta('auto').value, 'auto');
  for (const [tier, label] of Object.entries({ auto: 'Auto', standard: 'Standard', batch: 'Batch', flex: 'Flex', fast: 'Fast mode' })) {
    assert.equal(getServiceTierMeta(tier).label, label);
  }
  assert.equal(getServiceTierMeta('custom').label, 'custom');
  assert.match(getServiceTierMeta('auto').description, /无法确定最终计费档位/);
});

test('anthropic thinking type adaptive is labelled and treated as an active state', () => {
  // Claude 实际只发 {"type":...}，不含 effort；adaptive 曾裸显示且与 disabled 同为灰色
  assert.deepEqual(parseReasoningConfig('{"type":"adaptive"}'), { type: 'adaptive' });
  assert.equal(formatReasoningFieldValue('type', 'adaptive'), 'adaptive · 自适应');
  assert.equal(formatReasoningFieldValue('type', 'enabled'), 'enabled · 已开启');
  assert.equal(formatReasoningFieldValue('type', 'disabled'), 'disabled · 已关闭');
  // adaptive 是开启态，必须与 disabled 区分
  assert.equal(isThinkingTypeActive('adaptive'), true);
  assert.equal(isThinkingTypeActive('enabled'), true);
  assert.equal(isThinkingTypeActive('disabled'), false);
  assert.equal(isThinkingTypeActive(''), false);
  assert.equal(isThinkingTypeActive('ADAPTIVE'), true);
});
