import { useCallback, useEffect, useRef, useState } from 'react';
import { Cable, Check, ChevronDown, ChevronUp, Copy, FileCode2, Plus, RefreshCw, RotateCcw, Save } from 'lucide-react';
import { Button, CustomSelect } from '@/shared/ui/index.js';
import { manageSubscriptionConnection } from '@/services/wails/index.js';

const FIELD = 'w-full min-w-0 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20';
const errorText = (error) => typeof error === 'string' ? error : error?.message || '操作失败，请重试';

export default function SubscriptionConnectionPanel({ provider, channels = [], onUpdated, embedded = false, onBusyChange }) {
  const groups = channels.flatMap((channel) => (channel.credentialGroups || channel.credential_groups || [])
    .filter((group) => (group.authMode || group.auth_mode) === 'client_passthrough')
    .map((group) => ({ ...group, label: `${channel.name} / ${group.name}` })));
  const [expanded, setExpanded] = useState(false);
  const [groupId, setGroupId] = useState('');
  const [localKey, setLocalKey] = useState('');
  const [status, setStatus] = useState(null);
  const [preview, setPreview] = useState(null);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [statusError, setStatusError] = useState('');
  const [notice, setNotice] = useState('');
  const [confirmed, setConfirmed] = useState(false);
  const generation = useRef(0);
  useEffect(() => { onBusyChange?.(Boolean(busy)); }, [busy, onBusyChange]);
  const selectedGroup = groups.find((group) => String(group.id) === groupId);
  const groupVersion = selectedGroup?.subscription?.version;
  const load = useCallback(async () => {
    const version = ++generation.current;
    try {
      const result = await manageSubscriptionConnection(provider, 'status', { groupId: Number(groupId) });
      if (version === generation.current) { setStatus(result); setStatusError(''); }
    } catch (e) { if (version === generation.current) { setStatus(null); setStatusError(errorText(e)); } }
  }, [provider, groupId]);
  useEffect(() => {
    load();
    const timer = setInterval(load, 5000);
    return () => { clearInterval(timer); generation.current += 1; };
  }, [load]);
  useEffect(() => { setPreview(null); setConfirmed(false); }, [groupVersion]);
  const clearPreview = () => { setPreview(null); setConfirmed(false); setNotice(''); setError(''); };
  const selectGroup = (value) => { setGroupId(value); setLocalKey(''); clearPreview(); };
  const run = async (action) => {
    setBusy(action); setError(''); setNotice('');
    try {
      const result = await manageSubscriptionConnection(provider, action, {
        groupId: Number(groupId), localKey: ['preview', 'apply'].includes(action) ? localKey : '',
        revision: action === 'restore' ? status?.revision : preview?.revision,
        bindingVersion: preview?.bindingVersion, contentRevision: preview?.contentRevision
      });
      if (action === 'preview') { setPreview(result); setConfirmed(false); }
      if (action === 'apply') { setPreview(null); setLocalKey(''); setConfirmed(false); setNotice('已备份并应用客户端配置。请退出并重新启动客户端，使新接入生效。'); }
      if (action === 'restore') { setPreview(null); setLocalKey(''); setConfirmed(false); setNotice('已恢复应用接入前的配置，请重新启动客户端。'); }
      if (action === 'prepare_endpoint') { setPreview(null); setNotice('官方端点已准备，请核对分组后生成配置预览。'); await onUpdated?.(); }
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
  const stalePreview = preview && (preview.baseURL !== status?.baseURL || preview.revision !== status?.revision || preview.bindingVersion !== status?.bindingVersion);
  return <section className={embedded ? 'min-w-0' : 'mb-4 min-w-0 rounded-xl border border-slate-200 bg-white shadow-sm'}>
    <div className="flex min-w-0 flex-wrap items-center justify-between gap-2 px-4 py-3">
      <div className="flex min-w-0 flex-wrap items-center gap-2"><Cable size={16} className="text-indigo-600" /><h2 className="text-sm font-semibold text-slate-800">订阅接入配置</h2><span className={`rounded px-2 py-0.5 text-xs ${status?.running ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{status?.running ? '代理运行中' : status ? '代理未运行' : '状态未读取'}</span><span className="text-xs text-slate-500">{title} · 与普通端点共用监听端口</span></div>
      {!embedded && <button type="button" className="flex items-center gap-1 rounded-lg px-2 py-1 text-xs text-indigo-600 hover:bg-indigo-50" aria-expanded={expanded} onClick={() => expanded ? close() : setExpanded(true)}>{expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}{expanded ? '收起配置' : '配置客户端'}</button>}
    </div>
    {(embedded || expanded) && <div className="min-w-0 space-y-4 border-t border-slate-100 p-4">
      <p className="text-xs leading-5 text-slate-600">先在订阅账号中登录或导入，再在渠道凭证分组中选择「官方订阅」并保存本地密钥；使用客户端既有登录时可直接创建透传分组。接入按密钥识别分组，保留该分组的模型规则与授权备用。仅本机客户端接入，官方验证待完成。</p>
      <div className="grid min-w-0 gap-4 md:grid-cols-2">
        <div className="min-w-0 space-y-2"><label className="block text-xs font-medium text-slate-700">接入订阅分组</label><CustomSelect fullWidth ariaLabel="接入订阅分组" value={groupId} onChange={selectGroup} options={[{ value: '', label: '选择已保存的官方订阅分组' }, ...groups.map((group) => ({ value: String(group.id), label: group.label }))]} disabled={Boolean(busy)} />{groups.length === 0 && <p className="text-xs text-amber-700">尚无订阅分组，请先返回渠道卡片添加凭证分组，认证方式选择「官方订阅」。</p>}</div>
        <div className="min-w-0 space-y-2"><label htmlFor={`${provider}-relay-key`} className="text-xs font-medium text-slate-700">已保存的本地密钥</label><input id={`${provider}-relay-key`} type="password" autoComplete="off" value={localKey} onChange={(e) => { setLocalKey(e.target.value); clearPreview(); }} disabled={Boolean(busy) || !selectedGroup} className={FIELD} placeholder="粘贴所选分组的本地密钥" /><p className="text-xs text-slate-500">遗失时在分组编辑中轮换并保存；此处不会读取官方登录凭证。</p></div>
      </div>
      <div className="min-w-0 space-y-2 rounded-lg border border-slate-200 bg-slate-50 p-3 text-xs text-slate-600">
        <div className="flex min-w-0 items-start gap-2"><span className="shrink-0">接入地址</span><code className="min-w-0 flex-1 break-all">{status?.baseURL || '等待读取'}</code><button type="button" aria-label="复制接入地址" disabled={!status?.baseURL} onClick={() => copy(status.baseURL)} className="shrink-0 rounded p-1 text-indigo-600 hover:bg-indigo-100 disabled:opacity-40"><Copy size={13} /></button></div>
        <div className="flex min-w-0 gap-2"><span className="shrink-0">配置文件</span><code className="min-w-0 break-all">{status?.configPath || '等待读取'}</code></div>
        {selectedGroup && <><div>凭证来源：{status?.managed ? 'Relay 托管账号，自动刷新' : '客户端自己的官方登录与刷新'} · {status?.officialEnabled ? '允许官方订阅' : '官方已暂停，仅授权备用可用'}</div><div className="flex min-w-0 flex-wrap items-center gap-2"><span className={status?.endpointReady ? 'text-emerald-700' : 'text-amber-700'}>{status?.endpointReady ? '官方端点已配置' : '需要配置官方端点'}</span><code className="min-w-0 break-all">{status?.officialURL}</code>{status && !status.endpointReady && <Button type="button" variant="secondary" onClick={() => run('prepare_endpoint')} disabled={Boolean(busy)}><Plus size={13} />准备官方端点</Button>}</div></>}
      </div>
      <div className="flex flex-wrap gap-2"><Button type="button" variant="secondary" onClick={load} disabled={Boolean(busy)}><RefreshCw size={14} />刷新状态</Button><Button type="button" onClick={() => run('preview')} disabled={Boolean(busy) || !selectedGroup || !localKey || !status?.endpointReady}><FileCode2 size={14} />{busy === 'preview' ? '正在生成…' : '生成配置预览'}</Button><Button type="button" variant="secondary" onClick={() => run('restore')} disabled={Boolean(busy) || !status?.backupAvailable}><RotateCcw size={14} />恢复接入前配置</Button></div>
      {preview && <div className="min-w-0 space-y-3 rounded-lg border border-indigo-100 bg-indigo-50/40 p-3">
        <div className="flex items-center justify-between gap-2"><span className="text-xs font-medium text-slate-700">{provider === 'codex' ? 'config.toml 接入字段' : 'settings.json 接入字段'}</span><button type="button" onClick={() => copy(preview.snippet)} className="flex items-center gap-1 rounded px-2 py-1 text-xs text-indigo-600 hover:bg-indigo-100"><Copy size={13} />复制配置</button></div>
        <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-lg border border-slate-200 bg-white p-3 text-xs leading-5">{preview.snippet}</pre>
        {provider === 'claude' && <><div className="flex items-center justify-between gap-2"><span className="text-xs font-medium text-slate-700">临时启动（当前 PowerShell 窗口）</span><button type="button" onClick={() => copy(preview.command)} className="flex items-center gap-1 rounded px-2 py-1 text-xs text-indigo-600 hover:bg-indigo-100"><Copy size={13} />复制命令</button></div><pre className="max-h-52 overflow-auto whitespace-pre-wrap break-all rounded-lg border border-slate-200 bg-white p-3 text-xs leading-5">{preview.command}</pre></>}
        <p className="text-xs leading-5 text-slate-600">应用仅修改上方显示的客户端配置文件；保留其他配置值，整理文件格式，原文件加密备份。{provider === 'claude' ? '会移除文件中与所选模式冲突的 API Key、Auth Token、OAuth Token 环境字段、apiKeyHelper 和自定义认证头；已有其他自定义头保留。父进程环境和项目配置可能覆盖此设置。' : '会替换 relay_subscription 专用 provider，并同步当前内联 profile 的 provider；其他 profile、启动参数和托管策略可能覆盖此设置。auth.json 保持原样。'} 新启动的客户端生效。</p>
        <label className="flex items-start gap-2 text-xs leading-5 text-slate-700"><input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} disabled={Boolean(busy)} className="mt-1 accent-indigo-600" />我已核对分组、配置路径和接入字段，允许将本地密钥写入该客户端配置。</label>
        {stalePreview && <p className="text-xs text-amber-700">监听地址、订阅绑定或配置文件已变化，请重新生成预览。</p>}
        <Button type="button" onClick={() => run('apply')} disabled={Boolean(busy) || !confirmed || !status?.running || stalePreview}><Save size={14} />{busy === 'apply' ? '正在应用…' : '备份并应用配置'}</Button>
      </div>}
      {error && <p role="alert" className="break-words text-xs text-rose-600 [overflow-wrap:anywhere]">{error}</p>}
      {statusError && <p role="alert" className="break-words text-xs text-rose-600 [overflow-wrap:anywhere]">{statusError}</p>}
      {notice && <p role="status" className="flex items-start gap-1 break-words text-xs text-emerald-700 [overflow-wrap:anywhere]"><Check size={14} className="shrink-0" />{notice}</p>}
    </div>}
  </section>;
}
