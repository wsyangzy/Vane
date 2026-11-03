import { Activity, Check, ChevronDown, Fingerprint, FolderKey, ListOrdered, Route, Send, Server, Waves, X } from 'lucide-react';
import { affinityOptions, dispatchReason, strategyOptions } from '@/features/providers/shared/dispatch/dispatchState.js';
import { formatDuration } from '@/services/http/api.js';
import ModelTag from './ModelTag.jsx';
import './requestDispatch.css';

const optionLabel = (options, value) => options.find(item => item.value === value)?.label.split(' · ')[0] || value;
const actionLabels = { attempt: '已发送', response_started: '响应中', success: '成功', failed: '失败', skipped: '已跳过' };
const scopeLabels = { candidate: '当前候选', credential: '凭证', endpoint: '端点', model: '同账号模型', request: '当前请求' };
const dateLabel = value => value && Number.isFinite(new Date(value).getTime()) ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '未记录';
const eventMeta = {
  attempt: { title: '发送上游请求', description: '已向选定端点发起本次调用。', icon: Send },
  response_started: { title: '开始返回响应', description: '已开始向客户端返回响应，等待本次调用的最终结果。', icon: Waves },
  success: { title: '调用成功完成', description: '本次上游调用已成功完成。', icon: Check },
  failed: { title: '本次调用失败', description: '本次尝试未能成功完成。', icon: X },
};
const eventReason = reason => ({
  upstream_error: '上游返回错误', upstream_failure: '上游调用失败',
  incomplete_response: '响应未完整结束', response_incomplete: '响应未完整结束',
  network_error: '连接或网络异常', candidate_busy: '候选资源正在使用中',
}[reason] || dispatchReason(reason));

function DispatchEvents({ attempt }) {
  return <details className="request-dispatch-events">
    <summary aria-label={`执行事件 · ${attempt.events.length} 项`}><ListOrdered size={14} /><strong>执行事件</strong><span>{attempt.events.length} 个步骤</span><ChevronDown size={14} /></summary>
    <ol className="request-dispatch-timeline" aria-label="本次尝试的执行事件">{attempt.events.map((event, index) => {
      const meta = eventMeta[event.action] || { title: actionLabels[event.action] || event.action, description: '调度器记录了此执行事件。', icon: Activity };
      const Icon = meta.icon;
      const reason = event.reason ? eventReason(event.reason) : '';
      return <li key={index} data-event={event.action}>
        <span className="request-dispatch-event-icon"><Icon size={13} aria-hidden="true" /></span>
        <div className="request-dispatch-event-content">
          <div className="request-dispatch-event-heading"><strong>{meta.title}</strong><span>步骤 {String(index + 1).padStart(2, '0')}</span></div>
          <p>{meta.description}</p>
          {(event.reason || event.scope) && <dl className="request-dispatch-event-failure">
            {event.reason && <><div><dt>记录原因</dt><dd>{reason === event.reason ? '调度器返回的原因代码见下方。' : reason}</dd></div><div><dt>原因代码</dt><dd><code>{event.reason}</code></dd></div></>}
            {event.scope && <div><dt>影响范围</dt><dd>{scopeLabels[event.scope] || event.scope}</dd></div>}
          </dl>}
        </div>
      </li>;
    })}</ol>
  </details>;
}

function DispatchDiagnostics({ trace }) {
  return <details className="request-dispatch-diagnostics">
    <summary><Fingerprint size={14} /><strong>高级诊断</strong><span>历史身份与完整事件</span><ChevronDown size={14} /></summary>
    <div className="request-dispatch-diagnostics-body">
      <dl className="request-dispatch-identities"><div><dt>提供商</dt><dd>{trace.provider === 'claude' ? 'Claude' : trace.provider === 'codex' ? 'Codex' : '未记录'}</dd></div><div><dt>方案 ID</dt><dd>{trace.profileId}</dd></div><div className="request-dispatch-identity-wide"><dt>请求 ID</dt><dd>{trace.requestId || '未记录'}</dd></div></dl>
      {(trace.diagnostics || []).map((candidate, index) => <section className="request-dispatch-diagnostic-candidate" key={index} aria-label={`候选 ${index + 1} 诊断`}>
        <header><span>{String(index + 1).padStart(2, '0')}</span><strong>{candidate.channel} / {candidate.credentialGroup} / {candidate.endpoint}</strong><small>{candidate.events.length} 项事件</small></header>
        <dl className="request-dispatch-identities">
          <div><dt>渠道 ID</dt><dd>{candidate.channelId || '未记录'}</dd></div><div><dt>分组 ID</dt><dd>{candidate.groupId || '未记录'}</dd></div>
          <div><dt>凭证标识</dt><dd>{candidate.credentialLabel}</dd></div><div><dt>状态版本</dt><dd>{candidate.stateVersion ?? '未记录'}</dd></div>
          <div className="request-dispatch-identity-wide"><dt>端点 ID</dt><dd>{candidate.endpointId || '未记录'}</dd></div>
          <div><dt>目标模型</dt><dd>{candidate.model || '未记录'}</dd></div><div><dt>协议</dt><dd>{({ responses: 'Responses', messages: 'Messages' })[candidate.protocol] || candidate.protocol || '未记录'}</dd></div>
        </dl>
        <ol className="request-dispatch-diagnostic-events" aria-label={`候选 ${index + 1} 完整事件`}>{candidate.events.map((event, at) => <li key={at} data-event={event.action}>
          <span>{String(at + 1).padStart(2, '0')}</span><strong>{actionLabels[event.action] || event.action || '未知事件'}</strong>
          <div>{event.reason && <><span>{eventReason(event.reason)}</span><code>{event.reason}</code></>}{event.scope && <span>影响范围：{scopeLabels[event.scope] || event.scope}</span>}{!event.reason && !event.scope && <span>{eventMeta[event.action]?.description || '调度器记录了此执行事件。'}</span>}</div>
        </li>)}</ol>
      </section>)}
      {!trace.diagnostics?.length && <p className="request-dispatch-empty">此记录未保存候选身份与执行事件。</p>}
    </div>
  </details>;
}

export default function RequestDispatchDetails({ trace, expanded = false }) {
  if (!trace) return null;
  const pending = !trace.finished && !trace.successful && !trace.stopReason;
  const result = trace.successful ? '调用成功' : pending ? '处理中' : !trace.attemptCount ? '未发送' : trace.responseStarted ? '响应未完成' : '调用失败';
  return <section className="request-dispatch-details" data-expanded={expanded} aria-label="请求调度详情">
    <header className="request-dispatch-heading"><Route size={15} /><strong>{trace.profileName}</strong><span>{trace.attemptCount} 次上游尝试</span></header>
    <div className="request-dispatch-meta">
      <span>{trace.decisionLabel}</span>
      {trace.strategy && <span>{optionLabel(strategyOptions, trace.strategy)}</span>}
      {trace.affinityMode && <span>{optionLabel(affinityOptions, trace.affinityMode)}</span>}
      {trace.decision.rule_id > 0 && <span>规则 #{trace.decision.rule_id}</span>}
      {trace.decision.intent_id && <span>意图 {trace.decision.intent_id}</span>}
      {trace.decision.reused_turn && <span>复用本轮决策</span>}
      {trace.waitMs > 0 && <span>等待 {formatDuration(trace.waitMs)}</span>}
    </div>
    {expanded && <>
      <dl className="request-dispatch-summary"><div><dt>调度结果</dt><dd>{result}</dd></div><div><dt>上游尝试</dt><dd>{trace.attemptCount} 次</dd></div><div><dt>候选跳过</dt><dd>{trace.skipped.length} 条</dd></div><div><dt>等待恢复</dt><dd>{formatDuration(trace.waitMs)}</dd></div></dl>
      <h4 className="request-dispatch-section-title">实际发送路径</h4>
    </>}
    {!!trace.attempts.length && <ol className="request-dispatch-attempts" aria-label="实际调用路径">{trace.attempts.map((attempt, index) => <li key={`${attempt.key}/${index}`} data-result={attempt.action}>
      <span className="request-dispatch-sequence" aria-label={`第 ${index + 1} 次尝试`}>{String(index + 1).padStart(2, '0')}</span>
      <div className="request-dispatch-resource">
        {expanded ? <div className="request-dispatch-resource-info">
          <div className="request-dispatch-resource-header" tabIndex={0} aria-label="渠道、分组和端点">
            <div className="request-dispatch-channel"><span className="request-dispatch-channel-icon"><Route size={13} aria-hidden="true" /></span><strong>{attempt.channel || '渠道未记录'}</strong></div>
            <dl className="request-dispatch-targets">
              <div className="request-dispatch-target" data-kind="group"><dt><FolderKey size={12} aria-hidden="true" />分组</dt><dd>{attempt.credentialGroup || '未记录'}</dd></div>
              <div className="request-dispatch-target" data-kind="endpoint"><dt><Server size={12} aria-hidden="true" />端点</dt><dd>{attempt.endpoint || '未记录'}</dd></div>
            </dl>
          </div>
          <div className="request-dispatch-technical">
            <ModelTag model={attempt.model} compact className="h-[22px]!" title={`模型：${attempt.model || '未记录'}`} />
            <span className="request-dispatch-protocol"><span>协议</span><code>{({ responses: 'Responses', messages: 'Messages' })[attempt.protocol] || attempt.protocol || '未记录'}</code></span>
          </div>
        </div> : <><strong>{attempt.channel}</strong><span>{attempt.credentialGroup} / {attempt.endpoint}</span></>}
        {attempt.reason && <small>{eventReason(attempt.reason)}{attempt.scope && ` · 作用域：${scopeLabels[attempt.scope] || attempt.scope}`}</small>}
        {expanded && attempt.events?.length > 0 && <DispatchEvents attempt={attempt} />}
      </div>
      <div className="request-dispatch-result"><span>{!expanded && (attempt.action === 'success' ? <Check size={12} aria-hidden="true" /> : attempt.action === 'failed' ? <X size={12} aria-hidden="true" /> : <Activity size={12} aria-hidden="true" />)}{actionLabels[attempt.action] || '处理中'}</span>{attempt.durationMs != null && <small>{formatDuration(attempt.durationMs)}</small>}{attempt.statusCode > 0 && <small>HTTP {attempt.statusCode}</small>}</div>
    </li>)}</ol>}
    {!trace.attempts.length && <p className="request-dispatch-empty">{trace.attemptCount ? '此记录未保存逐次候选事件。' : trace.finished || trace.stopReason ? '未向上游发送请求。' : '等待调度选择上游。'}</p>}
    {trace.stopReason && <p className="request-dispatch-stop">结束原因：{dispatchReason(trace.stopReason)}</p>}
    {!!trace.skipped.length && <details className="request-dispatch-skipped"><summary>{trace.skipped.length} 条候选跳过记录</summary><ul>{trace.skipped.map((item, index) => <li key={index}><span>{item.channel} / {item.credentialGroup} / {item.endpoint}</span><small>{item.reason ? eventReason(item.reason) : '未发起上游调用'}</small></li>)}</ul></details>}
    {expanded && <DispatchDiagnostics trace={trace} />}
    <div className="request-dispatch-footnote">{expanded && <><p>记录范围：本次 {trace.provider === 'claude' ? 'Claude' : trace.provider === 'codex' ? 'Codex' : ''} 请求的调度过程。</p><p>开始：{dateLabel(trace.createdAt)} · 结束：{trace.finishedAt ? dateLabel(trace.finishedAt) : pending ? '进行中' : '未记录'}</p></>}<p>方案与决策取自请求当时的记录{expanded ? '；资源名称优先使用历史链路，其余按当前配置解析。' : '。'}</p></div>
  </section>;
}
