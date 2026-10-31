import { useCallback, useEffect, useRef, useState } from 'react';
import { Cable, Check, ChevronDown, ChevronUp, Copy, FileCode2, RefreshCw, RotateCcw, Save } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { manageSubscriptionConnection } from '@/services/wails/index.js';

const FIELD = 'w-full min-w-0 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20';
const errorText = (error) => typeof error === 'string' ? error : error?.message || '操作失败，请重试';

export default function SubscriptionConnectionPanel({ provider, initialLocalKey = '', embedded = false, onBusyChange }) {
  const [expanded, setExpanded] = useState(false);
  const [localKey, setLocalKey] = useState(initialLocalKey);
  const [status, setStatus] = useState(null);
  const [preview, setPreview] = useState(null);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [statusError, setStatusError] = useState('');
  const [notice, setNotice] = useState('');
  const [confirmed, setConfirmed] = useState(false);
  const generation = useRef(0);
  useEffect(() => { onBusyChange?.(Boolean(busy)); }, [busy, onBusyChange]);
  const load = useCallback(async () => {
    const version = ++generation.current;
    try {
      const result = await manageSubscriptionConnection(provider, 'status');
      if (version === generation.current) { setStatus(result); setStatusError(''); }
    } catch (e) { if (version === generation.current) { setStatus(null); setStatusError(errorText(e)); } }
  }, [provider]);
  useEffect(() => {
    load();
    const timer = setInterval(load, 5000);
    return () => { clearInterval(timer); generation.current += 1; };
  }, [load]);
  useEffect(() => { setPreview(null); setConfirmed(false); }, [status?.profileId, status?.bindingVersion, status?.managed, status?.authEnabled]);
  const clearPreview = () => { setPreview(null); setConfirmed(false); setNotice(''); setError(''); };
  const run = async (action) => {
    setBusy(action); setError(''); setNotice('');
    try {
      const result = await manageSubscriptionConnection(provider, action, {
        profileId: preview?.profileId, localKey: ['preview', 'apply'].includes(action) && status?.authEnabled ? localKey : '',
        revision: action === 'restore' ? status?.revision : preview?.revision,
        bindingVersion: preview?.bindingVersion, contentRevision: preview?.contentRevision
      });
      if (action === 'preview') { setPreview(result); setConfirmed(false); }
      if (action === 'apply') { setPreview(null); setLocalKey(''); setConfirmed(false); setNotice('已备份并应用客户端配置。请退出并重新启动客户端，使新接入生效。'); }
      if (action === 'restore') { setPreview(null); setLocalKey(''); setConfirmed(false); setNotice('已恢复应用接入前的配置，请重新启动客户端。'); }
      await load();
    } catch (e) { setPreview(null); setConfirmed(false); setError(errorText(e)); }
    finally { setBusy(''); }
  };
  const copy = async (value) => {
    try { await navigator.clipboard.writeText(value); setNotice('已复制'); }
    catch { setError('复制失败，请手动选择并复制'); }
  };
  const close = () => { setExpanded(false); setLocalKey(''); setPreview(null); setConfirmed(false); };
  const title = provider === 'codex' ? 'Codex' : 'Claude';
  const stalePreview = preview && (preview.baseURL !== status?.baseURL || preview.revision !== status?.revision || preview.profileId !== status?.profileId || preview.bindingVersion !== status?.bindingVersion || preview.managed !== status?.managed || preview.authEnabled !== status?.authEnabled);
  return <section className={embedded ? 'min-w-0' : 'mb-4 min-w-0 rounded-xl border border-slate-200 bg-white shadow-sm'}>
    <div className="flex min-w-0 flex-wrap items-center justify-between gap-2 px-4 py-3">
      <div className="flex min-w-0 flex-wrap items-center gap-2"><Cable size={16} className="text-indigo-600" /><h2 className="text-sm font-semibold text-slate-800">客户端接入配置</h2><span className={`rounded px-2 py-0.5 text-xs ${status?.running ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{status?.running ? '代理运行中' : status ? '代理未运行' : '状态未读取'}</span><span className="text-xs text-slate-500">{title} · 与普通端点共用监听端口</span></div>
      {!embedded && <button type="button" className="flex items-center gap-1 rounded-lg px-2 py-1 text-xs text-indigo-600 hover:bg-indigo-50" aria-expanded={expanded} onClick={() => expanded ? close() : setExpanded(true)}>{expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}{expanded ? '收起配置' : '配置客户端'}</button>}
    </div>
    {(embedded || expanded) && <div className="min-w-0 space-y-4 border-t border-slate-100 p-4">
      <p className="text-xs leading-5 text-slate-600">客户端统一接入当前默认调度方案。渠道、凭证分组和端点的选择由方案决定。</p>
      <div className="grid min-w-0 gap-4 md:grid-cols-2">
        <div className="min-w-0 space-y-2"><span className="block text-xs font-medium text-slate-700">当前默认方案</span><p className="wrap-anywhere rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-800">{status?.profileName || '尚未建立默认方案'}</p><p className={`text-xs ${status?.endpointReady ? 'text-emerald-600' : 'text-amber-700'}`}>{status?.endpointReady ? '默认方案配置就绪' : '请先在调度方案中检查成员、凭证、端点及应用级鉴权'}</p></div>
        <div className="min-w-0 space-y-2">{status?.authEnabled ? <><label htmlFor={`${provider}-relay-key`} className="text-xs font-medium text-slate-700">应用级代理令牌</label><input id={`${provider}-relay-key`} type="password" autoComplete="off" value={localKey} onChange={(e) => { setLocalKey(e.target.value); clearPreview(); }} disabled={Boolean(busy)} className={FIELD} placeholder="粘贴设置中保存的应用级代理令牌" /><p className="text-xs text-slate-500">在应用设置中管理，用于客户端连接 Vane。</p></> : <p className="rounded-lg bg-slate-50 px-3 py-2 text-xs leading-5 text-slate-600">{status ? '应用级鉴权未启用，可直接生成本机接入配置。' : '正在读取应用级鉴权设置…'}</p>}</div>
      </div>
      <div className="min-w-0 space-y-2 rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs text-slate-600">
        <div className="flex min-w-0 items-start gap-2"><span className="shrink-0">接入地址</span><code className="min-w-0 flex-1 break-all">{status?.baseURL || '等待读取'}</code><button type="button" aria-label="复制接入地址" disabled={!status?.baseURL} onClick={() => copy(status.baseURL)} className="shrink-0 rounded p-1 text-indigo-600 hover:bg-indigo-100 disabled:opacity-40"><Copy size={13} /></button></div>
        <div className="flex min-w-0 gap-2"><span className="shrink-0">配置文件</span><code className="min-w-0 break-all">{status?.configPath || '等待读取'}</code></div>
        {status?.profileId > 0 && <div className="leading-5">凭证来源：{status.managed ? 'API Key 和托管订阅由 Vane 按方案解析。' : '默认方案包含客户端透传分组，客户端需保留有效的官方登录；其他凭证由 Vane 按方案解析。'}</div>}
      </div>
      <div className="flex flex-wrap gap-2"><Button type="button" variant="secondary" onClick={load} disabled={Boolean(busy)}><RefreshCw size={14} />刷新状态</Button><Button type="button" onClick={() => run('preview')} disabled={Boolean(busy) || !status?.endpointReady || (status?.authEnabled && !localKey.trim())}><FileCode2 size={14} />{busy === 'preview' ? '正在生成…' : '生成配置预览'}</Button><Button type="button" variant="secondary" onClick={() => run('restore')} disabled={Boolean(busy) || !status?.backupAvailable}><RotateCcw size={14} />恢复原客户端配置</Button></div>
      {preview && <div className="min-w-0 space-y-3 rounded-lg border border-indigo-100 bg-indigo-50/40 p-3">
        <div className="flex items-center justify-between gap-2"><span className="text-xs font-medium text-slate-700">{provider === 'codex' ? 'config.toml 接入字段' : 'settings.json 接入字段'}</span><button type="button" onClick={() => copy(preview.snippet)} className="flex items-center gap-1 rounded px-2 py-1 text-xs text-indigo-600 hover:bg-indigo-100"><Copy size={13} />复制配置</button></div>
        <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-lg border border-slate-200 bg-white p-3 text-xs leading-5">{preview.snippet}</pre>
        {provider === 'claude' && <><div className="flex items-center justify-between gap-2"><span className="text-xs font-medium text-slate-700">临时启动（当前 PowerShell 窗口）</span><button type="button" onClick={() => copy(preview.command)} className="flex items-center gap-1 rounded px-2 py-1 text-xs text-indigo-600 hover:bg-indigo-100"><Copy size={13} />复制命令</button></div><pre className="max-h-52 overflow-auto whitespace-pre-wrap break-all rounded-lg border border-slate-200 bg-white p-3 text-xs leading-5">{preview.command}</pre></>}
        <p className="text-xs leading-5 text-slate-600">应用仅修改上方显示的客户端配置文件；保留其他配置值，整理文件格式，原文件加密备份。{provider === 'claude' ? '会移除文件中与所选模式冲突的 API Key、Auth Token、OAuth Token 环境字段、apiKeyHelper 和自定义认证头；已有其他自定义头保留。父进程环境和项目配置可能覆盖此设置。' : '会替换 relay_subscription 专用 provider，并同步当前内联 profile 的 provider；其他 profile、启动参数和托管策略可能覆盖此设置。auth.json 保持原样。'} 新启动的客户端生效。</p>
        <label className="flex items-start gap-2 text-xs leading-5 text-slate-700"><input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} disabled={Boolean(busy)} className="mt-1 accent-indigo-600" />我已核对目标、配置路径和接入字段，允许将应用级接入信息写入该客户端配置。</label>
        {stalePreview && <p className="text-xs text-amber-700">监听地址、默认方案、鉴权设置或配置文件已变化，请重新生成预览。</p>}
        <Button type="button" onClick={() => run('apply')} disabled={Boolean(busy) || !confirmed || !status?.running || !status?.endpointReady || stalePreview}><Save size={14} />{busy === 'apply' ? '正在应用…' : '备份并应用配置'}</Button>
      </div>}
      {error && <p role="alert" className="break-words text-xs text-rose-600 [overflow-wrap:anywhere]">{error}</p>}
      {statusError && <p role="alert" className="break-words text-xs text-rose-600 [overflow-wrap:anywhere]">{statusError}</p>}
      {notice && <p role="status" className="flex items-start gap-1 break-words text-xs text-emerald-700 [overflow-wrap:anywhere]"><Check size={14} className="shrink-0" />{notice}</p>}
    </div>}
  </section>;
}
