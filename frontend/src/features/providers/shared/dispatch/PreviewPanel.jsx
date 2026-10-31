import { useRef, useState } from 'react';
import { ChevronDown, CircleHelp, FlaskConical, GitBranch, Play, ShieldCheck, Sparkles, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { classifyIntent, previewDispatch } from '@/services/wails/dispatch.js';
import { classifierInputNotice, dispatchReason, ruleKindLabel, stateCompatibilityLabel } from './dispatchState.js';
import { formatUsd } from './manualBalanceState.js';
import './profileDetails.css';

const control = 'dp-input';
const loadNumber = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 1 });

function Candidates({ entries, channels, accounts }) {
  if (!entries.length) return <p className="dp-empty">没有可展示的候选，请检查方案成员和凭证资源。</p>;
  return <ol className="dp-candidates" aria-label="候选排序">{entries.map((entry, index) => {
    const candidate = entry.candidate;
    const quota = candidate.balance?.enabled ? formatUsd(candidate.balance.balanceUsd) : entry.quotaKnown && entry.usedPercent != null ? `${entry.usedPercent.toFixed(1)}% 已用` : candidate.credentialMode === 'managed' ? '未知' : '未提供额度';
    return <li key={`${candidate.provider}/${candidate.channelId}/${candidate.groupId}/${candidate.endpointId}/${candidate.upstreamModel}/${index}`}>
      <div className="dp-candidate-heading"><span className="dp-channel-order">{String(index + 1).padStart(2, '0')}</span><div><strong>{channels.find((channel) => channel.id === candidate.channelId)?.name || `渠道 ${candidate.channelId}`} / {accounts.find((account) => account.channelId === candidate.channelId && account.groupId === candidate.groupId)?.groupName || `分组 ${candidate.groupId}`}</strong><p>{candidate.upstreamModel || candidate.model || '沿用请求模型'} · {candidate.endpointId || '无端点'} · {candidate.protocol}</p></div><span className="dp-badge" data-tone={entry.eligible ? 'success' : 'warning'}>{entry.eligible ? '可参与' : '跳过'}</span></div>
      <dl className="dp-candidate-facts"><div><dt>额度 / 余额</dt><dd>{quota}{candidate.balance?.enabled && <small>本地估算</small>}</dd></div><div><dt>Vane 近期负载</dt><dd>{entry.load.concurrent}{entry.load.maxConcurrent ? ` / ${entry.load.maxConcurrent}` : ''} 并发<small>{loadNumber.format(entry.load.decayedTokens)} tokens · {loadNumber.format(entry.load.decayedRequests)} 次请求</small></dd></div><div><dt>资格与状态</dt><dd>{dispatchReason(entry.reason)}<small>{stateCompatibilityLabel(entry.stateCompatibility)}</small>{candidate.health.reason && <small className="text-amber-600">{dispatchReason(candidate.health.reason)}</small>}</dd></div></dl>
    </li>;
  })}</ol>;
}

export default function PreviewPanel({ provider, selected, channels, accounts, busy, perform }) {
  const [input, setInput] = useState({ model: '', streaming: true, conversationId: '', branchId: '', turnId: '', newTurn: false, text: '', previousUserMessage: '', previousAssistantText: '' });
  const [preview, setPreview] = useState(null);
  const [classification, setClassification] = useState(null);
  const previewButton = useRef(null);
  const classificationButton = useRef(null);
  const previewRevision = useRef(0);
  const classificationRevision = useRef(0);
  const update = (patch) => {
    setInput((previous) => ({ ...previous, ...patch }));
    if (Object.keys(patch).some((key) => ['model', 'streaming', 'conversationId', 'branchId', 'turnId', 'newTurn'].includes(key))) {
      previewRevision.current += 1;
      setPreview(null);
    }
    classificationRevision.current += 1;
    setClassification(null);
  };
  const params = { ...input, provider, profileId: selected.id };
  const runPreview = () => perform(async () => {
    const revision = previewRevision.current;
    const result = await previewDispatch(params);
    if (revision === previewRevision.current) setPreview(result);
  });
  const runClassification = () => perform(async () => {
    const revision = classificationRevision.current;
    const result = await classifyIntent(params);
    if (revision === classificationRevision.current) setClassification(result);
  });

  return <section className="dp-card" aria-label="模拟调度">
    <header className="dp-card-header"><div className="dp-heading"><FlaskConical size={15} /><h3>模拟调度</h3></div><span className="dp-badge">只读预览</span></header>
    <div className="dp-preview-body">
      <p className="dp-description">使用已保存的方案，不发送业务请求、不占用候选或推进轮询。</p>
      <div className="dp-preview-request"><label className="dp-field">请求模型<input className={control} placeholder={provider === 'codex' ? '例如 gpt-5' : '例如 claude-sonnet-4-5'} value={input.model} onChange={(event) => update({ model: event.target.value })} /></label><div className="dp-preview-actions"><label className="dp-check"><input type="checkbox" checked={input.streaming} onChange={(event) => update({ streaming: event.target.checked })} />流式请求</label><Button ref={previewButton} type="button" size="sm" variant="secondary" className="dp-action" icon={Play} disabled={busy || !input.model.trim()} onClick={runPreview}>预览候选</Button></div></div>

      <details className="dp-disclosure dp-request-context"><summary><GitBranch size={13} /><span>会话与轮次</span><small>可选</small><ChevronDown size={13} className="dp-chevron" /></summary><div className="dp-context-fields">{[['conversationId', '会话 ID'], ['turnId', '轮次 ID'], ['branchId', '分支 ID']].map(([key, label]) => <label key={key} className="dp-field">{label}<input className={control} value={input[key]} onChange={(event) => update({ [key]: event.target.value })} /></label>)}<label className="dp-check dp-new-turn"><input type="checkbox" checked={input.newTurn} onChange={(event) => update({ newTurn: event.target.checked })} />开始新轮次</label></div></details>

      {preview && <div className="dp-preview-result"><div className="dp-section-heading"><h4>候选预览结果</h4><div className="dp-result-actions"><span>{preview.candidates.filter((entry) => entry.eligible).length} / {preview.candidates.length} 个可参与</span><button type="button" className="dp-result-clear" aria-label="清除候选结果" disabled={busy} onClick={() => { previewRevision.current += 1; setPreview(null); previewButton.current?.focus(); }}><X size={12} />清除结果</button></div></div><p className="dp-result-decision">决策：{ruleKindLabel(preview.decision.kind || 'default')}{preview.decision.ruleId ? ` · 规则 ${preview.decision.ruleId}` : ''}{preview.decision.intentId ? ` · 意图 ${preview.decision.intentId}` : ''}{preview.decision.fallback ? ' · 已回退' : ''}{preview.stopReason ? ` · ${dispatchReason(preview.stopReason)}` : ''}</p><Candidates entries={preview.candidates} channels={channels} accounts={accounts} /><p className="dp-footnote">Vane 近期负载来自本地请求观测，tokens 和请求数按 1 小时半衰期衰减，用于调度排序。</p></div>}
    </div>

    {selected.classifier?.enabled && <details className="dp-disclosure dp-classifier"><summary><Sparkles size={14} /><span>意图分类预览</span><small>调用模型 · 消耗额度</small><ChevronDown size={13} className="dp-chevron" /></summary><div className="dp-classifier-body">
      <p className="dp-classifier-notice"><CircleHelp size={13} /><span>点击分类会调用已配置的模型并消耗额度。输入经服务端隐私处理，不写入调度记录。</span></p>
      <label className="dp-field">当前用户消息<textarea className={`${control} dp-message`} placeholder="输入需要识别意图的用户消息" value={input.text} onChange={(event) => update({ text: event.target.value })} /><span className="dp-input-note">{classifierInputNotice('text', input.text)}</span></label>
      <div className="dp-previous-messages">{[['previousUserMessage', '上一轮用户消息（可选）'], ['previousAssistantText', '上一轮助手摘要（可选）']].map(([key, label]) => <label key={key} className="dp-field">{label}<textarea className={control} value={input[key]} onChange={(event) => update({ [key]: event.target.value })} /><span className="dp-input-note">{classifierInputNotice(key, input[key])}</span></label>)}</div>
      <details className="dp-input-help"><summary><ShieldCheck size={12} />输入处理说明<ChevronDown size={12} /></summary><p>当前消息最多保留 4000 字符，上一轮用户消息和助手摘要各保留 1000 字符；超长输入保留头尾并标记截断，隐私处理无法完成时返回安全回退。</p></details>
      <div className="dp-classifier-actions"><Button ref={classificationButton} type="button" size="sm" variant="secondary" className="dp-action" icon={Sparkles} disabled={busy || !selected.classifier?.enabled || !input.text.trim()} onClick={runClassification}>调用模型分类</Button>{!selected.classifier?.enabled && <p className="dp-footnote">请先在方案中启用并保存分类器。</p>}</div>
      {classification && <div className="dp-classification-result"><div className="dp-section-heading"><h4>分类结果</h4><div className="dp-result-actions">{classification.cached && <span className="dp-badge" data-tone="accent">命中缓存</span>}<button type="button" className="dp-result-clear" aria-label="清除分类结果" disabled={busy} onClick={() => { classificationRevision.current += 1; setClassification(null); classificationButton.current?.focus(); }}><X size={12} />清除结果</button></div></div><div className="dp-classification-intent"><strong>{classification.intentId || '未命中'}</strong><span>可信度 {(classification.confidence * 100).toFixed(0)}%</span></div>{classification.reason && <p className="dp-result-decision">{classification.reason}</p>}<p className="dp-footnote">{classification.inputTokens} 输入 / {classification.outputTokens} 输出 token · 成本 ${classification.cost.toFixed(6)}</p></div>}
    </div></details>}
  </section>;
}
