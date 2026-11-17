import { useEffect, useRef, useState } from 'react';
import { FlaskConical, ScanLine } from 'lucide-react';
import { Button, CustomSelect } from '@/shared/ui/index.js';
import { probeCodexEndpoint } from '@/services/wails/index.js';
import { buildEndpointProbeInput, PATH_STRATEGY_OPTIONS, previewEndpointPath, PROBE_RESULT_LABELS } from '../pathPolicy.js';

export default function EndpointPathPolicy({ channel, credentialGroup, endpoint, baseURL, strategy, onChange, disabled, compact = false }) {
  const [model, setModel] = useState(channel?.model || '');
  const [pending, setPending] = useState('');
  const [probe, setProbe] = useState(null);
  const mounted = useRef(true);
  const identity = JSON.stringify([baseURL.trim(), channel?.id, credentialGroup?.id, credentialGroup?.apiKey || credentialGroup?.api_key, model, strategy]);
  const latestIdentity = useRef(identity);
  useEffect(() => { latestIdentity.current = identity; }, [identity]);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const runProbe = async (verifyResponse) => {
    const startedIdentity = identity;
    setPending(verifyResponse ? 'response' : 'models');
    try {
      const result = await probeCodexEndpoint(buildEndpointProbeInput({ channel, credentialGroup, endpoint, baseURL, model, pathStrategy: strategy, verifyResponse }));
      if (!mounted.current || latestIdentity.current !== startedIdentity) return;
      setProbe({ identity: startedIdentity, result });
    } catch (error) {
      if (mounted.current && latestIdentity.current === startedIdentity) setProbe({ identity: startedIdentity, error: error?.message || '检测失败，请稍后重试' });
    } finally {
      if (mounted.current) setPending('');
    }
  };
  const visible = probe?.identity === identity ? probe : null;
  const options = endpoint?.id || strategy === 'legacy'
    ? [{ value: 'legacy', label: '保持原有行为' }, ...PATH_STRATEGY_OPTIONS] : PATH_STRATEGY_OPTIONS;
  const pathButton = <Button type="button" variant="secondary" size="sm" icon={ScanLine} loading={pending === 'models'} disabled={disabled || Boolean(pending) || !baseURL.trim()} onClick={() => runProbe(false)}>检测路径</Button>;
  const responseButton = <Button type="button" variant="secondary" size="sm" icon={FlaskConical} loading={pending === 'response'} disabled={disabled || Boolean(pending) || strategy === 'legacy' || !model.trim() || !baseURL.trim()} onClick={() => runProbe(true)}>验证流式请求</Button>;
  const pathSelect = <CustomSelect ariaLabel="接口路径" options={options} value={strategy} onChange={onChange} disabled={disabled || Boolean(pending)} fullWidth size="md" />;
  const modelInput = <input aria-label="验证模型" value={model} onChange={event => setModel(event.target.value)} disabled={disabled || Boolean(pending)} placeholder="验证模型名称" className="min-w-0 flex-1 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20" />;
  return <div className={`rounded-xl border border-slate-200 bg-slate-50/60 p-3 sm:col-span-2 ${compact ? 'endpoint-path-policy-compact' : 'space-y-3'}`}>
    <div className="flex flex-wrap items-center justify-between gap-2">
      <span className="text-sm font-medium text-slate-700">接口路径</span>
      {pathButton}
    </div>
    {pathSelect}
    <p className="min-w-0 break-words font-mono text-xs text-slate-500 [overflow-wrap:anywhere]">{previewEndpointPath(baseURL, strategy) || '填写地址后显示实际请求地址'}</p>
    <div className="flex flex-wrap items-center gap-2">{modelInput}{responseButton}</div>
    <p className="text-xs text-slate-500">路径检测只读取模型目录；流式验证会发送一条简短请求，可能产生少量用量。检测失败也可手动选择后保存。</p>
    {visible && <div role="status" aria-live="polite" className={compact ? 'endpoint-path-policy-results text-xs' : 'space-y-2 text-xs'}>
      {visible.error && <p className="text-rose-600">{visible.error}</p>}
      {visible.result?.candidates?.map(candidate => <div key={candidate.url} className="min-w-0 rounded-lg border border-slate-200 bg-white p-2.5">
        <p className={candidate.verified ? 'text-emerald-700' : 'text-amber-700'}>{candidate.http_status ? `HTTP ${candidate.http_status} · ` : ''}{PROBE_RESULT_LABELS[candidate.result] || '协议未确认，请检查上游接口'}</p>
        <p className="mt-1 break-words font-mono text-slate-500 [overflow-wrap:anywhere]">{candidate.url}</p>
        {candidate.verified && candidate.path_strategy !== strategy && <Button type="button" variant="ghost" size="sm" icon={ScanLine} onClick={() => onChange(candidate.path_strategy)}>采用此路径</Button>}
      </div>)}
      {visible.result?.candidates?.filter(item => item.verified).length > 1 && <p className="text-slate-500">两种路径都可用，请选择需要使用的路径。</p>}
    </div>}
  </div>;
}
