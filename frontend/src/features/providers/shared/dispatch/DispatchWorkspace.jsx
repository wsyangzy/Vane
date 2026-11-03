import { getErrorMessage } from '@/shared/lib/errorMessage.js';
import Tooltip from '@/shared/ui/Tooltip.jsx';
import { useCallback, useEffect, useId, useRef, useState } from 'react';
import { Activity, ArrowLeft, ArrowUpRight, Ban, Layers, ListTree, Plus, RefreshCw, Server, ShieldCheck, Star } from 'lucide-react';
import { Button, ErrorMessage } from '@/shared/ui/index.js';
import { deleteRoutingProfile, checkRoutingConfiguration, listDispatchTraces, listQuotaSnapshots, listManualBalances, updateManualBalance, listRoutingProfiles, refreshQuota, saveRoutingProfile, setDefaultRoutingProfile } from '@/services/wails/dispatch.js';
import ProfileEditor from './ProfileEditor.jsx';
import DeleteProfileConfirmDialog from './DeleteProfileConfirmDialog.jsx';
import ProfileOverview from './ProfileOverview.jsx';
import PreviewPanel from './PreviewPanel.jsx';
import QuotaHealthWorkspace from './QuotaHealthWorkspace.jsx';
import DispatchTabs from './DispatchTabs.jsx';
import { newRoutingProfile, profileChannelIds, summarizeIntentTraces } from './dispatchState.js';
import { getSettingsByCategory } from '@/services/wails/settings.js';
import { readDispatchPolicy } from '@/shared/lib/dispatchPolicy.js';

const iconButton = 'inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-slate-500 hover:bg-slate-100 hover:text-slate-800 disabled:opacity-30';
const tabs = [['profiles', '调度方案', Layers], ['resources', '渠道资源', Server], ['quota', '额度与健康', Activity]];
const emptyState = { profiles: [], channels: [], accounts: [], candidates: [], quotas: [], balances: [] };

function ConfigurationNotice({ status }) {
  const failures = status?.checks.filter((check) => !check.passed) || [];
  return <section aria-label="当前方案配置状态" className={`dispatch-configuration rounded-xl border p-3 ${status && !status.ready ? 'border-amber-200 bg-amber-50/60' : 'border-slate-200 bg-white'}`}>
    <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
      <div className="flex min-w-0 items-center gap-2 text-xs"><ShieldCheck size={14} className={status?.ready ? 'text-emerald-500' : 'text-amber-500'} /><span className="font-medium text-slate-700">{status ? status.ready ? '当前方案配置就绪' : '当前方案需要完善' : '当前方案配置尚未检查'}</span></div>
    </div>
    {failures.length > 0 && <ul className="mt-2 space-y-1 text-xs leading-5 text-amber-800">{failures.map((check) => <li key={check.code} className="wrap-anywhere">{check.message || check.code}</li>)}</ul>}
  </section>;
}

export default function DispatchWorkspace({ provider, onError, onSuccess, onManageChannels, onViewRequests, resourceActions, onRefreshResources, resourcesBusy = false, children, refreshToken, endpoints = [] }) {
  const [state, setState] = useState(emptyState);
  const [policy, setPolicy] = useState(null);
  const [status, setStatus] = useState(null);
  const [tab, setTab] = useState('profiles');
  const tabsId = useId();
  const [selectedId, setSelectedId] = useState(0);
  const [editing, setEditing] = useState(null);
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState('');
  const [traces, setTraces] = useState([]);
  const [statisticsLoaded, setStatisticsLoaded] = useState(false);
  const [statisticsError, setStatisticsError] = useState('');
  const [quotaModel, setQuotaModel] = useState('');
  const generation = useRef(0);
  const reload = useCallback(async () => {
    const current = ++generation.current;
    const results = await Promise.allSettled([listRoutingProfiles(provider), checkRoutingConfiguration(provider), listDispatchTraces(provider, 100), getSettingsByCategory('retry').then(readDispatchPolicy)]);
    if (current !== generation.current) return;
    if (results[0].status === 'fulfilled') setState(results[0].value);
    if (results[1].status === 'fulfilled') setStatus(results[1].value);
    setPolicy(results[3].status === 'fulfilled' ? results[3].value : null);
    setTraces(results[2].status === 'fulfilled' ? results[2].value : []);
    setStatisticsLoaded(results[2].status === 'fulfilled');
    setStatisticsError(results[2].status === 'rejected' ? results[2].reason?.message || String(results[2].reason) : '');
    const failure = [results[0], results[1], results[3]].find((result) => result.status === 'rejected');
    setError(failure ? getErrorMessage(failure.reason, '读取调度配置失败') : '');
    setLoading(false);
  }, [provider]);
  useEffect(() => { reload(); }, [reload, refreshToken]);
  const perform = async (operation, message = '') => {
    if (busy) return false;
    setBusy(true); setError('');
    try { await operation(); if (message) onSuccess?.(message); return true; }
    catch (err) { const message = getErrorMessage(err, '操作失败'); setError(message); onError?.(message); return false; }
    finally { setBusy(false); }
  };
  const selected = state.profiles.find((profile) => profile.id === selectedId) || state.profiles.find((profile) => profile.isDefault) || state.profiles[0];
  const reloadStatistics = () => perform(async () => {
    const current = generation.current;
    try {
      const next = await listDispatchTraces(provider, 100);
      if (current !== generation.current) return;
      setTraces(next); setStatisticsLoaded(true); setStatisticsError('');
    } catch (err) {
      if (current !== generation.current) return;
      setStatisticsLoaded(false); setStatisticsError(err?.message || String(err));
      throw err;
    }
  });
  const intentStatistics = (profileId) => summarizeIntentTraces(traces, provider, profileId);
  const selectTab = (value) => {
    if (value === tab) return;
    setTab(value);
    if (value === 'quota') perform(async () => { const [quotas, balances] = await Promise.all([listQuotaSnapshots(provider), listManualBalances(provider)]); setState((previous) => ({ ...previous, quotas, balances })); });
  };
  const refreshWorkspace = () => {
    if (busy || resourcesBusy || refreshing) return;
    return perform(async () => {
      setRefreshing(true);
      try {
        const results = await Promise.allSettled([reload(), onRefreshResources?.()]);
        const failure = results.find((result) => result.status === 'rejected');
        if (failure) throw failure.reason;
      }
      finally { setRefreshing(false); }
    });
  };
  return <div className="dispatch-workspace"><div className="flex min-w-0 shrink-0 flex-wrap items-center justify-between gap-2 border-b border-slate-200"><DispatchTabs tabs={tabs} value={tab} onChange={selectTab} label={`${provider} 调度工作区`} id={tabsId} /><div className="flex shrink-0 items-center gap-2">{onViewRequests && <Button type="button" size="sm" variant="secondary" icon={ListTree} onClick={() => onViewRequests(provider)}>查看调度请求<ArrowUpRight size={12} className="text-slate-400" /></Button>}<Tooltip asChild content="刷新方案与渠道资源"><button type="button" className={iconButton} aria-label="刷新调度工作区" aria-busy={refreshing} disabled={busy || resourcesBusy} onClick={refreshWorkspace}><RefreshCw size={14} className={refreshing ? 'animate-spin' : ''} /></button></Tooltip></div></div>
    {error && <ErrorMessage message={error} onClose={() => setError('')} />}
    {loading ? <p className="py-8 text-center text-xs text-slate-400">加载调度配置…</p> : <div key={tab} id={`${tabsId}-${tab}-panel`} role="tabpanel" aria-labelledby={`${tabsId}-${tab}-tab`} className={`dispatch-tab-panel ${tab === 'profiles' ? editing ? 'dispatch-profile-editor-panel' : 'dispatch-profiles-panel' : 'space-y-3'} animate-in fade-in slide-in-from-bottom-2`}>
      {tab === 'profiles' && <>{editing ? <ProfileEditor key={editing.rowKey} profile={editing} profiles={state.profiles} channels={state.channels} accounts={state.accounts} policy={policy} busy={busy} statistics={intentStatistics(editing.id)} statisticsLoaded={statisticsLoaded} statisticsError={statisticsError} onRefreshStatistics={reloadStatistics} onCancel={() => setEditing(null)} onSave={(profile) => perform(async () => { const saved = await saveRoutingProfile(profile); setSelectedId(saved.id); setEditing(null); await reload(); }, '调度方案已保存')} /> : <><div className="flex flex-wrap items-center justify-between gap-2"><div className="flex min-w-0 items-center gap-2"><Layers size={15} className="shrink-0 text-indigo-500" /><h2 className="text-sm font-semibold text-slate-800">调度方案</h2><span className="text-xs tabular-nums text-slate-400">{state.profiles.length}</span></div><Button type="button" size="sm" variant="secondary" icon={Plus} disabled={busy || !policy} onClick={() => setEditing({ ...newRoutingProfile(provider, !state.profiles.length, policy), rowKey: crypto.randomUUID() })}>新建方案</Button></div>
        {!state.profiles.length && <div className="dispatch-profile-empty rounded-xl border border-dashed border-slate-300 bg-slate-50/60 px-4 py-8 text-center"><Layers size={22} className="mx-auto mb-2 text-slate-300" /><h3 className="text-sm font-medium text-slate-700">建立首个调度方案</h3><p className="mx-auto mt-2 max-w-lg text-xs leading-5 text-slate-400">渠道、凭证分组和端点继续保留。选择参与渠道、策略和轮次保持方式，首个方案保存后将自动作为当前方案，接管新请求。</p><Button type="button" size="sm" icon={Plus} className="mt-4" disabled={busy || !policy} onClick={() => setEditing({ ...newRoutingProfile(provider, true, policy), rowKey: crypto.randomUUID() })}>建立调度方案</Button></div>}
        {!!state.profiles.length && <div className="dispatch-profiles-grid"><nav aria-label="调度方案列表" className="dispatch-profile-list space-y-1 rounded-xl border border-slate-200 bg-white p-2">{state.profiles.map((profile) => <button type="button" key={profile.id} aria-pressed={selected?.id === profile.id} onClick={() => setSelectedId(profile.id)} className={`w-full rounded-lg px-3 py-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-indigo-400 focus-visible:ring-inset ${selected?.id === profile.id ? 'bg-indigo-50 text-indigo-700' : 'text-slate-600 hover:bg-slate-50'}`}>
          <span className="flex min-w-0 items-start gap-1.5 text-xs font-medium"><span className="min-w-0 flex-1 wrap-anywhere">{profile.name}</span>{profile.isDefault && <Star size={12} role="img" aria-label="当前调度方案" className="mt-0.5 shrink-0 fill-amber-400 text-amber-500" />}</span>
          <span className="mt-1.5 flex flex-wrap items-center justify-between gap-1.5"><span className="text-[11px] text-slate-500">{profileChannelIds(profile).length} 渠道 · {profile.members.filter((member) => member.enabled).length} 分组</span>{!profile.isDefault && <span className="dp-badge" data-tone={profile.enabled ? 'muted' : 'danger'}>{profile.enabled ? '可切换' : <><Ban size={11} />已停用</>}</span>}</span>
        </button>)}</nav>{selected && <div className="dispatch-profile-details">
          <ProfileOverview profile={selected} channels={state.channels} busy={busy}
            onSetDefault={() => perform(async () => { await setDefaultRoutingProfile(provider, selected.id); await reload(); }, `已切换至调度方案「${selected.name}」`)}
            onEdit={() => setEditing({ ...selected, rowKey: crypto.randomUUID() })}
            onDelete={() => { setError(''); setConfirmDelete(selected); }} />
          <PreviewPanel key={`${provider}/${selected.id}/${selected.revision}`} provider={provider} selected={selected} channels={state.channels} accounts={state.accounts} busy={busy} perform={perform} />
        </div>}</div>}
        <ConfigurationNotice status={status} /></>}</>}
      {tab === 'resources' && <><div className="dispatch-resource-toolbar"><p>管理渠道、凭证分组与端点；参与方式在调度方案中设置。</p><div role="group" aria-label="渠道资源操作" className="dispatch-resource-actions">{resourceActions}{onManageChannels && <Button type="button" size="sm" variant="secondary" icon={ArrowLeft} onClick={onManageChannels}>管理渠道</Button>}</div></div>{children || <p className="rounded-lg bg-slate-50 px-3 py-2 text-xs text-slate-500">请通过渠道管理入口查看和编辑凭证分组及其端点。</p>}</>}
      {tab === 'quota' && <QuotaHealthWorkspace key={provider} provider={provider} balances={state.balances} channels={state.channels} accounts={state.accounts} candidates={state.candidates} quotas={state.quotas} endpoints={endpoints} model={quotaModel} onModelChange={setQuotaModel} busy={busy}
        onRead={() => perform(async () => { const [quotas, balances] = await Promise.all([listQuotaSnapshots(provider), listManualBalances(provider)]); setState((previous) => ({ ...previous, quotas, balances })); })}
        onRefresh={(channelId, groupId, modelScope) => perform(async () => { await refreshQuota(provider, channelId, groupId, modelScope); const [quotas, balances] = await Promise.all([listQuotaSnapshots(provider), listManualBalances(provider)]); setState((previous) => ({ ...previous, quotas, balances })); })}
        onBalanceUpdate={async (input) => { await updateManualBalance(input); await reload(); onSuccess?.('手动余额已更新'); }}
        onPreview={() => setTab('profiles')} />}
    </div>}
    {confirmDelete && <DeleteProfileConfirmDialog profile={confirmDelete} busy={busy} error={error}
      onCancel={() => setConfirmDelete(null)}
      onConfirm={() => perform(async () => { await deleteRoutingProfile(provider, confirmDelete.id); setConfirmDelete(null); await reload(); }, '方案已删除')} />}
  </div>;
}
