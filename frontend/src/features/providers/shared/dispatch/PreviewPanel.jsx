import { useId, useRef, useState } from 'react';
import { ChevronDown, FlaskConical, GitBranch, Play, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { previewDispatch } from '@/services/wails/dispatch.js';
import { dispatchReason, ruleKindLabel, stateCompatibilityLabel } from './dispatchState.js';
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

export default function PreviewPanel({ provider, selected, channels, accounts, busy, perform, embedded = false }) {
  const [input, setInput] = useState({ model: '', streaming: true, conversationId: '', branchId: '', turnId: '', newTurn: false });
  const [preview, setPreview] = useState(null);
  const [contextOpen, setContextOpen] = useState(false);
  const contextId = useId();
  const previewButton = useRef(null);
  const previewRevision = useRef(0);
  const update = (patch) => {
    setInput((previous) => ({ ...previous, ...patch }));
    if (Object.keys(patch).some((key) => ['model', 'streaming', 'conversationId', 'branchId', 'turnId', 'newTurn'].includes(key))) {
      previewRevision.current += 1;
      setPreview(null);
    }
  };
  const params = { ...input, provider, profileId: selected.id };
  const runPreview = () => perform(async () => {
    const revision = previewRevision.current;
    const result = await previewDispatch(params);
    if (revision === previewRevision.current) setPreview(result);
  });
  return <section className={embedded ? 'dp-preview-panel' : 'dp-card dp-preview-card'} aria-label="模拟调度设置">
    {!embedded && <header className="dp-card-header"><div className="dp-heading"><FlaskConical size={15} /><h3>模拟调度</h3></div><p className="dp-description dp-preview-description">使用已保存的方案，不发送业务请求、不占用候选或推进轮询。</p><span className="dp-badge">只读预览</span></header>}
    <div className="dp-preview-body">
      {embedded && <p className="dp-description">使用已保存的方案，不发送业务请求、不占用候选或推进轮询。</p>}
      <div className="dp-preview-controls t-acc" data-open={contextOpen}>
      <div className="dp-preview-request"><label className="dp-field">请求模型<input className={control} placeholder={provider === 'codex' ? '例如 gpt-5' : '例如 claude-sonnet-4-5'} value={input.model} onChange={(event) => update({ model: event.target.value })} /></label><div className="dp-preview-actions"><label className="dp-check"><input type="checkbox" checked={input.streaming} onChange={(event) => update({ streaming: event.target.checked })} />流式请求</label><Button ref={previewButton} type="button" size="sm" variant="secondary" className="dp-action" icon={Play} disabled={busy || !input.model.trim()} onClick={runPreview}>预览候选</Button></div></div>

      <button type="button" className="dp-request-context t-acc-head" aria-expanded={contextOpen} aria-controls={contextId} onClick={() => setContextOpen(open => !open)}><GitBranch size={13} /><span>会话与轮次</span><small>可选</small><span className="t-acc-chevron"><ChevronDown size={13} /></span></button>
      <div id={contextId} className="t-acc-panel" aria-hidden={!contextOpen} inert={!contextOpen}><div className="t-acc-panel-inner"><div className="dp-context-fields">{[['conversationId', '会话 ID'], ['turnId', '轮次 ID'], ['branchId', '分支 ID']].map(([key, label]) => <label key={key} className="dp-field">{label}<input className={control} value={input[key]} onChange={(event) => update({ [key]: event.target.value })} /></label>)}<label className="dp-check dp-new-turn"><input type="checkbox" checked={input.newTurn} onChange={(event) => update({ newTurn: event.target.checked })} />开始新轮次</label></div></div></div>
      </div>

      {preview && <div className="dp-preview-result"><div className="dp-section-heading"><h4>候选预览结果</h4><div className="dp-result-actions"><span>{preview.candidates.filter((entry) => entry.eligible).length} / {preview.candidates.length} 个可参与</span><button type="button" className="dp-result-clear" aria-label="清除候选结果" disabled={busy} onClick={() => { previewRevision.current += 1; setPreview(null); previewButton.current?.focus(); }}><X size={12} />清除结果</button></div></div><p className="dp-result-decision">决策：{ruleKindLabel(preview.decision.kind || 'default')}{preview.decision.ruleId ? ` · 规则 ${preview.decision.ruleId}` : ''}{preview.decision.fallback ? ' · 已回退' : ''}{preview.stopReason ? ` · ${dispatchReason(preview.stopReason)}` : ''}</p><Candidates entries={preview.candidates} channels={channels} accounts={accounts} /><p className="dp-footnote">Vane 近期负载来自本地请求观测，tokens 和请求数按 1 小时半衰期衰减，用于调度排序。</p></div>}
    </div>

  </section>;
}
