export const dispatchSettingGroups = [
  { title: '预算与等待', keys: ['max_total_attempts', 'max_total_duration', 'wait_for_recovery'] },
  { title: '临时故障冷却', keys: ['cooldown_initial', 'cooldown_multiplier', 'cooldown_max'] },
  { title: '规则默认回退', keys: ['rule_fallback_to_default'] },
];

const durationUnits = { h: 3600000, m: 60000, s: 1000, ms: 1, us: 0.001, 'µs': 0.001, 'μs': 0.001, ns: 0.000001 };
function durationMs(value) {
  const parts = [...String(value).matchAll(/(\d+(?:\.\d+)?)(ms|us|µs|μs|ns|h|m|s)/g)];
  if (!parts.length || parts.map(([part]) => part).join('') !== value) return NaN;
  return parts.reduce((sum, [, number, unit]) => sum + Number(number) * durationUnits[unit], 0);
}

export function readDispatchPolicy(settings) {
  const values = Object.fromEntries(settings.filter((setting) => setting.category === 'retry' && (!setting.provider || setting.provider === 'global')).map((setting) => [setting.key, String(setting.value)]));
  if (dispatchSettingGroups.some((group) => group.keys.some((key) => !Object.hasOwn(values, key)))) throw new Error('调度保护设置不完整，请重启更新后的应用后重试');
  const bool = (key) => {
    if (!['true', 'false'].includes(values[key])) throw new Error('调度保护开关值无效');
    return values[key] === 'true';
  };
  const policy = {
    maxAttempts: Number(values.max_total_attempts), maxDurationMs: durationMs(values.max_total_duration),
    waitForRecovery: bool('wait_for_recovery'), cooldownInitialMs: durationMs(values.cooldown_initial),
    cooldownMaxMs: durationMs(values.cooldown_max), cooldownMultiplier: Number(values.cooldown_multiplier),
    ruleFallbackToDefault: bool('rule_fallback_to_default'),
  };
  if (!Number.isInteger(policy.maxAttempts) || policy.maxAttempts < 1 || policy.maxAttempts > 100 ||
    !Number.isFinite(policy.maxDurationMs) || policy.maxDurationMs < 1000 || policy.maxDurationMs > 600000 ||
    !Number.isFinite(policy.cooldownInitialMs) || policy.cooldownInitialMs < 1000 || policy.cooldownInitialMs > 3600000 ||
    !Number.isFinite(policy.cooldownMaxMs) || policy.cooldownMaxMs < policy.cooldownInitialMs || policy.cooldownMaxMs > 3600000 ||
    !Number.isFinite(policy.cooldownMultiplier) || policy.cooldownMultiplier < 1 || policy.cooldownMultiplier > 10) throw new Error('调度保护设置超出有效范围');
  return policy;
}

export const formatDispatchDuration = (milliseconds) => milliseconds % 60000 === 0 ? `${milliseconds / 60000} 分钟` : `${Number((milliseconds / 1000).toFixed(3))} 秒`;

export function ruleFallbackOptions(policy) {
  const inherited = policy?.ruleFallbackToDefault;
  return [
    { value: 'default', label: inherited == null ? '跟随全局（暂未读取）' : `跟随全局：${inherited ? '回到默认编排' : '仅尝试规则目标'}` },
    { value: 'yes', label: '允许回到默认编排' },
    { value: 'no', label: '禁止回到默认编排' },
  ];
}
