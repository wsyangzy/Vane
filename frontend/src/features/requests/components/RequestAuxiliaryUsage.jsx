import { memo, useMemo } from 'react';
import { Calculator, Search, Server } from 'lucide-react';
import { formatDuration } from '@/services/http/api.js';
import { getAuxiliaryRequestMeta } from '../utils/requestMetadata.js';
import { isRequestInProgress } from '../utils/requestOutcome.js';
import { buildFallbackAttempts, summarizeTavilyUsage } from '../utils/requestInsights.js';
import AnimatedMetric from './AnimatedMetric.jsx';
import './requestInsights.css';

const icons = { search: Search, token_count: Calculator, model_catalog: Server };
const stateLabels = {
  answered: '已响应', failed: '失败', trying: '进行中', skipped: '未发送',
  cancelled: '已取消', incomplete: '响应未完整结束', unknown: '结果未记录',
};

export default memo(function RequestAuxiliaryUsage({ request, meta = getAuxiliaryRequestMeta(request) }) {
  const attempts = useMemo(() => buildFallbackAttempts(request).filter(item => item.state !== 'skipped'), [request]);
  const live = isRequestInProgress(request);
  const tavily = summarizeTavilyUsage(attempts);
  const hasTavilyUsage = attempts.some(item => item.isTavily);
  const Icon = icons[meta?.kind] || Server;
  const creditsLabel = tavily.calls === 0 ? '—'
    : tavily.reportedCalls === tavily.calls ? null
      : tavily.reportedCalls > 0 ? `${tavily.reportedCalls} / ${tavily.calls} 次已回报` : '未记录';
  const requestDuration = request.duration == null ? null : Number(request.duration);

  return <section className="request-insight request-auxiliary-usage" aria-label="辅助请求用量">
    <header className="request-insight-heading">
      <Icon size={15} aria-hidden="true" />
      <h3>{meta?.label || '辅助请求'}用量</h3>
      {live && <span className="request-insight-live" role="status">请求进行中</span>}
      <code title={`${request.method || ''} ${request.path || ''}`}>{request.method} {request.path}</code>
    </header>
    <dl className="request-auxiliary-summary" data-with-credits={hasTavilyUsage}>
      <div><dt>上游调用</dt><dd><AnimatedMetric value={attempts.length} live={live} /></dd></div>
      {hasTavilyUsage && <div>
        <dt>Tavily credits</dt>
        <dd>{creditsLabel ?? <AnimatedMetric value={tavily.credits} live={live} />}</dd>
      </div>}
      <div><dt>请求耗时</dt><dd>{requestDuration == null ? '—' : formatDuration(requestDuration)}</dd></div>
    </dl>
    {attempts.length > 0 ? <div className="request-auxiliary-table-scroll">
      <table className="request-auxiliary-usage-table" data-with-credits={hasTavilyUsage}>
        <thead><tr><th scope="col">上游服务</th><th scope="col">结果</th>{hasTavilyUsage && <th scope="col">credits</th>}<th scope="col">耗时</th></tr></thead>
        <tbody>{attempts.map(item => <tr key={item.id} data-outcome={item.state}>
          <th scope="row"><strong>{item.endpoint}</strong><small>{item.channel || '服务端点'}</small></th>
          <td>{stateLabels[item.state]}{item.statusCode ? ` · HTTP ${item.statusCode}` : ''}
            {item.state === 'failed' && item.reason && <small className="mt-0.5 block max-w-64 truncate text-slate-500" title={item.reason}>{item.reason}</small>}
          </td>
          {hasTavilyUsage && <td>{item.usageCredits == null
            ? item.isTavily ? '未记录' : '—'
            : <AnimatedMetric value={item.usageCredits} live={live} />}</td>}
          <td>{item.duration == null ? '—' : formatDuration(item.duration)}</td>
        </tr>)}</tbody>
      </table>
    </div> : <p className="request-insight-empty">
      {live ? '等待辅助接口调用记录。' : '此辅助请求没有记录上游调用。'}
    </p>}
  </section>;
});
