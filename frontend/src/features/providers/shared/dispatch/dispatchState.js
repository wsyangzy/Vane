export const strategyOptions = [
  { value: 'smart', label: '智能 · smart', menuDescription: '综合可用性与并发，适合日常中转。', description: '优先可用凭证、分散并发；有有效额度反馈时，再参考额度与重置时间。', note: '中转站无需提供额度接口；没有额度反馈时，主要按并发与配置顺序选择。' },
  { value: 'order', label: '配置顺序 · order', menuDescription: '优先排在前面的资源，不可用时再切换。', description: '持续使用排序靠前的可用凭证，遇到失败或不可用时再切换。' },
  { value: 'rotate', label: '轮询 · rotate', menuDescription: '在可用凭证间轮流分配。', description: '在可用凭证间轮流分配请求，并发时优先空闲凭证。' },
  { value: 'usage', label: '使用率均衡 · usage', badge: '订阅增强', menuDescription: '优先较低使用率；无额度时按本地负载。', description: '额度完整时优先使用率较低的凭证；额度未知时按近期 token 与请求负载均衡。', quotaBased: true, note: '额度排序主要面向有周期额度反馈的订阅账号。中转站无此反馈时，按 Vane 近期负载均衡；手动余额不作为额度使用率。' },
  { value: 'pace', label: '额度节奏 · pace', badge: '订阅额度', menuDescription: '根据剩余额度和重置时间安排使用。', description: '结合剩余额度与重置时间安排使用；缺少周期数据时按近期负载选择。', quotaBased: true, note: '主要面向有周期额度和重置时间的订阅账号。普通中转站通常无法发挥额度节奏作用，会退化为近期负载选择；手动余额不提供重置周期。' },
  { value: 'weighted', label: '平滑加权 · weighted', menuDescription: '按分组权重，分配长期请求比例。', description: '按凭证分组权重分配请求；权重越大，长期分配越多，并发和轮次保持仍会影响结果。' },
];
export const candidateModeOptions = [{ value: 'ordered_channels', label: '按渠道顺序' }, { value: 'unified_candidates', label: '统一候选池' }];
export const affinityOptions = [{ value: 'turn', label: '保持当前轮次' }, { value: 'session', label: '保持会话' }, { value: 'adaptive', label: '自适应保持' }, { value: 'none', label: '不保持' }];
export const stateCompatibilityLabel = (value) => ({ compatible: '状态兼容', incompatible: '状态不兼容', unknown: '状态兼容性未知' }[value] || '状态兼容性未知');
export const ruleKindLabel = (kind) => ({ model: '模型路由', mapping: '模型路由 · 本渠道映射', non_stream: '非流改道', composite: '条件路由', intent: '意图路由', default: '默认编排' }[kind] || kind);

export function setIntentRoutingEnabled(profile, enabled) {
  return { ...profile, classifier: { provider: profile.provider, model: '', channelId: 0, groupId: 0, timeoutMs: 8000, confidence: 0.6, failureMode: 'default', ...profile.classifier, enabled } };
}

export function mappingTargetAllowed(profile, target) {
  return !target.profileId && profile.members.some((member) => member.enabled && member.channelId === target.channelId && (member.groupId === 0 || member.groupId === target.groupId));
}
export const classifierInputLimits = { text: 4000, previousUserMessage: 1000, previousAssistantText: 1000 };
export const classifierInputNotice = (key, value) => { const length = [...value].length, limit = classifierInputLimits[key]; return `${length} / ${limit} 字符${length > limit ? ' · 超出部分将保留头尾并标记截断' : ''}`; };

export function newRoutingProfile(provider, isDefault = false, policy) {
  return { id: 0, provider, name: '', enabled: true, isDefault, candidateMode: 'ordered_channels', strategy: 'smart', affinityMode: 'turn', maxAttempts: policy?.maxAttempts ?? '', maxDurationMs: policy?.maxDurationMs ?? '', waitForRecovery: policy?.waitForRecovery ?? false, revision: 0, members: [], rules: [], intents: [], classifier: null };
}

export function editableProfile(profile, createKey = () => crypto.randomUUID()) {
  const copy = structuredClone(profile);
  copy.rules = copy.rules.map((rule) => ({ ...rule, rowKey: createKey(), targets: rule.targets.map((target) => ({ ...target, rowKey: createKey() })) }));
  copy.intents = copy.intents.map((intent) => ({ ...intent, rowKey: createKey(), targets: intent.targets.map((target) => ({ ...target, rowKey: createKey() })) }));
  return copy;
}

export const profileChannelIds = (profile) => [...new Set(profile.members.map((member) => member.channelId))];

export function moveProfileChannel(profile, channelId, delta) {
  const ids = profileChannelIds(profile);
  const index = ids.indexOf(channelId), destination = index + delta;
  if (index < 0 || destination < 0 || destination >= ids.length) return profile;
  [ids[index], ids[destination]] = [ids[destination], ids[index]];
  return { ...profile, members: ids.flatMap((id) => profile.members.filter((member) => member.channelId === id)).map((member, position) => ({ ...member, position })) };
}

export function addProfileChannel(profile, channelId, accounts) {
  if (profileChannelIds(profile).includes(channelId)) return profile;
  const groups = accounts.filter((account) => account.channelId === channelId);
  const members = groups.length ? groups.map((account) => ({ channelId, groupId: account.groupId, enabled: true, weight: 1, upstreamModel: '' })) : [{ channelId, groupId: 0, enabled: false, weight: 1, upstreamModel: '' }];
  return { ...profile, members: [...profile.members, ...members].map((member, position) => ({ ...member, position })) };
}

export function selectProfileChannels(profile, selectedIds, accounts) {
  const selected = new Set(selectedIds);
  let next = { ...profile, members: profile.members.filter((member) => selected.has(member.channelId)) };
  for (const channelId of selected) next = addProfileChannel(next, channelId, accounts);
  return { ...next, members: profileChannelIds(next).flatMap((id) => next.members.filter((member) => member.channelId === id)).map((member, position) => ({ ...member, position })) };
}

export function updateProfileMembers(profile, channelId, groupIds, patch) {
  const selected = new Set(groupIds);
  const members = profile.members.map((member) => member.channelId === channelId && selected.has(member.groupId) ? { ...member, ...patch } : member);
  for (const groupId of selected) {
    if (!members.some((member) => member.channelId === channelId && member.groupId === groupId)) {
      members.push({ channelId, groupId, enabled: false, weight: 1, upstreamModel: '', ...patch });
    }
  }
  const ids = [...new Set(members.map((member) => member.channelId))];
  return { ...profile, members: ids.flatMap((id) => members.filter((member) => member.channelId === id)).map((member, position) => ({ ...member, position })) };
}

export function validateProfileDraft(profile) {
  if (!profile.name.trim()) return '请输入调度方案名称';
  if (!Number.isInteger(Number(profile.maxAttempts)) || Number(profile.maxAttempts) < 1 || Number(profile.maxAttempts) > 100) return '尝试次数需为 1–100 的整数';
  if (!Number.isFinite(Number(profile.maxDurationMs)) || Number(profile.maxDurationMs) < 1000 || Number(profile.maxDurationMs) > 600000) return '调度预算需为 1–600 秒';
  if (profile.isDefault && !profile.enabled) return '当前方案必须保持可用，请先切换到其他方案后再停用';
  const ids = profile.intents.map((intent) => intent.id.trim());
  if (ids.some((id) => !id) || new Set(ids).size !== ids.length) return '意图 ID 不能为空或重复';
  if (profile.intents.some((intent) => intent.fallbackIntent && !ids.includes(intent.fallbackIntent))) return '回退意图必须指向现有意图';
  if (profile.rules.some((rule) => ['model', 'mapping', 'composite'].includes(rule.kind) && !rule.matchModel.trim())) return '模型路由或映射需要填写请求模型';
  if (profile.rules.some((rule) => rule.enabled && rule.kind === 'mapping' && (!rule.targets.length || rule.targets.some((target) => !mappingTargetAllowed(profile, target) || !target.upstreamModel?.trim())))) return '模型映射需选择本方案已参与的渠道 / 分组，并填写实际模型';
  if (profile.rules.some((rule) => rule.kind === 'intent' && !ids.includes(rule.intentId))) return '意图规则需要选择现有意图';
  return '';
}

const reasonLabels = { manual_balance_exhausted: '手动余额已耗尽', manual_balance_unavailable: '暂时无法读取手动余额，未发送请求', routing_profile_missing: '尚未建立启用的默认调度方案，请先在调度方案中配置', unified_default_profile_required: '默认方案必须启用并至少包含一个启用的凭证分组', disabled: '已停用', account_disabled: '凭证分组已全局停用', account_missing: '凭证分组不存在', candidate_disabled: '端点或凭证配置不可用', credential_missing: '缺少凭证', credential_rejected: '上游拒绝凭证，请更新后重试', endpoint_unavailable: '端点暂不可用', upstream_unavailable: '上游暂不可用', repair_required: '凭证需要修复', credential_repair_required: '凭证需要修复', account_repair_required: '凭证需要修复', account_probe_in_progress: '恢复探测中', cooling_down: '健康冷却中', cooldown: '健康冷却中', account_cooling_down: '健康冷却中', state_origin_unknown: '无法确认会话状态来源，请在当前接入下新建会话', state_backend_incompatible: '会话状态属于其他账号、模型或端点', model_unavailable: '模型不可用', model_unsupported: '模型不受支持', protocol_incompatible: '协议不兼容', max_concurrent: '达到并发上限', concurrency_limit: '达到并发上限', quota_exhausted: '额度耗尽', profile_disabled: '目标方案已停用', no_candidates: '没有有效候选', budget_exhausted: '调度预算已用完', response_started: '响应已开始，禁止重放' };
export const dispatchReason = (reason) => reasonLabels[reason] || (reason?.startsWith('quota_unknown') ? '额度未知，按负载或顺序退化' : reason?.startsWith('quota_stale') ? '额度已过期，按负载或顺序退化' : reason || '符合资格');

export function candidateHealthPresentation(candidate) {
  const health = candidate.health || {};
  if (health.reason === 'account_disabled') return { label: '分组已停用', tone: 'muted', reason: dispatchReason(health.reason), until: null };
  if (health.reason === 'account_missing') return { label: '资源缺失', tone: 'danger', reason: dispatchReason(health.reason), until: null };
  if (health.repairRequired) return { label: '凭证待修复', tone: 'danger', reason: health.reason ? dispatchReason(health.reason) : '请修复或更新凭证', until: null };
  if (!candidate.enabled) return { label: '待配置', tone: 'warning', reason: health.reason ? dispatchReason(health.reason) : '缺少可用的端点或凭证配置', until: null };
  if (health.coolingDown) return { label: health.reason === 'account_probe_in_progress' ? '恢复探测中' : '冷却中', tone: 'warning', reason: health.reason ? dispatchReason(health.reason) : '等待健康恢复', until: health.until };
  return { label: '正常', tone: 'success', reason: '', until: null };
}
export function quotaPresentation(quota, now = Date.now()) {
  const expired = !quota.expiresAt || !Number.isFinite(new Date(quota.expiresAt).getTime()) || new Date(quota.expiresAt).getTime() <= now;
  const stale = quota.confidence === 'stale' || expired;
  const known = quota.confidence !== 'unknown' && !stale && quota.usedPercent != null;
  return { known, stale, label: known ? `${Number(quota.usedPercent).toFixed(1)}%` : stale && quota.usedPercent != null ? `${Number(quota.usedPercent).toFixed(1)}%（历史）` : '未知', status: quota.confidence === 'unknown' ? '未知' : stale ? '已过期 / 待刷新' : quota.confidence === 'learned' ? '响应学习' : '官方读取' };
}

const skippedClassificationReasons = new Set(['classifier_disabled', 'classifier_preview_not_requested', 'classifier_no_new_turn', 'classifier_cooling_down', 'classifier_busy', 'classifier_cancelled', 'classifier_privacy_rejected', 'turn_reused']);

export function summarizeIntentTraces(traces, provider, profileId) {
  const recent = traces.slice(0, 100).filter((trace) => trace.provider === provider);
  const matching = recent.filter((trace) => trace.profileId === profileId);
  const stats = { sampled: recent.length, matching: matching.length, calls: 0, hits: 0, failures: 0, cost: 0, inputTokens: 0, outputTokens: 0, intents: {} };
  const hitsByIntent = new Map();
  const positiveNumber = (value) => Number.isFinite(Number(value)) ? Math.max(0, Number(value)) : 0;
  for (const { intent = {} } of matching) {
    if (intent.cached || !intent.model || skippedClassificationReasons.has(intent.reason) || (!intent.intentId && !intent.reason)) continue;
    stats.calls += 1;
    if (intent.reason || intent.fallback) stats.failures += 1;
    else {
      stats.hits += 1;
      hitsByIntent.set(intent.intentId, (hitsByIntent.get(intent.intentId) || 0) + 1);
    }
    stats.cost += positiveNumber(intent.cost);
    stats.inputTokens += positiveNumber(intent.inputTokens);
    stats.outputTokens += positiveNumber(intent.outputTokens);
  }
  stats.intents = Object.fromEntries(hitsByIntent);
  return stats;
}
export function credentialGroupRoutingImpact(workspaces, provider, groupId) {
  const id = Number(groupId);
  if (!id) return [];
  const workspace = workspaces.find((item) => item.provider === provider);
  const account = workspace?.accounts.find((item) => item.groupId === id);
  const matchesGroup = (target) => target.groupId === id || (target.groupId === 0 && account && target.channelId === account.channelId);
  const matchesTarget = (target) => !target.profileId && matchesGroup(target);
  return workspaces.flatMap((workspace) => workspace.profiles.flatMap((profile) => {
    const ownsGroup = workspace.provider === provider;
    const memberReference = ownsGroup && profile.members.some((member) => member.enabled && matchesGroup(member));
    const targetReference = ownsGroup && [...profile.rules, ...profile.intents].some((rule) => rule.targets.some(matchesTarget));
    const classifierReference = (profile.classifier?.provider || workspace.provider) === provider && profile.classifier?.groupId === id;
    if (!memberReference && !targetReference && !classifierReference) return [];
    const losesLastMember = ownsGroup && profile.members.some((member) => member.enabled && member.groupId === id) && !profile.members.some((member) => member.enabled && member.groupId !== id);
    return [{ id: profile.id, provider: workspace.provider, name: profile.name, losesLastMember, classifierReference }];
  }));
}
