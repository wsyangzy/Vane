export const mapBlockage = (value) => value ? {
  provider: value.provider || '', channelId: Number(value.channel_id ?? value.channelId) || 0,
  groupId: Number(value.group_id ?? value.groupId) || 0, endpointId: String(value.endpoint_id ?? value.endpointId ?? ''),
  channelName: value.channel_name ?? value.channelName ?? '', groupName: value.group_name ?? value.groupName ?? '',
  endpointName: value.endpoint_name ?? value.endpointName ?? '', model: value.model || '', reason: value.reason || '',
  cause: value.cause || '', recovery: value.recovery || 'unknown', recoverAt: value.recover_at ?? value.recoverAt ?? null,
  dailyUsedUsd: value.daily_used_usd ?? value.dailyUsedUsd ?? null, dailyLimitUsd: value.daily_limit_usd ?? value.dailyLimitUsd ?? null,
  observedAt: value.observed_at ?? value.observedAt ?? null,
} : null;

const labels = {
  daily_cost_limit_exceeded: '已达每日成本限额', daily_cost_state_unavailable: '每日成本读取失败',
  cooldown: '端点冷却中', cooling_down: '端点冷却中', account_cooling_down: '端点冷却中',
  account_repair_required: '凭证需要修复', credential_repair_required: '凭证需要修复', credential_rejected: '上游拒绝凭证',
  manual_balance_exhausted: '本地记录的余额已耗尽', manual_balance_unavailable: '本地余额读取失败',
  account_disabled: '分组已停用', candidate_disabled: '候选配置不可用', account_missing: '分组已不存在',
  account_probe_in_progress: '正在验证恢复', quota_exhausted: '供应商额度已耗尽',
  concurrency_limit: '达到并发上限', candidate_busy: '候选正在处理请求', model_unsupported: '不支持此模型', model_unavailable: '模型不可用',
  credential_missing: '分组缺少凭证', protocol_incompatible: '接口协议不兼容', profile_disabled: '调度方案已停用',
  subscription_state_unavailable: '订阅账号状态读取失败', subscription_account_disabled: '订阅账号已停用',
  subscription_account_unavailable: '订阅账号已不可用', subscription_binding_unavailable: '分组缺少有效订阅绑定',
  subscription_authentication_required: '订阅账号需要重新登录', subscription_sign_in_required: '订阅账号需要重新登录',
  subscription_authentication_rejected: '订阅凭证格式无效', subscription_account_header_rejected: '客户端账号身份头无效',
  state_backend_incompatible: '当前会话状态与此分组不兼容', state_origin_unknown: '无法确认会话状态来源',
};

export const blockageLabel = (reason) => labels[reason] || reason || '可发送，等待业务请求验证';
export const isHealthCooldown = (reason) => ['cooldown', 'cooling_down', 'account_cooling_down'].includes(reason);
const date = (value) => value && Number.isFinite(new Date(value).getTime()) ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '';
const usd = (value) => `$${Number(value).toFixed(4)}`;

export function describeBlockageRecovery(value, now = Date.now()) {
  const state = mapBlockage(value) || {};
  const reset = date(state.recoverAt);
  const expired = reset && new Date(state.recoverAt).getTime() <= now;
  const costs = state.dailyUsedUsd != null && state.dailyLimitUsd != null
    ? `今日已用 ${usd(state.dailyUsedUsd)} / 限额 ${usd(state.dailyLimitUsd)}`
    : state.reason?.startsWith('daily_cost_') ? '此记录未保存已用金额与限额，请刷新当前状态核对。' : '';
  let recovery = state.recovery === 'ready' ? '当前可发送，仍需业务请求验证上游是否正常。'
    : state.recovery === 'automatic' && reset ? expired ? '记录的等待时间已到，请刷新当前状态；这不代表上游已恢复。' : `${state.reason === 'daily_cost_limit_exceeded' ? '限额重置' : '可重新尝试'}：${reset}`
      : state.recovery === 'manual' ? '需要手动处理，单纯等待不会解除。'
        : state.recovery === 'pending' || state.recovery === 'probing' ? '等待正在处理的请求结束后重新检查。'
          : '恢复时间未知，请刷新当前状态核对。';
  let action = '';
  if (state.reason === 'daily_cost_limit_exceeded') action = '前往渠道资源编辑此凭证分组的每日成本限额，或等待下一自然日重置；调整后重新发起请求。';
  else if (state.reason === 'manual_balance_exhausted') action = '在“额度与健康”中核对平台余额，并记录充值或校准本地余额。';
  else if (state.reason === 'account_disabled' || state.reason === 'candidate_disabled') action = '检查此分组的凭证与端点配置，并在调度方案中启用需要参与的成员。';
  else if (state.reason?.startsWith('state_')) action = '使用原分组继续会话，或在当前分组新建会话后重试。';
  else if (state.reason?.includes('repair') || state.reason === 'credential_rejected') action = '核对并更新此分组的凭证；修复后允许同一分组重新尝试。';
  else if (isHealthCooldown(state.reason)) action = '核对上游最近失败原因；修复后可让同一分组重新尝试，失败时仍按原策略冷却。';
  else if (state.reason === 'quota_exhausted') action = '在“额度与健康”刷新供应商额度；有明确重置时间时可等待重置。';
  else if (['model_unsupported', 'model_unavailable', 'protocol_incompatible'].includes(state.reason)) action = '检查分组支持的模型、模型映射和端点协议配置，再使用匹配的模型发起请求。';
  else if (state.reason?.startsWith('subscription_')) action = '在订阅账号中核对账号是否启用，完成重新登录或凭证刷新，并检查分组绑定；客户端凭证透传时检查客户端登录与身份头。';
  else if (state.reason?.includes('unavailable')) action = '刷新当前状态；若仍失败，请检查数据读取或分组配置。';
  else if (state.reason === 'account_missing') action = '检查当前渠道资源；历史记录引用的分组可能已被删除。';
  else if (state.reason === 'credential_missing') action = '在渠道资源中为此凭证分组补全凭证。';
  else if (state.reason === 'profile_disabled') action = '在调度方案中启用需要使用的方案。';
  else if (state.recovery === 'pending' || state.recovery === 'probing') action = '等待当前请求结束后刷新状态，再发起请求。';
  else if (state.reason) action = '核对渠道资源与最近失败记录，处理原因后刷新当前状态。';
  if (state.reason === 'daily_cost_limit_exceeded' && !reset) recovery = '每日成本限额按应用时区的下一自然日重置；此旧记录未保存准确时刻。';
  return { label: blockageLabel(state.reason), costs, recovery, action, cause: state.cause };
}
