import { getEndpointChainItems, parseChainNode } from './endpointChain.js';
import { getRequestOutcome, isRequestInProgress } from './requestOutcome.js';

// Evidence belongs to the recorded endpoint/model, never to an entire channel.
export const getRequestCompactionEvidence = (request = {}) => {
  let summary = request.contextSummary ?? request.context_summary;
  if (typeof summary === 'string') {
    try { summary = JSON.parse(summary); } catch { summary = null; }
  }
  const count = summary?.protocol?.compaction_items;
  const compactionItems = Number.isSafeInteger(count) && count >= 0 ? count : null;
  const breakdown = summary?.parts?.summary?.breakdown;
  const hasCompaction = compactionItems > 0
    || compactionItems === null && Array.isArray(breakdown) && breakdown.some(item => item?.kind === 'compaction');
  const hop = getEndpointChainItems(request).map(item => parseChainNode(item))
    .filter(item => !item.local && !item.skipReason && item.statusCode > 0).at(-1);
  const recordedStatus = request.httpStatusCode ?? request.http_status_code;
  const upstream = request.httpStatusIsUpstream ?? request.http_status_is_upstream;
  const httpStatus = hop?.statusCode || (upstream === true && Number.isInteger(recordedStatus) ? recordedStatus : null);
  const outcome = getRequestOutcome(request);
  const streaming = request.isStreaming ?? request.is_streaming;
  const complete = request.status === 'completed' && (outcome === 'completed' || outcome === 'not_streaming'
    || !outcome && streaming === false);
  const evidence = {
    compactionItems, hasCompaction, httpStatus,
    channel: hop?.channel || request.channel || '',
    endpoint: hop?.endpoint || request.endpoint || request.endpoint_name || '',
    model: request.modelRewriteTarget || request.model_rewrite_target || request.model || request.model_name || '',
    state: 'unknown', label: '尚未验证', tone: 'slate',
    description: compactionItems === 0 ? '本次请求未携带原生压缩项，不能验证上游是否支持。'
      : !hasCompaction ? '这条记录没有保存原生压缩项信息，无法判断支持情况。'
        : '本次请求含原生压缩项，但尚无完整成功结果，无法确认支持情况。',
  };
  if (!hasCompaction || isRequestInProgress(request)) return evidence;
  if (complete && httpStatus >= 200 && httpStatus < 300) {
    return { ...evidence, state: 'verified', label: '本次验证通过', tone: 'emerald',
      description: '本次请求携带原生压缩项，上游已成功处理并完整返回结果。' };
  }
  if (httpStatus >= 400) {
    return { ...evidence, state: 'rejected', label: '本次请求未通过', tone: 'amber',
      description: `本次请求含原生压缩项，上游返回 HTTP ${httpStatus}。失败原因需结合上游错误判断，不能直接判定为不支持。` };
  }
  return evidence;
};
