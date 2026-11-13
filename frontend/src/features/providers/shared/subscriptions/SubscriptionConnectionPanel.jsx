import { useCallback, useEffect, useRef, useState } from 'react';
import { Cable, Check, CheckCircle2, ChevronDown, ChevronUp, Copy, FileCode2, FileJson2, FolderOpen, Globe2, RefreshCw, RotateCcw } from 'lucide-react';
import { Button, CustomSelect } from '@/shared/ui/index.js';
import { listRoutingProfiles, manageSubscriptionConnection, setDefaultRoutingProfile } from '@/services/wails/index.js';
import SubscriptionConnectionPreview from './SubscriptionConnectionPreview.jsx';
import { dispatchReason, profileSwitchBlockReason } from '../dispatch/dispatchState.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';

const errorText = (error) => dispatchReason(getErrorMessage(error, '操作失败，请重试'));

export default function SubscriptionConnectionPanel({ provider, embedded = false, onBusyChange, onUpdated }) {
  const [expanded, setExpanded] = useState(false);
  const [status, setStatus] = useState(null);
  const [profiles, setProfiles] = useState([]);
  const [preview, setPreview] = useState(null);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [statusError, setStatusError] = useState('');
  const [notice, setNotice] = useState('');
  const [confirmed, setConfirmed] = useState(false);
  const [catalogChoice, setCatalogChoice] = useState(null);
  const catalogMode = catalogChoice?.mode ?? status?.modelCatalogMode ?? 'local';
  const catalogPath = catalogChoice?.path ?? status?.modelCatalogPath ?? '';
  const generation = useRef(0);
  const previewTriggerRef = useRef(null);
  const hadPreview = useRef(false);
  useEffect(() => { onBusyChange?.(Boolean(busy)); }, [busy, onBusyChange]);
  useEffect(() => {
    if (preview) hadPreview.current = true;
    else if (hadPreview.current && !busy) { previewTriggerRef.current?.focus(); hadPreview.current = false; }
  }, [preview, busy]);
  const load = useCallback(async () => {
    const version = ++generation.current;
    try {
      const [result, workspace] = await Promise.all([manageSubscriptionConnection(provider, 'status'), listRoutingProfiles(provider)]);
      if (version === generation.current) { setStatus(result); setProfiles(workspace.profiles); setStatusError(''); }
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
        profileId: preview?.profileId,
        revision: action === 'restore' ? status?.revision : preview?.revision,
        bindingVersion: preview?.bindingVersion, contentRevision: preview?.contentRevision,
        ...(provider === 'codex' ? { modelCatalogMode: catalogMode, modelCatalogPath: catalogPath } : {})
      });
      if (action === 'preview') { setPreview(result); setConfirmed(false); }
      if (action === 'apply') { setCatalogChoice(null); setPreview(null); setConfirmed(false); setNotice('已备份并自动写入客户端配置。请重新启动客户端使配置生效。'); }
      if (action === 'restore') { setCatalogChoice(null); setPreview(null); setConfirmed(false); setNotice('已恢复接入 Vane 前的客户端配置，请重新启动客户端。'); }
      await load();
    } catch (e) { setPreview(null); setConfirmed(false); setError(errorText(e)); }
    finally { setBusy(''); }
  };
  const changeCatalog = (mode, path = catalogPath) => {
    setCatalogChoice({ mode, path }); clearPreview();
  };
  const chooseCatalog = async () => {
    setBusy('catalog'); setError('');
    try {
      const result = await manageSubscriptionConnection(provider, 'choose_model_catalog');
      if (result.modelCatalogPath) changeCatalog('local', result.modelCatalogPath);
    } catch (e) { setError(errorText(e)); }
    finally { setBusy(''); }
  };
  const selectProfile = async (value) => {
    const profile = profiles.find((item) => item.id === Number(value));
    if (busy || !profile || profileSwitchBlockReason(profile) || profile.id === status?.profileId) return;
    clearPreview(); setBusy('profile');
    try {
      await setDefaultRoutingProfile(provider, Number(value));
      await load();
      await onUpdated?.();
      setNotice('已切换默认调度方案，新请求立即使用该方案。');
    } catch (e) { setError(errorText(e)); await load(); }
    finally { setBusy(''); }
  };
  const copy = async (value) => {
    try { await navigator.clipboard.writeText(value); setNotice('已复制'); }
    catch { setError('复制失败，请手动选择并复制'); }
  };
  const close = () => { setExpanded(false); setPreview(null); setConfirmed(false); };
  const title = provider === 'codex' ? 'Codex' : 'Claude Code';
  const stalePreview = preview && (preview.baseURL !== status?.baseURL || preview.revision !== status?.revision || preview.profileId !== status?.profileId || preview.bindingVersion !== status?.bindingVersion || preview.managed !== status?.managed || preview.authEnabled !== status?.authEnabled);
  return <section className={embedded ? 'min-w-0' : 'mb-4 min-w-0 rounded-xl border border-slate-200 bg-white shadow-sm'}>
    <div className="flex min-w-0 flex-wrap items-center justify-between gap-2 px-3 py-1">
      <div className="flex min-w-0 flex-wrap items-center gap-2"><Cable size={16} className="text-indigo-600" /><h2 className="text-sm font-semibold text-slate-800">连接到 Vane</h2><span className={`rounded px-2 py-0.5 text-xs ${status?.running ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{status?.running ? '代理运行中' : status ? '代理未运行' : '状态未读取'}</span><span className="text-xs text-slate-500">{title} → Vane</span></div>
      {!embedded && <button type="button" className="flex items-center gap-1 rounded-lg px-2 py-1 text-xs text-indigo-600 hover:bg-indigo-50" aria-expanded={expanded} onClick={() => expanded ? close() : setExpanded(true)}>{expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}{expanded ? '收起配置' : '配置客户端'}</button>}
    </div>
    {(embedded || expanded) && <div className="min-w-0 space-y-1.5 border-t border-slate-100 p-2.5">
      {!embedded && <p className="text-xs leading-5 text-slate-600">为本机 {title} 生成接入配置，请求由 Vane 默认调度方案转发。</p>}
      <div aria-label="客户端接入设置" className="grid min-w-0 items-center gap-3 rounded-lg border border-slate-200 bg-slate-50/70 p-2.5 md:grid-cols-2 md:gap-4">
        <div className="min-w-0 space-y-1.5 border-b border-slate-200 pb-3 md:border-r md:border-b-0 md:pr-4 md:pb-0">
          <div className="flex min-w-0 flex-wrap items-center justify-between gap-1.5">
            <span className="text-xs font-medium text-slate-700">默认调度方案</span>
            {status?.endpointReady && <span className="flex items-center gap-1 text-[11px] text-emerald-600"><CheckCircle2 size={12} className="shrink-0" />新请求立即生效</span>}
          </div>
          <CustomSelect ariaLabel="默认调度方案" fullWidth wrapOptions value={status?.profileId} options={profiles.filter((profile) => profile.enabled).map((profile) => ({ value: profile.id, label: profile.name, disabled: Boolean(profileSwitchBlockReason(profile)), badge: profileSwitchBlockReason(profile) || undefined }))} onChange={selectProfile} disabled={Boolean(busy) || !status || profiles.length === 0} placeholder="请选择调度方案" />
          {!status?.endpointReady && <p className="text-xs leading-4 text-amber-700">请先检查方案成员、凭证和端点。</p>}
        </div>
        <div className="min-w-0 space-y-1.5 text-xs text-slate-600">
          <dl className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-x-2 gap-y-1.5">
            <dt className="pt-0.5 text-slate-500">接入地址</dt><dd className="min-w-0 pt-0.5"><code className="break-all">{status?.baseURL || '等待读取'}</code></dd><dd><button type="button" aria-label="复制接入地址" disabled={!status?.baseURL} onClick={() => copy(status.baseURL)} className="rounded p-0.5 text-indigo-600 hover:bg-indigo-100 disabled:opacity-40"><Copy size={13} /></button></dd>
            <dt className="text-slate-500">目标文件</dt><dd className="col-span-2 min-w-0"><code className="break-all">{status?.configPath || '等待读取'}</code></dd>
          </dl>
          {status && <p className="leading-4 text-slate-500">{status.authEnabled ? '自动使用 Vane 代理令牌。' : 'Vane 未开启代理鉴权。'}{status.profileId > 0 && (status.managed ? '上游凭证由 Vane 提供。' : `需要 ${title} 提供官方登录凭证。`)}</p>}
        </div>
      </div>
      {provider === 'codex' && <fieldset className="min-w-0 space-y-2 rounded-lg border border-slate-200 p-2.5 [&>legend+*]:mt-0" disabled={Boolean(busy) || !status}>
        <legend className="px-1 text-xs font-medium text-slate-700">模型目录来源</legend>
        <div className="grid min-w-0 gap-2 sm:grid-cols-2">
          {[
            { value: 'local', label: '本地模型文件', description: '启动时读取所选 JSON，不发起远程目录查询。', icon: FileJson2 },
            { value: 'remote', label: '远程模型目录', description: '由 Codex 通过 Vane 获取和刷新模型目录。', icon: Globe2 }
          ].map(({ value, label, description, icon: Icon }) => <label key={value} className={`flex min-w-0 cursor-pointer items-start gap-2 rounded-lg border p-2 transition-colors ${catalogMode === value ? 'border-indigo-300 bg-indigo-50/60' : 'border-slate-200 bg-white hover:border-slate-300'} ${busy || !status ? 'cursor-default opacity-60' : ''}`}>
            <input type="radio" name={`model-catalog-${embedded ? 'dialog' : 'panel'}`} value={value} checked={catalogMode === value} onChange={() => changeCatalog(value)} className="mt-0.5 shrink-0 accent-indigo-600" />
            <span className="min-w-0 space-y-1"><span className="flex items-center gap-1.5 text-xs font-medium text-slate-800"><Icon size={14} className="shrink-0 text-indigo-600" />{label}</span><span className="block text-xs leading-5 text-slate-500">{description}</span></span>
          </label>)}
        </div>
        {catalogMode === 'local' ? <div className="min-w-0 space-y-2">
          <label htmlFor={`catalog-path-${embedded ? 'dialog' : 'panel'}`} className="block text-xs font-medium text-slate-700">模型文件路径</label>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <input id={`catalog-path-${embedded ? 'dialog' : 'panel'}`} type="text" value={catalogPath} onChange={(e) => changeCatalog('local', e.target.value)} placeholder="~/.codex/codex-models.json" spellCheck={false} className="h-9 min-w-0 flex-1 basis-64 rounded-lg border border-slate-200 bg-white px-3 text-xs text-slate-700 outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 disabled:bg-slate-50" />
            <Button type="button" variant="secondary" className="gap-1.5" onClick={chooseCatalog}><FolderOpen size={14} />{busy === 'catalog' ? '正在选择…' : '选择文件'}</Button>
          </div>
          <p className="text-xs leading-5 text-slate-500">支持 ~/ 路径。请选择 Codex 模型目录 JSON；修改文件后需重启 Codex。</p>
        </div> : <div className="min-w-0 space-y-1.5 text-xs">
          <span className="block font-medium text-slate-700">模型目录地址</span>
          <code className="block min-w-0 break-all rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-slate-600">{status?.baseURL ? `${status.baseURL.replace(/\/$/, '')}/models` : '等待读取'}</code>
          <p className="leading-5 text-slate-500">目录地址随 Vane 接入地址自动更新。</p>
        </div>}
      </fieldset>}
      {status?.managed === false && <div className="space-y-2 border-l-2 border-amber-300 pl-3 text-xs leading-5 text-slate-600">
        <p className="font-medium text-slate-700">客户端官方登录接入步骤</p>
        <ol className="list-decimal space-y-1 pl-4">
          <li>先在本机 {title} 完成官方账号登录。</li>
          <li>生成下方配置预览，核对后点击「备份并应用配置」。</li>
          <li>重新启动 {title}，由 Vane 转发请求。</li>
        </ol>
        <p>{provider === 'codex' ? '专用模板会自动添加 X-Relay-Key，用于连接 Vane；API 渠道和 Vane 托管账号的默认模板无需此项。' : '专用模板会自动补充连接 Vane 的认证配置。'}</p>
      </div>}
      <div className="flex flex-wrap gap-2"><Button type="button" size="sm" className="gap-1.5" variant="secondary" onClick={load} disabled={Boolean(busy)}><RefreshCw size={14} />刷新状态</Button><Button ref={previewTriggerRef} type="button" size="sm" className="gap-1.5" onClick={() => run('preview')} disabled={Boolean(busy) || !status?.endpointReady || (provider === 'codex' && catalogMode === 'local' && !catalogPath.trim())}><FileCode2 size={14} />{busy === 'preview' ? '正在生成…' : '生成配置预览'}</Button><Button type="button" size="sm" className="gap-1.5" variant="secondary" onClick={() => run('restore')} disabled={Boolean(busy) || !status?.backupAvailable}><RotateCcw size={14} />恢复原客户端配置</Button></div>
      {!embedded && <p className="text-xs leading-5 text-slate-500">在预览窗口核对配置后，可备份并写入目标文件。</p>}
      {error && <p role="alert" className="break-words text-xs text-rose-600 [overflow-wrap:anywhere]">{error}</p>}
      {statusError && <p role="alert" className="break-words text-xs text-rose-600 [overflow-wrap:anywhere]">{statusError}</p>}
      {notice && <p role="status" className="flex items-start gap-1 break-words text-xs text-emerald-700 [overflow-wrap:anywhere]"><Check size={14} className="shrink-0" />{notice}</p>}
    </div>}
    {preview && <SubscriptionConnectionPreview provider={provider} preview={preview} confirmed={confirmed} onConfirmedChange={setConfirmed} busy={busy} stale={stalePreview} canApply={Boolean(status?.running && status?.endpointReady)} error={error} notice={notice} onCopy={copy} onRegenerate={() => run('preview')} onApply={() => run('apply')} onClose={() => { setPreview(null); setConfirmed(false); }} triggerRef={previewTriggerRef} />}
  </section>;
}
