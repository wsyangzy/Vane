// ============================================
// EndpointRow - 端点表格行组件
// ============================================

import { useState } from 'react';
import {
  Globe,
  Pencil,
  Trash2,
  ArrowRightLeft,
  Calculator,
  ShieldCheck,
  CheckCircle2,
  XCircle,
  Clock,
  // Timer, // v6.2: 已移除，冷却机制不再使用
  ShieldOff,
  ShieldBan,
  Loader2
} from 'lucide-react';
import PriorityBadge from './PriorityBadge.jsx';

import { SetEndpointSkipHealthCheckByID } from '@wailsjs/go/main/App';

// ============================================
// 健康状态徽章
// v6.1: 支持四种状态 - skipped/unchecked/healthy/unhealthy
// v6.1.0: 新增 request_failed 状态，区分健康检查失败和请求转发失败
// v6.2.10: 异常状态支持显示失败原因 tooltip
// ============================================

const HealthBadge = ({ healthState, failCode, failReason }) => {
  // 构建失败原因的 tooltip 文本
  const buildFailTooltip = () => {
    if (!failCode && !failReason) return '端点当前不可用';
    const parts = [];
    if (failCode) parts.push(`状态码: ${failCode}`);
    if (failReason) parts.push(`原因: ${failReason}`);
    return parts.join('\n');
  };

  switch (healthState) {
    case 'skipped':
      return (
        <div className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-purple-50 text-purple-600 border border-purple-200 whitespace-nowrap flex-shrink-0">
          <ShieldOff size={10} className="mr-1" />
          跳过检测
        </div>
      );
    case 'unchecked':
      return (
        <div className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-slate-50 text-slate-400 border border-slate-200">
          <Clock size={10} className="mr-1" />
          未检测
        </div>
      );
    case 'healthy':
      return (
        <div className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-emerald-50 text-emerald-600 border border-emerald-100">
          <CheckCircle2 size={10} className="mr-1" />
          健康
        </div>
      );
    // v6.1.0: 请求失败状态（与健康检查失败区分）
    case 'request_failed':
      return (
        <div
          className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-orange-50 text-orange-600 border border-orange-200 cursor-help"
          title={buildFailTooltip()}
        >
          <XCircle size={10} className="mr-1" />
          请求失败
          {failCode > 0 && (
            <span className="ml-1 font-mono">({failCode})</span>
          )}
        </div>
      );
    case 'unhealthy':
      return (
        <div
          className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-rose-50 text-rose-600 border border-rose-100 cursor-help"
          title={buildFailTooltip()}
        >
          <XCircle size={10} className="mr-1" />
          异常
          {failCode > 0 && (
            <span className="ml-1 font-mono">({failCode})</span>
          )}
        </div>
      );
    default:
      // 兼容：未提供 health_state 时回退到旧逻辑
      return (
        <div className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-slate-50 text-slate-400 border border-slate-200">
          <Clock size={10} className="mr-1" />
          -
        </div>
      );
  }
};

// ============================================
// v6.2: CooldownBadge 已移除，冷却机制不再使用
// ============================================

// ============================================
// 延迟指示器
// ============================================

const LatencyBadge = ({ ms }) => {
  const msValue = Number(ms);
  if (!Number.isFinite(msValue) || msValue <= 0) return <span className="text-slate-300 text-xs">-</span>;

  const msInt = Math.max(1, Math.round(msValue));

  let colorClass = 'text-emerald-600 bg-emerald-50 border-emerald-100';
  if (msInt > 500) colorClass = 'text-amber-600 bg-amber-50 border-amber-100';
  if (msInt > 1000) colorClass = 'text-rose-600 bg-rose-50 border-rose-100';

  return (
    <span className={`font-mono text-xs font-medium px-2 py-0.5 rounded border whitespace-nowrap ${colorClass}`}>
      {msInt}ms
    </span>
  );
};

// ============================================
// 端点表格行组件
// ============================================

const EndpointRow = ({
  endpoint,
  storageMode,
  onActivateGroup,
  onEdit,
  onDelete,
  onToggle,
  isGrouped = false,
  onRefresh // v6.2.11: 刷新列表回调
}) => {
  // v6.2.11: 跳过健康检测切换状态
  const [skipToggling, setSkipToggling] = useState(false);

  if (!endpoint) return null;

  // 格式化最后检查时间
  const formatLastCheck = (time) => {
    if (!time || time === '-') return '-';
    try {
      const date = new Date(time);
      return date.toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit'
      });
    } catch {
      return time;
    }
  };

  const isSqliteMode = storageMode === 'sqlite';
  const isActive = isSqliteMode ? endpoint.enabled : endpoint.group_is_active;
  const responseTime = endpoint.response_time || endpoint.responseTimeMs || 0;
  // v6.1: 使用 health_state 字段
  const healthState = endpoint.health_state || endpoint.healthState;
  // v6.2.10: 获取失败信息
  const lastFailCode = endpoint.last_fail_code || endpoint.lastFailCode || 0;
  const lastFailReason = endpoint.last_fail_reason || endpoint.lastFailReason || '';
  // v6.2.11: 跳过健康检测状态
  const skipHealthCheck = endpoint.skip_health_check || endpoint.skipHealthCheck || false;
  const healthBadges = [];
  if (skipHealthCheck) {
    healthBadges.push(<HealthBadge key="skipped" healthState="skipped" />);
  }
  if (healthState && healthState !== 'skipped') {
    healthBadges.push(
      <HealthBadge key={healthState} healthState={healthState} failCode={lastFailCode} failReason={lastFailReason} />
    );
  } else if (!skipHealthCheck) {
    healthBadges.push(
      <HealthBadge key="unknown" healthState={healthState} failCode={lastFailCode} failReason={lastFailReason} />
    );
  }

  // v6.2.11: 切换跳过健康检测
  const handleToggleSkipHealthCheck = async () => {
    if (!endpoint.id || skipToggling) return;

    setSkipToggling(true);
    try {
      await SetEndpointSkipHealthCheckByID(endpoint.id, !skipHealthCheck);
      // 刷新列表
      onRefresh?.();
    } catch (err) {
      console.error('Failed to toggle skip health check:', err);
    } finally {
      setSkipToggling(false);
    }
  };

  // 获取认证类型显示
  const getAuthType = () => {
    if (endpoint.token || endpoint.tokenMasked) return 'Token';
    if (endpoint.apiKey) return 'API Key';
    return null;
  };

  return (
    <tr className={`transition-colors group ${isActive ? 'hover:bg-slate-50/50' : 'bg-slate-50/30 opacity-70'}`}>
      {/* 启用状态 Toggle */}
      <td className={`py-4 ${isGrouped ? 'px-6 pl-10' : 'px-6'}`}>
        <div className="flex items-center">
          {isGrouped && (
            <span className="text-slate-300 mr-2">└</span>
          )}
          <div
          className="cursor-pointer"
          onClick={() => {
            if (isSqliteMode) {
              onToggle?.(endpoint.name, !isActive);
            } else {
              onActivateGroup?.(endpoint.name, endpoint.group);
            }
          }}
        >
          {isActive ? (
            <div className="w-10 h-6 bg-emerald-500 rounded-full relative transition-colors shadow-inner">
              <div className="absolute right-1 top-1 w-4 h-4 bg-white rounded-full shadow-sm"></div>
            </div>
          ) : (
            <div className="w-10 h-6 bg-slate-200 rounded-full relative transition-colors shadow-inner">
              <div className="absolute left-1 top-1 w-4 h-4 bg-white rounded-full shadow-sm"></div>
            </div>
          )}
        </div>
        </div>
      </td>

      {/* 渠道 / 名称 / 健康状态 */}
      <td className="px-6 py-4">
        <div className="flex flex-col space-y-1.5">
          <span className="font-bold text-slate-900 text-sm">{endpoint.name}</span>
          <div className="flex items-center space-x-2 flex-wrap gap-y-1">
            <span className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-medium bg-blue-50 text-blue-600 border border-blue-100">
              {endpoint.channel || endpoint.group || '-'}
            </span>
            {healthBadges}
            {/* v6.2: CooldownBadge 已移除 */}
          </div>
        </div>
      </td>

      {/* URL / 认证 */}
      <td className="px-6 py-4">
        <div className="flex flex-col space-y-1.5">
          <div className="flex items-center text-slate-500 max-w-[240px]" title={endpoint.url}>
            <Globe size={12} className="mr-1.5 text-slate-400 flex-shrink-0" />
            <span className="truncate text-xs font-mono">{endpoint.url}</span>
          </div>
          {getAuthType() && (
            <div className="flex items-center">
              <div className="flex items-center text-[10px] text-slate-400 bg-slate-100 px-1.5 py-0.5 rounded border border-slate-200">
                <ShieldCheck size={10} className="mr-1 text-amber-500" />
                已配置 {getAuthType()}
              </div>
            </div>
          )}
        </div>
      </td>

      {/* 优先级 */}
      <td className="px-6 py-4 text-center">
        <PriorityBadge priority={endpoint.priority || 1} size="md" />
      </td>

      {/* 高级特性 */}
      <td className="px-6 py-4">
        <div className="flex items-center space-x-2">
          <div
            className={`p-1.5 rounded-md ${endpoint.failoverEnabled !== false ? 'bg-indigo-50 text-indigo-600' : 'bg-slate-100 text-slate-300'}`}
            title="故障转移"
          >
            <ArrowRightLeft size={14} />
          </div>
          <div
            className={`p-1.5 rounded-md ${endpoint.supportsCountTokens ? 'bg-purple-50 text-purple-600' : 'bg-slate-100 text-slate-300'}`}
            title="支持 Token 计数"
          >
            <Calculator size={14} />
          </div>
        </div>
      </td>

      {/* 响应延迟 */}
      <td className="px-6 py-4 text-center">
        <LatencyBadge ms={responseTime} />
      </td>

      {/* 倍率 */}
      <td className="px-6 py-4 text-center">
        <span className={`text-xs font-mono font-medium px-2 py-1 rounded ${
          (endpoint.costMultiplier || 1) > 1.0
            ? 'bg-orange-50 text-orange-600 border border-orange-100'
            : 'text-slate-500 bg-slate-50'
        }`}>
          {endpoint.costMultiplier || 1.0}x
        </span>
      </td>

      {/* 最后检查 */}
      <td className="px-6 py-4 text-slate-400 font-mono text-xs">
        {formatLastCheck(endpoint.lastCheck || endpoint.last_check || endpoint.updatedAt)}
      </td>

      {/* 操作 */}
      <td className="px-6 py-4 text-right">
        <div className="flex items-center justify-end space-x-1">
          {isSqliteMode && (
            <>
              {/* v6.2.11: 跳过健康检测快捷开关 */}
              <button
                onClick={handleToggleSkipHealthCheck}
                disabled={skipToggling}
                className={`p-1.5 rounded-md transition-colors ${
                  skipToggling
                    ? 'opacity-50 cursor-wait'
                    : skipHealthCheck
                      ? 'bg-purple-50 text-purple-600 hover:bg-purple-100'
                      : 'text-slate-400 hover:bg-slate-100 hover:text-purple-600'
                }`}
                title={skipHealthCheck ? '已跳过健康检测 (点击关闭)' : '跳过健康检测 (点击开启)'}
              >
                {skipToggling ? (
                  <Loader2 size={14} className="animate-spin" />
                ) : skipHealthCheck ? (
                  <ShieldOff size={14} />
                ) : (
                  <ShieldBan size={14} />
                )}
              </button>
              <button
                onClick={() => onEdit?.(endpoint)}
                className="p-1.5 text-slate-400 hover:bg-slate-100 hover:text-indigo-600 rounded-md transition-colors"
                title="编辑"
              >
                <Pencil size={14} />
              </button>
              <button
                onClick={() => onDelete?.(endpoint)}
                className="p-1.5 text-slate-400 hover:bg-rose-50 hover:text-rose-600 rounded-md transition-colors"
                title="删除"
              >
                <Trash2 size={14} />
              </button>
            </>
          )}
        </div>
      </td>
    </tr>
  );
};

export default EndpointRow;
