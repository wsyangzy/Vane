// ============================================
// RequestsTable - 请求列表表格（支持动态列配置）
// 2025-12-01 10:31:09
// 更新：添加双击查看详情功能
// ============================================

import { useState, useRef, useEffect } from 'react';
import { Waves, RefreshCw, Route } from 'lucide-react';
import { LoadingSpinner, Tooltip } from '@/shared/ui/index.js';
import { formatCost, formatDuration, formatNumber, formatTimestamp } from '@/services/http/api.js';
import DurationBadge from './DurationBadge.jsx';
import RequestStatusBadge from './RequestStatusBadge.jsx';
import ModelTag from './ModelTag.jsx';
import Pagination from './Pagination.jsx';
import RequestRouteCell from './RequestRouteCell.jsx';
import { getDisplayInputTokens } from '../utils/tokenDisplay.js';
import { requestColumnLayout, requestRowHeight } from '../utils/tableLayout.js';
import {
  countChannelSwitches,
  getEndpointChainItems,
} from '../utils/endpointChain.js';
import {
  getReasoningEffortMeta,
  getRequestServiceTierMeta,
  isThinkingTypeActive,
  parseReasoningConfig
} from '../utils/requestMetadata.js';

const getReasoningDisplay = (raw, compact) => {
  const reasoning = parseReasoningConfig(raw);
  // output_config.effort 才是推理强度；thinking.type 只是扩展思考的开关模式
  // （adaptive 表示由模型自行决定思考量，与强度档位无关）。
  // 没有 effort 时如实展示模式本身，并在 tooltip 里说明，不让它冒充强度。
  const effort = reasoning.effort || '';
  const thinkingType = reasoning.type || '';
  const value = effort || thinkingType;
  if (!value) return <span className="text-gray-400 text-xs">-</span>;

  const meta = effort ? getReasoningEffortMeta(effort) : {
    label: thinkingType,
    description: `扩展思考模式 ${thinkingType}，不是推理强度；该请求未指定 effort。`,
    className: isThinkingTypeActive(thinkingType)
      ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
      : 'border-slate-200 bg-slate-50 text-slate-600'
  };
  return (
    <Tooltip content={meta.description} className="min-w-0 max-w-full">
      <span className={`block max-w-full truncate rounded border py-0.5 text-[11px] font-medium ${compact ? 'px-1' : 'px-2'} ${meta.className}`}>
        {meta.label}
      </span>
    </Tooltip>
  );
};

const ChannelCell = ({ request, compact }) => {
  const channelLabel = request.channel || '-';
  const channelTransferCount = countChannelSwitches(getEndpointChainItems(request));

  return (
    <span className="inline-flex w-full max-w-full items-center justify-center gap-1 min-w-0">
      <span className="min-w-0 truncate text-purple-600 text-xs font-medium">{channelLabel}</span>
      {channelTransferCount > 0 && (
        <Tooltip asChild content={`渠道切换 ${channelTransferCount} 次`}><span
          className={`inline-flex shrink-0 items-center gap-0.5 py-0.5 text-indigo-600 text-[10px] font-medium rounded whitespace-nowrap ${compact ? '' : 'border border-indigo-100 bg-indigo-50 px-1'}`}
          aria-label={`渠道切换 ${channelTransferCount} 次`}
        >
          <Route className="w-2.5 h-2.5" />
          {!compact && <span>{channelTransferCount}</span>}
        </span></Tooltip>
      )}
    </span>
  );
};

const FirstByteBadge = ({ ms, status }) => {
  if (ms === null || ms === undefined) {
    return (
      <span className="inline-flex items-center rounded-full border border-slate-200 bg-slate-50 px-1.5 py-0.5 font-mono text-[11px] text-slate-400 whitespace-nowrap">
        -
      </span>
    );
  }

  return <DurationBadge ms={ms} status={status} size="xs" />;
};

const LatencyCell = ({ request, compact }) => compact ? (
  <Tooltip asChild content={`用时 ${request.duration ?? '—'}ms / 首字 ${request.firstByteMs ?? '—'}ms`}><span className="inline-flex max-w-full items-center gap-0.5 font-mono text-[10px] tabular-nums">
    <span className="min-w-0 truncate text-slate-600">{request.duration == null ? '—' : formatDuration(request.duration)}</span>
    <span className="text-slate-300">/</span>
    <span className="min-w-0 truncate text-emerald-600">{request.firstByteMs == null ? '—' : formatDuration(request.firstByteMs)}</span>
  </span></Tooltip>
) : (
  <Tooltip asChild content={`用时 ${request.duration ?? '—'}ms / 首字 ${request.firstByteMs ?? '—'}ms`}><div className="inline-flex max-w-full items-center justify-center gap-1 whitespace-nowrap">
    <DurationBadge ms={request.duration} status={request.status} size="xs" />
    <FirstByteBadge ms={request.firstByteMs} status={request.status} />
  </div></Tooltip>
);

/**
 * 渲染单元格内容
 * @param {Object} column - 列配置及当前分配宽度
 * @param {Object} request - 请求数据
 */
const renderCell = (column, request, onViewDispatch) => {
  const columnId = column.id;
  const compact = column.layoutWidth < 100;
  const tokenText = (value) => value == null ? '—' : column.layoutWidth < 62 ? formatNumber(Number(value)) : value;
  switch (columnId) {
    case 'requestId':
      const StreamIcon = request.isStreaming ? Waves : RefreshCw;
      const streamTitle = request.isStreaming ? '流式请求' : '常规请求';
      const iconColor = request.isStreaming ? 'text-blue-500' : 'text-slate-400';
      return (
        <div className="flex w-full min-w-0 items-center justify-center gap-1 text-blue-600 font-mono text-[11px] group-hover:text-indigo-600 transition-colors">
          <Tooltip content={streamTitle}>
            <StreamIcon className={`w-3 h-3 ${iconColor} flex-shrink-0`} />
          </Tooltip>
          <span className="min-w-0 truncate">{compact ? request.requestId?.replace(/^req-/, '') : request.requestId}</span>
        </div>
      );
    case 'timestamp': {
      const fullTime = formatTimestamp(request.timestamp);
      const time = column.layoutWidth < 90 ? fullTime.split(' ').at(-1) : column.layoutWidth < 132 ? fullTime.replace(/^\d{4}\//, '') : fullTime;
      return <Tooltip asChild content={fullTime}><span className="block truncate text-[11px] tabular-nums text-gray-400">{time}</span></Tooltip>;
    }
    case 'provider':
      const providerValue = request.provider || 'claude';
      const isCodex = providerValue === 'codex';
      return (
        <span className={`inline-flex max-w-full items-center justify-center rounded border py-0.5 text-[11px] font-medium whitespace-nowrap ${compact ? 'border-transparent! bg-transparent! px-0' : 'px-2'} ${
          isCodex
            ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
            : 'border-indigo-200 bg-indigo-50 text-indigo-700'
        }`}>
          {isCodex ? 'Codex' : 'Claude'}
        </span>
      );
    case 'requestType':
      return (
        <span className={`inline-flex max-w-full items-center justify-center rounded border py-0.5 text-[11px] font-medium whitespace-nowrap ${compact ? 'border-transparent! bg-transparent! px-0' : 'px-2'} ${
          request.isStreaming
            ? 'border-blue-200 bg-blue-50 text-blue-700'
            : 'border-slate-200 bg-slate-50 text-slate-600'
        }`}>
          {request.isStreaming ? '流式' : '非流'}
        </span>
      );
    case 'status':
      return <span className={`inline-flex max-w-full [&>div]:max-w-full [&>div]:gap-1 [&>div]:px-1 [&>div]:text-[11px] ${column.layoutWidth < 58 ? '[&_svg]:hidden' : ''}`}><RequestStatusBadge status={request.status} /></span>;
    case 'model':
      return (
        <ModelTag
          compact
          model={request.model}
          rewriteSource={request.modelRewriteSource || request.model_rewrite_source || ''}
          originalModel={request.originalModelName || request.original_model_name || ''}
          rewriteTarget={request.modelRewriteTarget || request.model_rewrite_target || request.model || ''}
          routeSnapshot={request.routeSnapshot || request.route_snapshot}
        />
      );
    case 'channel':
      return <ChannelCell request={request} compact={column.layoutWidth < 88} />;
    case 'serviceTier': {
      const tier = getRequestServiceTierMeta(request);
      if (!tier) return <span className="text-xs text-slate-400">—</span>;
      return <Tooltip content={tier.description} className="min-w-0 max-w-full">
        <span aria-label={`服务档位：${tier.label}`} className={`block max-w-full truncate rounded border py-0.5 text-[11px] font-medium ${compact ? 'px-1' : 'px-2'} ${tier.className}`}>
          {tier.label}
        </span>
      </Tooltip>;
    }
    case 'endpoint':
      return <RequestRouteCell request={request} compact={column.layoutWidth < 112} onViewDetails={onViewDispatch ? () => onViewDispatch(request) : undefined} />;
    case 'duration':
      return <LatencyCell request={request} compact={column.layoutWidth < 126} />;
    case 'inputTokens':
      return <Tooltip asChild content={String(getDisplayInputTokens(request))}><span className="text-gray-700 font-mono text-[11px]">{tokenText(getDisplayInputTokens(request))}</span></Tooltip>;
    case 'outputTokens':
      return <Tooltip asChild content={String(request.outputTokens ?? '—')}><span className="text-gray-700 font-mono text-[11px]">{tokenText(request.outputTokens)}</span></Tooltip>;
    case 'reasoningEffort':
      return getReasoningDisplay(request.reasoningEffort || request.reasoning_effort, compact);
    case 'cacheCreationTokens':
      return <Tooltip asChild content={String(request.cacheCreationTokens ?? '—')}><span className="text-gray-500 font-mono text-[11px]">{tokenText(request.cacheCreationTokens)}</span></Tooltip>;
    case 'cacheReadTokens':
      return <Tooltip asChild content={String(request.cacheReadTokens ?? '—')}><span className="text-gray-500 font-mono text-[11px]">{tokenText(request.cacheReadTokens)}</span></Tooltip>;
    case 'cost':
      return <Tooltip asChild content={request.cost == null ? undefined : `$${request.cost}`}><span className="font-mono text-orange-500 font-medium text-[11px]">{formatCost(request.cost)}</span></Tooltip>;
    default:
      return null;
  }
};

/**
 * RequestRow - 单行请求数据（支持单击复制、双击查看详情）
 */
const RequestRow = ({ request, columns, rowHeight, onCopyId, onDoubleClick, onViewDispatch }) => {
  const clickTimer = useRef(null);

  useEffect(() => () => clearTimeout(clickTimer.current), []);

  const handleRowClick = () => {
    if (clickTimer.current) {
      clearTimeout(clickTimer.current);
      clickTimer.current = null;
      onDoubleClick?.(request);
    } else {
      clickTimer.current = setTimeout(() => {
        clickTimer.current = null;
        onCopyId?.(request.requestId);
      }, 250);
    }
  };

  return (
    <tbody
      className="group cursor-pointer border-b border-gray-100 transition-colors hover:bg-gray-50/80"
      onClick={handleRowClick}
    >
      <tr style={{ height: rowHeight }}>
        {columns.map((column) => (
          <td key={column.id} className={`min-w-0 px-0.5 ${column.id === 'endpoint' ? 'py-0.5' : 'py-1.5'} text-center align-middle whitespace-nowrap`}>
            <div className="min-w-0 max-w-full overflow-hidden text-ellipsis whitespace-nowrap [&>span]:max-w-full [&>span]:min-w-0">{renderCell(column, request, onViewDispatch)}</div>
          </td>
        ))}
      </tr>
    </tbody>
  );
};

/**
 * RequestsTable - 请求列表表格组件（支持动态列）
 * @param {Object} props
 * @param {Array} props.requests - 请求列表
 * @param {React.ReactNode} props.summary - 请求统计栏
 * @param {boolean} props.loading - 加载状态
 * @param {Object} props.pagination - 分页信息
 * @param {Function} props.onPageChange - 页码变更回调
 * @param {Function} props.onPageSizeChange - 每页条数变更回调
 * @param {Array} props.visibleColumns - 可见列ID数组
 * @param {Array} props.columnConfigs - 列配置数组
 * @param {Function} props.onRowDoubleClick - 双击行回调
 * @param {Function} props.onViewDispatch - 打开当前请求的完整调度详情
 */
const RequestsTable = ({
  summary,
  requests = [],
  loading = false,
  pagination = { page: 1, pageSize: 10, total: 0 },
  onPageChange,
  onPageSizeChange,
  visibleColumns = [],
  columnConfigs = [],
  onRowDoubleClick,
  onViewDispatch
}) => {
  const regionRef = useRef(null);
  const headerRef = useRef(null);
  const [tableWidth, setTableWidth] = useState(900);
  const [bodyHeight, setBodyHeight] = useState(0);

  useEffect(() => {
    const region = regionRef.current;
    const measure = () => {
      setTableWidth(region.clientWidth);
      setBodyHeight(Math.max(0, region.clientHeight - (headerRef.current?.offsetHeight || 0)));
    };
    measure();
    if (typeof ResizeObserver !== 'undefined') {
      const observer = new ResizeObserver(measure);
      observer.observe(region);
      if (headerRef.current) observer.observe(headerRef.current);
      return () => observer.disconnect();
    }
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, []);

  // 复制请求 ID
  const handleCopyId = (id) => {
    navigator.clipboard.writeText(id);
  };

  // 获取可见的列配置
  const visibleColumnConfigs = columnConfigs.filter(col => visibleColumns.includes(col.id));
  const layout = requestColumnLayout(visibleColumnConfigs, tableWidth);
  const rowHeight = requestRowHeight(bodyHeight, 0, pagination.pageSize);
  const showScrollbar = rowHeight * requests.length > bodyHeight;

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm">
      {summary}

      {/* 表格 */}
      <div ref={regionRef} role="region" aria-label="请求明细表格" tabIndex={0} className={`${showScrollbar ? 'scrollbar-visible ' : ''}relative min-h-0 min-w-0 flex-1 overflow-auto focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-indigo-500`}>
        {loading && (
          <div className="absolute inset-0 z-10 bg-white/70 backdrop-blur-[1px] flex items-center justify-center pointer-events-none">
            <LoadingSpinner text="加载请求数据..." />
          </div>
        )}

        <table className={`w-full table-fixed text-sm ${loading ? 'opacity-60' : ''}`}>
          <colgroup>
            {layout.columns.map(col => <col key={col.id} style={{ width: `${col.layoutWidth / layout.tableWidth * 100}%` }} />)}
          </colgroup>
          <thead ref={headerRef} className="sticky top-0 z-10 bg-gray-50 text-gray-500 border-b border-gray-100">
            <tr>
              {layout.columns.map(col => (
                <th
                  key={col.id}
                  className="min-w-0 whitespace-nowrap px-0.5 py-2.5 font-medium text-[11px] text-center align-middle"
                  scope="col"
                >
                  <span className="block truncate">{col.layoutWidth < 58 ? ({ serviceTier: '档位', reasoningEffort: '推理', cacheCreationTokens: '创建', cacheReadTokens: '读取', requestId: '请求 ID', requestType: '类型' }[col.id] || col.label) : col.label}</span>
                </th>
              ))}
            </tr>
          </thead>
          {requests.length === 0 ? (
            <tbody>
              <tr>
                <td colSpan={Math.max(layout.columns.length, 1)} className="px-5 py-12 text-center text-slate-500">
                  暂无请求数据
                </td>
              </tr>
            </tbody>
          ) : (
            requests.map((req) => (
              <RequestRow
                key={req.rawRequestId || req.requestId}
                request={req}
                rowHeight={rowHeight}
                columns={layout.columns}
                onCopyId={handleCopyId}
                onDoubleClick={onRowDoubleClick}
                onViewDispatch={onViewDispatch}
              />
            ))
          )}
        </table>
      </div>

      {/* 分页 */}
      <Pagination
        currentPage={pagination.page}
        pageSize={pagination.pageSize}
        total={pagination.total}
        onPageChange={onPageChange}
        onPageSizeChange={onPageSizeChange}
      />
    </div>
  );
};

export default RequestsTable;
