// 凭证分组的端点归属与转移统计
// ============================================
// 端点归属规则必须与两侧 ChannelCard 的 groupEndpointFilter 完全一致，否则
// 分段条上的指示器会和下方端点列表对不上号。

export const isDefaultGroup = (group) => Boolean(group?.isDefault ?? group?.is_default);

export const getGroupID = (group) => Number(group?.id || 0);

// 显式指向该分组，或未指定分组而被默认分组收编
const endpointBelongsToGroup = (endpoint, group) => {
  const endpointGroupID = Number(endpoint?.credentialGroupId ?? endpoint?.credential_group_id ?? 0);
  if (endpointGroupID > 0) return endpointGroupID === getGroupID(group);
  return isDefaultGroup(group);
};

// failoverEnabled 缺省视为参与；历史数据里出现过 '0' 字符串，只判 !== false 会漏
const participatesInFailover = (endpoint) => {
  const value = endpoint?.failoverEnabled ?? endpoint?.failover_enabled;
  if (value === undefined || value === null) return true;
  return !(value === false || value === 0 || value === '0');
};

export const buildCredentialGroupStats = (groups = [], endpoints = []) => {
  const stats = {};
  groups.forEach((group) => {
    const owned = endpoints.filter((endpoint) => endpointBelongsToGroup(endpoint, group));
    stats[getGroupID(group)] = {
      total: owned.length,
      failover: owned.filter(participatesInFailover).length,
      // 不参与的排在后面：气泡里的清单和上方的计数读起来才是同一个顺序
      endpoints: owned
        .map((endpoint) => ({
          name: endpoint?.name || '未命名端点',
          failover: participatesInFailover(endpoint)
        }))
        .sort((left, right) => Number(right.failover) - Number(left.failover))
    };
  });
  return stats;
};
