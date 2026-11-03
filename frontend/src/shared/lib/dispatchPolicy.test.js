import assert from 'node:assert/strict';
import test from 'node:test';
import { readDispatchPolicy, ruleFallbackOptions } from './dispatchPolicy.js';

const settings = (patch = {}) => Object.entries({ max_total_attempts: '24', max_total_duration: '2m30s', wait_for_recovery: 'false', cooldown_initial: '4s', cooldown_multiplier: '1.5', cooldown_max: '2m0s', rule_fallback_to_default: 'false', ...patch }).map(([key, value]) => ({ provider: 'global', category: 'retry', key, value }));

test('saved routing settings reach budgets, compound duration units and inherited rule options', () => {
  const policy = readDispatchPolicy(settings());
  assert.equal(policy.maxAttempts, 24);
  assert.equal(policy.maxDurationMs, 150000);
  assert.equal(policy.cooldownMaxMs, 120000);
  assert.equal(policy.waitForRecovery, false);
  assert.equal(policy.cooldownMultiplier, 1.5);
  assert.match(ruleFallbackOptions(policy)[0].label, /跟随全局：仅尝试规则目标/);
  assert.equal(policy.ruleFallbackToDefault, false);
  const enabled = readDispatchPolicy(settings({ rule_fallback_to_default: 'true' }));
  assert.match(ruleFallbackOptions(enabled)[0].label, /跟随全局：回到默认编排/);
  assert.equal(enabled.ruleFallbackToDefault, true);
  assert.deepEqual(ruleFallbackOptions(policy).slice(1), ruleFallbackOptions(enabled).slice(1));
});

test('missing or invalid settings are explicit and do not manufacture a runtime default', () => {
  assert.throws(() => readDispatchPolicy([]), /不完整/);
  for (const patch of [{ cooldown_initial: 'bad' }, { cooldown_max: '1s' }, { cooldown_multiplier: 'Infinity' }, { max_total_duration: '2m junk' }, { rule_fallback_to_default: 'yes' }]) {
    assert.throws(() => readDispatchPolicy(settings(patch)));
  }
  assert.match(ruleFallbackOptions(null)[0].label, /暂未读取/);
});

test('the unified global value wins over retired type defaults and provider-specific values', () => {
  const policy = readDispatchPolicy([
    ...settings({ model_rule_fallback: 'true', mapping_rule_fallback: 'true', non_stream_rule_fallback: 'true', intent_rule_fallback: 'true' }),
    { provider: 'codex', category: 'retry', key: 'rule_fallback_to_default', value: 'true' },
  ]);
  assert.equal(policy.ruleFallbackToDefault, false);
  assert.throws(() => readDispatchPolicy(settings().filter(({ key }) => key !== 'rule_fallback_to_default')), /不完整/);
});
