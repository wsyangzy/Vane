import SubscriptionConnectionPanel from '@/features/providers/shared/subscriptions/SubscriptionConnectionPanel.jsx';
import { useCallback, useEffect, useState } from 'react';
import { ArrowDown, ArrowRightLeft, ArrowUp, Check, ChevronDown, Copy, KeyRound, Layers, Pencil, Plus, RefreshCw, Server, Star, Trash2, X } from 'lucide-react';
import { Button, CustomSelect, ErrorMessage } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { copyToClipboard } from '@/services/wails/index.js';
import { deleteAccountPool, listAccountPools, rotateAccountPoolKey, saveAccountPool, setPoolAccountEnabled } from '@/services/wails/pools.js';

import { PoolContext, usePoolAccounts } from './poolContext.js';
const memberKey = (member) => `${member.channelId}/${member.groupId}`;
const control = 'h-8 min-w-0 rounded-lg border border-slate-200 bg-white px-2.5 text-sm outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 disabled:opacity-50';
const iconButton = 'inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-slate-500 hover:bg-slate-100 hover:text-slate-900 disabled:opacity-30';
const tiers = { primary: '主要', backup: '备用' };
const cooldownLabels = {
  quota_exceeded: '账号额度暂不可用', usage_limit_reached: '账号额度暂不可用',
  network_error: '端点连接失败', endpoint_unavailable: '端点暂不可用', upstream_unavailable: '上游服务暂不可用',
  credential_rejected: '凭证失效，请更新或重新登录', model_not_found: '当前端点不支持此模型',
  model_not_available: '此模型暂不可用', model_not_supported: '当前端点不支持此模型',
  recovery_unconfirmed: '恢复尚未验证', recovery_persistence_failed: '恢复状态保存失败，稍后重试'
};

export function AccountSwitch({ channelId, groupId }) {
  const context = usePoolAccounts();
  const account = context?.accounts.find((item) => item.channelId === Number(channelId) && item.groupId === Number(groupId));
  return <button type="button" role="switch" aria-label={`全局启停 ${account?.groupName || '账号'}`} aria-checked={account?.enabled || false} disabled={!account || context.busy} onClick={() => context.toggleAccount(account)} title="对所有账号池和直接路由生效" className={`relative h-5 w-9 shrink-0 rounded-full transition-colors disabled:opacity-40 ${account?.enabled ? 'bg-indigo-600' : 'bg-slate-300'}`}><span className={`absolute top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform ${account?.enabled ? 'left-0.5 translate-x-4' : 'left-0.5'}`} /></button>;
}

export default function PoolWorkspace({ provider, children, refreshToken }) {
  const [state, setState] = useState({ pools: [], accounts: [], cooldowns: [] });
  const [tab, setTab] = useState(() => { try { return localStorage.getItem(`vane.${provider}.managementTab`) === 'channels' ? 'channels' : 'pools'; } catch { return 'pools'; } });
  const [selectedId, setSelectedId] = useState(0);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(null);
  const [keyResult, setKeyResult] = useState(null);
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [confirmRotate, setConfirmRotate] = useState(null);
  const [coolingOpen, setCoolingOpen] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const reload = useCallback(async () => {
    try { setState(await listAccountPools(provider)); setError(''); }
    catch (err) { setError(err.message || '加载账号池失败'); }
    finally { setLoading(false); }
  }, [provider]);
  useEffect(() => { reload(); const timer = setInterval(reload, 15000); return () => clearInterval(timer); }, [reload, refreshToken]);
  useEffect(() => { const timer = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, []);
  const selected = state.pools.find((pool) => pool.id === selectedId) || state.pools.find((pool) => pool.isDefault) || state.pools[0];
  const mutate = async (operation) => { if (busy) return; setBusy(true); setError(''); try { await operation(); await reload(); } catch (err) { setError(err.message || '保存失败'); } finally { setBusy(false); } };
  const save = (pool) => mutate(async () => { const id = await saveAccountPool(pool); setSelectedId(id); setEditing(null); });
  const toggleAccount = (account) => mutate(() => setPoolAccountEnabled(provider, account.channelId, account.groupId, !account.enabled));
  const updateMembers = (members) => save({ ...selected, members });
  const accountFor = (member) => state.accounts.find((account) => memberKey(account) === memberKey(member));
  const memberStates = (member) => state.cooldowns.filter((item) => memberKey(item) === memberKey(member));
  const cooling = selected?.members.filter((member) => memberStates(member).length > 0) || [];
  const reorder = (member, delta) => { const members = [...selected.members]; const at = members.findIndex((item) => memberKey(item) === memberKey(member)); let to = at + delta; while (to >= 0 && to < members.length && members[to].tier !== member.tier) to += delta; if (to < 0 || to >= members.length) return; [members[at], members[to]] = [members[to], members[at]]; updateMembers(members); };
  const context = { accounts: state.accounts, busy, toggleAccount };
  return <PoolContext.Provider value={context}>
    <div className="mb-3 flex min-h-8 flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b border-slate-200 text-xs">
      <div className="flex items-center gap-4" role="tablist" aria-label="管理视图">{[['pools', '账号池', Layers], ['channels', '渠道', Server]].map(([value, label, Icon]) => <button key={value} type="button" role="tab" aria-selected={tab === value} onClick={() => { setTab(value); try { localStorage.setItem(`vane.${provider}.managementTab`, value); } catch { /* Storage is optional in restricted WebViews. */ } }} className={`flex h-9 items-center gap-1.5 border-b-2 px-1 font-medium ${tab === value ? 'border-indigo-600 text-indigo-600' : 'border-transparent text-slate-500 hover:text-slate-900'}`}><Icon size={14} />{label}<span className="text-[11px] tabular-nums text-slate-400">{value === 'pools' ? state.pools.length : new Set(state.accounts.map((item) => item.channelId)).size}</span></button>)}</div>
      <div className="flex flex-wrap items-center gap-3 pb-1 text-slate-500"><span><b className="font-medium tabular-nums text-slate-800">{state.accounts.filter((item) => item.enabled).length}</b> 启用账号</span><span><b className="font-medium tabular-nums text-amber-600">{state.cooldowns.length}</b> 冷却项</span><span className="text-slate-400">请求内自动调度</span></div>
    </div>
    {error && <div className="mb-3"><ErrorMessage message={error} onClose={() => setError('')} /></div>}
    {tab === 'channels' ? children : <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2"><CustomSelect ariaLabel="选择账号池" value={selected?.id || 0} options={state.pools.map((pool) => ({ value: pool.id, label: `${pool.name}${pool.isDefault ? ' · 默认' : ''}` }))} onChange={setSelectedId} placeholder={loading ? '加载中…' : '暂无账号池'} minWidth={180} />{selected && <span className="text-xs text-slate-500">{selected.strategy === 'round_robin' ? '轮询' : '顺序兜底'}</span>}</div>
        <div className="flex items-center gap-1">
          <button type="button" className={iconButton} aria-label="刷新账号池" onClick={reload}><RefreshCw size={14} /></button>
          {selected && <><button type="button" className={iconButton} aria-label="设为默认入口" title="设为默认入口" disabled={busy || selected.isDefault} onClick={() => save({ ...selected, isDefault: true })}><Star size={14} className={selected.isDefault ? 'fill-amber-400 text-amber-500' : ''} /></button><button type="button" className={iconButton} aria-label="配置池接入密钥" title={selected.hasKey ? '轮换池接入密钥' : '生成池接入密钥'} disabled={busy} onClick={() => selected.hasKey ? setConfirmRotate(selected) : mutate(async () => setKeyResult(await rotateAccountPoolKey(provider, selected.id)))}><KeyRound size={14} /></button><button type="button" className={iconButton} aria-label="编辑账号池" onClick={() => setEditing(structuredClone(selected))}><Pencil size={14} /></button><button type="button" className={iconButton} aria-label="删除账号池" disabled={busy || selected.isDefault} onClick={() => setConfirmDelete(selected)}><Trash2 size={14} /></button></>}
          <Button size="sm" icon={Plus} onClick={() => setEditing({ id: 0, provider, name: '', strategy: 'sequential', isDefault: !state.pools.length, members: [], rules: [], nonStream: null })}>新建池</Button>
        </div>
      </div>
      {selected ? <>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">{Object.entries(tiers).map(([tier, label]) => <section key={tier} className="min-w-0 rounded-xl border border-slate-200 bg-white">
          <div className="flex h-9 items-center justify-between border-b border-slate-100 px-3"><h2 className="text-xs font-semibold text-slate-700">{label}<span className="ml-2 font-normal tabular-nums text-slate-400">{selected.members.filter((member) => member.tier === tier).length}</span></h2><button type="button" className={iconButton} title={`添加${label}账号`} aria-label={`添加${label}账号`} onClick={() => setEditing({ ...structuredClone(selected), editTier: tier })}><Plus size={14} /></button></div>
          <div className="divide-y divide-slate-100">{selected.members.filter((member) => member.tier === tier).map((member, index, members) => { const account = accountFor(member); const states = memberStates(member); return <div key={memberKey(member)} className={`flex min-h-12 items-center gap-2 px-3 py-2 ${account?.enabled ? '' : 'bg-slate-50/70'}`}>
            <span className="w-3 shrink-0 text-[11px] tabular-nums text-slate-400">{index + 1}</span><span className={`h-1.5 w-1.5 shrink-0 rounded-full ${!account?.enabled ? 'bg-slate-300' : states.length ? 'bg-amber-400' : 'bg-emerald-500'}`} />
            <div className="min-w-0 flex-1"><div className="truncate text-sm font-medium text-slate-800" title={account?.groupName}>{account?.groupName || '账号已删除'}</div><div className="truncate text-xs text-slate-400">{account?.channelName || `渠道 ${member.channelId}`}{account?.managed ? ' · 订阅' : ''}{states.length ? ' · 冷却中' : ''}</div></div>
            <div className="flex shrink-0 items-center"><button className={iconButton} type="button" disabled={busy || index === 0} aria-label="上移账号" onClick={() => reorder(member, -1)}><ArrowUp size={13} /></button><button className={iconButton} type="button" disabled={busy || index === members.length - 1} aria-label="下移账号" onClick={() => reorder(member, 1)}><ArrowDown size={13} /></button><button className={iconButton} type="button" disabled={busy} title={`移至${tier === 'primary' ? '备用' : '主要'}`} aria-label="切换主备层" onClick={() => updateMembers(selected.members.map((item) => memberKey(item) === memberKey(member) ? { ...item, tier: tier === 'primary' ? 'backup' : 'primary' } : item))}><ArrowRightLeft size={13} /></button><button className={iconButton} type="button" disabled={busy} title="从当前池移出" aria-label="从当前池移出" onClick={() => updateMembers(selected.members.filter((item) => memberKey(item) !== memberKey(member)))}><X size={13} /></button></div>
            <AccountSwitch channelId={member.channelId} groupId={member.groupId} />
          </div>; })}{!selected.members.some((member) => member.tier === tier) && <button type="button" className="flex w-full items-center justify-center gap-1.5 py-6 text-xs text-slate-400 hover:text-indigo-600" onClick={() => setEditing({ ...structuredClone(selected), editTier: tier })}><Plus size={14} />添加{label}账号</button>}</div>
        </section>)}</div>
        <section className="overflow-hidden rounded-xl border border-slate-200 bg-white"><button type="button" onClick={() => setCoolingOpen(!coolingOpen)} aria-expanded={coolingOpen} className="flex h-9 w-full items-center justify-between px-3 text-xs text-slate-600"><span>自动冷却 <span className="ml-1.5 tabular-nums text-amber-600">{cooling.length}</span><span className="ml-3 text-slate-400">恢复后回到原层</span></span><ChevronDown size={14} className={coolingOpen ? 'rotate-180' : ''} /></button>{coolingOpen && <div className="divide-y divide-slate-100 border-t border-slate-100">{cooling.flatMap((member) => memberStates(member).map((item, index) => <div key={`${memberKey(member)}/${index}`} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-xs"><span className="font-medium text-slate-700">{accountFor(member)?.groupName}</span><span className="text-slate-400">{tiers[member.tier]} · {item.model || '全部模型'}</span><span className="min-w-0 flex-1 break-words text-amber-700" title={item.reason}>{cooldownLabels[item.reason] || (item.repair ? '凭证需要修复' : '暂不可用，等待恢复验证')}</span><span className="text-slate-500">{item.repair ? '待修复 / 重新登录' : item.probing ? '验证中' : new Date(item.until).getTime() <= now ? '到期待验证' : `${Math.ceil((new Date(item.until).getTime() - now) / 1000)} 秒后验证`}</span></div>))}{!cooling.length && <p className="px-3 py-3 text-xs text-slate-400">当前池没有冷却账号</p>}</div>}</section>
        <SubscriptionConnectionPanel key={selected.id} provider={provider} pool={selected} onUpdated={reload} />
        <div className="flex flex-wrap items-center gap-3 px-1 text-xs text-slate-400"><span>模型路由 {selected.rules.length} 条</span><span>非流改道 {selected.nonStream ? '已开启' : '未开启'}</span><button type="button" onClick={() => setEditing({ ...structuredClone(selected), editTab: 'routing' })} className="inline-flex items-center gap-1 text-indigo-600"><Pencil size={12} />配置规则</button></div>
      </> : !loading && <div className="rounded-xl border border-dashed border-slate-300 py-12 text-center"><Layers size={28} className="mx-auto mb-3 text-slate-300" /><p className="text-sm text-slate-600">创建账号池，编排主要与备用账号</p><p className="mt-1 text-xs text-slate-400">先在渠道中添加账号，再将账号加入池</p></div>}
    </div>}
    {editing && <PoolEditor key={editing.id} pool={editing} accounts={state.accounts} pools={state.pools} busy={busy} error={error} onClose={() => setEditing(null)} onSave={save} />}
    {confirmRotate && <PoolDialog title="轮换池接入密钥" onClose={() => setConfirmRotate(null)}><p className="text-sm leading-6 text-slate-600">轮换「{confirmRotate.name}」的密钥后，旧密钥立即失效。请将使用此池的客户端更新为新密钥。</p>{error && <div className="mt-3"><ErrorMessage message={error} /></div>}<div className="mt-4 flex justify-end gap-2"><Button size="sm" variant="secondary" onClick={() => setConfirmRotate(null)}>取消</Button><Button size="sm" icon={KeyRound} loading={busy} onClick={() => mutate(async () => { setKeyResult(await rotateAccountPoolKey(provider, confirmRotate.id)); setConfirmRotate(null); })}>轮换密钥</Button></div></PoolDialog>}
    {keyResult && <PoolDialog title="保存池接入密钥" onClose={() => setKeyResult(null)}><p className="mb-3 text-sm text-slate-600">此密钥仅展示一次。客户端使用 <code>X-Relay-Key</code> 请求头连接此池，托管订阅账号会按所选成员认证。</p><code className="block break-all rounded-lg bg-slate-50 p-3 text-xs text-slate-800">{keyResult.key}</code><div className="mt-4 flex justify-end gap-2"><Button size="sm" variant="secondary" icon={Copy} onClick={() => copyToClipboard(keyResult.key)}>复制</Button><Button size="sm" icon={Check} onClick={() => setKeyResult(null)}>已保存</Button></div></PoolDialog>}
    {confirmDelete && <PoolDialog title="删除账号池" onClose={() => setConfirmDelete(null)}><p className="text-sm text-slate-600">删除「{confirmDelete.name}」及其成员编排，账号仍保留在渠道中。</p><div className="mt-4 flex justify-end gap-2"><Button size="sm" variant="secondary" onClick={() => setConfirmDelete(null)}>取消</Button><Button size="sm" icon={Trash2} loading={busy} onClick={() => mutate(async () => { await deleteAccountPool(provider, confirmDelete.id); setConfirmDelete(null); })}>删除</Button></div></PoolDialog>}
  </PoolContext.Provider>;
}

function PoolDialog({ title, children, onClose }) {
  useEffect(() => { const close = (event) => { if (event.key === 'Escape') onClose(); }; document.addEventListener('keydown', close); return () => document.removeEventListener('keydown', close); }, [onClose]);
  return renderModalPortal(<div className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-900/35 px-4 pb-4 pt-12" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}><section role="dialog" aria-modal="true" aria-label={title} className="flex max-h-full w-full max-w-2xl flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-xl"><div className="flex h-12 shrink-0 items-center justify-between border-b border-slate-100 px-4"><h2 className="text-base font-semibold text-slate-900">{title}</h2><button className={iconButton} type="button" aria-label="关闭" onClick={onClose}><X size={16} /></button></div><div className="overflow-y-auto p-4">{children}</div></section></div>);
}

function PoolEditor({ pool, accounts, pools, busy, error, onClose, onSave }) {
  const [draft, setDraft] = useState(() => ({ ...pool, rules: pool.rules.map((rule) => ({ ...rule, targets: rule.targets.map((target) => ({ ...target, rowId: crypto.randomUUID() })) })), nonStream: pool.nonStream ? { ...pool.nonStream, targets: pool.nonStream.targets.map((target) => ({ ...target, rowId: crypto.randomUUID() })) } : null }));
  const [tab, setTab] = useState(pool.editTab || 'basic');
  const [tier, setTier] = useState(pool.editTier || 'primary');
  const update = (changes) => setDraft((previous) => ({ ...previous, ...changes }));
  return <PoolDialog title={pool.id ? `编辑 ${pool.name}` : '新建账号池'} onClose={onClose}>
    <div className="mb-4 flex gap-1 rounded-lg bg-slate-100 p-1">{[['basic', '基础信息'], ['routing', '请求路由']].map(([value, label]) => <button key={value} type="button" onClick={() => setTab(value)} className={`flex-1 rounded-md py-1.5 text-xs font-medium ${tab === value ? 'bg-white text-indigo-600 shadow-sm' : 'text-slate-500'}`}>{label}</button>)}</div>
    {tab === 'basic' ? <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3"><label className="space-y-1.5 text-xs text-slate-600"><span>池名称</span><input value={draft.name} maxLength={100} onChange={(event) => update({ name: event.target.value })} className={`${control} w-full`} /></label><div className="space-y-1.5 text-xs text-slate-600"><span className="block">调度策略</span><CustomSelect ariaLabel="调度策略" fullWidth value={draft.strategy} options={[{ value: 'sequential', label: '顺序兜底' }, { value: 'round_robin', label: '轮询' }]} onChange={(strategy) => update({ strategy })} /></div></div>
      <label className="flex items-center gap-2 text-xs text-slate-600"><input type="checkbox" checked={draft.isDefault} disabled={pool.isDefault} onChange={(event) => update({ isDefault: event.target.checked })} />用作默认入口池</label>
      <div className="flex items-center justify-between"><span className="text-xs font-semibold text-slate-700">选择入池账号</span><CustomSelect ariaLabel="新增成员层级" value={tier} options={Object.entries(tiers).map(([value, label]) => ({ value, label }))} onChange={setTier} /></div>
      <div className="max-h-60 divide-y divide-slate-100 overflow-y-auto rounded-lg border border-slate-200">{accounts.map((account) => { const member = draft.members.find((item) => memberKey(item) === memberKey(account)); return <label key={memberKey(account)} className="flex cursor-pointer items-center gap-3 px-3 py-2 hover:bg-slate-50"><input type="checkbox" checked={Boolean(member)} onChange={(event) => update({ members: event.target.checked ? [...draft.members, { channelId: account.channelId, groupId: account.groupId, tier }] : draft.members.filter((item) => memberKey(item) !== memberKey(account)) })} /><span className="min-w-0 flex-1 truncate text-sm text-slate-700">{account.groupName}<span className="ml-2 text-xs text-slate-400">{account.channelName}{account.managed ? ' · 订阅' : ''}{!account.enabled ? ' · 已停用' : ''}</span></span>{member && <span className="text-xs text-indigo-600">{tiers[member.tier]}</span>}</label>; })}{!accounts.length && <p className="p-3 text-xs text-slate-400">请先在渠道中添加账号</p>}</div>
    </div> : <PoolRulesEditor draft={draft} pools={pools} update={update} />}
    {error && <div className="mt-3"><ErrorMessage message={error} /></div>}
    <div className="mt-5 flex justify-end gap-2 border-t border-slate-100 pt-3"><Button size="sm" variant="secondary" onClick={onClose}>取消</Button><Button size="sm" icon={Check} loading={busy} disabled={!draft.name.trim()} onClick={() => onSave(draft)}>保存</Button></div>
  </PoolDialog>;
}

function PoolRulesEditor({ draft, pools, update }) {
  const targets = pools.map((pool) => ({ value: pool.id, label: pool.name }));
  const updateRule = (index, changes) => update({ rules: draft.rules.map((rule, at) => at === index ? { ...rule, ...changes } : rule) });
  const renderTarget = (value, onChange) => <div className="space-y-2">{value.targets.map((target, index) => <div className="flex min-w-0 items-center gap-2" key={target.rowId}><div className="min-w-0 flex-1"><CustomSelect ariaLabel="目标账号池" fullWidth value={target.poolId} options={targets} placeholder={target.channelId ? `兼容目标 · 渠道 ${target.channelId}` : '选择目标池'} onChange={(poolId) => onChange({ ...value, targets: value.targets.map((item, at) => at === index ? { ...item, poolId, channelId: 0, groupId: 0 } : item) })} /></div><input className={`${control} w-40`} aria-label="上游模型（可选）" placeholder="上游模型（可选）" value={target.upstreamModel} onChange={(event) => onChange({ ...value, targets: value.targets.map((item, at) => at === index ? { ...item, upstreamModel: event.target.value } : item) })} /><button type="button" className={iconButton} aria-label="移除目标" onClick={() => onChange({ ...value, targets: value.targets.filter((_, at) => at !== index) })}><X size={14} /></button></div>)}<button type="button" className="inline-flex items-center gap-1 text-xs text-indigo-600" onClick={() => onChange({ ...value, targets: [...value.targets, { poolId: 0, upstreamModel: '', rowId: crypto.randomUUID() }] })}><Plus size={13} />添加目标池</button></div>;
  return <div className="space-y-4"><p className="text-xs leading-5 text-slate-500">模型规则优先于非流改道。目标池仅展开成员，规则不会递归执行。</p>
    <div className="flex items-center justify-between"><h3 className="text-xs font-semibold text-slate-700">模型路由</h3><button type="button" className={iconButton} aria-label="新增模型规则" onClick={() => update({ rules: [...draft.rules, { id: Math.max(0, ...draft.rules.map((rule) => rule.id)) + 1, name: '', disabled: false, priority: draft.rules.length, matchModel: '', mode: 'prefer', targets: [] }] })}><Plus size={14} /></button></div>
    {draft.rules.map((rule, index) => <div key={rule.id} className="space-y-2 rounded-lg border border-slate-200 p-3"><div className="flex flex-wrap gap-2"><label className="flex items-center gap-1 text-xs text-slate-500"><input type="checkbox" aria-label="启用规则" checked={!rule.disabled} onChange={(event) => updateRule(index, { disabled: !event.target.checked })} />启用</label><input type="number" aria-label="规则优先级" className={`${control} w-20`} value={rule.priority} onChange={(event) => updateRule(index, { priority: event.target.value })} /><input className={`${control} w-32`} aria-label="规则名称" placeholder="规则名称" value={rule.name} onChange={(event) => updateRule(index, { name: event.target.value })} /><input className={`${control} flex-1`} aria-label="匹配模型" placeholder="模型，如 gpt-*" value={rule.matchModel} onChange={(event) => updateRule(index, { matchModel: event.target.value })} /><button type="button" className={iconButton} aria-label="删除规则" onClick={() => update({ rules: draft.rules.filter((_, at) => at !== index) })}><Trash2 size={14} /></button></div><CustomSelect ariaLabel="模型路由顺序" value={rule.mode} options={[{ value: 'prefer', label: '目标池优先，失败回入口池' }, { value: 'fallback', label: '入口池优先，失败尝试目标池' }]} onChange={(mode) => updateRule(index, { mode })} />{renderTarget(rule, (changes) => updateRule(index, changes))}</div>)}
    <label className="flex items-center justify-between border-t border-slate-100 pt-3 text-xs font-semibold text-slate-700">非流改道<input type="checkbox" checked={Boolean(draft.nonStream)} onChange={(event) => update({ nonStream: event.target.checked ? { mode: 'prefer', targets: [] } : null })} /></label>
    {draft.nonStream && <div className="space-y-2 rounded-lg border border-slate-200 p-3"><CustomSelect ariaLabel="非流改道顺序" value={draft.nonStream.mode} options={[{ value: 'prefer', label: '直接使用目标池' }, { value: 'fallback', label: '入口池失败后使用目标池' }]} onChange={(mode) => update({ nonStream: { ...draft.nonStream, mode } })} />{renderTarget(draft.nonStream, (nonStream) => update({ nonStream }))}</div>}
  </div>;
}
