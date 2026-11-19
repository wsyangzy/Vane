import { memo, useMemo, useState } from 'react';
import { ArrowDownLeft, ArrowUpRight, ChartNoAxesCombined, ChevronDown, Database, Layers, PanelsTopLeft, Server, TriangleAlert } from 'lucide-react';
import { getEndpointChainItems } from '../utils/endpointChain.js';
import { isRequestInProgress } from '../utils/requestOutcome.js';
import { accumulateWaterfall, buildContextCostEstimate, buildCostSteps, buildFallbackAttempts, formatRequestInsightCost, hasReportedRequestUsage } from '../utils/requestInsights.js';
import { getServiceTierMeta } from '../utils/requestMetadata.js';
import { revealInsight } from './revealInsight.js';
import AnimatedMetric from './AnimatedMetric.jsx';
import './requestInsights.css';

const compact = new Intl.NumberFormat('zh-CN', { notation: 'compact', maximumFractionDigits: 1 });
const attemptStates = {
  answered: '已响应', failed: '失败', trying: '进行中', skipped: '未发送',
  cancelled: '已取消', incomplete: '响应未完成', unknown: '结果未记录',
};

function WaterfallMetrics({ input, output, cost, time, animate }) {
  return <>
    <td><AnimatedMetric value={input} live={animate} /></td>
    <td><AnimatedMetric value={output} live={animate} /></td>
    <td><AnimatedMetric value={cost} live={animate} format="cost" /></td>
    <td><AnimatedMetric value={time} live={animate} format="duration" /></td>
  </>;
}

function WaterfallTrack({ row, scale }) {
  return <td className="request-waterfall-plot">
    <div className="request-waterfall-track" aria-hidden="true">
      {row && row.value !== 0 && <i data-negative={row.value < 0} style={{
        left: `${Math.min(row.start, row.end) / scale * 100}%`,
        width: `${Math.abs(row.value) / scale * 100}%`,
      }} />}
    </div>
  </td>;
}

function WaterfallGroup({ label, detail, Icon, open, toggle, metrics, row, scale, animate }) {
  return <tr className="request-waterfall-group">
    <th scope="row">
      <button type="button" aria-expanded={open} onClick={toggle}>
        <ChevronDown size={13} className="request-waterfall-chevron" aria-hidden="true" />
        <Icon size={14} aria-hidden="true" />
        <span className="request-waterfall-name"><strong>{label}</strong>{detail && <small>{detail}</small>}</span>
      </button>
    </th>
    <WaterfallTrack row={row} scale={scale} />
    <WaterfallMetrics {...metrics} animate={animate} />
  </tr>;
}

function ContextCostValues({ row }) {
  return <>
    <span>{row.tokens.toLocaleString()} Token</span>
    <strong title={`约 $${row.cost}`}>≈ {formatRequestInsightCost(row.cost)}</strong>
  </>;
}

function ContextCostEstimate({ estimate }) {
  return <details className="request-context-cost request-context-contents">
    <summary className="request-context-cost-line">
      <span><ChevronDown size={13} aria-hidden="true" /><PanelsTopLeft size={14} aria-hidden="true" />上下文成本估算</span>
      <strong>{formatRequestInsightCost(estimate.cost)}</strong>
    </summary>
    <p>将输入、缓存读取及写入的成本合计按上下文 Token 占比分摊。各条目的缓存命中情况未记录。</p>
    <ul>
      {estimate.rows.map(row => <li key={row.id} data-tone={row.tone}>
        {row.entries?.length ? <details>
          <summary className="request-context-cost-line">
            <span><ChevronDown size={12} aria-hidden="true" /><i aria-hidden="true" />{row.label}</span>
            <ContextCostValues row={row} />
          </summary>
          <ul>
            {row.entries.map(entry => <li key={entry.id} className="request-context-cost-line">
              <span>{entry.label}{entry.items > 1 && <small>×{entry.items}</small>}</span>
              <ContextCostValues row={entry} />
            </li>)}
          </ul>
        </details> : <div className="request-context-cost-line">
          <span><i aria-hidden="true" />{row.label}</span>
          <ContextCostValues row={row} />
        </div>}
      </li>)}
    </ul>
  </details>;
}

export default memo(function RequestCostWaterfall({ request }) {
  const data = useMemo(() => buildCostSteps(request), [request]);
  const estimate = useMemo(() => buildContextCostEstimate(request, data), [request, data]);
  const attempts = useMemo(() => request.dispatchTrace?.attempts?.length || getEndpointChainItems(request).length
    ? buildFallbackAttempts(request).filter(row => row.state !== 'skipped')
    : [], [request]);
  const live = isRequestInProgress(request);
  const waitingForUsage = live && !hasReportedRequestUsage(request);
  const unpriced = data.unpriced || (live && !waitingForUsage && request.billingRuleSnapshot?.input_price == null);
  const [requestedMode, setMode] = useState('cost');
  const mode = unpriced && !waitingForUsage ? 'tokens' : requestedMode;
  const [showBilling, setShowBilling] = useState(true);
  const [showAttempts, setShowAttempts] = useState(false);
  const { steps, scale } = useMemo(() => accumulateWaterfall(data.rows, mode), [data.rows, mode]);
  const highest = data.rows.reduce((a, b) => !b.residual && b.cost > (a?.cost || 0) ? b : a, null);
  const metrics = { input: waitingForUsage ? null : data.input, output: waitingForUsage ? null : data.output, cost: waitingForUsage || unpriced ? null : data.total };
  const axisScale = steps.some(row => row.value !== 0) ? scale : 0;
  const axisLabel = value => mode === 'cost' ? formatRequestInsightCost(value) : compact.format(Math.round(value));
  const total = mode === 'cost' ? data.total : data.tokens;
  const totalBar = axisScale > 0 ? { start: 0, end: total, value: total } : null;
  const attemptCount = request.dispatchTrace?.attemptCount
    || (attempts.every(row => row.attempts > 0) ? attempts.reduce((sum, row) => sum + row.attempts, 0) : 0);
  const snapshot = request.billingRuleSnapshot;
  const pricingDetail = [snapshot?.pricing_model,
    snapshot?.pricing_model && getServiceTierMeta(snapshot.pricing_service_tier || 'standard')?.label,
    snapshot?.context_mode === 'long' && '长上下文单价',
  ].filter(Boolean).join(' · ');

  return <section className="request-insight" aria-label="成本瀑布图">
    <header className="request-insight-heading">
      <ChartNoAxesCombined size={15} aria-hidden="true" /><h3>成本瀑布</h3>
      {live && <span className="request-insight-live" role="status">{waitingForUsage ? (request.firstByteMs == null ? '等待上游用量' : '接收响应 · 等待用量') : '已收到用量'}</span>}
      {request.model && <span className="request-waterfall-model" title={request.model}>{request.model}</span>}
      <div className="request-insight-toggle" role="group" aria-label="瀑布图指标">
        <button type="button" disabled={unpriced && !waitingForUsage} aria-pressed={mode === 'cost'} onClick={() => setMode('cost')}>成本</button>
        <button type="button" aria-pressed={mode === 'tokens'} onClick={() => setMode('tokens')}>Token</button>
      </div>
    </header>
    <dl className="request-cost-summary">
      <div><dt>{live ? '当前估算成本' : '请求总成本'}</dt><dd>{waitingForUsage ? '—' : unpriced ? '未定价' : <AnimatedMetric value={data.total} live={live} format="cost" />}</dd></div>
      <div><dt>计费 Token</dt><dd><AnimatedMetric value={waitingForUsage ? null : data.tokens} live={live} />{!waitingForUsage && <small><AnimatedMetric value={data.input} live={live} /> 输入 · <AnimatedMetric value={data.output} live={live} /> 输出</small>}</dd></div>
      <div><dt>最高成本分项</dt><dd>{highest?.label || '—'}{highest && <small><AnimatedMetric value={highest.cost} live={live} format="cost" /></small>}</dd></div>
    </dl>
    {!steps.length && !attempts.length
      ? <p className="request-insight-empty">{waitingForUsage ? '收到上游用量后显示计费分项。' : '此请求暂无计费或执行记录。'}</p>
      : <div className="request-waterfall-scroll" role="region" aria-label="成本瀑布明细" tabIndex={0}>
        <table key={mode} data-live={live} className="request-waterfall" aria-label={mode === 'cost' ? '累计成本分解' : '累计计费 Token 分解'}>
          <colgroup><col className="request-waterfall-step-col" /><col /><col className="request-waterfall-number-col" span={2} /><col className="request-waterfall-cost-col" /><col className="request-waterfall-number-col" /></colgroup>
          <thead ref={revealInsight}><tr>
            <th scope="col">步骤</th>
            <th scope="col" className="request-waterfall-plot"><span className="sr-only">{mode === 'cost' ? '累计成本' : '累计计费 Token'}</span>
              <span className="request-waterfall-axis" aria-hidden="true">
                <span>{axisLabel(0)}</span>{axisScale >= (mode === 'cost' ? .000002 : 2) && <span className="request-waterfall-midpoint">{axisLabel(axisScale / 2)}</span>}
                {axisScale > 0 && <span>{axisLabel(axisScale)}</span>}
              </span>
            </th>
            <th scope="col">输入</th><th scope="col">输出</th><th scope="col">成本</th><th scope="col">耗时</th>
          </tr></thead>
          {steps.length > 0 && <tbody>
            <WaterfallGroup label="计费分项" detail={pricingDetail} Icon={Layers} open={showBilling}
              toggle={() => setShowBilling(value => !value)} metrics={metrics} row={showBilling ? null : totalBar} scale={scale} animate={live} />
            {showBilling && steps.map((row, index) => <tr className="request-waterfall-child" data-tone={row.tone} key={row.id}
              style={{ '--insight-step': index }}>
              <th scope="row"><span className="request-waterfall-label">
                {row.residual ? <TriangleAlert size={13} aria-hidden="true" /> : row.output != null ? <ArrowUpRight size={13} aria-hidden="true" /> : row.id === 'input' ? <ArrowDownLeft size={13} aria-hidden="true" /> : <Database size={13} aria-hidden="true" />}
                <span className="request-waterfall-name"><strong>{row.label}</strong>
                  {row.detail && <small>{row.detail}</small>}
                  {!unpriced && row.pricing && <small className="request-waterfall-pricing">
                    <span>{formatRequestInsightCost(row.pricing.unit)} / 百万 Token</span>
                    {row.pricing.multiplier != null && <span>分项 ×{row.pricing.multiplier}</span>}
                    {row.pricing.totalMultiplier != null && <span>总倍率 ×{row.pricing.totalMultiplier}</span>}
                  </small>}
                </span>
              </span></th>
              <WaterfallTrack row={row} scale={scale} />
              <WaterfallMetrics input={row.input} output={row.output} cost={unpriced ? null : row.cost} animate={live} />
            </tr>)}
          </tbody>}
          {attempts.length > 0 && <tbody>
            <WaterfallGroup label="上游尝试" detail={attemptCount ? `${attemptCount} 次` : `${attempts.length} 个链路节点`} Icon={Server} open={showAttempts}
              toggle={() => setShowAttempts(value => !value)} animate={live} />
            {showAttempts && attempts.map((row, index) => <tr className="request-waterfall-child" key={row.id} data-outcome={row.state}>
              <th scope="row"><span className="request-waterfall-label">
                <Server size={13} aria-hidden="true" />
                <span className="request-waterfall-name">
                  <strong>{row.attempts > 1 ? '合并尝试' : `节点 ${index + 1}`} · {attemptStates[row.state]}{row.statusCode ? ` · HTTP ${row.statusCode}` : ''}</strong>
                  <small>{[row.channel, row.group, row.endpoint, row.model, row.attempts > 1 ? `${row.attempts} 次` : null].filter(Boolean).join(' / ')}</small>
                </span>
              </span></th>
              <WaterfallTrack />
              <WaterfallMetrics time={row.duration} animate={live} />
            </tr>)}
          </tbody>}
          <tfoot><tr className="request-waterfall-total">
            <th scope="row">{live ? '当前累计' : '请求合计'}</th>
            <WaterfallTrack row={totalBar} scale={scale} />
            <WaterfallMetrics {...metrics} time={live ? null : request.duration} animate={live} />
          </tr></tfoot>
        </table>
      </div>}
    {estimate && !unpriced && !waitingForUsage && <ContextCostEstimate estimate={estimate} />}
    {unpriced && !waitingForUsage && <p className="request-insight-note">此请求未匹配模型价格，成本尚未计算。</p>}
    {data.rows.some(row => row.residual) && <p className="request-insight-note">历史成本差额为记录总价与已知分项的差值。</p>}
  </section>;
});
