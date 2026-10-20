import { parseMultiplierExpression } from '../../shared/lib/multiplier.js';

const parseEndpointMultiplier = (value, fallback = 1.0) => {
  const parsed = parseMultiplierExpression(value, fallback);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback;
};

const parseNonNegativeNumber = (value, fallback = 0) => {
  if (value === '' || value === null || value === undefined) return fallback;
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : fallback;
};

const parseEndpointPriority = (value, fallback = 0) => {
  if (typeof value === 'string' && value.trim() === '') return fallback;
  if (value === null || value === undefined) return fallback;
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed >= 0 ? parsed : fallback;
};

const normalizeModelMapping = (mapping) => {
  if (!mapping || typeof mapping !== 'object' || Array.isArray(mapping)) {
    return {};
  }
  return Object.entries(mapping).reduce((result, [sourceModel, targetModel]) => {
    const source = String(sourceModel || '').trim();
    const target = String(targetModel || '').trim();
    if (source && target) {
      result[source] = target;
    }
    return result;
  }, {});
};

const normalizeModelMultipliers = (rules) => {
  if (!Array.isArray(rules)) return [];
  return rules
    .map((rule) => ({
      model: String(rule?.model || '').trim(),
      costMultiplier: parseEndpointMultiplier(rule?.costMultiplier),
      inputCostMultiplier: parseEndpointMultiplier(rule?.inputCostMultiplier),
      outputCostMultiplier: parseEndpointMultiplier(rule?.outputCostMultiplier),
      cacheReadCostMultiplier: parseEndpointMultiplier(rule?.cacheReadCostMultiplier)
    }))
    .filter((rule) => rule.model);
};

const hasOwn = (value, key) => Object.prototype.hasOwnProperty.call(value || {}, key);

const getExplicitModelMultipliers = (input, normalizer = normalizeModelMultipliers) => {
  if (hasOwn(input, 'modelMultipliers')) {
    return normalizer(input.modelMultipliers);
  }
  if (hasOwn(input, 'model_multipliers')) {
    return normalizer(input.model_multipliers);
  }
  return undefined;
};

const mapSubscription = (binding) => binding ? ({
  accountId: binding.account_id || binding.accountId || '',
  version: binding.version || 0,
  enabled: Boolean(binding.enabled),
  backupTargets: (binding.backup_targets || binding.backupTargets || []).map((target) => ({
    channelId: Number(target.channel_id ?? target.channelId ?? 0),
    groupId: Number(target.group_id ?? target.groupId ?? 0),
    upstreamModel: target.upstream_model ?? target.upstreamModel ?? ''
  }))
}) : null;

export { parseEndpointPriority, parseEndpointMultiplier, parseNonNegativeNumber, normalizeModelMapping, normalizeModelMultipliers, hasOwn, getExplicitModelMultipliers, mapSubscription };
