// ============================================
// RequestDetailModal - 请求详情模态框
// Command Palette 风格
// 2025-12-01
// ============================================

import { useState, useEffect, useLayoutEffect, useRef } from 'react';
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
  Replace
} from 'lucide-react';
import RequestStatusBadge from './RequestStatusBadge.jsx';
import ModelTag from './ModelTag.jsx';
import DurationBadge from './DurationBadge.jsx';
import { formatCost, formatDuration, formatTimestamp } from '@utils/api.js';
import { formatMultiplierDisplayValue } from '@utils/multiplier.js';
import { Tooltip } from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';
import {
  getBillableInputTokens,
  getDisplayInputTokens,
  getDisplayTotalTokens,
  isCacheInclusiveProvider
} from '../utils/tokenDisplay.js';
import {
  countChannelSwitches,
  getEndpointChainItems,
  getRequestNetworkDisplayState,
  getRequestChannelLabel,
  getRequestCredentialGroupLabel,
  parseChainNode,
  requestHasInterChannelTransfer,
  summarizeEndpointAttempts,
} from '../utils/endpointChain.js';
import {
  formatReasoningFieldValue,
  getReasoningEffortMeta,
  getServiceTierMeta,
  isThinkingTypeActive,
  getPrivacyDisplay,
  parseReasoningConfig,
  PRIVACY_RULE_SOURCE_LABELS,
  PRIVACY_STATE_STYLES,
  REASONING_FIELD_LABELS
} from '../utils/requestMetadata.js';

// failure_reason 的存储格式为 "<分类>: <详情>"（tracker.go RecordRequestFinalFailure 拼接），
// 只拆出 Relay 自己的分类标签；服务商详情保持原文展示。
const splitFailureReason = (raw) => {
  const text = String(raw || '').trim();
  if (!text) return { category: '', detail: '' };
  const withDetail = text.match(/^([a-z][a-z0-9_]*):\s([\s\S]+)$/);
  if (withDetail) return { category: withDetail[1], detail: withDetail[2] };
  if (/^[a-z][a-z0-9_]*$/.test(text)) return { category: text, detail: '' };
  return { category: '', detail: text };
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
    <div className="flex items-start justify-between py-2 border-b border-slate-100 last:border-b-0 group">
      <div className="flex items-center gap-2 text-slate-500 min-w-[120px]">
        <Icon className="w-4 h-4 flex-shrink-0" />
        <span className="text-sm font-medium">{label}</span>
      </div>
      <div className="flex min-w-0 flex-1 items-center justify-end gap-2">
        <span className="min-w-0 max-w-full whitespace-pre-wrap break-words text-right font-mono text-sm text-slate-900 [overflow-wrap:anywhere]">
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
const RequestDetailModal = ({ isOpen, onClose, request, positioningRef }) => {
  const [activeTab, setActiveTab] = useState('overview');
  const [copiedError, setCopiedError] = useState(false);
  const [copiedRequestId, setCopiedRequestId] = useState(false);
  const requestIdCopyTimerRef = useRef(null);
  const modalPositionRef = useRef(null);

  useLayoutEffect(() => {
    if (!isOpen) return undefined;

    const target = positioningRef?.current;
    const layer = modalPositionRef.current;
    if (!target || !layer) return undefined;

    const updateModalPosition = () => {
      const rect = target.getBoundingClientRect();
      layer.style.top = `${Math.max(0, rect.top - 24)}px`;
      layer.style.right = `${Math.max(0, document.documentElement.clientWidth - rect.right)}px`;
      layer.style.bottom = `${Math.max(0, window.innerHeight - rect.bottom)}px`;
      layer.style.left = `${Math.max(0, rect.left)}px`;
    };

    updateModalPosition();
    window.addEventListener('resize', updateModalPosition);
    const resizeObserver = typeof ResizeObserver !== 'undefined'
      ? new ResizeObserver(updateModalPosition)
      : null;
    resizeObserver?.observe(target);

    return () => {
      window.removeEventListener('resize', updateModalPosition);
      resizeObserver?.disconnect();
    };
  }, [isOpen, positioningRef]);

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

  // 计算 Token 总数：Codex/OpenAI input_tokens 已包含 cached_tokens；兼容旧 Codex 记录的非缓存 input。
  const inputTokenCount = getDisplayInputTokens(request);
  const outputTokenCount = request.outputTokens || 0;
  const reasoningTokenCount = request.reasoningTokens || 0;
  const visibleOutputTokenCount = Math.max(0, outputTokenCount - reasoningTokenCount);
  const billingOutputTokenSource = request.billingOutputTokenSource || 'output_tokens';
  const billingRuleSnapshot = request.billingRuleSnapshot || null;
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

  const endpointChain = getEndpointChainItems(request);
  const chainNodes = endpointChain.map((item, index) => {
    const node = parseChainNode(item, index + 1);
    if (!node.raw && !node.endpoint && !node.channel && !node.credentialGroup) return null;
    if (endpointChain.length === 1 && !node.channel) {
      return {
        ...node,
        channel: getRequestChannelLabel(request, [node], ''),
        credentialGroup: node.credentialGroup || request.credentialGroup || request.groupName || '',
      };
    }
    return node;
  }).filter(Boolean);
  const networkSummary = summarizeEndpointAttempts(request, chainNodes);
  const networkDisplayState = getRequestNetworkDisplayState(request);
  const networkHTTPStatus = request.httpStatusCode ?? request.http_status_code ?? request.statusCode;
  const networkFirstByteMs = request.firstByteMs ?? request.first_byte_ms;
  const currentCredentialGroupLabel = getRequestCredentialGroupLabel(request, chainNodes);
  const showChannelInChain = requestHasInterChannelTransfer(request);
  const currentChannelLabel = getRequestChannelLabel(request, chainNodes);
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
  const failureParts = splitFailureReason(request.failure_reason);
  const errorText = [
    networkHTTPStatus ? `HTTP 状态码: ${networkHTTPStatus}` : null,
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
  const reasoningRaw = request.reasoningEffort || request.reasoning_effort || '';
  const reasoning = parseReasoningConfig(reasoningRaw);
  const reasoningEffort = reasoning.effort || '';
  const reasoningEffortMeta = getReasoningEffortMeta(reasoningEffort);
  const hasReasoning = Object.keys(reasoning).length > 0;
  const serviceTier = request.serviceTier || request.service_tier || '';
  const serviceTierMeta = getServiceTierMeta(serviceTier);
  const privacy = getPrivacyDisplay(request);
  const privacyRedacted = privacy.state !== 'none';
  const privacyHits = privacy.hits;
  const privacyDetectOnly = privacy.detectOnly;

  return renderModalPortal(
    <div className="fixed inset-0 z-[10000]">
      {/* 背景遮罩 */}
      <div
        className="absolute inset-0 bg-slate-900/20 backdrop-blur-sm animate-in fade-in duration-200"
        onClick={onClose}
      />
      <div
        ref={modalPositionRef}
        className="absolute inset-0 flex items-center justify-center p-4"
        onMouseDown={(event) => {
          if (event.target === event.currentTarget) onClose();
        }}
      >
        {/* 模态框内容 - 固定高度，内容区域滚动 */}
      <div className="relative flex h-[80vh] max-h-[48rem] w-full max-w-3xl flex-col overflow-hidden rounded-2xl bg-white shadow-2xl ring-1 ring-black/5 animate-in zoom-in-95 fade-in duration-200">
        {/* 头部 - 固定不滚动 */}
        <div className="flex flex-shrink-0 items-center justify-between rounded-t-2xl border-b border-slate-100 bg-white px-6 py-4">
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
        <div className="flex-shrink-0 border-b border-slate-100 bg-slate-50/50 px-6 pt-4">
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
        <div className="min-h-0 flex-1 overflow-y-auto py-6 pl-6 pr-2 [scrollbar-gutter:stable] scrollbar-thin scrollbar-thumb-slate-200">
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
                <div className="px-4">
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
                  <InfoRow
                    icon={Layers}
                    label="渠道"
                    value={(
                      <span className="inline-flex min-w-0 max-w-full items-center rounded-md border border-blue-200 bg-blue-50 px-2 py-1 text-right text-xs font-medium text-blue-700 [overflow-wrap:anywhere]">
                        {currentChannelLabel || '-'}
                      </span>
                    )}
                  />
                  <InfoRow
                    icon={Database}
                    label="分组"
                    value={(
                      <span className="inline-flex min-w-0 max-w-full items-center rounded-md border border-violet-200 bg-violet-50 px-2 py-1 text-right text-xs font-medium text-violet-700 [overflow-wrap:anywhere]">
                        {currentCredentialGroupLabel || '-'}
                      </span>
                    )}
                  />
                  <InfoRow
                    icon={Server}
                    label="端点"
                    value={(
                      <span className="inline-flex min-w-0 max-w-full items-center rounded-md border border-slate-200 bg-slate-50 px-2 py-1 text-right text-xs font-medium text-slate-700 [overflow-wrap:anywhere]">
                        {request.endpoint || '-'}
                      </span>
                    )}
                  />
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
                  {hasReasoning && (
                    <div className="border-t border-slate-100 pt-3 mt-1 space-y-2">
                      <div className="text-xs font-semibold text-slate-400 uppercase tracking-wide">Reasoning / Thinking</div>
                      {reasoningEffort && (
                        <div className="flex items-center justify-between py-1.5">
                          <span className="text-sm font-medium text-slate-500">{REASONING_FIELD_LABELS.effort}</span>
                          <Tooltip content={reasoningEffortMeta.description}>
                            <span className={`inline-flex items-center whitespace-nowrap rounded border px-2 py-0.5 text-xs font-medium ${reasoningEffortMeta.className}`}>
                              {reasoningEffortMeta.label}
                            </span>
                          </Tooltip>
                        </div>
                      )}
                      {Object.entries(reasoning)
                        .filter(([k]) => k !== 'effort')
                        .map(([key, value]) => {
                          if (value == null || value === '') return null;
                          const label = REASONING_FIELD_LABELS[key] || key;
                          const rawValue = String(value);
                          const isType = key === 'type';
                          const mappedValue = formatReasoningFieldValue(key, value);
                          return (
                            <div key={key} className="flex items-start justify-between gap-4 py-1.5">
                              <span className="shrink-0 text-sm font-medium text-slate-500">{label}</span>
                              {isType ? (
                                <span className={`inline-flex items-center rounded border px-2 py-0.5 text-xs font-medium whitespace-nowrap ${
                                  isThinkingTypeActive(rawValue) ? 'bg-emerald-50 text-emerald-600 border-emerald-200' : 'bg-slate-50 text-slate-500 border-slate-200'
                                }`}>
                                  {mappedValue}
                                </span>
                              ) : (
                                <span className="min-w-0 text-right text-sm text-slate-700 [overflow-wrap:anywhere]">{mappedValue}</span>
                              )}
                            </div>
                          );
                        })}
                    </div>
                  )}
                  {serviceTierMeta && (
                    <div className="flex items-center justify-between py-3 border-t border-slate-100">
                      <span className="text-sm font-medium text-slate-500">服务档位</span>
                      <Tooltip content={serviceTierMeta.description}>
                        <span className={`inline-flex items-center whitespace-nowrap rounded border px-2 py-0.5 text-xs font-medium ${serviceTierMeta.className}`}>
                          {serviceTierMeta.label}
                        </span>
                      </Tooltip>
                    </div>
                  )}
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
              {((request.cost || 0) > 0 || (request.inputCost || 0) > 0 || (request.outputCost || 0) > 0 || billingRuleSnapshot?.rule_key) && (() => {
                const calcUnitPrice = (cost, tokens) => {
                  if (!tokens || tokens <= 0) return 0
                  return (cost / tokens) * 1_000_000
                }
                const safeMultiplier = (value) => {
                  const parsed = Number(value)
                  return Number.isFinite(parsed) && parsed > 0 ? parsed : 1.0
                }

                // 计费决策快照：后端在成本计算时无条件落库的命中定价行、生效单价与各层倍率。
                // 有快照就正向渲染（tokens × 单价 × 倍率）；只有旧记录才回退到除法反推单价，
                // 反推会把 long context 倍率之类未单独落库的层默默吸收进「基础单价」。
                const snapshot = billingRuleSnapshot
                const hasSnapshot = Boolean(snapshot?.context_mode)
                const snapPrice = (value) => {
                  const parsed = Number(value)
                  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0
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
                const totalMultiplier = safeMultiplier(hasSnapshot ? snapshot.group_cost_multiplier : request.costMultiplier)
                const inputMultiplier = safeMultiplier(request.inputCostMultiplier)
                const outputMultiplier = safeMultiplier(request.outputCostMultiplier)
                const cacheReadMultiplier = safeMultiplier(request.cacheReadCostMultiplier)
                const cache5mMultiplier = safeMultiplier(request.cacheCreationCostMultiplier)
                const cache1hMultiplier = safeMultiplier(request.cacheCreationCostMultiplier1h)
                const modelTotalMultiplier = safeMultiplier(hasSnapshot ? snapshot.model_cost_multiplier : request.modelCostMultiplier)
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
                // 有快照直接取落库单价，无快照才反推。两条路径都在 token 为 0 时归零，
                // 否则快照里的 1h 缓存兜底价会让没有 1h 用量的请求也长出一个单价徽章。
                const resolvePrice = (tokens, snapshotField, legacyPrice) => {
                  if (!tokens || tokens <= 0) return 0
                  return hasSnapshot ? snapPrice(snapshot[snapshotField]) : legacyPrice()
                }
                const inputBasePrice = resolvePrice(billableInputTokens, 'input_price',
                  () => calcBasePrice(calcUnitPrice(inputCost, billableInputTokens), inputMultiplier, modelInputMultiplier))
                const outputBasePrice = resolvePrice(billableOutputTokens, 'output_price',
                  () => calcBasePrice(calcUnitPrice(outputCost, billableOutputTokens), outputMultiplier, modelOutputMultiplier))
                const cacheReadBasePrice = resolvePrice(cacheReadTokens, 'cache_read_price',
                  () => calcBasePrice(calcUnitPrice(cacheReadCost, cacheReadTokens), cacheReadMultiplier, modelCacheReadMultiplier))
                const cache5mBasePrice = resolvePrice(cache5mTokens, 'cache_creation_price',
                  () => calcBasePrice(calcUnitPrice(cache5mCost, cache5mTokens), cache5mMultiplier))
                const cache1hBasePrice = resolvePrice(cache1hTokens, 'cache_creation_1h_price',
                  () => calcBasePrice(calcUnitPrice(cache1hCost, cache1hTokens), cache1hMultiplier))
                // 兼容旧数据：无分项 5m/1h 时，展示合并的缓存创建价格（旧数据无倍率字段，默认 1.0）
                const hasSplitCache = cache5mTokens > 0 || cache1hTokens > 0
                const aggregateCacheCreationPrice = hasSplitCache
                  ? 0
                  : resolvePrice(cacheCreationTokens, 'cache_creation_price',
                    () => calcBasePrice(calcUnitPrice(cacheCreationCost, cacheCreationTokens), 1.0))
                const outputTokenExpression = billingSource === 'visible_output_tokens'
                  ? `${outputTokens.toLocaleString()} - ${reasoningTokens.toLocaleString()} = ${billableOutputTokens.toLocaleString()}`
                  : billableOutputTokens.toLocaleString()
                const buildMultiplierText = (...multipliers) => {
                  return multipliers
                    .filter((value) => isMultiplierAdjusted(value))
                    .map((value) => formatMultiplier(value))
                    .join(' ')
                }
                // 公式行：有快照就正向算（tokens × 落库单价 × 分项倍率），再乘「应用总倍率」行
                // 即为落库成本。快照的分项倍率已合并端点/分组、模型、long context 三层，
                // 所以只显示这一个数；旧记录仍从成本里除掉总倍率反推。
                const rowMultiplierText = (snapshotField, ...legacyMultipliers) => hasSnapshot
                  ? buildMultiplierText(safeMultiplier(snapshot[snapshotField]))
                  : buildMultiplierText(...legacyMultipliers)
                const rowCost = (tokens, price, snapshotField, storedCost) => hasSnapshot
                  ? tokens * price / 1_000_000 * safeMultiplier(snapshot[snapshotField])
                  : removeTotalMultiplier(storedCost)

                const priceBadges = [
                  inputBasePrice > 0 && { key: 'input', label: '输入', value: inputBasePrice, color: 'blue' },
                  outputBasePrice > 0 && { key: 'output', label: '输出', value: outputBasePrice, color: 'emerald' },
                  cacheReadBasePrice > 0 && { key: 'cacheRead', label: '缓存读取', value: cacheReadBasePrice, color: 'amber' },
                  cache5mBasePrice > 0 && { key: 'cache5m', label: '5m 缓存创建', value: cache5mBasePrice, color: 'purple' },
                  cache1hBasePrice > 0 && { key: 'cache1h', label: '1h 缓存创建', value: cache1hBasePrice, color: 'violet' },
                  aggregateCacheCreationPrice > 0 && { key: 'cacheCreation', label: isCodexRequest ? '缓存写入' : '缓存创建', value: aggregateCacheCreationPrice, color: 'slate' }
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
                    multiplierText: rowMultiplierText('input_cost_multiplier', inputMultiplier, modelInputMultiplier),
                    cost: rowCost(billableInputTokens, inputBasePrice, 'input_cost_multiplier', inputCost)
                  },
                  billableOutputTokens > 0 && {
                    key: 'output',
                    label: isCodexRequest ? `输出(计费:${billingOutputSourceLabel})` : '输出',
                    tokens: billableOutputTokens,
                    tokenExpression: outputTokenExpression,
                    price: outputBasePrice,
                    multiplierText: rowMultiplierText('output_cost_multiplier', outputMultiplier, modelOutputMultiplier),
                    cost: rowCost(billableOutputTokens, outputBasePrice, 'output_cost_multiplier', outputCost)
                  },
                  cacheReadTokens > 0 && {
                    key: 'cacheRead', label: '缓存读取', tokens: cacheReadTokens, price: cacheReadBasePrice, multiplierText: rowMultiplierText('cache_read_cost_multiplier', cacheReadMultiplier, modelCacheReadMultiplier), cost: rowCost(cacheReadTokens, cacheReadBasePrice, 'cache_read_cost_multiplier', cacheReadCost)
                  },
                  cache5mTokens > 0 && {
                    key: 'cache5m', label: '5m 缓存创建', tokens: cache5mTokens, price: cache5mBasePrice, multiplierText: rowMultiplierText('cache_creation_cost_multiplier', cache5mMultiplier), cost: rowCost(cache5mTokens, cache5mBasePrice, 'cache_creation_cost_multiplier', cache5mCost)
                  },
                  cache1hTokens > 0 && {
                    key: 'cache1h', label: '1h 缓存创建', tokens: cache1hTokens, price: cache1hBasePrice, multiplierText: rowMultiplierText('cache_creation_1h_cost_multiplier', cache1hMultiplier), cost: rowCost(cache1hTokens, cache1hBasePrice, 'cache_creation_1h_cost_multiplier', cache1hCost)
                  },
                  !hasSplitCache && cacheCreationTokens > 0 && {
                    key: 'cacheCreation', label: isCodexRequest ? '缓存写入' : '缓存创建', tokens: cacheCreationTokens, price: aggregateCacheCreationPrice, multiplierText: rowMultiplierText('cache_creation_cost_multiplier'), cost: rowCost(cacheCreationTokens, aggregateCacheCreationPrice, 'cache_creation_cost_multiplier', cacheCreationCost)
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
                      {hasSnapshot && (
                        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 border-b border-slate-100 pb-3 text-xs text-slate-600">
                          <span className="min-w-0 break-all font-semibold text-slate-700">
                            定价 {snapshot.pricing_model || '-'}{snapshot.pricing_service_tier ? ` · ${snapshot.pricing_service_tier}` : ''}
                          </span>
                          <span className={snapshot.context_mode === 'long' ? 'font-semibold text-amber-600' : ''}>
                            {snapshot.context_mode === 'long' ? '长上下文单价' : '标准上下文单价'}
                          </span>
                          {Number(snapshot.long_context_threshold) > 0 && (
                            <span className="text-slate-400">
                              输入 {Number(snapshot.effective_input_tokens || 0).toLocaleString()} / 阈值 {Number(snapshot.long_context_threshold).toLocaleString()}
                            </span>
                          )}
                          {snapshot.rule_key && (
                            <span className="min-w-0 break-all font-semibold text-indigo-700">
                              组合规则 · {snapshot.rule_mode === 'fixed' ? '固定单价' : '倍率'} · {snapshot.rule_key}
                            </span>
                          )}
                        </div>
                      )}
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
                    {(networkHTTPStatus || failureParts.category) && (
                      <div className="flex flex-wrap items-center gap-2">
                        {networkHTTPStatus && (
                          <span className="inline-flex items-center rounded-md border border-rose-300 bg-rose-100 px-2 py-0.5 font-mono text-xs font-semibold text-rose-800">
                            HTTP {networkHTTPStatus}
                          </span>
                        )}
                        {failureParts.category && (
                          <span className="inline-flex items-center rounded-md border border-rose-200 bg-white px-2 py-0.5 font-mono text-xs text-rose-700">
                            {failureParts.category}
                          </span>
                        )}
                      </div>
                    )}
                    {failureParts.detail && (
                      <div className="flex w-full min-w-0 items-start gap-1.5 text-sm">
                        <span className="shrink-0 font-medium text-slate-600">原始信息：</span>
                        <span className="w-0 min-w-0 flex-1 whitespace-pre-wrap break-all leading-6 text-rose-700 [overflow-wrap:anywhere]">{failureParts.detail}</span>
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
                const channelSwitches = showChannelInChain ? countChannelSwitches(chainNodes) : 0;
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
                          <Layers className="w-3 h-3" />
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

                    <div className="ml-auto flex items-center gap-2">
                      <span className="inline-flex items-center rounded-lg border border-slate-200 bg-slate-50 px-2.5 py-1 text-xs font-medium text-slate-500">
                        时间线
                      </span>
                    </div>
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
                        const endpointError = node.error || '';
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
                                <span className="text-[10px] text-amber-600 font-medium">渠道切换至 {channelLabel}</span>
                              </div>
                            )}
                            {/* 卡片行：相对定位容器，圆点绝对定位到卡片垂直中心 */}
                            <div className="relative">
                              {/* 圆点 - 绝对定位到卡片左侧、垂直居中 */}
                              <div className={`absolute -left-8 top-1/2 -translate-y-1/2 w-[10px] h-[10px] rounded-full z-10 ring-[3px] ring-white ${
                                isSingle ? 'bg-indigo-500' : isFirst ? 'bg-blue-500' : isLast ? 'bg-emerald-500' : 'bg-slate-300'
                              }`} style={{ left: '-25px' }} />
                              {/* 卡片 */}
                              <div className={`border ${tone.border} rounded-lg px-2.5 py-1.5 ${isLast && !isSingle ? 'bg-emerald-50/30' : isFirst && !isSingle ? 'bg-blue-50/30' : 'bg-slate-50/50'}`}>
                                <div className="flex items-center gap-1.5 min-w-0">
                                  <span className={`text-[10px] font-semibold shrink-0 ${
                                    isSingle ? 'text-indigo-500' : isFirst ? 'text-blue-500' : isLast ? 'text-emerald-500' : 'text-slate-400'
                                  }`}>
                                    #{node.hop}
                                  </span>
                                  {showChannelInChain && (
                                    <span className={`inline-flex items-center rounded px-1.5 py-[1px] text-[9px] font-medium border whitespace-nowrap ${tone.badge}`}>
                                      <span className="mr-1 text-current/70">渠道</span>{channelLabel}
                                    </span>
                                  )}
                                  {groupLabel && (
                                    <span className="inline-flex max-w-[7rem] shrink-0 items-center rounded border border-violet-200/70 bg-violet-50/80 px-1.5 py-[1px] text-[9px] font-medium text-violet-600">
                                      <span className="mr-1 text-violet-400">分组</span>
                                      <span className="truncate">{groupLabel}</span>
                                    </span>
                                  )}
                                  <div className="flex min-w-0 flex-1 items-center gap-1.5">
                                    <Tooltip content={routeLabel} className="min-w-0 max-w-[12rem]">
                                      <span className="block truncate text-[13px] font-semibold text-slate-800">
                                        {endpointLabel}
                                      </span>
                                    </Tooltip>
                                  </div>
                                  <span className={`shrink-0 text-[10px] ${
                                    isSingle ? 'text-indigo-500' : isFirst ? 'text-blue-400' : isLast ? 'text-emerald-500' : 'text-slate-400'
                                  }`}>
                                    {roleLabel}
                                  </span>
                                </div>
                                {endpointError && (
                                  <div className="mt-1.5 min-w-0 rounded-md border border-rose-100 bg-rose-50/80 px-2 py-1 text-[11px] leading-relaxed text-rose-700 break-words [overflow-wrap:anywhere]">
                                    <span className="shrink-0 font-medium text-rose-600">错误：</span>
                                    <span className="whitespace-pre-wrap break-words [overflow-wrap:anywhere]">{endpointError}</span>
                                  </div>
                                )}
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
            <div className="space-y-4">
              <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
                <div className="px-4 py-3 bg-slate-50 border-b border-slate-100">
                  <h3 className="text-sm font-semibold text-slate-900">请求结果</h3>
                </div>
                <div className="px-4">
                  <InfoRow icon={Server} label="HTTP 状态码" value={networkHTTPStatus > 0 ? networkHTTPStatus : '-'} />
                  <InfoRow icon={Activity} label="请求状态" value={<RequestStatusBadge status={request.status} />} />
                  <InfoRow icon={Waves} label="流式状态" value={networkDisplayState.isStreaming == null ? '-' : (networkDisplayState.isStreaming ? '流式' : '非流式')} />
                  <InfoRow icon={Clock} label="总耗时" value={networkDisplayState.durationMs == null ? '-' : formatDuration(networkDisplayState.durationMs)} />
                  <InfoRow icon={Zap} label="首字耗时" value={networkFirstByteMs == null ? '-' : formatDuration(networkFirstByteMs)} />
                </div>
              </div>

              <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
                <div className="px-4 py-3 bg-slate-50 border-b border-slate-100">
                  <h3 className="text-sm font-semibold text-slate-900">路由与尝试</h3>
                </div>
                <div className="px-4">
                  <InfoRow icon={Replace} label="是否转移" value={networkSummary.transferred ? '已转移' : '未转移'} />
                  <InfoRow icon={Activity} label="尝试次数" value={`${networkSummary.attemptCount} 次`} />
                  <InfoRow icon={RefreshCw} label="重试次数" value={`${networkSummary.retryCount} 次`} />
                  <InfoRow icon={Layers} label="转移次数" value={`${networkSummary.transferCount} 次`} />
                  <InfoRow icon={Server} label="起始端点" value={networkSummary.firstEndpoint || '-'} />
                  <InfoRow icon={Server} label="最终端点" value={networkSummary.finalEndpoint || request.endpoint || '-'} />
                </div>
              </div>

              <div className="bg-white rounded-xl border border-slate-200 overflow-hidden">
                <div className="px-4 py-3 bg-slate-50 border-b border-slate-100">
                  <h3 className="text-sm font-semibold text-slate-900">请求信息</h3>
                </div>
                <div className="px-4">
                  <InfoRow icon={Activity} label="请求方法" value={request.method || '-'} />
                  <InfoRow icon={FileText} label="请求路径" value={request.path || '-'} copyable />
                  <InfoRow icon={Server} label="客户端 IP" value={request.clientIP || request.client_ip || '-'} copyable />
                  <InfoRow icon={FileText} label="User-Agent" value={request.userAgent || request.user_agent || '-'} copyable />
                </div>
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
                    <span className={`inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-semibold ${PRIVACY_STATE_STYLES[privacy.state]}`}>
                      {privacy.label}
                      {privacy.hitCount > 0 && <span className="opacity-70">· {privacy.hitCount} 处</span>}
                    </span>
                  </div>
                  {privacyDetectOnly && (
                    <p className="mt-2 text-xs text-slate-500">
                      当前为「仅检测」模式，命中已统计但请求正文未被改写。
                    </p>
                  )}
                  {privacyHits.length > 0 && (
                    <div className="mt-3 flex flex-wrap gap-2">
                      {privacyHits.map((hit, index) => (
                        <span
                          key={`${hit.rule_id || index}-${hit.rule_name || ''}`}
                          className="min-w-0 max-w-full rounded-md border border-emerald-200 bg-emerald-50 px-2 py-1 text-xs font-medium text-emerald-700 break-words [overflow-wrap:anywhere]"
                        >
                          {(hit.rule_name || '规则')}
                          {hit.source ? ` · ${PRIVACY_RULE_SOURCE_LABELS[hit.source] || hit.source}` : ''}
                          {` × ${hit.count || 0}`}
                        </span>
                      ))}
                    </div>
                  )}
                  {privacyRedacted && (
                    <p className="mt-4 rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-xs leading-relaxed text-slate-500">
                      仅展示命中的规则与次数。命中的原文不会写入数据库、日志或本页面 —— 否则隐私防护本身就会变成一份客户信息的集中副本。
                    </p>
                  )}
                </div>
              </div>
            </div>
          )}
        </div>

        {/* 底部操作栏 - 固定不滚动 */}
        <div className="flex flex-shrink-0 items-center justify-between rounded-b-2xl border-t border-slate-100 bg-slate-50/80 px-6 py-4 backdrop-blur-sm">
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
  </div>
  );
};

export default RequestDetailModal;
