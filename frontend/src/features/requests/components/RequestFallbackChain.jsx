import { memo, useMemo, useState } from 'react';
import { ArrowRight, Check, ChevronDown, CircleHelp, Clock3, GitBranch, MessageSquare, Send, Server, X } from 'lucide-react';
import { formatCost, formatDuration } from '@/services/http/api.js';
import { Tooltip } from '@/shared/ui/index.js';
import { buildFallbackAttempts, summarizeTavilyUsage } from '../utils/requestInsights.js';
import { describeFailureReason } from '../utils/failureSummary.js';
import './requestInsights.css';

const states = {
  answered: ['已响应', Check], failed: ['失败', X], trying: ['请求中', Send],
  skipped: ['未发送', ArrowRight], cancelled: ['已取消', X],
  incomplete: ['响应未完整结束', X], unknown: ['结果未记录', CircleHelp],
};

function Branches({ attempts, outgoing }) {
  const height = attempts.length * 72 - 8;
  const center = height / 2;
  return <div className={`request-fallback-lines ${outgoing ? 'request-fallback-outgoing' : ''}`} aria-hidden="true">
    <svg viewBox={`0 0 52 ${height}`} preserveAspectRatio="none">
    {attempts.map((item, index) => {
      const y = index * 72 + 32;
      const active = outgoing ? item.state === 'answered' : !['skipped', 'unknown'].includes(item.state);
      const path = outgoing ? `M0 ${y} C26 ${y} 26 ${center} 52 ${center}` : `M0 ${center} C26 ${center} 26 ${y} 52 ${y}`;
      return <g key={item.id} data-state={active ? item.state : 'skipped'} style={{ '--insight-step': Math.min(index, 5) }}>
        <path className="request-fallback-wire" d={path} />
        {active && index < 6 && <path className="request-fallback-signal" d={path} pathLength="1" />}
      </g>;
    })}
    </svg>
  </div>;
}

function ChainEnd({ label, detail, Icon, state }) {
  return <Tooltip asChild content={[label, detail].filter(Boolean).join(' · ')}>
    <div className="request-fallback-end" data-state={state} tabIndex={0}>
      <span className="request-fallback-icon"><Icon size={14} /></span>
      <div><strong>{label}</strong><small>{detail}</small></div>
    </div>
  </Tooltip>;
}

export default memo(function RequestFallbackChain({ request, animate = false }) {
  const attempts = useMemo(() => buildFallbackAttempts(request), [request]);
  const tavilyUsage = summarizeTavilyUsage(attempts);
  const longest = attempts.reduce((max, item) => Math.max(max, item.duration || 0), 0);
  const [expanded, setExpanded] = useState(false);
  const [interacted, setInteracted] = useState(false);
  const [activeReason, setActiveReason] = useState(null);
  const answered = attempts.find(item => item.state === 'answered');
  const trying = attempts.find(item => item.state === 'trying');
  const visible = expanded || attempts.length <= 5 ? attempts : [...attempts.slice(0, 4), attempts.at(-1)];
  const reason = visible.find(item => item.id === activeReason)?.reason;
  const sends = attempts.filter(item => item.state !== 'skipped').length;
  const result = answered ? '已响应' : trying ? '请求中' : sends ? '未完成响应'
    : attempts.length || request.dispatchTrace?.attemptCount === 0 ? '未向上游发送' : '无链路记录';
  const originalModel = request.dispatchTrace?.requestedModel || request.originalModelName || request.model || request.path;
  return <section className="request-insight" aria-label="故障转移链">
    <header className="request-insight-heading request-fallback-heading">
      <GitBranch size={15} /><h3>故障转移链</h3>
      <div className="request-fallback-summary" role="status">
        <span data-outcome={answered ? 'answered' : trying ? 'trying' : sends ? 'failed' : 'skipped'}><i />{result}</span>
        {attempts.length > 0 && <span>{sends ? `${sends} 项上游尝试` : `${attempts.length} 个跳过节点`}</span>}
        {tavilyUsage.calls > 0 && <span>Tavily {tavilyUsage.calls} 次 · {tavilyUsage.reportedCalls === tavilyUsage.calls
          ? `${tavilyUsage.credits} credits`
          : tavilyUsage.reportedCalls > 0
            ? `已回报 ${tavilyUsage.credits} credits / ${tavilyUsage.reportedCalls} 次`
            : 'credits 未记录'}</span>}
        {request.duration != null && <span><Clock3 size={12} /><span className="sr-only">总用时</span>{formatDuration(request.duration)}</span>}
        {request.cost != null && request.billingRuleSnapshot?.pricing_fallback !== 'missing_price' && <span><span className="sr-only">总成本</span>{formatCost(request.cost)}</span>}
      </div>
    </header>
    {!attempts.length ? <p className="request-insight-empty">此请求未记录上游尝试。</p> : <>
      <div className="request-fallback-canvas" data-enter={animate && !interacted} style={{ '--insight-last-step': Math.min(visible.length - 1, 5) }}>
        <ChainEnd label="请求" detail={originalModel} Icon={Send} state="request" />
        <Branches attempts={visible} />
        <ol className="request-fallback-attempts" aria-label="上游尝试顺序">{visible.map((item, index) => {
          const [label, Icon] = states[item.state];
          const hop = Number(item.id) + 1;
          const resource = [item.channel, item.group, item.endpoint].filter(Boolean).join(' / ');
          const usage = item.isTavily ? item.usageCredits == null ? 'credits 未记录' : `${item.usageCredits} credits` : '';
          const status = [label, item.statusCode && `HTTP ${item.statusCode}`, item.attempts > 1 && `${item.attempts} 次`, usage].filter(Boolean).join(' · ');
          const description = item.reason && (describeFailureReason(item.reason) || item.reason);
          return <li key={item.id} data-state={item.state} style={{ '--insight-step': Math.min(index, 5) }}>
            <Tooltip asChild content={<div className="text-left [overflow-wrap:anywhere]"><div>{item.model || '模型未记录'}</div><div>{resource}</div><div>{status}</div>{description && <div>{description}</div>}</div>}>
              <div className="request-fallback-node" tabIndex={0}>
                <div className="request-fallback-icon"><Server size={14} /><span className="request-fallback-number">{hop}</span></div>
                <div className="request-fallback-content">
                  <div><strong>{item.model || item.endpoint}</strong><small>{resource}</small></div>
                  <span><Icon size={12} />{status}</span>
                </div>
                <span className="request-fallback-duration">{item.duration == null ? '–' : formatDuration(item.duration)}</span>
              </div>
            </Tooltip>
            {item.reason && <button type="button" className="request-fallback-reason" title="查看记录原因" aria-label={`查看第 ${hop} 跳原因`} aria-expanded={activeReason === item.id} onClick={() => setActiveReason(value => value === item.id ? null : item.id)}><ChevronDown size={14} /></button>}
            <div className="request-fallback-meter" aria-hidden="true">
              <i style={{ width: longest && item.duration != null ? `${item.duration / longest * 100}%` : '0%' }} />
            </div>
          </li>;
        })}</ol>
        <Branches attempts={visible} outgoing />
        <ChainEnd label="响应" detail={answered?.endpoint || (trying ? '等待响应' : '无完整响应')} Icon={MessageSquare} state={answered ? 'answered' : 'waiting'} />
      </div>
      {reason && <p className="request-fallback-description">{describeFailureReason(reason) || reason}</p>}
      {attempts.length > 5 && <button type="button" className="request-insight-more" onClick={() => {
        setInteracted(true);
        setExpanded(value => !value);
      }} aria-expanded={expanded}><ChevronDown size={13} />{expanded ? '收起节点' : `查看全部 ${attempts.length} 个节点`}</button>}
    </>}
  </section>;
});
