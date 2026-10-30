import { Activity, Clock, FileSearch, ShieldCheck } from 'lucide-react';
import { PRIVACY_RULE_SOURCE_LABELS, PRIVACY_STATE_STYLES } from '../utils/requestMetadata.js';

const reasonLabels = {
  skipped_unsupported_path: '该请求路径未纳入扫描范围', skipped_empty_body: '请求正文为空',
  skipped_non_text_body: '请求正文不是文本', skipped_no_rules: '没有启用的防护规则',
  skipped_no_text: '未发现可扫描的文本字段', scan_error: '扫描失败，原文放行',
  skipped_scan_limit: '超过扫描上限，整次跳过扫描并原文放行', scan_truncated: '超过扫描上限，仅处理已扫描部分', privacy_scan_failed: '扫描失败',
  encoded_response: '压缩响应未还原', response_too_large: '响应过大，停止还原',
  large_stream_event: '超大流事件仅还原完整替身', mapping_capacity: '还原映射容量已满，本次原文放行', restoration_disabled: '运行设置已关闭回复还原', rule_restoration_disabled: '部分命中规则已关闭回复还原', privacy_restore_capacity: '历史记录：还原映射容量已满，请求曾在本机拒发'
};
const reasonLabel = reason => reasonLabels[reason] || (reason?.startsWith('scan_error:') ? '扫描失败，原文放行' : reason);
const stateNotes = {
  redacted: '请求中命中的内容已按规则处理。响应还原情况见下方记录。',
  detected: '规则检测到了内容，此记录未确认正文已脱敏。',
  clean: '本次扫描已完成，没有命中已启用的规则。',
  disabled: '本次请求未启用隐私防护。',
  blocked: '本次请求在防护检查阶段被拒发。',
  skipped: '本次请求未完成完整扫描，原因见下方。',
  truncated: '本次扫描达到上限，未扫描部分的内容无法据此判断。',
  unknown: '未找到可关联的扫描状态。',
  none: '未找到本次请求的防护记录，无法据此判断是否扫描或命中。历史防护记录最多保留 30 天 / 5,000 条。'
};

export default function RequestPrivacyDetails({ privacy }) {
  const activity = privacy.activity;
  const hasScan = activity && !['disabled', 'skipped', 'unknown'].includes(privacy.state);
  const warnings = [...new Set([activity?.skipped_reason, activity?.restore_skipped].filter(Boolean))];
  return <div className="request-privacy-details space-y-4">
    <section className="rounded-xl border border-slate-200 bg-white p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex items-center gap-2"><ShieldCheck className="h-4 w-4 text-indigo-500" /><h3 className="text-sm font-semibold text-slate-900">本次请求的防护结果</h3></div>
        <span className={`rounded-md border px-2 py-1 text-xs font-semibold ${PRIVACY_STATE_STYLES[privacy.state]}`}>{privacy.label}</span>
      </div>
      <p className="mt-3 text-sm leading-6 text-slate-500">{stateNotes[privacy.state]}</p>
      <div className="mt-4 grid grid-cols-3 divide-x divide-slate-100 rounded-lg border border-slate-100 bg-slate-50/60 py-3">
        {[
          ['命中内容', privacy.state === 'none' ? '—' : `${privacy.hitCount} 处`, FileSearch],
          ['扫描耗时', hasScan ? `${Math.max(0, Number(activity.scan_duration_ms) || 0).toFixed(1)} ms` : '—', Clock],
          ['响应还原', activity && privacy.state === 'redacted' ? `${Math.max(0, Number(activity.restored_count) || 0)} 处` : '—', Activity],
        ].map(([label, value, Icon]) => <div key={label} className="min-w-0 px-3 sm:px-4"><div className="flex items-center gap-1.5 text-xs text-slate-500"><Icon className="hidden h-3.5 w-3.5 sm:block" />{label}</div><strong className="mt-1.5 block text-base font-semibold tabular-nums text-slate-800">{value}</strong></div>)}
      </div>
      {activity?.truncated && <p className="mt-3 text-xs text-amber-700">扫描范围受限：仅检查了部分内容。</p>}
      {warnings.length > 0 && <div className="mt-3 space-y-1 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800">{warnings.map(reason => <p key={reason}>{reasonLabel(reason)}</p>)}</div>}
    </section>
    <section className="overflow-hidden rounded-xl border border-slate-200 bg-white">
      <header className="flex items-center justify-between border-b border-slate-100 bg-slate-50 px-4 py-3"><h3 className="text-sm font-semibold text-slate-900">规则命中明细</h3><span className="text-xs text-slate-500">{privacy.hits.length} 条规则</span></header>
      {privacy.hits.length ? <div className="divide-y divide-slate-100">{privacy.hits.map((hit, index) => <div key={`${hit.rule_id || index}/${hit.rule_name}`} className="flex items-center gap-3 px-4 py-3"><span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-indigo-50 text-xs text-indigo-500">{index + 1}</span><div className="min-w-0 flex-1"><strong className="block text-sm font-medium text-slate-700 [overflow-wrap:anywhere]">{hit.rule_name || '未命名规则'}</strong><span className="text-xs text-slate-400">{PRIVACY_RULE_SOURCE_LABELS[hit.source] || '规则来源未记录'} · {{redact: '脱敏', detect: '仅检测', block: '历史拦截'}[hit.action] || '已命中'}</span></div><span className="shrink-0 text-xs tabular-nums text-slate-600">{hit.count || 0} 处</span></div>)}</div> : <p className="px-4 py-6 text-sm text-slate-400">{privacy.state === 'clean' ? '本次扫描没有规则命中。' : '此记录没有可展示的规则命中明细。'}</p>}
    </section>
    <div className="rounded-lg border border-slate-200 bg-slate-50/70 px-4 py-3 text-xs leading-6 text-slate-500">
      {activity ? <><p>记录来源：隐私防护 → 防护记录，已按当前请求 ID 和服务关联。</p><p>扫描路径：<span className="font-mono [overflow-wrap:anywhere]">{activity.path || '未记录'}</span>{activity.created_at && <> · 记录时间：{new Date(activity.created_at).toLocaleString('zh-CN', { hour12: false })}</>}</p></> : privacy.state !== 'none' && <p>此历史请求仅保留命中摘要，扫描耗时、跳过原因和响应还原情况未记录。</p>}
      <p>仅展示处理状态、规则与次数，不保存或展示命中原文、请求正文及可逆映射。</p>
    </div>
  </div>;
}
