import { WailsApp, initWails } from './connection.js';

// ============================================
// 请求级路由：模型路由规则 + 渠道非流改道
// 两者共用同一种目标结构 {channel_id, group_id, upstream_model}；group_id=0 表示渠道下全部分组
// ============================================

const mapRouteTarget = (target = {}) => ({
  channelId: Number(target.channel_id ?? target.channelId ?? 0),
  groupId: Number(target.group_id ?? target.groupId ?? 0),
  upstreamModel: target.upstream_model ?? target.upstreamModel ?? '',
  channelName: target.channel_name ?? target.channelName ?? '',
  groupName: target.group_name ?? target.groupName ?? '',
  missing: Boolean(target.missing)
});

const buildRouteTargetsPayload = (targets = []) =>
  (Array.isArray(targets) ? targets : [])
    .map((target) => ({
      channel_id: Number(target?.channelId ?? target?.channel_id ?? 0),
      group_id: Number(target?.groupId ?? target?.group_id ?? 0),
      upstream_model: String(target?.upstreamModel ?? target?.upstream_model ?? '').trim()
    }))
    .filter((target) => Number.isInteger(target.channel_id) && target.channel_id > 0);

// 非流改道：后端返回 null 表示关闭
function mapNonStreamRoute(route) {
  if (!route || !Array.isArray(route.targets) || route.targets.length === 0) return null;
  return { mode: route.mode || 'prefer', targets: route.targets.map(mapRouteTarget) };
}

const mapModelRoute = (route = {}) => ({
  id: Number(route.id ?? 0),
  provider: route.provider || 'claude',
  sourceChannelId: Number(route.source_channel_id ?? route.sourceChannelId ?? 0),
  name: route.name || '',
  enabled: Boolean(route.enabled),
  priority: Number(route.priority ?? 100),
  matchModel: route.match_model ?? route.matchModel ?? '',
  mode: route.mode || 'prefer',
  targets: (route.targets || []).map(mapRouteTarget),
  updatedAt: route.updated_at ?? route.updatedAt ?? ''
});

const buildModelRoutePayload = (input = {}) => {
  const priority = Number.parseInt(input.priority, 10);
  return {
    id: Number(input.id || 0),
    provider: input.provider || 'claude',
    source_channel_id: Number(input.sourceChannelId ?? input.source_channel_id ?? 0),
    name: String(input.name || '').trim(),
    enabled: input.enabled !== false,
    priority: Number.isInteger(priority) ? priority : 100,
    match_model: String(input.matchModel ?? input.match_model ?? '').trim(),
    mode: input.mode || 'prefer',
    targets: buildRouteTargetsPayload(input.targets)
  };
};

const mapRouteSaveResult = (result) => ({
  id: Number(result?.id ?? 0),
  warnings: Array.isArray(result?.warnings) ? result.warnings : []
});

// 模型路由规则挂在来源渠道上，只在该渠道为激活渠道时生效
const listModelRoutes = async (provider, sourceChannelId) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const routes = await WailsApp.ListModelRoutes(provider || 'claude', Number(sourceChannelId));
  return (routes || []).map(mapModelRoute);
};

const saveModelRoute = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return mapRouteSaveResult(await WailsApp.SaveModelRoute(buildModelRoutePayload(input)));
};

const deleteModelRoute = async (id) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.DeleteModelRoute(Number(id));
  return { success: true };
};

const setModelRouteEnabled = async (id, enabled) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  await WailsApp.SetModelRouteEnabled(Number(id), Boolean(enabled));
  return { success: true };
};

const getRouteTargetOptions = async (provider) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const options = await WailsApp.GetRouteTargetOptions(provider || 'claude');
  return {
    channels: (options?.channels || []).map((channel) => ({
      id: Number(channel.id ?? 0),
      name: channel.name || '',
      priority: Number(channel.priority ?? 0),
      paused: Boolean(channel.paused),
      active: Boolean(channel.active),
      nonStreamRouteEnabled: Boolean(channel.non_stream_route_enabled),
      groups: (channel.groups || []).map((group) => ({ id: Number(group.id ?? 0), name: group.name || '' }))
    })),
    models: Array.isArray(options?.models) ? options.models : []
  };
};

// 预演假设 sourceChannelId 为激活渠道
const previewRoute = async (provider, sourceChannelId, model, streaming) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const result = await WailsApp.PreviewRoute(provider || 'claude', Number(sourceChannelId), String(model || '').trim(), Boolean(streaming));
  return {
    kind: result?.kind || '',
    mode: result?.mode || '',
    ruleId: Number(result?.rule_id ?? 0),
    ruleName: result?.rule_name || '',
    sourceChannel: result?.source_channel || '',
    stages: (result?.stages || []).map((stage) => ({
      target: Number(stage.target ?? 0),
      channelId: Number(stage.channel_id ?? 0),
      channelName: stage.channel_name || '',
      groupId: Number(stage.group_id ?? 0),
      groupName: stage.group_name || '',
      upstreamModel: stage.upstream_model || '',
      missing: Boolean(stage.missing),
      paused: Boolean(stage.paused)
    }))
  };
};

// route 为 null 或没有目标表示关闭非流改道
const setChannelNonStreamRoute = async (provider, channelId, route) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const payload = {
    mode: route?.mode || 'prefer',
    targets: route ? buildRouteTargetsPayload(route.targets) : []
  };
  return mapRouteSaveResult(await WailsApp.SetChannelNonStreamRoute(provider || 'claude', Number(channelId), payload));
};

/**
 * 按渠道获取端点
 * @param {string} channel - 渠道名称
 * @returns {Promise<Array>} - 端点记录数组
 */
const getEndpointsByChannel = async (channel) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const records = await WailsApp.GetEndpointsByChannel(channel);
  return (records || []).map(r => ({
    id: r.id,
    channel: r.channel,
    name: r.name,
    url: r.url,
    tokenMasked: r.token_masked,
    priority: r.priority,
    failoverEnabled: r.failover_enabled,
    enabled: r.enabled,
    healthy: r.healthy,
    responseTimeMs: r.response_time_ms ?? r.responseTimeMs ?? r.ResponseTimeMs,
    createdAt: r.created_at || r.createdAt || r.CreatedAt || '',
    updatedAt: r.updated_at || r.updatedAt || r.UpdatedAt || '',
    lastCheck: r.last_check || r.lastCheck || r.LastCheck || ''
  }));
};

export { mapNonStreamRoute, mapRouteTarget, buildRouteTargetsPayload, mapModelRoute, buildModelRoutePayload, listModelRoutes, saveModelRoute, deleteModelRoute, setModelRouteEnabled, getRouteTargetOptions, previewRoute, setChannelNonStreamRoute, getEndpointsByChannel };
