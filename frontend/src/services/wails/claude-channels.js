import { normalizeModelMapping, hasOwn } from './mapping.js';
import { mapClaudeCredentialGroup } from './claude-endpoints.js';
import { normalizeChannelPrivacyLevel } from './privacy.js';
import { WailsApp, initWails } from './connection.js';
import { mapNonStreamRoute } from './routing.js';

// ============================================
// 组管理 API
// ============================================

const getGroups = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  // 获取组、端点和 Keys 信息
  const [groups, endpointsData, keysData] = await Promise.all([
    WailsApp.GetGroups(),
    WailsApp.GetEndpoints(),
    WailsApp.GetKeysOverview().catch(() => ({ endpoints: [] })) // 容错：获取 keys 失败不影响主流程
  ]);

  // 组名 = 渠道(channel)（后端已在 GetEndpoints 中映射到 ep.group）
  // 计算每个组的健康端点统计
  const groupHealthMap = new Map();
  endpointsData.forEach(ep => {
    const groupName = ep.group || ep.channel || ep.name;
    if (!groupHealthMap.has(groupName)) {
      groupHealthMap.set(groupName, { total: 0, healthy: 0 });
    }
    const stats = groupHealthMap.get(groupName);
    stats.total++;
    if (ep.healthy) {
      stats.healthy++;
    }
  });

  // 构建端点名到 tokens 的映射
  const endpointTokensMap = new Map();
  (keysData.endpoints || []).forEach(ep => {
    const tokens = (ep.tokens || []).map(t => ({
      index: t.index,
      name: t.name || `Token ${t.index + 1}`,
      key: t.value, // 脱敏的 key 值
      is_active: t.is_active,
      endpoint: ep.endpoint, // 关联的端点名
      type: inferTokenType(t.name) // 推断 Token 类型
    }));
    endpointTokensMap.set(ep.endpoint, tokens);
  });

  // 将端点 tokens 聚合到组（渠道）
  const groupTokensMap = new Map();
  endpointsData.forEach(ep => {
    const groupName = ep.group || ep.channel || ep.name;
    const endpointTokens = endpointTokensMap.get(ep.name) || [];
    if (!groupTokensMap.has(groupName)) {
      groupTokensMap.set(groupName, []);
    }
    const existing = groupTokensMap.get(groupName);
    existing.push(...endpointTokens);
  });

  // 推断 Token 类型的辅助函数
  function inferTokenType(name) {
    if (!name) return 'Std';
    const lowerName = name.toLowerCase();
    if (lowerName.includes('pro') || lowerName.includes('特价')) return 'Pro';
    if (lowerName.includes('ent') || lowerName.includes('主号')) return 'Ent';
    if (lowerName.includes('free') || lowerName.includes('测试')) return 'Free';
    return 'Std';
  }

  // 转换为前端期望的格式
  const formattedGroups = groups.map(g => {
    const healthStats = groupHealthMap.get(g.name) || { total: 0, healthy: 0 };
    // 组名 = 渠道(channel)，聚合组内所有端点 tokens
    const tokens = groupTokensMap.get(g.name) || [];

    return {
      name: g.name,
      channel: g.channel,  // 渠道名称
      is_active: g.active,
      paused: g.paused,
      priority: g.priority,
      endpoint_count: g.endpoint_count,
      total_endpoints: healthStats.total,
      healthy_endpoints: healthStats.healthy,
      unhealthy_endpoints: healthStats.total - healthStats.healthy,
      in_cooldown: g.in_cooldown,
      cooldown_remain_ms: g.cooldown_remain_ms,
      tokens: tokens // 添加 tokens 数组
    };
  });

  const activeGroup = formattedGroups.find(g => g.is_active);

  return {
    groups: formattedGroups,
    active_group: activeGroup?.name || null,
    total_suspended_requests: 0
  };
};

// 获取后端原始组信息（轻量：仅 GetGroups，不做额外聚合）
const getGroupsRaw = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const groups = await WailsApp.GetGroups();
  return Array.isArray(groups) ? groups : [];
};

// 检查是否启用渠道间故障转移
// 用于前端判断是否显示渠道切换器
const isInterChannelFailoverEnabled = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.IsInterChannelFailoverEnabled();
};

// 获取下一个可用渠道（用于"暂停活跃渠道"前的提示/确认）
const getNextAvailableChannel = async (excludeChannel) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const name = String(excludeChannel || '').trim();
  return await WailsApp.GetNextAvailableChannel(name);
};

const activateGroup = async (groupName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.ActivateGroup(groupName);
  return { success: true };
};

const pauseGroup = async (groupName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.PauseGroup(groupName);
  return { success: true };
};

const resumeGroup = async (groupName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.ResumeGroup(groupName);
  return { success: true };
};

/**
 * 获取所有渠道
 * @returns {Promise<Array>} - 渠道列表 [{name, endpointCount}]
 */
const getChannels = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const channels = await WailsApp.GetChannels();
  return (channels || []).map(c => ({
    id: Number(c.id ?? 0),
    name: c.name,
    website: c.website || '',
    priority: c.priority || 1,
    modelMapping: normalizeModelMapping(c.model_mapping || c.modelMapping),
    nonStreamRoute: mapNonStreamRoute(c.non_stream_route || c.nonStreamRoute),
    privacyLevel: normalizeChannelPrivacyLevel(c.privacy_level ?? c.privacyLevel),
    createdAt: c.created_at || '',
    endpointCount: c.endpoint_count,
    credentialGroups: (c.credential_groups || c.credentialGroups || []).map(mapClaudeCredentialGroup)
  }));
};

const createChannel = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const priority = parseInt(input?.priority, 10);
  const payload = {
    name: (input?.name || '').trim(),
    website: (input?.website || '').trim(),
    // 传 0 时让后端自动递增
    priority: Number.isInteger(priority) ? priority : 0,
    model_mapping: normalizeModelMapping(input?.modelMapping || input?.model_mapping)
  };

  await WailsApp.CreateChannel(payload);
  return { success: true };
};

const updateChannel = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const channelName = (input?.name || '').trim();
  if (!channelName) throw new Error('渠道名称不能为空');

  const priority = parseInt(input?.priority, 10);
  const payload = {
    name: channelName,
    website: (input?.website || '').trim(),
    priority: Number.isFinite(priority) && priority > 0 ? priority : 1
  };
  if (hasOwn(input, 'modelMapping') || hasOwn(input, 'model_mapping')) {
    payload.model_mapping = normalizeModelMapping(input?.modelMapping || input?.model_mapping);
  }

  await WailsApp.UpdateChannel(payload);
  return { success: true };
};

const deleteChannel = async (name, deleteEndpoints = false) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const channelName = (name || '').trim();
  if (!channelName) throw new Error('渠道名称不能为空');

  await WailsApp.DeleteChannel(channelName, !!deleteEndpoints);
  return { success: true };
};

export { getGroups, getGroupsRaw, isInterChannelFailoverEnabled, getNextAvailableChannel, activateGroup, pauseGroup, resumeGroup, getChannels, createChannel, updateChannel, deleteChannel };
