import { useState } from 'react';
import { Check, Copy, CircleSlash, Unplug } from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';
import { describeFailureReason } from '../utils/failureSummary.js';

export default function LocalRequestFailure({ details }) {
  const [copied, setCopied] = useState(false);
  const Icon = details.localTitle === '请求未发送' ? CircleSlash : Unplug;
  const singleHop = details.localHops.length === 1 ? details.localHops[0] : null;
  const repeatsHop = singleHop && (describeFailureReason(singleHop.skipReason) || singleHop.error) === details.localSummary;
  const text = [details.localTitle, details.localSummary, details.localDetail,
    ...details.localHops.map(hop => [hop.channel, hop.credentialGroup, hop.endpoint,
      describeFailureReason(hop.skipReason) || hop.skipReason || hop.error].filter(Boolean).join(' / '))
  ].filter(Boolean).join('\n');
  return <section aria-label="本地处理状态" className="min-w-0 overflow-hidden rounded-xl border border-amber-200 bg-amber-50/70">
    <div className="flex items-center justify-between border-b border-amber-200/80 bg-amber-100/50 px-4 py-3">
      <h3 className="flex items-center gap-2 text-sm font-semibold text-amber-950"><Icon className="h-4 w-4" />{details.localTitle}</h3>
      <Tooltip content={copied ? '已复制' : '复制本地处理信息'}><button type="button" aria-label="复制本地处理信息" className="motion-button rounded p-1 text-amber-600 hover:bg-amber-100 hover:text-amber-800" onClick={async () => {
        await navigator.clipboard.writeText(text);
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      }}>{copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}</button></Tooltip>
    </div>
    <div className="min-w-0 space-y-3 p-4 text-sm text-amber-950">
      {!repeatsHop && <p className="font-medium leading-6 [overflow-wrap:anywhere]">{details.localSummary}</p>}
      {details.localHops.length > 0 && <ul className="divide-y divide-amber-200/70 rounded-lg border border-amber-200/80 bg-white/60">
        {details.localHops.map((hop, index) => <li key={`${hop.hop}/${index}`} className="min-w-0 space-y-1 px-3 py-2">
          <div className="flex flex-wrap items-center gap-2 text-xs text-slate-600">
            <span className="min-w-0 [overflow-wrap:anywhere]">{[hop.channel, hop.credentialGroup, hop.endpoint].filter(Boolean).join(' / ')}</span>
            <span className="rounded border border-amber-200 bg-amber-50 px-1.5 py-0.5 text-amber-800">{hop.skipReason ? '发送前跳过' : '连接或传输失败'}</span>
          </div>
          <p className="whitespace-pre-wrap text-xs leading-5 [overflow-wrap:anywhere]">{describeFailureReason(hop.skipReason) || hop.skipReason || hop.error || '未记录具体原因'}</p>
        </li>)}
      </ul>}
      {details.localDetail && !details.localHops.some(hop => hop.error === details.localDetail || hop.skipReason === details.localDetail) && <pre className="max-h-60 overflow-auto whitespace-pre-wrap rounded-lg bg-white/70 p-3 font-mono text-xs leading-5 text-slate-700 [overflow-wrap:anywhere]">{details.localDetail}</pre>}
    </div>
  </section>;
}
