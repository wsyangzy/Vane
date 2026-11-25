import { CheckCircle2, CircleHelp, Shrink } from 'lucide-react';
import { getRequestCompactionEvidence } from '../utils/compactionSupport.js';
import { getRequestProtocolDiagnostics } from '../utils/failureSummary.js';

const tones = {
  slate: 'border-slate-200 bg-slate-50 text-slate-600',
  emerald: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  amber: 'border-amber-200 bg-amber-50 text-amber-700',
};

export default function RequestCompactionStatus({ request }) {
  if (String(request.provider).toLowerCase() !== 'codex' || !/\/responses\/?$/.test(request.path || '')) return null;
  const evidence = getRequestCompactionEvidence(request);
  const diagnostics = getRequestProtocolDiagnostics(request);
  const StatusIcon = evidence.state === 'verified' ? CheckCircle2 : CircleHelp;
  const countLabel = evidence.compactionItems !== null ? `${evidence.compactionItems} 项`
    : evidence.hasCompaction ? '检测到（数量未记录）' : '未记录';
  return <section aria-label="原生压缩验证" className="min-w-0 overflow-hidden rounded-xl border border-slate-200 bg-white">
    <header className="flex min-w-0 flex-wrap items-center justify-between gap-2 border-b border-slate-100 bg-slate-50 px-4 py-3">
      <h3 className="flex items-center gap-2 text-sm font-semibold text-slate-900"><Shrink size={16} className="text-slate-500" />原生压缩验证</h3>
      <span className={`inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-medium ${tones[evidence.tone]}`}><StatusIcon size={13} />{evidence.label}</span>
    </header>
    <div className="space-y-3 p-4 text-xs leading-5 text-slate-600 [overflow-wrap:anywhere]">
      <p>{evidence.description}</p>
      <dl className="grid min-w-0 grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-4">
        {[
          ['原生压缩项', countLabel], ['上游状态', evidence.httpStatus ? `HTTP ${evidence.httpStatus}` : '未记录'],
          ['端点', evidence.endpoint || '未记录'], ['模型', evidence.model || '未记录'],
        ].map(([label, value]) => <div key={label} className="min-w-0"><dt className="text-[11px] text-slate-400">{label}</dt><dd className="mt-0.5 font-medium text-slate-700 [overflow-wrap:anywhere]">{value}</dd></div>)}
      </dl>
      <p className="border-t border-slate-100 pt-2 text-[11px] text-slate-500">结论仅适用于这次请求的端点和模型，不代表整个渠道或其他路由目标都支持。</p>
      {diagnostics.length > 0 && <details className="rounded-lg border border-slate-200 bg-slate-50 p-2.5">
        <summary className="cursor-pointer font-medium text-slate-700">请求格式诊断</summary>
        <ul className="mt-2 space-y-1">{diagnostics.map(line => <li key={line}>{line}</li>)}</ul>
      </details>}
    </div>
  </section>;
}
