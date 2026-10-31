import test from 'node:test';
import assert from 'node:assert/strict';
import {
  formatReasoningFieldValue,
  getReasoningEffortMeta,
  getRequestServiceTierMeta,
  getRequestBillingMultiplierSources,
  getServiceTierMeta,
  getStreamOutcomeMeta,
  isThinkingTypeActive,
  getPrivacyDisplay,
  parseReasoningConfig
} from './requestMetadata.js';

test('请求计费公式使用快照区分分组、规则与最终倍率，并兼容旧快照', () => {
  const snapshot = { context_mode: 'normal', rule_key: 'fast', rule_group_multiplier_mode: 'inherit', group_cost_multiplier: 1.5, rule_cost_multiplier: 1.25, total_cost_multiplier: 1.875 };
  const result = getRequestBillingMultiplierSources({ costMultiplier: 999, billingRuleSnapshot: snapshot });
  assert.equal(result.group, 1.5);
  assert.equal(result.rule, 1.25);
  assert.equal(result.total, 1.875);
  assert.equal(result.hasRuleSources, true);
  assert.equal(result.ruleGroupMode, 'inherit');
  assert.equal(result.groupApplied, true);
  const old = getRequestBillingMultiplierSources({ billingRuleSnapshot: { context_mode: 'normal', group_cost_multiplier: 1.25, total_cost_multiplier: 1.25 } });
  assert.equal(old.total, 1.25);
  assert.equal(old.rule, 1);
  assert.equal(old.hasRuleSources, false);
  assert.equal(old.groupApplied, true);
  assert.equal(getRequestBillingMultiplierSources({ costMultiplier: 1.5, modelCostMultiplier: 2 }).total, 3);
});

test('独立规则快照中的中性 1x 不作为已参与的分组基础倍率展示', () => {
  const result = getRequestBillingMultiplierSources({
    costMultiplier: 1.5,
    billingRuleSnapshot: {
      context_mode: 'normal', rule_key: 'model=*;tier=*;context=*',
      rule_group_multiplier_mode: 'override', group_cost_multiplier: 1,
      rule_cost_multiplier: 1.5, total_cost_multiplier: 1.5
    }
  });
  assert.equal(result.hasRuleSources, true);
  assert.equal(result.groupApplied, false);
  assert.equal(result.group, 1);
  assert.equal(result.rule, 1.5);
  assert.equal(result.total, 1.5);
  const baseCost = ((106326 - 104960) * 2 + 118 * 10 + 104960 * 0.1) / 1e6;
  assert.ok(Math.abs(baseCost * result.total - 0.021612) < 1e-12);
});

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
  assert.equal(getServiceTierMeta('priority').label, 'Fast');
  assert.equal(getServiceTierMeta('default').value, 'standard');
  assert.equal(getServiceTierMeta('auto').value, 'auto');
  for (const [tier, label] of Object.entries({ auto: 'Auto', standard: 'Standard', batch: 'Batch', flex: 'Flex', fast: 'Fast' })) {
    assert.equal(getServiceTierMeta(tier).label, label);
  }
  assert.equal(getServiceTierMeta('custom').label, 'custom');
  assert.match(getServiceTierMeta('auto').description, /无法确定最终计费档位/);
});

test('request service tier follows the billing decision while preserving upstream downgrades and legacy records', () => {
  assert.equal(getRequestServiceTierMeta({
    serviceTier: 'default',
    billingRuleSnapshot: {
      pricing_provider: 'codex',
      requested_service_tier: 'priority', response_service_tier: 'default',
      billing_service_tier: 'priority', service_tier_source: 'request', pricing_service_tier: ''
    }
  }).label, 'Fast');
  assert.equal(getRequestServiceTierMeta({
    serviceTier: 'priority',
    billingRuleSnapshot: {
      pricing_provider: 'codex',
      requested_service_tier: 'priority', response_service_tier: 'default',
      billing_service_tier: 'default', service_tier_source: 'response'
    }
  }).label, 'Standard');
  assert.equal(getRequestServiceTierMeta({ serviceTier: 'priority', billingRuleSnapshot: { pricing_provider: 'codex', service_tier_source: 'default' } }).label, 'Standard');
  assert.equal(getRequestServiceTierMeta({ serviceTier: 'priority' }).label, 'Fast');
  assert.equal(getRequestServiceTierMeta({ service_tier: 'priority', billingRuleSnapshot: { pricing_service_tier: '' } }).label, 'Fast');
  assert.equal(getRequestServiceTierMeta({ serviceTier: 'auto' }).label, 'Auto');
  assert.equal(getRequestServiceTierMeta({ billingRuleSnapshot: { pricing_provider: 'claude', service_tier_source: 'default' } }), null);
  assert.equal(getRequestServiceTierMeta({}), null);
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
