// ============================================
// RequestDetailModal - 请求详情模态框
// Command Palette 风格
// 2025-12-01
// ============================================

import { useState, useEffect, useRef } from 'react';
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
  ShieldCheck,
  TrendingUp,
  TrendingDown,
  Zap,
  Calculator,
  Replace,
  KeyRound
} from 'lucide-react';
import RequestStatusBadge from './RequestStatusBadge.jsx';
import ModelTag from './ModelTag.jsx';
import DurationBadge from './DurationBadge.jsx';
import { formatCost, formatTimestamp } from '@utils/api.js';
import { formatMultiplierDisplayValue } from '@utils/multiplier.js';
import { Tooltip } from '@components/ui';
import {
  getBillableInputTokens,
  getDisplayInputTokens,
  getDisplayTotalTokens,
  isCacheInclusiveProvider
} from '../utils/tokenDisplay.js';

const privacyRuleTypeLabels = {
  exact: '精准',
  regex: '正则',
  field: '字段',
  keyword: '关键词',
  validator: '校验',
  entropy: '高熵',
  structured: '结构'
};

const parsePrivacySummary = (value) => {
  if (!value) return { changed: false, hits: [] };
  if (typeof value === 'object') {
    return {
      changed: Boolean(value.changed),
      hits: Array.isArray(value.hits) ? value.hits : []
    };
  }
  if (typeof value !== 'string') return { changed: false, hits: [] };
  try {
    const parsed = JSON.parse(value);
    return {
      changed: Boolean(parsed?.changed),
      hits: Array.isArray(parsed?.hits) ? parsed.hits : []
    };
  } catch {
    return { changed: false, hits: [] };
  }
};

const decodeBase64Utf8 = (value) => {
  if (!value || typeof value !== 'string') return '';
  try {
    const binary = window.atob(value);
    const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
    return new TextDecoder().decode(bytes);
  } catch {
    return '';
  }
};

const parsePrivacyMatches = (value) => {
  if (!value) return [];
  if (Array.isArray(value)) return value;
  if (typeof value !== 'string') return [];
  try {
    const parsed = JSON.parse(value);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
};

const getPrivacyMatchRaw = (match) => {
  if (!match || typeof match !== 'object') return '';
  return match.raw || match.raw_value || decodeBase64Utf8(match.raw_base64 || match.rawBase64);
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
          <Tooltip content={copied ? '已复制' : '复制'}>
            <button
              onClick={handleCopy}
              className="p-1 text-slate-400 hover:text-slate-600 hover:bg-slate-100 hover:scale-110 rounded transition-all"
            >
              {copied ? (
                <Check className="w-3.5 h-3.5 text-emerald-600" />
              ) : (
                <Copy className="w-3.5 h-3.5" />
              )}
            </button>
          </Tooltip>
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

const FirstByteDetailBadge = ({ ms, status }) => {
  if (ms === null || ms === undefined) {
    return (
      <span className="inline-flex items-center rounded-md border border-slate-200 bg-slate-50 px-2 py-0.5 font-mono text-sm text-slate-400 whitespace-nowrap">
        -
      </span>
    );
  }

  return <DurationBadge ms={ms} status={status} />;
};

/**
 * 请求详情模态框主组件
 */
const RequestDetailModal = ({ isOpen, onClose, request, interChannelFailoverEnabled = false }) => {
  const [activeTab, setActiveTab] = useState('overview');
  const [copiedError, setCopiedError] = useState(false);
  const [copiedRequestId, setCopiedRequestId] = useState(false);
  const requestIdCopyTimerRef = useRef(null);

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

  useEffect(() => () => {
    if (requestIdCopyTimerRef.current) {
      clearTimeout(requestIdCopyTimerRef.current);
    }
  }, []);

  if (!isOpen || !request) return null;

  const handleCopyRequestId = async () => {
    await navigator.clipboard.writeText(request.requestId);
    setCopiedRequestId(true);
    if (requestIdCopyTimerRef.current) {
      clearTimeout(requestIdCopyTimerRef.current);
    }
    requestIdCopyTimerRef.current = setTimeout(() => {
      setCopiedRequestId(false);
      requestIdCopyTimerRef.current = null;
    }, 2000);
  };

  const isCodexRequest = isCacheInclusiveProvider(request.provider);
  const isClaudeRequest = !isCodexRequest;
  const showChannelInChain = isClaudeRequest && Boolean(interChannelFailoverEnabled);

  // 计算 Token 总数：Codex/OpenAI input_tokens 已包含 cached_tokens；兼容旧 Codex 记录的非缓存 input。
  const inputTokenCount = getDisplayInputTokens(request);
  const outputTokenCount = request.outputTokens || 0;
  const reasoningTokenCount = request.reasoningTokens || 0;
  const visibleOutputTokenCount = Math.max(0, outputTokenCount - reasoningTokenCount);
  const billingOutputTokenSource = request.billingOutputTokenSource || 'output_tokens';
  const billableOutputTokenCount = request.billableOutputTokens ?? outputTokenCount;
  const totalTokens = getDisplayTotalTokens(request);
  const inputPercent = totalTokens > 0 ? Math.round((inputTokenCount / totalTokens) * 100) : 0;
  const outputPercent = totalTokens > 0 ? Math.round((outputTokenCount / totalTokens) * 100) : 0;
  const cachePercent = !isCodexRequest && totalTokens > 0 ? Math.max(0, 100 - inputPercent - outputPercent) : 0;
  const outputSourceLabelMap = {
    output_tokens: '总输出',
    reasoning_tokens: '推理',
    visible_output_tokens: '可见补全'
  };
  const billingOutputSourceLabel = outputSourceLabelMap[billingOutputTokenSource] || outputSourceLabelMap.output_tokens;

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
    // Handle structured object format: {hop, channel, credential_group, endpoint}
    if (typeof item === 'object' && item !== null) {
      const channel = item.channel || '';
      const credentialGroup = item.credential_group || item.credentialGroup || item.group_name || item.group || '';
      const endpoint = item.endpoint || '';
      const raw = channel
        ? `${channel}::${credentialGroup ? `${credentialGroup}::` : ''}${endpoint}`
        : endpoint;
      return { raw, channel, credentialGroup, endpoint, hop };
    }
    // Handle legacy string format: "channel::endpoint", "channel::group::endpoint", or plain endpoint name
    const raw = String(item || '').trim();
    if (!raw) return null;
    if (raw.includes('::')) {
      const parts = raw.split('::');
      const channel = parts.shift() || '';
      const credentialGroup = parts.length >= 2 ? (parts.shift() || '') : '';
      const endpoint = parts.join('::') || '';
      return { raw, channel, credentialGroup, endpoint, hop };
    }
    const fallbackChannel = endpointChain.length === 1 ? (request.channel || request.group || '') : '';
    return { raw, channel: fallbackChannel, credentialGroup: '', endpoint: raw, hop };
  }).filter(Boolean);
  const currentChannelLabel = request.channel || request.group || chainNodes.find((node) => node.channel)?.channel || '';
  const hasUnknownChannel = showChannelInChain && chainNodes.some((node) => !node.channel);
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
  const getChainRoleLabel = (index, total) => {
    if (total <= 1) return '直达';
    if (index === 0) return '起始';
    if (index === total - 1) return '最终';
    return '中转';
  };

  const errorText = [
    request.failure_reason ? `失败原因: ${request.failure_reason}` : null,
    request.cancel_reason ? `取消原因: ${request.cancel_reason}` : null
  ].filter(Boolean).join('\n');
  const modelRewriteSource = request.modelRewriteSource || request.model_rewrite_source || '';
  const originalModelName = request.originalModelName || request.original_model_name || '';
  const modelRewriteTarget = request.modelRewriteTarget || request.model_rewrite_target || request.model || '';
  const modelRewriteLabelMap = {
    mapping: '模型映射',
    channel_model: '渠道覆盖'
  };
  const modelRewriteIconClassMap = {
    mapping: 'text-amber-500',
    channel_model: 'text-violet-500'
  };
  const modelRewriteLabel = modelRewriteLabelMap[modelRewriteSource] || (modelRewriteSource ? '模型改写' : '');
  const hasModelRewrite = Boolean(modelRewriteLabel && modelRewriteTarget);
  const privacySummary = parsePrivacySummary(request.privacyRedactionSummary || request.privacy_redaction_summary);
  const privacyRedacted = Boolean(request.privacyRedacted || request.privacy_redacted || privacySummary.changed);
  const privacyHits = Array.isArray(privacySummary.hits) ? privacySummary.hits : [];
  const privacyMatches = parsePrivacyMatches(request.privacyRedactionMatches || request.privacy_redaction_matches);

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
              { id: 'privacy', label: '隐私防护', icon: ShieldCheck },
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
                  <InfoRow
                    icon={Clock}
                    label="用时/首字"
                    value={(
                      <span className="inline-flex items-center justify-end gap-1.5">
                        <DurationBadge ms={request.duration} status={request.status} />
                        <FirstByteDetailBadge ms={request.firstByteMs} status={request.status} />
                      </span>
                    )}
                  />
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
                  {hasModelRewrite && (
                    <div className="flex items-center justify-between gap-4 py-3 border-t border-slate-100">
                      <span className="text-sm font-medium text-slate-500">{modelRewriteLabel}</span>
                      <div className="flex items-center justify-end gap-2 min-w-0">
                        <Replace className={`h-3.5 w-3.5 shrink-0 -scale-x-100 stroke-[2.4] ${modelRewriteIconClassMap[modelRewriteSource] || 'text-slate-400'}`} />
                        <span className="text-sm font-mono text-slate-900 text-right break-all">
                          {originalModelName || '-'} -&gt; {modelRewriteTarget}
                        </span>
                      </div>
                    </div>
                  )}
                </div>
              </div>

              {/* 计费详情 */}
              {((request.cost || 0) > 0 || (request.inputCost || 0) > 0 || (request.outputCost || 0) > 0) && (() => {
                const calcUnitPrice = (cost, tokens) => {
                  if (!tokens || tokens <= 0) return 0
                  return (cost / tokens) * 1_000_000
                }
                const safeMultiplier = (value) => {
                  const parsed = Number(value)
                  return Number.isFinite(parsed) && parsed > 0 ? parsed : 1.0
                }

                const outputTokens = request.outputTokens || 0
                const reasoningTokens = request.reasoningTokens || 0
                const billableOutputTokens = request.billableOutputTokens ?? outputTokens
                const billingSource = request.billingOutputTokenSource || 'output_tokens'
                const cacheReadTokens = request.cacheReadTokens || 0
                const billableInputTokens = getBillableInputTokens(request)
                const displayInputTokens = getDisplayInputTokens(request)
                const inputDeductionTokens = Math.max(0, displayInputTokens - billableInputTokens)
                const cache5mTokens = request.cacheCreation5mTokens || 0
                const cache1hTokens = request.cacheCreation1hTokens || 0
                const cacheCreationTokens = request.cacheCreationTokens || 0

                const inputCost = request.inputCost || 0
                const outputCost = request.outputCost || 0
                const cacheReadCost = request.cacheReadCost || 0
                const cache5mCost = request.cacheCreation5mCost || 0
                const cache1hCost = request.cacheCreation1hCost || 0
                const cacheCreationCost = request.cacheCreationCost || 0

                // 端点倍率（默认 1.0）
                const totalMultiplier = safeMultiplier(request.costMultiplier)
                const inputMultiplier = safeMultiplier(request.inputCostMultiplier)
                const outputMultiplier = safeMultiplier(request.outputCostMultiplier)
                const cacheReadMultiplier = safeMultiplier(request.cacheReadCostMultiplier)
                const cache5mMultiplier = safeMultiplier(request.cacheCreationCostMultiplier)
                const cache1hMultiplier = safeMultiplier(request.cacheCreationCostMultiplier1h)
                const modelTotalMultiplier = safeMultiplier(request.modelCostMultiplier)
                const modelInputMultiplier = safeMultiplier(request.modelInputCostMultiplier)
                const modelOutputMultiplier = safeMultiplier(request.modelOutputCostMultiplier)
                const modelCacheReadMultiplier = safeMultiplier(request.modelCacheReadCostMultiplier)

                const calcBasePrice = (effectivePrice, categoryMultiplier, modelCategoryMultiplier = 1.0) => {
                  const combinedMultiplier = totalMultiplier * modelTotalMultiplier * categoryMultiplier * modelCategoryMultiplier
                  return combinedMultiplier > 0 ? effectivePrice / combinedMultiplier : effectivePrice
                }

                const formatPrice = (n) => `$${(n || 0).toFixed(6)}`
                const formatMultiplier = (n) => `× ${formatMultiplierDisplayValue(n, 3)}`
                const isMultiplierAdjusted = (value) => Math.abs(safeMultiplier(value) - 1.0) > 0.000001
                const combinedTotalMultiplier = totalMultiplier * modelTotalMultiplier
                const removeTotalMultiplier = (cost) => combinedTotalMultiplier > 0 ? cost / combinedTotalMultiplier : cost
                const inputEffectivePrice = calcUnitPrice(inputCost, billableInputTokens)
                const outputEffectivePrice = calcUnitPrice(outputCost, billableOutputTokens)
                const cacheReadEffectivePrice = calcUnitPrice(cacheReadCost, cacheReadTokens)
                const cache5mEffectivePrice = calcUnitPrice(cache5mCost, cache5mTokens)
                const cache1hEffectivePrice = calcUnitPrice(cache1hCost, cache1hTokens)
                const inputBasePrice = calcBasePrice(inputEffectivePrice, inputMultiplier, modelInputMultiplier)
                const outputBasePrice = calcBasePrice(outputEffectivePrice, outputMultiplier, modelOutputMultiplier)
                const cacheReadBasePrice = calcBasePrice(cacheReadEffectivePrice, cacheReadMultiplier, modelCacheReadMultiplier)
                const cache5mBasePrice = calcBasePrice(cache5mEffectivePrice, cache5mMultiplier)
                const cache1hBasePrice = calcBasePrice(cache1hEffectivePrice, cache1hMultiplier)
                // 兼容旧数据：无分项 5m/1h 时，展示合并的缓存创建价格（旧数据无倍率字段，默认 1.0）
                const hasSplitCache = cache5mTokens > 0 || cache1hTokens > 0
                const aggregateCacheCreationPrice = !hasSplitCache
                  ? calcBasePrice(calcUnitPrice(cacheCreationCost, cacheCreationTokens), 1.0)
                  : 0
                const outputTokenExpression = billingSource === 'visible_output_tokens'
                  ? `${outputTokens.toLocaleString()} - ${reasoningTokens.toLocaleString()} = ${billableOutputTokens.toLocaleString()}`
                  : billableOutputTokens.toLocaleString()
                const buildMultiplierText = (...multipliers) => {
                  return multipliers
                    .filter((value) => isMultiplierAdjusted(value))
                    .map((value) => formatMultiplier(value))
                    .join(' ')
                }

                const priceBadges = [
                  inputBasePrice > 0 && { key: 'input', label: '输入', value: inputBasePrice, color: 'blue' },
                  outputBasePrice > 0 && { key: 'output', label: '输出', value: outputBasePrice, color: 'emerald' },
                  cacheReadBasePrice > 0 && { key: 'cacheRead', label: '缓存读取', value: cacheReadBasePrice, color: 'amber' },
                  cache5mBasePrice > 0 && { key: 'cache5m', label: '5m 缓存创建', value: cache5mBasePrice, color: 'purple' },
                  cache1hBasePrice > 0 && { key: 'cache1h', label: '1h 缓存创建', value: cache1hBasePrice, color: 'violet' },
                  aggregateCacheCreationPrice > 0 && { key: 'cacheCreation', label: '缓存创建', value: aggregateCacheCreationPrice, color: 'slate' }
                ].filter(Boolean)

                const multiplierBadges = [
                  { key: 'total', label: '总倍率', value: totalMultiplier, color: 'indigo' },
                  inputBasePrice > 0 && { key: 'input', label: '输入', value: inputMultiplier, color: 'slate' },
                  outputBasePrice > 0 && { key: 'output', label: '输出', value: outputMultiplier, color: 'indigo' },
                  cacheReadBasePrice > 0 && { key: 'cacheRead', label: '缓存读取', value: cacheReadMultiplier, color: 'slate' },
                  cache5mBasePrice > 0 && { key: 'cache5m', label: '5m 缓存', value: cache5mMultiplier, color: 'slate' },
                  cache1hBasePrice > 0 && { key: 'cache1h', label: '1h 缓存', value: cache1hMultiplier, color: 'slate' },
                  isMultiplierAdjusted(modelTotalMultiplier) && { key: 'modelTotal', label: '模型总倍率', value: modelTotalMultiplier, color: 'sky' },
                  inputBasePrice > 0 && isMultiplierAdjusted(modelInputMultiplier) && { key: 'modelInput', label: '模型输入', value: modelInputMultiplier, color: 'cyan' },
                  outputBasePrice > 0 && isMultiplierAdjusted(modelOutputMultiplier) && { key: 'modelOutput', label: '模型输出', value: modelOutputMultiplier, color: 'sky' },
                  cacheReadBasePrice > 0 && isMultiplierAdjusted(modelCacheReadMultiplier) && { key: 'modelCacheRead', label: '模型缓存读取', value: modelCacheReadMultiplier, color: 'cyan' }
                ].filter(Boolean)

                const badgeColorMap = {
                  blue: 'bg-blue-50 border-blue-100 text-blue-700',
                  emerald: 'bg-emerald-50 border-emerald-100 text-emerald-700',
                  amber: 'bg-amber-50 border-amber-100 text-amber-700',
                  purple: 'bg-purple-50 border-purple-100 text-purple-700',
                  violet: 'bg-violet-50 border-violet-100 text-violet-700',
                  indigo: 'bg-indigo-50 border-indigo-100 text-indigo-700',
                  sky: 'bg-sky-50 border-sky-100 text-sky-700',
                  cyan: 'bg-cyan-50 border-cyan-100 text-cyan-700',
                  slate: 'bg-slate-50 border-slate-200 text-slate-700'
                }
                const valueColorMap = {
                  blue: 'text-blue-600',
                  emerald: 'text-emerald-600',
                  amber: 'text-amber-600',
                  purple: 'text-purple-600',
                  violet: 'text-violet-600',
                  indigo: 'text-indigo-500',
                  sky: 'text-sky-600',
                  cyan: 'text-cyan-600',
                  slate: 'text-slate-600'
                }

                const formulaRows = [
                  billableInputTokens > 0 && {
                    key: 'input',
                    label: isCodexRequest ? '输入(计费)' : '输入',
                    tokens: billableInputTokens,
                    tokenExpression: isCodexRequest && inputDeductionTokens > 0
                      ? `${displayInputTokens.toLocaleString()} - ${inputDeductionTokens.toLocaleString()} = ${billableInputTokens.toLocaleString()}`
                      : billableInputTokens.toLocaleString(),
                    price: inputBasePrice,
                    multiplierText: buildMultiplierText(inputMultiplier, modelInputMultiplier),
                    cost: removeTotalMultiplier(inputCost)
                  },
                  billableOutputTokens > 0 && {
                    key: 'output',
                    label: isCodexRequest ? `输出(计费:${billingOutputSourceLabel})` : '输出',
                    tokens: billableOutputTokens,
                    tokenExpression: outputTokenExpression,
                    price: outputBasePrice,
                    multiplierText: buildMultiplierText(outputMultiplier, modelOutputMultiplier),
                    cost: removeTotalMultiplier(outputCost)
                  },
                  cacheReadTokens > 0 && {
                    key: 'cacheRead', label: '缓存读取', tokens: cacheReadTokens, price: cacheReadBasePrice, multiplierText: buildMultiplierText(cacheReadMultiplier, modelCacheReadMultiplier), cost: removeTotalMultiplier(cacheReadCost)
                  },
                  cache5mTokens > 0 && {
                    key: 'cache5m', label: '5m 缓存创建', tokens: cache5mTokens, price: cache5mBasePrice, multiplierText: buildMultiplierText(cache5mMultiplier), cost: removeTotalMultiplier(cache5mCost)
                  },
                  cache1hTokens > 0 && {
                    key: 'cache1h', label: '1h 缓存创建', tokens: cache1hTokens, price: cache1hBasePrice, multiplierText: buildMultiplierText(cache1hMultiplier), cost: removeTotalMultiplier(cache1hCost)
                  },
                  !hasSplitCache && cacheCreationTokens > 0 && {
                    key: 'cacheCreation', label: '缓存创建', tokens: cacheCreationTokens, price: aggregateCacheCreationPrice, cost: removeTotalMultiplier(cacheCreationCost)
                  }
                ].filter(Boolean)

                const totalCost = request.cost || 0
                // 是否缺少分项明细（总成本有值但无法展示公式）
                const hasNoBreakdown = formulaRows.length === 0 && priceBadges.length === 0 && totalCost > 0

                return (
                  <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
                    <div className="px-4 py-3 bg-gradient-to-r from-emerald-50 to-teal-50 border-b border-emerald-100 flex items-center justify-between">
                      <h3 className="text-sm font-semibold text-slate-900 flex items-center gap-2">
                        <Calculator className="w-4 h-4 text-emerald-600" />
                        计费详情
                      </h3>
                      <span className="text-xs text-slate-500">仅供参考，以实际扣费为准</span>
                    </div>

                    <div className="p-4 space-y-4">
                      {/* 单价摘要 - 横向徽章 */}
                      {(priceBadges.length > 0 || multiplierBadges.length > 0) && (
                        <div className="space-y-2">
                          {priceBadges.length > 0 && (
                            <div className="flex flex-wrap gap-2">
                              {priceBadges.map((badge) => (
                                <div
                                  key={badge.key}
                                  className={`inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 ${badgeColorMap[badge.color]}`}
                                >
                                  <span className="text-xs font-medium">{badge.label}</span>
                                  <span className={`text-xs font-mono ${valueColorMap[badge.color]}`}>
                                    {formatPrice(badge.value)} / 1M
                                  </span>
                                </div>
                              ))}
                            </div>
                          )}

                          {multiplierBadges.length > 0 && (
                            <div className="flex flex-wrap gap-2">
                              {multiplierBadges.map((badge) => (
                                <div
                                  key={badge.key}
                                  className={`inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 ${badgeColorMap[badge.color]}`}
                                >
                                  <span className="text-xs font-medium">{badge.label}</span>
                                  <span className={`text-xs font-mono ${valueColorMap[badge.color]}`}>
                                    {formatMultiplierDisplayValue(badge.value, 3)}
                                  </span>
                                </div>
                              ))}
                            </div>
                          )}
                        </div>
                      )}

                      {/* 详细计算过程 */}
                      <div className="bg-slate-50 rounded-lg p-3 border border-slate-100">
                        <div className="text-xs text-slate-500 mb-2 font-medium">计算公式</div>
                        <div className="space-y-1.5 text-xs text-slate-700">
                          {hasNoBreakdown ? (
                            <div className="flex items-center justify-between">
                              <span className="text-slate-500">总成本（无分项明细）</span>
                              <span className="text-emerald-600 font-bold text-base font-mono">
                                {formatPrice(totalCost)}
                              </span>
                            </div>
                          ) : formulaRows.length === 0 ? (
                            <div className="text-slate-400">无可计算的 Token 明细</div>
                          ) : (
                            formulaRows.map((row) => (
                              <div key={row.key} className="flex items-center justify-between gap-3 font-mono">
                                <span className="truncate">
                                  <span className="text-slate-500">{row.label}:</span>{' '}
                                  {row.tokenExpression || row.tokens.toLocaleString()} × {formatPrice(row.price)} / 1M{row.multiplierText ? ` ${row.multiplierText}` : ''}
                                </span>
                                <span className="text-slate-600 shrink-0">
                                  = {formatPrice(row.cost)}
                                </span>
                              </div>
                            ))
                          )}

                          {!hasNoBreakdown && (
                            <>
                              <div className="pt-2 mt-2 border-t border-slate-200 flex items-center gap-2 font-mono">
                                <span className="text-slate-500">应用总倍率:</span>
                                <span className="font-bold text-indigo-500">{formatMultiplier(totalMultiplier)}</span>
                                {isMultiplierAdjusted(modelTotalMultiplier) && (
                                  <span className="font-bold text-sky-600">{formatMultiplier(modelTotalMultiplier)}</span>
                                )}
                              </div>
                              <div className="pt-2 mt-2 border-t border-slate-200 flex items-center justify-between">
                                <span className="text-slate-600 font-medium text-sm">合计</span>
                                <span className="text-emerald-600 font-bold text-base font-mono">
                                  {formatPrice(totalCost)}
                                </span>
                              </div>
                            </>
                          )}
                        </div>
                      </div>
                    </div>
                  </div>
                )
              })()}

              {/* 错误信息（如果有） */}
              {(['failed', 'error', 'cancelled', 'timeout', 'partial_failed'].includes(request.status) ||
                request.failure_reason || request.cancel_reason) && (
                <div className="min-w-0 max-w-full overflow-hidden rounded-xl border border-rose-200 bg-rose-50">
                  <div className="px-4 py-3 bg-rose-100/50 border-b border-rose-200 flex items-center justify-between">
                    <h3 className="text-sm font-semibold text-rose-900 flex items-center gap-2">
                      <AlertCircle className="w-4 h-4" />
                      错误信息
                    </h3>
                    {errorText && (
                      <Tooltip content={copiedError ? '已复制' : '复制错误信息'}>
                        <button
                          onClick={() => {
                            navigator.clipboard.writeText(errorText);
                            setCopiedError(true);
                            setTimeout(() => setCopiedError(false), 2000);
                          }}
                          className="p-1 text-rose-400 hover:text-rose-600 hover:bg-rose-100/80 hover:scale-110 rounded transition-all"
                        >
                          {copiedError ? (
                            <Check className="w-3.5 h-3.5 text-emerald-600" />
                          ) : (
                            <Copy className="w-3.5 h-3.5" />
                          )}
                        </button>
                      </Tooltip>
                    )}
                  </div>
                  <div className="min-w-0 max-w-full space-y-2 overflow-hidden p-4">
                    {request.failure_reason && (
                      <div className="flex w-full min-w-0 items-start gap-1.5 text-sm">
                        <span className="shrink-0 font-medium text-slate-600">失败原因：</span>
                        <span className="w-0 min-w-0 flex-1 whitespace-pre-wrap break-all leading-6 text-rose-700 [overflow-wrap:anywhere]">{request.failure_reason}</span>
                      </div>
                    )}
                    {request.cancel_reason && (
                      <div className="flex w-full min-w-0 items-start gap-1.5 text-sm">
                        <span className="shrink-0 font-medium text-slate-600">取消原因：</span>
                        <span className="w-0 min-w-0 flex-1 whitespace-pre-wrap break-all leading-6 text-rose-700 [overflow-wrap:anywhere]">{request.cancel_reason}</span>
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
                const uniqueGroups = new Set(chainNodes.map(n => n.credentialGroup).filter(Boolean));
                const uniqueEndpoints = new Set(chainNodes.map(n => n.endpoint).filter(Boolean));
                const channelSwitches = showChannelInChain
                  ? chainNodes.filter((n, i) => i > 0 && n.channel && chainNodes[i - 1].channel && n.channel !== chainNodes[i - 1].channel).length
                  : 0;
                return (
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="inline-flex items-center gap-1.5 px-2.5 py-1 bg-indigo-50 text-indigo-700 text-xs font-medium rounded-lg border border-indigo-100">
                        <RefreshCw className="w-3 h-3" />
                        {chainNodes.length} 跳
                      </span>
                      {showChannelInChain ? (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 bg-slate-50 text-slate-600 text-xs font-medium rounded-lg border border-slate-200">
                          <Layers className="w-3 h-3" />
                          {uniqueChannels.size} 渠道
                        </span>
                      ) : currentChannelLabel && (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 bg-slate-50 text-slate-600 text-xs font-medium rounded-lg border border-slate-200">
                          <Layers className="w-3 h-3" />
                          渠道 {currentChannelLabel}
                        </span>
                      )}
                      {uniqueGroups.size > 0 && (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 bg-violet-50 text-violet-700 text-xs font-medium rounded-lg border border-violet-200">
                          <KeyRound className="w-3 h-3" />
                          {uniqueGroups.size} 分组
                        </span>
                      )}
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

                    <span className="inline-flex items-center rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1 text-xs font-medium text-slate-500">
                      时间线
                    </span>
                  </div>
                );
              })()}

              {/* 端点链路内容 */}
              <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
                <div className="p-4">
                  {chainNodes.length === 0 ? (
                    <div className="text-sm text-slate-400 text-center py-4">暂无端点链路记录</div>
                  ) : (
                    <div className="relative pl-8">
                      {/* 贯穿竖线（绝对定位，首尾圆点之间） */}
                      {chainNodes.length > 1 && (
                        <div className="absolute left-[11px] top-0 bottom-0 w-px bg-slate-200" />
                      )}
                      {chainNodes.map((node, idx) => {
                        const channelLabel = node.channel || '未知渠道';
                        const groupLabel = node.credentialGroup || '';
                        const endpointLabel = node.endpoint || node.raw || '-';
                        const routeLabel = [
                          showChannelInChain ? channelLabel : '',
                          groupLabel,
                          endpointLabel
                        ].filter(Boolean).join(' / ');
                        const tone = getChannelTone(node.channel);
                        const isFirst = idx === 0;
                        const isLast = idx === chainNodes.length - 1;
                        const isSingle = chainNodes.length === 1;
                        const isChannelSwitch = showChannelInChain && idx > 0 && node.channel && chainNodes[idx - 1].channel && node.channel !== chainNodes[idx - 1].channel;
                        const roleLabel = getChainRoleLabel(idx, chainNodes.length);

                        return (
                          <div key={`${node.raw}-${idx}`} className={`relative ${isLast ? '' : 'pb-2.5'}`}>
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
                              <div className={`border ${tone.border} rounded-lg px-3 py-2 ${isLast && !isSingle ? 'bg-emerald-50/30' : isFirst && !isSingle ? 'bg-blue-50/30' : 'bg-slate-50/50'}`}>
                                <div className="flex items-center gap-2 min-w-0">
                                  <span className={`text-[10px] font-bold shrink-0 ${
                                    isSingle ? 'text-indigo-500' : isFirst ? 'text-blue-500' : isLast ? 'text-emerald-500' : 'text-slate-400'
                                  }`}>
                                    #{node.hop}
                                  </span>
                                  {showChannelInChain && (
                                    <span className={`inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium border whitespace-nowrap ${tone.badge}`}>
                                      <span className="mr-1 text-current/70">渠道</span>{channelLabel}
                                    </span>
                                  )}
                                  {groupLabel && (
                                    <span className="inline-flex max-w-[6rem] items-center rounded border border-violet-200 bg-violet-50 px-1.5 py-0.5 text-[10px] font-medium text-violet-700 shrink-0 truncate">
                                      <span className="mr-1 text-violet-400">分组</span>{groupLabel}
                                    </span>
                                  )}
                                  <Tooltip content={routeLabel} className="min-w-0">
                                    <span className="text-sm font-semibold text-slate-900 truncate">
                                      {endpointLabel}
                                    </span>
                                  </Tooltip>
                                  <span className={`ml-auto shrink-0 text-[10px] ${
                                    isSingle ? 'text-indigo-500' : isFirst ? 'text-blue-400' : isLast ? 'text-emerald-500' : 'text-slate-400'
                                  }`}>
                                    {roleLabel}
                                  </span>
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
                  value={inputTokenCount}
                  colorClass={{ bg: 'bg-blue-50', border: 'border-blue-200', text: 'text-blue-600', icon: 'text-blue-600' }}
                />
                <TokenCard
                  icon={TrendingDown}
                  label="输出 Token"
                  value={outputTokenCount}
                  colorClass={{ bg: 'bg-emerald-50', border: 'border-emerald-200', text: 'text-emerald-600', icon: 'text-emerald-600' }}
                />
              </div>

              {isCodexRequest && (
                <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
                  <div className="px-4 py-3 bg-slate-50 border-b border-slate-100 flex items-center justify-between gap-3">
                    <h3 className="text-sm font-semibold text-slate-900">输出拆分</h3>
                    <span className="text-xs text-slate-500">计费口径：{billingOutputSourceLabel}</span>
                  </div>
                  <div className="grid grid-cols-2 md:grid-cols-4 divide-x divide-y md:divide-y-0 divide-slate-100">
                    {[
                      { label: '原始输出', value: outputTokenCount, tone: 'text-emerald-600' },
                      { label: '推理', value: reasoningTokenCount, tone: 'text-violet-600' },
                      { label: '可见补全', value: visibleOutputTokenCount, tone: 'text-sky-600' },
                      { label: '计费输出', value: billableOutputTokenCount, tone: 'text-orange-600' }
                    ].map((item) => (
                      <div key={item.label} className="p-4">
                        <div className="text-xs font-medium text-slate-500">{item.label}</div>
                        <div className={`mt-1 text-lg font-bold font-mono ${item.tone}`}>
                          {item.value.toLocaleString()}
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Token 卡片 - 缓存 */}
              <div className="grid grid-cols-2 gap-4">
                {isClaudeRequest && (
                  <>
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
                  </>
                )}
                <TokenCard
                  icon={Zap}
                  label="缓存读取"
                  value={request.cacheReadTokens || 0}
                  colorClass={{ bg: 'bg-amber-50', border: 'border-amber-200', text: 'text-amber-600', icon: 'text-amber-600' }}
                />
                {isClaudeRequest && (
                  <TokenCard
                    icon={Database}
                    label="缓存创建 (总计)"
                    value={request.cacheCreationTokens || 0}
                    colorClass={{ bg: 'bg-slate-50', border: 'border-slate-200', text: 'text-slate-600', icon: 'text-slate-500' }}
                  />
                )}
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
                      <div className="text-xs text-slate-400">{isCodexRequest ? 'Input + Output' : 'Input + Output + Cache'}</div>
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
                    {!isCodexRequest && <span>缓存 {cachePercent}%</span>}
                  </div>
                  <div className="h-2 bg-slate-200 rounded-full overflow-hidden flex">
                    <div className="bg-blue-500" style={{ width: `${inputPercent}%` }} />
                    <div className="bg-emerald-500" style={{ width: `${outputPercent}%` }} />
                    {!isCodexRequest && <div className="bg-amber-500" style={{ width: `${cachePercent}%` }} />}
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

          {/* 隐私防护 Tab */}
          {activeTab === 'privacy' && (
            <div className="space-y-6">
              <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
                <div className="px-4 py-3 bg-slate-50 border-b border-slate-100">
                  <h3 className="text-sm font-semibold text-slate-900 flex items-center gap-2">
                    <ShieldCheck className="w-4 h-4 text-emerald-600" />
                    隐私防护
                  </h3>
                </div>
                <div className="p-4">
                  <div className="flex items-center justify-between gap-4">
                    <span className="text-sm font-medium text-slate-500">处理结果</span>
                    <span className={`inline-flex items-center rounded-md border px-2 py-1 text-xs font-semibold ${
                      privacyRedacted
                        ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
                        : 'border-slate-200 bg-slate-50 text-slate-500'
                    }`}>
                      {privacyRedacted ? '已脱敏' : '未命中'}
                    </span>
                  </div>
                  {privacyHits.length > 0 && (
                    <div className="mt-3 flex flex-wrap gap-2">
                      {privacyHits.map((hit, index) => (
                        <span
                          key={`${hit.rule_id || index}-${hit.type}`}
                          className="min-w-0 max-w-full rounded-md border border-emerald-200 bg-emerald-50 px-2 py-1 text-xs font-medium text-emerald-700 break-words [overflow-wrap:anywhere]"
                        >
                          {(hit.rule_name || '规则')} · {privacyRuleTypeLabels[hit.type] || hit.type || '规则'} × {hit.count || 0}
                        </span>
                      ))}
                    </div>
                  )}
                  {privacyMatches.length > 0 && (
                    <div className="mt-4 overflow-hidden rounded-lg border border-emerald-100 bg-emerald-50/40">
                      <div className="border-b border-emerald-100 bg-white/70 px-3 py-2">
                        <span className="text-xs font-semibold text-emerald-800">具体处理内容</span>
                      </div>
                      <div className="divide-y divide-emerald-100">
                        {privacyMatches.map((match, index) => {
                          const raw = getPrivacyMatchRaw(match);
                          const replacement = match.replacement || match.replacement_value || '';
                          const typeLabel = privacyRuleTypeLabels[match.type] || match.type || '规则';
                          return (
                            <div
                              key={`${match.rule_id || 'rule'}-${index}-${replacement}`}
                              className="grid gap-2 px-3 py-2.5 sm:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)] sm:items-center"
                            >
                              <div className="min-w-0">
                                <div className="mb-1 flex items-center gap-2">
                                  <span className="rounded border border-emerald-200 bg-white px-1.5 py-0.5 text-[11px] font-medium text-emerald-700">
                                    原文
                                  </span>
                                  <span className="min-w-0 truncate text-[11px] text-slate-500">
                                    {match.rule_name || '规则'} · {typeLabel}
                                  </span>
                                </div>
                                <div className="rounded-md border border-slate-200 bg-white px-2 py-1.5 font-mono text-xs leading-relaxed text-slate-800 break-words [overflow-wrap:anywhere]">
                                  {raw || '-'}
                                </div>
                              </div>
                              <div className="hidden text-xs font-semibold text-emerald-600 sm:block">-&gt;</div>
                              <div className="min-w-0">
                                <div className="mb-1">
                                  <span className="rounded border border-emerald-200 bg-white px-1.5 py-0.5 text-[11px] font-medium text-emerald-700">
                                    替换值
                                  </span>
                                </div>
                                <div className="rounded-md border border-emerald-200 bg-white px-2 py-1.5 font-mono text-xs leading-relaxed text-emerald-800 break-words [overflow-wrap:anywhere]">
                                  {replacement || '-'}
                                </div>
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    </div>
                  )}
                </div>
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
              onClick={handleCopyRequestId}
              className={`h-8 px-3 text-xs font-medium rounded-lg transition-colors inline-flex items-center justify-center gap-1.5 ${
                copiedRequestId
                  ? 'text-emerald-700 bg-emerald-50 border border-emerald-200'
                  : 'text-slate-600 bg-white border border-slate-200 hover:bg-slate-50'
              }`}
            >
              {copiedRequestId ? (
                <>
                  <Check className="w-3.5 h-3.5" /> 已复制
                </>
              ) : (
                <>
                  <Copy className="w-3.5 h-3.5" /> 复制 ID
                </>
              )}
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
