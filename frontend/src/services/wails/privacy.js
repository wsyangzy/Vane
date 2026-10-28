import { WailsApp, initWails } from './connection.js';

const normalizePrivacyRule = (rule = {}) => ({
  kind: rule.kind || 'custom',
  id: rule.id,
  name: rule.name || '',
  description: rule.description || '',
  matchType: rule.match_type || rule.matchType || 'literal',
  pattern: rule.pattern || '',
  action: rule.action || 'redact',
  replacementMode: rule.replacement_mode || rule.replacementMode || 'placeholder',
  placeholder: rule.placeholder || '',
  pseudonymKind: rule.pseudonym_kind || rule.pseudonymKind || '',
  source: rule.source || 'custom',
  enabled: Boolean(rule.enabled),
  priority: Number(rule.priority ?? 100),
  createdAt: rule.created_at || rule.createdAt || '',
  updatedAt: rule.updated_at || rule.updatedAt || ''
});

// 旧版规则只读快照。pattern 已由后端掩码，前端永远拿不到敏感原值。
const normalizeLegacyPrivacyRule = (rule = {}) => ({
  id: rule.id,
  name: rule.name || '',
  type: rule.type || '',
  category: rule.category || '',
  patternMasked: rule.pattern_masked || rule.patternMasked || '',
  valueLength: Number(rule.value_length ?? rule.valueLength ?? 0),
  replacementMode: rule.replacement_mode || rule.replacementMode || '',
  enabled: Boolean(rule.enabled),
  priority: Number(rule.priority ?? 100)
});

export const normalizePrivacySettings = (settings = {}) => ({
  restoreResponses: Boolean(settings.restore_responses ?? settings.restoreResponses),
  keepKeyFormat: Boolean(settings.keep_key_format ?? settings.keepKeyFormat),
  mode: settings.mode || 'detect',
  scanMaxBytes: Number(settings.scan_max_bytes ?? settings.scanMaxBytes ?? 4194304),
  overLimitAction: settings.over_limit_action || settings.overLimitAction || 'scan_prefix',
  onError: settings.on_error || settings.onError || 'fail_open',
  unsupportedPathAction: settings.unsupported_path_action || settings.unsupportedPathAction || 'warn'
});

export const denormalizePrivacySettings = (settings = {}) => ({
  restore_responses: Boolean(settings.restoreResponses),
  keep_key_format: Boolean(settings.keepKeyFormat),
  mode: settings.mode || 'detect',
  scan_max_bytes: Number(settings.scanMaxBytes ?? 4194304),
  over_limit_action: settings.overLimitAction || 'scan_prefix',
  on_error: settings.onError || 'fail_open',
  unsupported_path_action: settings.unsupportedPathAction || 'warn'
});

const denormalizePrivacyRule = (rule = {}) => ({
  id: Number(rule.id ?? 0),
  enabled: Boolean(rule.enabled),
  name: rule.name || '',
  description: rule.description || '',
  priority: Number(rule.priority ?? 100),
  match_type: rule.matchType || 'literal',
  pattern: rule.pattern || '',
  action: rule.action || 'redact',
  replacement_mode: rule.replacementMode || 'placeholder',
  placeholder: rule.placeholder || '',
  pseudonym_kind: rule.pseudonymKind || '',
  source: rule.source || 'custom'
});

const normalizePrivacyRuleHit = (hit = {}) => ({
  kind: hit.kind || '',
  ruleId: hit.rule_id ?? hit.ruleId ?? 0,
  ruleName: hit.rule_name || hit.ruleName || '',
  source: hit.source || '',
  action: hit.action || '',
  count: Number(hit.count ?? 0)
});

const normalizeUnscannedPath = (stat = {}) => ({
  path: stat.path || '',
  count: Number(stat.count ?? 0),
  lastSeenAt: stat.last_seen_at || stat.lastSeenAt || ''
});

export const normalizePrivacyActivity = (item = {}) => ({
  id: Number(item.id ?? 0),
  requestId: item.request_id || '',
  provider: item.provider || '',
  path: item.path || '',
  createdAt: item.created_at || '',
  state: item.state || 'unknown',
  hitCount: Number(item.hit_count ?? 0),
  ruleHits: (item.rule_hits || []).map(normalizePrivacyRuleHit),
  scanDurationMs: Number(item.scan_duration_ms ?? 0),
  skippedReason: item.skipped_reason || '',
  truncated: Boolean(item.truncated),
  restoredCount: Number(item.restored_count ?? 0),
  restoreSkipped: item.restore_skipped || ''
});

const getPrivacyOverview = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const overview = await WailsApp.GetPrivacyOverview();
  return {
    settings: normalizePrivacySettings(overview?.settings),
    activity: (overview?.activity || []).map(normalizePrivacyActivity),
    rules: (overview?.rules || []).map(normalizePrivacyRule),
    legacyRules: (overview?.legacy_rules || []).map(normalizeLegacyPrivacyRule),
    unscannedPaths: (overview?.unscanned_paths || []).map(normalizeUnscannedPath),
    presets: (overview?.presets || []).map((preset) => ({
      id: preset.id,
      name: preset.name || '',
      description: preset.description || '',
      ruleCount: Array.isArray(preset.rules) ? preset.rules.length : 0
    })),
    pseudonymKinds: overview?.pseudonym_kinds || [],
    builtinMatchers: (overview?.builtin_matchers || []).map((matcher) => ({
      pattern: matcher.pattern || '',
      name: matcher.name || '',
      description: matcher.description || '',
      placeholder: matcher.placeholder || '',
      pseudonymKind: matcher.pseudonym_kind || ''
    }))
  };
};

const updatePrivacySettings = async (settings) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.UpdatePrivacySettings(denormalizePrivacySettings(settings));
};

const createPrivacyRule = async (rule) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return normalizePrivacyRule(await WailsApp.CreatePrivacyRule(denormalizePrivacyRule(rule)));
};

const updatePrivacyRule = async (id, rule) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return normalizePrivacyRule(await WailsApp.UpdatePrivacyRule(Number(id), denormalizePrivacyRule(rule)));
};

const deletePrivacyRule = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.DeletePrivacyRule(Number(id));
};

const deletePrivacyRules = async (ids) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.DeletePrivacyRules((ids || []).map(Number));
};

const setPrivacyRulesEnabled = async (ids, enabled) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.SetPrivacyRulesEnabled((ids || []).map(Number), Boolean(enabled));
};

const applyPrivacyPreset = async (presetId) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.ApplyPrivacyPreset(String(presetId));
};

const discardLegacyPrivacyRules = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.DiscardLegacyPrivacyRules();
};

const previewPrivacyRedaction = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const result = await WailsApp.PreviewPrivacyRedaction(String(input ?? ''));
  return {
    output: result?.output || '',
    blocked: Boolean(result?.blocked),
    changed: Boolean(result?.changed),
    hitCount: Number(result?.hit_count ?? 0),
    ruleHits: (result?.rule_hits || []).map(normalizePrivacyRuleHit)
  };
};

// 完整 JSON 请求体预演：返回哪些字段被扫描、哪些被改写（不含命中原文）
const mapPrivacyRequestPreview = (result = {}) => ({
  blocked: Boolean(result?.blocked),
  supported: Boolean(result?.supported),
  error: result?.error || '',
  output: result?.output || '',
  changed: Boolean(result?.changed),
  hitCount: Number(result?.hit_count ?? 0),
  ruleHits: (result?.rule_hits || []).map(normalizePrivacyRuleHit),
  fields: (result?.fields || []).map((field) => ({
    path: field.path || '',
    bytes: Number(field.bytes ?? 0),
    redacted: Boolean(field.redacted)
  }))
});

const previewPrivacyRequest = async (path, body) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return mapPrivacyRequestPreview(await WailsApp.PreviewPrivacyRequest(String(path || ''), String(body ?? '')));
};

const getPrivacyUnscannedPaths = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return ((await WailsApp.GetPrivacyUnscannedPaths()) || []).map(normalizeUnscannedPath);
};

const clearPrivacyUnscannedPaths = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.ClearPrivacyUnscannedPaths();
};

// 渠道隐私级别：'' 跟随全局 / 'third_party' 三方中转强制脱敏 / 'trusted' 可信渠道只检测
const normalizeChannelPrivacyLevel = (level) => (
  level === 'third_party' || level === 'trusted' ? level : ''
);

const setChannelPrivacyLevel = async (provider, channelId, level) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.SetChannelPrivacyLevel(provider || 'claude', Number(channelId), normalizeChannelPrivacyLevel(level));
  return { success: true };
};

export { getPrivacyOverview, updatePrivacySettings, createPrivacyRule, updatePrivacyRule, deletePrivacyRule, deletePrivacyRules, setPrivacyRulesEnabled, applyPrivacyPreset, discardLegacyPrivacyRules, previewPrivacyRedaction, mapPrivacyRequestPreview, previewPrivacyRequest, getPrivacyUnscannedPaths, clearPrivacyUnscannedPaths, normalizeChannelPrivacyLevel, setChannelPrivacyLevel };
