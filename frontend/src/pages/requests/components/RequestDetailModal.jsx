// ============================================
// RequestDetailModal - 请求详情模态框
// Command Palette 风格
// 2025-12-01
// ============================================

import { useState, useEffect } from 'react';
import {
  X,
  Copy,
  Check,
  Clock,
  Activity,
  DollarSign,
  Server,
  Layers,
  FileText,
  Waves,
  RefreshCw,
  Calendar,
  Database,
  AlertCircle,
  TrendingUp,
  TrendingDown,
  Zap
} from 'lucide-react';
import RequestStatusBadge from './RequestStatusBadge.jsx';
import ModelTag from './ModelTag.jsx';
import DurationBadge from './DurationBadge.jsx';
import { formatCost, formatTimestamp } from '@utils/api.js';

const CHAIN_VIEW_STORAGE_KEY = 'request-detail.chain-view-mode';

const getCompactColumns = () => {
  if (typeof window === 'undefined') return 3;
  const width = window.innerWidth;
  if (width >= 1024) return 3;
  if (width >= 640) return 2;
  return 1;
};

/**
 * 信息行组件
 */
const InfoRow = ({ icon: Icon, label, value, copyable = false }) => {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    if (copyable && value) {
      navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div className="flex items-start justify-between py-3 border-b border-slate-100 last:border-b-0 group">
      <div className="flex items-center gap-2 text-slate-500 min-w-[120px]">
        <Icon className="w-4 h-4 flex-shrink-0" />
        <span className="text-sm font-medium">{label}</span>
      </div>
      <div className="flex items-center gap-2 flex-1 justify-end">
        <span className="text-sm font-mono text-slate-900 text-right">
          {value || '-'}
        </span>
        {copyable && (
          <button
            onClick={handleCopy}
            className="p-1 text-slate-400 hover:text-slate-600 hover:bg-slate-100 hover:scale-110 rounded transition-all"
            title="复制"
          >
            {copied ? (
              <Check className="w-3.5 h-3.5 text-emerald-600" />
            ) : (
              <Copy className="w-3.5 h-3.5" />
            )}
          </button>
        )}
      </div>
    </div>
  );
};

/**
 * Token 指标卡片
 */
const TokenCard = ({ icon: Icon, label, value, colorClass }) => (
  <div className={`rounded-lg p-4 border ${colorClass.border} ${colorClass.bg}`}>
    <div className="flex items-center justify-between mb-2">
      <Icon className={`w-4 h-4 ${colorClass.icon}`} />
    </div>
    <div className={`text-2xl font-bold font-mono ${colorClass.text}`}>
      {(value || 0).toLocaleString()}
    </div>
    <div className="text-xs text-slate-500 mt-1">{label}</div>
  </div>
);

/**
 * 请求详情模态框主组件
 */
const RequestDetailModal = ({ isOpen, onClose, request }) => {
  const [activeTab, setActiveTab] = useState('overview');
  const [copiedError, setCopiedError] = useState(false);
  const [compactColumns, setCompactColumns] = useState(() => getCompactColumns());
  const [chainViewMode, setChainViewMode] = useState(() => {
    if (typeof window === 'undefined') return 'timeline';
    try {
      const savedMode = window.localStorage.getItem(CHAIN_VIEW_STORAGE_KEY);
      return savedMode === 'timeline' || savedMode === 'compact' ? savedMode : 'timeline';
    } catch {
      return 'timeline';
    }
  });

  // ESC 键关闭 & 阻止滚动
  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape') onClose();
    };

    if (isOpen) {
      document.body.style.overflow = 'hidden';
      window.addEventListener('keydown', handleKeyDown);
    } else {
      document.body.style.overflow = 'unset';
    }

    return () => {
      document.body.style.overflow = 'unset';
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [isOpen, onClose]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    try {
      window.localStorage.setItem(CHAIN_VIEW_STORAGE_KEY, chainViewMode);
    } catch {
      // 忽略持久化异常（如隐私模式）
    }
  }, [chainViewMode]);

  useEffect(() => {
    if (typeof window === 'undefined') return;

    const handleResize = () => {
      setCompactColumns(getCompactColumns());
    };

    handleResize();
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  if (!isOpen || !request) return null;

  // 计算 Token 总数
  const totalTokens = (request.inputTokens || 0) + (request.outputTokens || 0);
  const inputPercent = totalTokens > 0 ? Math.round((request.inputTokens / totalTokens) * 100) : 0;
  const outputPercent = totalTokens > 0 ? Math.round((request.outputTokens / totalTokens) * 100) : 0;

  // 请求类型
  const StreamIcon = request.isStreaming ? Waves : RefreshCw;
  const streamLabel = request.isStreaming ? '流式请求' : '常规请求';
  const streamColor = request.isStreaming ? 'text-blue-600 bg-blue-50' : 'text-slate-600 bg-slate-50';

  const rawEndpointChain = request.endpointChain || request.endpoint_chain || [];
  const endpointChain = Array.isArray(rawEndpointChain)
    ? rawEndpointChain
    : (typeof rawEndpointChain === 'string' ? rawEndpointChain.split(' -> ').filter(Boolean) : []);
  const chainNodes = endpointChain.map((item, index) => {
    const hop = (typeof item === 'object' && item?.hop) || (index + 1);
    // Handle structured object format: {hop, channel, endpoint}
    if (typeof item === 'object' && item !== null) {
      const channel = item.channel || '';
      const endpoint = item.endpoint || '';
      return { raw: channel ? `${channel}::${endpoint}` : endpoint, channel, endpoint, hop };
    }
    // Handle legacy string format: "channel::endpoint" or plain endpoint name
    const raw = String(item || '').trim();
    if (!raw) return null;
    if (raw.includes('::')) {
      const parts = raw.split('::');
      const channel = parts.shift() || '';
      const endpoint = parts.join('::') || '';
      return { raw, channel, endpoint, hop };
    }
    const fallbackChannel = endpointChain.length === 1 ? (request.channel || request.group || '') : '';
    return { raw, channel: fallbackChannel, endpoint: raw, hop };
  }).filter(Boolean);
  const compactRows = [];
  for (let i = 0; i < chainNodes.length; i += compactColumns) {
    compactRows.push(chainNodes.slice(i, i + compactColumns));
  }
  const compactCardWidthClass = compactColumns === 1
    ? 'w-full'
    : compactColumns === 2
      ? 'w-[calc((100%-0.625rem)/2)]'
      : 'w-[calc((100%-1.25rem)/3)]';
  const chainNodeIndexMap = new Map(chainNodes.map((node, idx) => [node, idx]));

  const hasUnknownChannel = chainNodes.some((node) => !node.channel);
  const channelTones = [
    { badge: 'bg-blue-50 text-blue-600 border-blue-100', border: 'border-blue-200/70' },
    { badge: 'bg-emerald-50 text-emerald-600 border-emerald-100', border: 'border-emerald-200/70' },
    { badge: 'bg-amber-50 text-amber-700 border-amber-200', border: 'border-amber-200/70' },
    { badge: 'bg-rose-50 text-rose-600 border-rose-100', border: 'border-rose-200/70' },
    { badge: 'bg-indigo-50 text-indigo-600 border-indigo-100', border: 'border-indigo-200/70' },
    { badge: 'bg-teal-50 text-teal-600 border-teal-100', border: 'border-teal-200/70' },
    { badge: 'bg-violet-50 text-violet-600 border-violet-100', border: 'border-violet-200/70' },
    { badge: 'bg-cyan-50 text-cyan-600 border-cyan-100', border: 'border-cyan-200/70' }
  ];
  const defaultChannelTone = { badge: 'bg-slate-50 text-slate-500 border-slate-200', border: 'border-slate-200' };
  const getChannelTone = (channel) => {
    if (!channel) return defaultChannelTone;
    let sum = 0;
    for (let i = 0; i < channel.length; i += 1) {
      sum += channel.charCodeAt(i);
    }
    return channelTones[sum % channelTones.length];
  };

  const errorText = [
    request.failure_reason ? `失败原因: ${request.failure_reason}` : null,
    request.cancel_reason ? `取消原因: ${request.cancel_reason}` : null
  ].filter(Boolean).join('\n');

  return (
    <div className="fixed inset-0 z-[10000] flex items-start justify-center pt-[15vh] px-4">
      {/* 背景遮罩 */}
      <div
        className="fixed inset-0 bg-slate-900/20 backdrop-blur-sm animate-in fade-in duration-200"
        onClick={onClose}
      />

      {/* 模态框内容 - 固定高度，内容区域滚动 */}
      <div className="relative w-full max-w-3xl max-h-[80vh] bg-white rounded-2xl shadow-2xl ring-1 ring-black/5 animate-in zoom-in-95 fade-in duration-200 flex flex-col overflow-hidden">
        {/* 头部 - 固定不滚动 */}
        <div className="flex-shrink-0 px-6 py-4 border-b border-slate-100 bg-white rounded-t-2xl flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className="p-2 bg-indigo-50 rounded-lg">
              <FileText className="w-5 h-5 text-indigo-600" />
            </div>
            <div>
              <h2 className="text-lg font-bold text-slate-900">请求详情</h2>
              <div className="flex items-center gap-2 mt-0.5">
                <span className="text-xs text-slate-400 font-mono">{request.requestId}</span>
                <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-md text-xs font-medium ${streamColor}`}>
                  <StreamIcon className="w-3 h-3" />
                  {streamLabel}
                </span>
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <kbd className="hidden sm:inline-block px-2 py-1 bg-slate-100 border border-slate-200 rounded text-xs text-slate-500">ESC</kbd>
            <button onClick={onClose} className="p-2 hover:bg-slate-100 rounded-lg transition-colors text-slate-400 hover:text-slate-600">
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        {/* Tab 导航 - 固定不滚动 */}
        <div className="flex-shrink-0 px-6 pt-4 border-b border-slate-100 bg-slate-50/50">
          <div className="flex gap-1 p-1 bg-slate-100/60 border border-slate-200/70 rounded-xl">
            {[
              { id: 'overview', label: '概览', icon: Activity },
              { id: 'tokens', label: 'Token 详情', icon: Database },
              { id: 'network', label: '网络信息', icon: Server },
              { id: 'chain', label: '端点链路', icon: Layers }
            ].map((tab) => (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id)}
                aria-current={activeTab === tab.id ? 'page' : undefined}
                className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors border ${
                  activeTab === tab.id
                    ? 'bg-white text-indigo-700 border-slate-200 shadow-sm'
                    : 'bg-transparent text-slate-600 border-transparent hover:bg-white/60 hover:text-slate-900'
                } focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-200 focus-visible:ring-offset-2 focus-visible:ring-offset-slate-50`}
              >
                <tab.icon className="w-4 h-4" />
                {tab.label}
              </button>
            ))}
          </div>
        </div>

        {/* 内容区域 - 自动填充剩余空间并滚动 */}
        <div className="flex-1 p-6 overflow-y-auto scrollbar-thin scrollbar-thumb-slate-200 min-h-0">
          {/* 概览 Tab */}
          {activeTab === 'overview' && (
            <div className="space-y-6">
              {/* 状态 & 成本卡片 */}
              <div className="grid grid-cols-2 gap-4">
                <div className="bg-gradient-to-br from-indigo-50 to-blue-50 rounded-xl p-4 border border-indigo-100">
                  <div className="flex items-center justify-between mb-3">
                    <span className="text-sm font-medium text-slate-600">请求状态</span>
                    <Activity className="w-4 h-4 text-indigo-500" />
                  </div>
                  <RequestStatusBadge status={request.status} />
                </div>

                <div className="bg-gradient-to-br from-orange-50 to-amber-50 rounded-xl p-4 border border-orange-100">
                  <div className="flex items-center justify-between mb-3">
                    <span className="text-sm font-medium text-slate-600">总成本</span>
                    <DollarSign className="w-4 h-4 text-orange-500" />
                  </div>
                  <div className="text-2xl font-bold text-orange-600 font-mono">
                    {formatCost(request.cost)}
                  </div>
                </div>
              </div>

              {/* 基本信息 */}
              <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
                <div className="px-4 py-3 bg-slate-50 border-b border-slate-100">
                  <h3 className="text-sm font-semibold text-slate-900">基本信息</h3>
                </div>
                <div className="p-4">
                  <InfoRow icon={FileText} label="请求 ID" value={request.requestId} copyable />
                  {request.rawRequestId && request.rawRequestId !== request.requestId && (
                    <InfoRow icon={FileText} label="记录 ID" value={request.rawRequestId} copyable />
                  )}
                  <InfoRow icon={Calendar} label="时间戳" value={formatTimestamp(request.timestamp)} />
                  <InfoRow icon={Clock} label="持续时间" value={<DurationBadge ms={request.duration} status={request.status} />} />
                  <InfoRow icon={Server} label="端点" value={request.endpoint} />
                  <InfoRow icon={Layers} label="渠道" value={request.channel || request.group} />
                </div>
              </div>

              {/* 模型信息 */}
              <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
                <div className="px-4 py-3 bg-slate-50 border-b border-slate-100">
                  <h3 className="text-sm font-semibold text-slate-900">模型信息</h3>
                </div>
                <div className="p-4">
                  <div className="flex items-center justify-between py-3">
                    <span className="text-sm font-medium text-slate-500">模型名称</span>
                    <ModelTag model={request.model} />
                  </div>
                </div>
              </div>

              {/* 错误信息（如果有） */}
              {(['failed', 'error', 'cancelled', 'timeout', 'partial_failed'].includes(request.status) ||
                request.failure_reason || request.cancel_reason) && (
                <div className="bg-rose-50 rounded-xl border border-rose-200 overflow-hidden">
                  <div className="px-4 py-3 bg-rose-100/50 border-b border-rose-200 flex items-center justify-between">
                    <h3 className="text-sm font-semibold text-rose-900 flex items-center gap-2">
                      <AlertCircle className="w-4 h-4" />
                      错误信息
                    </h3>
                    {errorText && (
                      <button
                        onClick={() => {
                          navigator.clipboard.writeText(errorText);
                          setCopiedError(true);
                          setTimeout(() => setCopiedError(false), 2000);
                        }}
                        className="p-1 text-rose-400 hover:text-rose-600 hover:bg-rose-100/80 hover:scale-110 rounded transition-all"
                        title="复制错误信息"
                      >
                        {copiedError ? (
                          <Check className="w-3.5 h-3.5 text-emerald-600" />
                        ) : (
                          <Copy className="w-3.5 h-3.5" />
                        )}
                      </button>
                    )}
                  </div>
                  <div className="p-4 space-y-2">
                    {request.failure_reason && (
                      <div className="text-sm">
                        <span className="font-medium text-slate-600">失败原因：</span>
                        <span className="text-rose-700">{request.failure_reason}</span>
                      </div>
                    )}
                    {request.cancel_reason && (
                      <div className="text-sm">
                        <span className="font-medium text-slate-600">取消原因：</span>
                        <span className="text-rose-700">{request.cancel_reason}</span>
                      </div>
                    )}
                  </div>
                </div>
              )}
            </div>
          )}

          {/* 端点链路 Tab */}
          {activeTab === 'chain' && (
            <div className="space-y-4">
              {/* 统计摘要条 */}
              {chainNodes.length > 0 && (() => {
                const uniqueChannels = new Set(chainNodes.map(n => n.channel).filter(Boolean));
                const uniqueEndpoints = new Set(chainNodes.map(n => n.endpoint).filter(Boolean));
                const channelSwitches = chainNodes.filter((n, i) => i > 0 && n.channel && chainNodes[i - 1].channel && n.channel !== chainNodes[i - 1].channel).length;
                return (
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 bg-indigo-50 text-indigo-700 text-xs font-medium rounded-lg border border-indigo-100">
                        <RefreshCw className="w-3 h-3" />
                        {chainNodes.length} 跳
                      </span>
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 bg-slate-50 text-slate-600 text-xs font-medium rounded-lg border border-slate-200">
                        <Layers className="w-3 h-3" />
                        {uniqueChannels.size} 渠道
                      </span>
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 bg-slate-50 text-slate-600 text-xs font-medium rounded-lg border border-slate-200">
                        <Server className="w-3 h-3" />
                        {uniqueEndpoints.size} 端点
                      </span>
                      {channelSwitches > 0 && (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 bg-amber-50 text-amber-700 text-xs font-medium rounded-lg border border-amber-200">
                          <Zap className="w-3 h-3" />
                          {channelSwitches} 次渠道切换
                        </span>
                      )}
                    </div>

                    <div className="inline-flex p-0.5 bg-slate-100 border border-slate-200 rounded-lg">
                      <button
                        type="button"
                        onClick={() => setChainViewMode('timeline')}
                        className={`px-2.5 py-1 text-xs font-medium rounded-md transition-colors ${
                          chainViewMode === 'timeline'
                            ? 'bg-white text-slate-800 shadow-sm'
                            : 'text-slate-500 hover:text-slate-700'
                        }`}
                        aria-pressed={chainViewMode === 'timeline'}
                      >
                        时间线
                      </button>
                      <button
                        type="button"
                        onClick={() => setChainViewMode('compact')}
                        className={`px-2.5 py-1 text-xs font-medium rounded-md transition-colors ${
                          chainViewMode === 'compact'
                            ? 'bg-white text-slate-800 shadow-sm'
                            : 'text-slate-500 hover:text-slate-700'
                        }`}
                        aria-pressed={chainViewMode === 'compact'}
                      >
                        紧凑
                      </button>
                    </div>
                  </div>
                );
              })()}

              {/* 端点链路内容 */}
              <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
                <div className="p-4">
                  {chainNodes.length === 0 ? (
                    <div className="text-sm text-slate-400 text-center py-4">暂无端点链路记录</div>
                  ) : chainViewMode === 'compact' ? (
                    <div className="space-y-1.5">
                      {compactRows.map((row, rowIdx) => {
                        const isReverseRow = rowIdx % 2 === 1;
                        const displayRow = isReverseRow ? [...row].reverse() : row;
                        const isLastRow = rowIdx === compactRows.length - 1;

                        return (
                          <div key={`row-${rowIdx}`}>
                            <div className="flex items-stretch gap-2.5">
                              {displayRow.map((node, displayIdx) => {
                                const originalIndex = chainNodeIndexMap.get(node) ?? 0;
                                const channelLabel = node.channel || '未知渠道';
                                const endpointLabel = node.endpoint || node.raw || '-';
                                const tone = getChannelTone(node.channel);
                                const isFirst = originalIndex === 0;
                                const isLast = originalIndex === chainNodes.length - 1;
                                const isSingle = chainNodes.length === 1;
                                const prevNode = chainNodes[originalIndex - 1];
                                const isChannelSwitch = originalIndex > 0 && node.channel && prevNode?.channel && node.channel !== prevNode.channel;
                                const hasNextInRow = displayIdx < displayRow.length - 1;
                                const rowArrow = isReverseRow ? '←' : '→';

                                return (
                                  <div key={`${node.raw}-${originalIndex}`} className={`${compactCardWidthClass} min-w-0 flex items-center gap-2`}>
                                    <div
                                      className={`border ${tone.border} rounded-lg px-3 py-2 bg-white min-w-0 flex-1 ${isLast && !isSingle ? 'ring-1 ring-emerald-200/60' : ''}`}
                                    >
                                      <div className="flex items-center justify-between gap-2 mb-1.5">
                                        <span className={`text-[10px] font-bold ${
                                          isSingle ? 'text-indigo-500' : isFirst ? 'text-blue-500' : isLast ? 'text-emerald-500' : 'text-slate-400'
                                        }`}>
                                          #{node.hop}
                                        </span>

                                        <div className="flex items-center gap-1">
                                          {isChannelSwitch && (
                                            <span className="inline-flex items-center gap-0.5 text-[10px] text-amber-600 font-medium">
                                              <Zap className="w-2.5 h-2.5" />
                                              切换
                                            </span>
                                          )}
                                          {isSingle && <span className="text-[10px] text-slate-400">直达</span>}
                                          {isFirst && !isSingle && <span className="text-[10px] text-blue-400">起始</span>}
                                          {isLast && !isSingle && <span className="text-[10px] text-emerald-500">最终</span>}
                                        </div>
                                      </div>

                                      <div className="flex items-center gap-1.5 min-w-0" title={`${channelLabel} / ${endpointLabel}`}>
                                        <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border shrink-0 whitespace-nowrap ${tone.badge}`}>
                                          {channelLabel}
                                        </span>
                                        <span className="text-xs font-semibold text-slate-800 truncate" title={endpointLabel}>
                                          {endpointLabel}
                                        </span>
                                      </div>
                                    </div>

                                    {hasNextInRow && (
                                      <span className="text-slate-300 text-sm shrink-0">{rowArrow}</span>
                                    )}
                                  </div>
                                );
                              })}
                            </div>

                            {!isLastRow && (
                              <div className={`flex ${isReverseRow ? 'justify-start' : 'justify-end'} px-1 mt-1 mb-0.5`}>
                                <span className="text-slate-300 text-sm leading-none">↓</span>
                              </div>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  ) : (
                    <div className="relative pl-8">
                      {/* 贯穿竖线（绝对定位，首尾圆点之间） */}
                      {chainNodes.length > 1 && (
                        <div className="absolute left-[11px] top-0 bottom-0 w-px bg-slate-200" />
                      )}
                      {chainNodes.map((node, idx) => {
                        const channelLabel = node.channel || '未知渠道';
                        const endpointLabel = node.endpoint || node.raw || '-';
                        const tone = getChannelTone(node.channel);
                        const isFirst = idx === 0;
                        const isLast = idx === chainNodes.length - 1;
                        const isSingle = chainNodes.length === 1;
                        const isChannelSwitch = idx > 0 && node.channel && chainNodes[idx - 1].channel && node.channel !== chainNodes[idx - 1].channel;

                        return (
                          <div key={`${node.raw}-${idx}`} className={`relative ${isLast ? '' : 'pb-3'}`}>
                            {/* 渠道切换提示（在卡片上方） */}
                            {isChannelSwitch && (
                              <div className="flex items-center gap-1 mb-1.5 ml-1">
                                <Zap className="w-3 h-3 text-amber-500" />
                                <span className="text-[10px] text-amber-600 font-medium">渠道切换</span>
                              </div>
                            )}
                            {/* 卡片行：相对定位容器，圆点绝对定位到卡片垂直中心 */}
                            <div className="relative">
                              {/* 圆点 - 绝对定位到卡片左侧、垂直居中 */}
                              <div className={`absolute -left-8 top-1/2 -translate-y-1/2 w-[10px] h-[10px] rounded-full z-10 ring-[3px] ring-white ${
                                isSingle ? 'bg-indigo-500' : isFirst ? 'bg-blue-500' : isLast ? 'bg-emerald-500' : 'bg-slate-300'
                              }`} style={{ left: '-25px' }} />
                              {/* 卡片 */}
                              <div className={`border ${tone.border} rounded-lg px-3 py-2 ${isLast && !isSingle ? 'bg-emerald-50/40' : isFirst && !isSingle ? 'bg-blue-50/40' : 'bg-slate-50/60'}`}>
                                <div className="flex items-center gap-2 min-w-0">
                                  <span className={`text-[10px] font-bold shrink-0 ${
                                    isSingle ? 'text-indigo-500' : isFirst ? 'text-blue-500' : isLast ? 'text-emerald-500' : 'text-slate-400'
                                  }`}>
                                    #{node.hop}
                                  </span>
                                  <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border whitespace-nowrap ${tone.badge}`}>
                                    {channelLabel}
                                  </span>
                                  <span className="text-sm font-semibold text-slate-800 truncate" title={endpointLabel}>
                                    {endpointLabel}
                                  </span>
                                  {isSingle && (
                                    <span className="text-[10px] text-slate-400 ml-auto shrink-0">直达</span>
                                  )}
                                  {isFirst && !isSingle && (
                                    <span className="text-[10px] text-blue-400 ml-auto shrink-0">起始</span>
                                  )}
                                  {isLast && !isSingle && (
                                    <span className="text-[10px] text-emerald-500 ml-auto shrink-0">最终</span>
                                  )}
                                </div>
                              </div>
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              </div>

              {hasUnknownChannel && (
                <div className="text-xs text-amber-600 bg-amber-50 border border-amber-200 rounded-lg px-3 py-2">
                  部分记录缺少渠道信息（历史数据），新请求已使用结构化格式。
                </div>
              )}
            </div>
          )}

          {/* Token 详情 Tab */}
          {activeTab === 'tokens' && (
            <div className="space-y-6">
              {/* Token 卡片 - 基础 */}
              <div className="grid grid-cols-2 gap-4">
                <TokenCard
                  icon={TrendingUp}
                  label="输入 Token"
                  value={request.inputTokens || 0}
                  colorClass={{ bg: 'bg-blue-50', border: 'border-blue-200', text: 'text-blue-600', icon: 'text-blue-600' }}
                />
                <TokenCard
                  icon={TrendingDown}
                  label="输出 Token"
                  value={request.outputTokens || 0}
                  colorClass={{ bg: 'bg-emerald-50', border: 'border-emerald-200', text: 'text-emerald-600', icon: 'text-emerald-600' }}
                />
              </div>

              {/* Token 卡片 - 缓存 */}
              <div className="grid grid-cols-2 gap-4">
                <TokenCard
                  icon={Database}
                  label="缓存创建 (5分钟)"
                  value={request.cacheCreation5mTokens || 0}
                  colorClass={{ bg: 'bg-purple-50', border: 'border-purple-200', text: 'text-purple-600', icon: 'text-purple-600' }}
                />
                <TokenCard
                  icon={Database}
                  label="缓存创建 (1小时)"
                  value={request.cacheCreation1hTokens || 0}
                  colorClass={{ bg: 'bg-violet-50', border: 'border-violet-200', text: 'text-violet-600', icon: 'text-violet-600' }}
                />
                <TokenCard
                  icon={Zap}
                  label="缓存读取"
                  value={request.cacheReadTokens || 0}
                  colorClass={{ bg: 'bg-amber-50', border: 'border-amber-200', text: 'text-amber-600', icon: 'text-amber-600' }}
                />
                <TokenCard
                  icon={Database}
                  label="缓存创建 (总计)"
                  value={request.cacheCreationTokens || 0}
                  colorClass={{ bg: 'bg-slate-50', border: 'border-slate-200', text: 'text-slate-600', icon: 'text-slate-500' }}
                />
              </div>

              {/* Token 总计 */}
              <div className="bg-gradient-to-r from-indigo-50 to-blue-50 rounded-xl p-6 border border-indigo-100">
                <div className="flex items-center justify-between mb-4">
                  <div className="flex items-center gap-3">
                    <div className="p-2 bg-white rounded-lg shadow-sm">
                      <FileText className="w-5 h-5 text-indigo-600" />
                    </div>
                    <div>
                      <div className="text-sm font-medium text-slate-600">总 Token 消耗</div>
                      <div className="text-xs text-slate-400">Input + Output</div>
                    </div>
                  </div>
                  <div className="text-3xl font-bold text-indigo-600 font-mono">
                    {totalTokens.toLocaleString()}
                  </div>
                </div>

                {/* 分布条 */}
                <div className="space-y-2">
                  <div className="flex items-center justify-between text-xs text-slate-600">
                    <span>输入 {inputPercent}%</span>
                    <span>输出 {outputPercent}%</span>
                  </div>
                  <div className="h-2 bg-slate-200 rounded-full overflow-hidden flex">
                    <div className="bg-blue-500" style={{ width: `${inputPercent}%` }} />
                    <div className="bg-emerald-500" style={{ width: `${outputPercent}%` }} />
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* 网络信息 Tab */}
          {activeTab === 'network' && (
            <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
              <div className="px-4 py-3 bg-slate-50 border-b border-slate-100">
                <h3 className="text-sm font-semibold text-slate-900">网络详情</h3>
              </div>
              <div className="p-4">
                <InfoRow icon={Server} label="HTTP 状态码" value={request.http_status_code || request.httpStatusCode || '-'} />
                <InfoRow icon={RefreshCw} label="重试次数" value={`${request.retry_count || request.retryCount || 0} 次`} />
                <InfoRow icon={Activity} label="请求方法" value={request.method || 'POST'} />
                <InfoRow icon={FileText} label="请求路径" value={request.path || '/v1/messages'} copyable />
              </div>
            </div>
          )}
        </div>

        {/* 底部操作栏 - 固定不滚动 */}
        <div className="flex-shrink-0 px-6 py-4 border-t border-slate-100 bg-slate-50/80 backdrop-blur-sm rounded-b-2xl flex justify-between items-center">
          <div className="text-xs text-slate-500">
            Request ID: <span className="font-mono text-slate-700">{request.requestId}</span>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => navigator.clipboard.writeText(request.requestId)}
              className="h-8 px-3 text-xs font-medium text-slate-600 bg-white border border-slate-200 rounded-lg hover:bg-slate-50 transition-colors inline-flex items-center justify-center gap-1.5"
            >
              <Copy className="w-3.5 h-3.5" /> 复制 ID
            </button>
            <button
              onClick={onClose}
              className="h-8 px-3 text-xs font-medium text-white bg-indigo-600 rounded-lg hover:bg-indigo-700 transition-colors inline-flex items-center justify-center"
            >
              关闭
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default RequestDetailModal;
