import { Clock3, Coins, FolderOpen, KeyRound, Layers3, MessageSquare, Network, Zap } from 'lucide-react';
import { count, duration, money, projectName } from '../usageDashboard.js';

export default function UsageSummary({ data, source }) {
  const local = source === 'local';
  const stats = data?.summary;
  const cacheRate = stats?.cacheHitRate == null ? '暂无输入用量' : `命中率 ${(stats.cacheHitRate * 100).toFixed(1)}%`;
  const topProject = data?.projects.find((item) => item.key);
  const items = [
    local ? { label: '会话数', value: count(stats?.sessions), detail: `中位 ${count(stats?.medianTokens)} · p90 ${count(stats?.p90Tokens)}`, icon: MessageSquare }
      : { label: '请求数', value: count(stats?.requests), detail: `成功 ${count(stats?.successRequests)} · 失败 ${count(stats?.failedRequests)}`, icon: Network },
    { label: 'Token', value: count(stats?.tokens), detail: `输入 ${count(stats?.input)} · 输出 ${count(stats?.output)}`, icon: Zap },
    { label: local ? '估算费用' : '记录费用', value: money(stats?.cost, stats?.unpriced, local), detail: stats?.unpriced ? `${data.unpricedModels.length} 个模型未计价 · ${count(stats.unpriced)} 次调用` : local ? '按当前模型定价估算' : '使用请求计费快照', icon: Coins },
    { label: '缓存读取', value: count(stats?.cacheRead), detail: `${cacheRate} · 写入 ${count(stats?.cacheCreation)}`, icon: Layers3 },
    local ? { label: '活跃时长', value: duration(stats?.activeSeconds), detail: `共 ${stats?.activeDays || 0} 个活跃日`, icon: Clock3 }
      : { label: '平均响应', value: `${((stats?.avgDurationMs || 0) / 1000).toFixed(2)} 秒`, detail: `${stats?.activeDays || 0} 个有请求的日期`, icon: Clock3 },
    local ? { label: '项目', value: count(stats?.projects), detail: topProject && stats?.tokens ? `${projectName(topProject.key)} 占 ${(topProject.tokens / stats.tokens * 100).toFixed(0)}%` : '按会话工作目录归属', icon: FolderOpen }
      : { label: '凭证分组', value: count(stats?.credentialGroups), detail: '本期实际产生调用的分组', icon: KeyRound },
  ];
  return <section className="usage-summary" aria-label="用量汇总">{items.map(({ label, value, detail, icon: Icon }) => <div className="usage-summary-cell" key={label}>
    <div className="usage-summary-label"><Icon size={14} /><span>{label}</span></div>
    <div className="usage-summary-value" data-long={value.length > 10}>{stats ? value : '—'}</div>
    <div className="usage-summary-detail">{stats ? detail : '等待用量数据'}</div>
  </div>)}</section>;
}
