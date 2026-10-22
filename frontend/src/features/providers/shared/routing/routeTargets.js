// ============================================
// 请求级路由的表单辅助：模型路由规则与渠道非流改道共用同一种目标结构
// {channelId, groupId, upstreamModel}：groupId=0 表示渠道下全部凭证分组，
// upstreamModel 为空时沿用目标分组的模型策略
// ============================================

export const emptyRouteTarget = () => ({ channelId: 0, groupId: 0, upstreamModel: '' });

// 两种功能的 prefer 语义不同：模型规则的目标全部失败后仍会回到来源渠道；
// 非流改道不回退——来源渠道已声明不接非流，再试只会多消耗预算、多一次 429
export const ROUTE_MODE_OPTIONS = {
  model_rule: [
    { value: 'prefer', label: '优先目标', description: '依次尝试目标，全部失败后回到当前激活渠道（只在该渠道内换端点）。' },
    { value: 'fallback', label: '兜底目标', description: '先在当前激活渠道内尝试，全部失败后再依次尝试目标。' }
  ],
  non_stream: [
    { value: 'prefer', label: '直接改道', description: '非流请求直接发往目标，不再请求本渠道。适合基本不接非流请求的渠道。' },
    { value: 'fallback', label: '失败后改道', description: '先在本渠道内尝试，全部失败后再发往目标。适合偶尔拒绝非流请求的渠道。' }
  ]
};

export const routeModeLabel = (kind, mode) =>
  ROUTE_MODE_OPTIONS[kind]?.find((item) => item.value === mode)?.label || '';

const isSelectedTarget = (target) => Number(target?.channelId) > 0;

// 目标选择器里的渠道名附带会影响命中结果的状态
export const routeChannelLabel = (channel) => {
  const flags = [];
  if (channel.active) flags.push('当前');
  if (channel.paused && !channel.active) flags.push('已暂停');
  if (channel.nonStreamRouteEnabled) flags.push('不接非流');
  return flags.length > 0 ? `${channel.name}（${flags.join(' · ')}）` : channel.name;
};

export const moveRouteTarget = (targets, index, offset) => {
  const next = [...(targets || [])];
  const to = index + offset;
  if (index < 0 || index >= next.length || to < 0 || to >= next.length) return next;
  [next[index], next[to]] = [next[to], next[index]];
  return next;
};

// 渠道与分组的存在性由后端按目标目录校验；前端只拦「一个渠道都没选」
export const validateRouteTargets = (targets) =>
  (targets || []).some(isSelectedTarget) ? '' : '请至少选择一个目标渠道';

// 模型路由规则表单提交前的本地校验，返回错误文案（空串表示通过）
export const validateModelRouteForm = (rule) => {
  if (!String(rule?.name || '').trim()) return '请填写规则名称';
  if (!String(rule?.matchModel || '').trim()) return '请填写匹配模型';
  if (!/^-?\d+$/.test(String(rule?.priority ?? '').trim())) return '优先级必须是整数';
  return validateRouteTargets(rule?.targets);
};

export const buildNonStreamRouteForm = (route) => {
  const targets = (route?.targets || []).map((target) => ({ ...target }));
  return {
    enabled: targets.length > 0,
    mode: route?.mode === 'fallback' ? 'fallback' : 'prefer',
    targets: targets.length > 0 ? targets : [emptyRouteTarget()]
  };
};

// 开关关闭时返回 null。开启时必须先过 validateRouteTargets：
// 后端把「没有目标」视为关闭，放过去就成了开关打开、实际没生效
export const nonStreamRouteFromForm = (form) => {
  if (!form?.enabled) return null;
  return {
    mode: form.mode === 'fallback' ? 'fallback' : 'prefer',
    targets: (form.targets || []).filter(isSelectedTarget)
  };
};

// 只比较会提交的字段：渠道名、missing 等展示字段不同不算变更
export const nonStreamRouteSignature = (route) => {
  const targets = (route?.targets || []).filter(isSelectedTarget);
  if (targets.length === 0) return '';
  return JSON.stringify([
    route.mode === 'fallback' ? 'fallback' : 'prefer',
    targets.map((target) => [
      Number(target.channelId),
      Number(target.groupId) || 0,
      String(target.upstreamModel || '').trim()
    ])
  ]);
};

// request_logs.route_snapshot → 请求详情展示。
// target=0 表示最终落在来源渠道（请求发起时的激活渠道）；
// 模型能力改道没有配置目标，target 是按渠道优先级挑出的候选序号
export const describeRouteSnapshot = (raw) => {
  let snapshot = raw;
  if (typeof raw === 'string') {
    if (!raw.trim()) return null;
    try {
      snapshot = JSON.parse(raw);
    } catch {
      return null;
    }
  }
  if (!snapshot || typeof snapshot !== 'object' || !snapshot.kind) return null;

  const kind = String(snapshot.kind);
  const target = Math.max(0, Math.trunc(Number(snapshot.target) || 0));
  const source = snapshot.pool_name || snapshot.channel || '激活渠道';
  const ruleName = `「${snapshot.name || `#${snapshot.id || '?'}`}」`;
  const titles = {
    // 规则挂在渠道上；早期快照没有 channel 字段
    pool: `账号池（${source}）`,
    model_rule: snapshot.pool_name ? `模型规则${ruleName}（${source}）` : snapshot.channel ? `模型规则${ruleName}（${snapshot.channel}）` : `模型规则${ruleName}`,
    non_stream: `非流改道（来自 ${source}）`,
    subscription: `官方订阅与授权备用（${source}）`,
    model_capability: `模型能力改道（${source} 的分组均不支持该模型）`
  };
  let stage = snapshot.pool_name ? '入口池' : '来源渠道';
  if (target > 0) {
    stage = kind === 'model_capability' ? `第 ${target} 个候选渠道` : `第 ${target} 个目标`;
  }
  if (snapshot.member) { const member = snapshot.member; stage = `${member.pool_name || source} · ${member.tier === 'backup' ? '备用' : '主要'} · 账号 #${member.group_id}`; }
  return {
    kind,
    title: titles[kind] || kind,
    mode: routeModeLabel(kind, snapshot.mode),
    stage
  };
};

export const describeRouteExecution = (raw) => {
  let snapshot = raw;
  if (typeof raw === 'string') {
    try { snapshot = JSON.parse(raw); } catch { return null; }
  }
  if (!snapshot || !snapshot.execution || !Array.isArray(snapshot.execution.events)) return null;
  return {
    subscription: snapshot.subscription ? {
      groupId: Number(snapshot.subscription.group_id || 0),
      bindingVersion: Number(snapshot.subscription.binding_version || 0),
      officialEnabled: Boolean(snapshot.subscription.official_enabled),
      availability: (snapshot.subscription.availability || []).map((pause) => ({
        model: String(pause.model || ''), state: String(pause.state || ''), reason: String(pause.reason || ''),
        source: String(pause.source || ''), resumeNotBefore: pause.resume_not_before,
        resetKnown: Boolean(pause.reset_known)
      }))
    } : null,
    attempts: Math.max(0, Number(snapshot.attempts) || 0),
    waitMs: Math.max(0, Number(snapshot.wait_ms) || 0),
    stopReason: String(snapshot.stop_reason || ''),
    dropped: Math.max(0, Number(snapshot.execution.dropped_events) || 0),
    events: snapshot.execution.events.filter((event) => event && typeof event === 'object').map((event) => ({
      channel: String(event.candidate?.channel || ''),
      pool: String(event.member?.pool_name || ''),
      tier: event.member?.tier === 'backup' ? '备用' : event.member ? '主要' : '',
      groupId: Number(event.member?.group_id || 0),
      endpoint: String(event.candidate?.endpoint || ''),
      model: String(event.candidate?.model || ''),
      action: String(event.action || ''),
      reason: String(event.reason || ''),
      scope: String(event.failure_scope || ''),
    })),
  };
};
