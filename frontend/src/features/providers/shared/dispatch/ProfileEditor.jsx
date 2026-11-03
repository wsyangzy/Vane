import { useState } from 'react';
import { ArrowDown, ArrowUp, Ban, Check, CheckCircle2, GitBranch, Info, Plus, Replace, Route, Save, Sparkles, Trash2, X } from 'lucide-react';
import { Button, CustomSelect, Tooltip } from '@/shared/ui/index.js';
import IntentStatistics from './IntentStatistics.jsx';
import ProfileMembersEditor from './ProfileMembersEditor.jsx';
import { affinityOptions, candidateModeOptions, editableProfile, ruleKindLabel, setIntentRoutingEnabled, strategyOptions, validateProfileDraft } from './dispatchState.js';
import { formatDispatchDuration, ruleFallbackOptions } from '@/shared/lib/dispatchPolicy.js';
import './profileEditor.css';

const control = 'h-8 w-full min-w-0 rounded-lg border border-slate-200 bg-white px-2.5 text-xs outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 disabled:opacity-50';
const iconButton = 'inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-slate-500 hover:bg-slate-100 disabled:opacity-30';
const modes = [{ value: 'prefer', label: '优先使用规则目标' }, { value: 'fallback', label: '仅在默认编排失败后改道' }];
const triState = (value) => value == null ? 'default' : value ? 'yes' : 'no';
const fromTriState = (value) => value === 'default' ? null : value === 'yes';

function Field({ label, value, onChange, ...props }) { return <label className="block min-w-0 space-y-1 text-xs text-slate-500"><span>{label}</span><input {...props} className={control} value={value} onChange={(event) => onChange(event.target.value)} /></label>; }
function Choice({ label, value, options, onChange, disabled }) { return <div className="min-w-0 space-y-1 text-xs text-slate-500"><span>{label}</span><CustomSelect fullWidth ariaLabel={label} value={value} options={options} onChange={onChange} disabled={disabled} /></div>; }
function CheckBox({ label, value, onChange, disabled }) { return <label className="inline-flex items-center gap-2 text-xs text-slate-600"><input type="checkbox" className="accent-indigo-600" checked={value} disabled={disabled} onChange={(event) => onChange(event.target.checked)} />{label}</label>; }
function Section({ title, children, action }) { return <section className="min-w-0 rounded-xl border border-slate-200 bg-white"><div className="flex min-h-10 items-center justify-between gap-2 border-b border-slate-100 px-3"><h3 className="text-xs font-semibold text-slate-700">{title}</h3>{action}</div><div className="space-y-3 p-3">{children}</div></section>; }

function TargetsEditor({ targets, onChange, profiles, channels, accounts, profile, mapping = false }) {
  const update = (index, patch) => onChange(targets.map((target, at) => at === index ? { ...target, ...patch } : target));
  const move = (index, delta) => { const next = [...targets]; [next[index], next[index + delta]] = [next[index + delta], next[index]]; onChange(next); };
  const members = profile.members.filter((member) => member.enabled);
  const availableChannels = mapping ? channels.filter((channel) => members.some((member) => member.channelId === channel.id)) : channels;
  const groupsFor = (channelId) => accounts.filter((account) => account.channelId === channelId && (!mapping || members.some((member) => member.channelId === channelId && (member.groupId === 0 || member.groupId === account.groupId))));
  const initialGroup = (channelId) => mapping ? members.find((member) => member.channelId === channelId)?.groupId ?? 0 : 0;
  const addTarget = () => onChange([...targets, { rowKey: crypto.randomUUID(), profileId: 0, channelId: mapping ? availableChannels[0]?.id || 0 : 0, groupId: mapping ? initialGroup(availableChannels[0]?.id) : 0, upstreamModel: '' }]);
  return <div className="space-y-2">
    <div className="flex min-w-0 flex-wrap items-center justify-between gap-1 text-[11px] leading-5"><span className="font-medium text-slate-600">{mapping ? '映射目标' : '路由目标'} · {targets.length}</span><span className="text-slate-400">{profile.candidateMode === 'ordered_channels' ? '按上下顺序选择渠道，归属保持优先复用已选资源' : '目标汇入统一候选池，由调度策略选择'}</span></div>
    {targets.map((target, index) => {
      const useProfile = !mapping && (target.profileId || target.targetKind === 'profile');
      const allGroups = !mapping || members.some((member) => member.enabled && member.channelId === target.channelId && member.groupId === 0);
      return <div key={target.rowKey} role="group" aria-label={`路由目标 ${index + 1}`} className="dispatch-target-card">
        <div className="mb-2 flex min-w-0 items-center justify-between gap-2"><span className="flex items-center gap-2 text-[11px] font-medium text-slate-600"><span className="inline-flex h-5 min-w-5 items-center justify-center rounded bg-white px-1 font-mono text-[10px] text-slate-400 ring-1 ring-slate-200">{String(index + 1).padStart(2, '0')}</span>{profile.candidateMode === 'ordered_channels' ? index === 0 ? '优先目标' : '后备目标' : `候选目标 ${index + 1}`}</span><div className="flex shrink-0"><button type="button" className={iconButton} aria-label={`上移目标 ${index + 1}`} disabled={index === 0} onClick={() => move(index, -1)}><ArrowUp size={13} /></button><button type="button" className={iconButton} aria-label={`下移目标 ${index + 1}`} disabled={index === targets.length - 1} onClick={() => move(index, 1)}><ArrowDown size={13} /></button><button type="button" className={iconButton} aria-label={`删除目标 ${index + 1}`} onClick={() => onChange(targets.filter((_, at) => at !== index))}><X size={13} /></button></div></div>
        <div className={`dispatch-target-fields${mapping ? ' dispatch-target-fields-mapping' : ''}`}>
          {!mapping && <Choice label="目标类型" value={useProfile ? 'profile' : 'channel'} options={[{ value: 'profile', label: '调度方案' }, { value: 'channel', label: '渠道 / 分组' }]} onChange={(targetKind) => update(index, { targetKind, profileId: 0, channelId: 0, groupId: 0 })} />}
          {useProfile ? <Choice label="目标方案" value={target.profileId} options={profiles.map((item) => ({ value: item.id, label: `${item.name}${item.enabled ? '' : '（已停用）'}` }))} onChange={(profileId) => update(index, { profileId })} /> : <Choice label={mapping ? '本方案渠道' : '目标渠道'} value={target.channelId} options={availableChannels.map((channel) => ({ value: channel.id, label: channel.name }))} onChange={(channelId) => update(index, { channelId, profileId: 0, groupId: initialGroup(channelId) })} />}
          <Choice label="目标分组" disabled={Boolean(useProfile)} value={target.groupId} options={[...(allGroups ? [{ value: 0, label: '全部分组 / 方案成员' }] : []), ...groupsFor(target.channelId).map((account) => ({ value: account.groupId, label: account.groupName }))]} onChange={(groupId) => update(index, { groupId })} />
          <Field label={mapping ? '映射后的实际模型' : '实际模型（可选）'} required={mapping} placeholder={mapping ? '填写支持的模型名' : '沿用渠道模型策略'} value={target.upstreamModel} onChange={(upstreamModel) => update(index, { upstreamModel })} />
        </div>
      </div>;
    })}
    <Button type="button" size="sm" variant="ghost" icon={Plus} disabled={mapping && !availableChannels.length} onClick={addTarget}>{mapping ? '添加本渠道映射目标' : '添加目标'}</Button>
    {!mapping && <p className="text-[11px] leading-5 text-slate-400">引用其他方案时仅使用其成员，不再执行该方案的规则或意图分类。</p>}
    {mapping && !availableChannels.length && <p className="text-xs text-amber-700">请先在编排中选择参与的渠道和凭证分组。</p>}
  </div>;
}

const ruleDescriptions = {
  model: '按客户端请求的模型名选择目标渠道、分组或方案，可同时指定实际模型。',
  mapping: '模型路由中的名称替换：只选择本方案已参与的渠道 / 分组，改用该资源支持的实际模型。',
  non_stream: '只对非流式请求选择其他处理目标；保持客户端的 stream 字段，不转换请求格式。',
  composite: '同时匹配请求模型和附加条件，再选择路由目标。',
  intent: '使用意图分类结果选择目标；仅在意图路由总开关开启时生效。',
};

function RuleEditor({ rule, index, intents, intentEnabled, targetProps, policy, onChange, onDelete }) {
  const modelCondition = ['model', 'mapping', 'composite'].includes(rule.kind);
  const intentCondition = intentEnabled && ['intent', 'composite'].includes(rule.kind);
  const KindIcon = rule.kind === 'mapping' ? Replace : rule.kind === 'non_stream' ? Route : rule.kind === 'intent' ? Sparkles : GitBranch;
  const kindTone = rule.kind === 'mapping' ? 'border-teal-200/70 bg-teal-50 text-teal-700' : rule.kind === 'non_stream' ? 'border-amber-200/70 bg-amber-50 text-amber-700' : 'border-indigo-200/70 bg-indigo-50 text-indigo-600';
  const fallback = rule.fallbackToDefault ?? policy?.ruleFallbackToDefault;
  const path = rule.mode === 'fallback' ? '默认编排 → 规则目标' : fallback == null ? '规则目标 → 跟随全局回退设置' : fallback ? '规则目标 → 默认编排' : '仅尝试规则目标';
  const condition = rule.kind === 'non_stream' ? '仅非流式请求' : modelCondition ? rule.matchModel || '待填写请求模型' : intents.find((intent) => intent.id === rule.intentId)?.name || rule.intentId || '待选择意图';
  return <details open className="min-w-0 rounded-lg border border-slate-200 bg-white p-3">
    <summary className="cursor-pointer text-xs font-medium text-slate-700"><span className="ml-1 inline-flex max-w-[95%] items-center gap-2 align-middle"><span className="shrink-0 font-mono text-[10px] text-slate-400">{String(index + 1).padStart(2, '0')}</span><Tooltip content={ruleDescriptions[rule.kind]} className="shrink-0"><span className={`inline-flex items-center gap-1 rounded-md border px-2 py-1 text-[10px] ${kindTone}`}><KindIcon size={11} aria-hidden="true" />{rule.kind === 'mapping' ? '模型映射' : ruleKindLabel(rule.kind)}</span></Tooltip><span className="min-w-0 truncate font-normal">{condition}</span>{!rule.enabled && <span className="shrink-0 text-slate-400">已停用</span>}</span></summary>
    <div className="mt-3 space-y-3">
      {(modelCondition || intentCondition) && <div className={`grid gap-3 ${modelCondition && intentCondition ? 'sm:grid-cols-2' : ''}`}>{modelCondition && <Field label="客户端请求模型（支持 *）" required placeholder="如 gpt-*、claude-sonnet-*" value={rule.matchModel} onChange={(matchModel) => onChange({ matchModel })} />}{intentCondition && <Choice label="匹配意图" value={rule.intentId} options={[{ value: '', label: '不限意图' }, ...intents.map((intent) => ({ value: intent.id, label: intent.name || intent.id }))]} onChange={(intentId) => onChange({ intentId })} />}</div>}
      <TargetsEditor {...targetProps} mapping={rule.kind === 'mapping'} targets={rule.targets} onChange={(targets) => onChange({ targets })} />
      <div role="group" aria-label="执行顺序与回退" className="rounded-lg border border-slate-200 bg-slate-50/40 p-3"><h4 className="mb-3 flex items-center gap-1.5 text-[11px] font-medium text-slate-600"><Route size={13} />执行顺序与回退</h4><div className="dispatch-execution-fields"><Field label="同类规则优先级（小者优先）" type="number" value={rule.priority} onChange={(priority) => onChange({ priority })} />{rule.kind !== 'non_stream' && <Choice label="流式条件" value={triState(rule.requireStream)} options={[{ value: 'default', label: '流式 / 非流式均可' }, { value: 'yes', label: '仅流式' }, { value: 'no', label: '仅非流式' }]} onChange={(value) => onChange({ requireStream: fromTriState(value) })} />}<Choice label="目标使用时机" value={rule.mode} options={modes} onChange={(mode) => onChange({ mode })} /><Choice label="目标失败后" value={triState(rule.fallbackToDefault)} options={ruleFallbackOptions(policy)} onChange={(value) => onChange({ fallbackToDefault: fromTriState(value) })} /></div><p role="status" className="mt-3 flex flex-wrap gap-x-2 gap-y-1 border-t border-slate-200/70 pt-2 text-[11px] leading-5"><span className="text-slate-500">本轮路径</span><span className="font-medium text-indigo-600">{path}</span></p><p className="mt-1 text-[11px] leading-5 text-slate-400">同类规则优先级用于选择命中的规则；目标上下顺序用于渠道选择。全部候选失败后，等待重试由设置页「调度保护」控制。</p></div>
      <div className="flex items-center justify-between gap-2"><CheckBox label="启用规则" value={rule.enabled} onChange={(enabled) => onChange({ enabled })} /><Button type="button" size="sm" variant="ghost" icon={Trash2} onClick={onDelete}>删除规则</Button></div>
    </div>
  </details>;
}

export default function ProfileEditor({ profile, profiles, channels, accounts, policy, busy, statistics, statisticsLoaded, statisticsError, onRefreshStatistics, onSave, onCancel }) {
  const [draft, setDraft] = useState(() => editableProfile(profile));
  const [error, setError] = useState('');
  const update = (patch) => setDraft((previous) => ({ ...previous, ...patch }));
  const updateRule = (index, patch) => update({ rules: draft.rules.map((rule, at) => at === index ? { ...rule, ...patch } : rule) });
  const updateIntent = (index, patch) => update({ intents: draft.intents.map((intent, at) => at === index ? { ...intent, ...patch } : intent) });
  const classifier = draft.classifier;
  const classifierChange = (patch) => update({ classifier: { ...classifier, ...patch } });
  const intentEnabled = Boolean(classifier?.enabled);
  const strategy = strategyOptions.find((option) => option.value === draft.strategy);
  const targetProps = { profiles, channels, accounts, profile: draft };
  const visibleRules = draft.rules.map((rule, index) => ({ rule, index })).filter(({ rule }) => intentEnabled || (rule.kind !== 'intent' && !rule.intentId));
  const hiddenIntentRules = draft.rules.length - visibleRules.length;
  const availabilityChanged = draft.enabled !== profile.enabled || !draft.id;
  const availabilityLabel = availabilityChanged ? `待保存：${draft.enabled ? '启用' : '停用'}` : draft.enabled ? '已启用' : '已停用';
  const createRule = (kind) => update({ rules: [...draft.rules, { rowKey: crypto.randomUUID(), id: Math.max(0, ...draft.rules.map((rule) => rule.id)) + 1, enabled: true, kind, name: '', priority: draft.rules.length, matchModel: '', requireStream: kind === 'non_stream' ? false : null, intentId: '', mode: 'prefer', targets: [], fallbackToDefault: null }] });
  return <form className="dispatch-profile-form flex min-h-0 min-w-0 flex-1 flex-col gap-3 overflow-hidden" onSubmit={(event) => { event.preventDefault(); const message = validateProfileDraft(draft); setError(message); if (!message) onSave(draft); }}>
    <div className="flex shrink-0 flex-wrap items-center justify-between gap-2">
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
        <h2 className="min-w-0 text-sm font-semibold text-slate-800 wrap-anywhere">{draft.id ? `编辑 · ${profile.name}` : '新建调度方案'}</h2>
        <span role="status" className={`inline-flex shrink-0 items-center gap-1 rounded-md px-2 py-0.5 text-[11px] font-medium ${draft.enabled ? 'bg-emerald-50 text-emerald-700' : 'bg-rose-50 text-rose-700'}`}>
          {draft.enabled ? <CheckCircle2 size={12} /> : <Ban size={12} />}{availabilityLabel}
        </span>
      </div>
      <Button type="button" variant="ghost" size="sm" icon={X} onClick={onCancel} disabled={busy}>返回</Button>
    </div>
    <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain pb-1 pr-1">
    <fieldset disabled={busy} className="min-w-0 space-y-3">
      <Section title="方案与预算"><div className="grid gap-3 sm:grid-cols-3"><Field label="方案名称" required maxLength={120} value={draft.name} onChange={(name) => update({ name })} /><Choice label="候选模式" value={draft.candidateMode} options={candidateModeOptions} onChange={(candidateMode) => update({ candidateMode })} /><Choice label="调度策略" value={draft.strategy} options={strategyOptions} onChange={(strategy) => update({ strategy })} /><Choice label="归属保持" value={draft.affinityMode} options={affinityOptions} onChange={(affinityMode) => update({ affinityMode })} /><Field label="方案尝试次数上限" type="number" min="1" max="100" value={draft.maxAttempts} onChange={(maxAttempts) => update({ maxAttempts })} /><Field label="方案调度时限（毫秒）" type="number" min="1000" max="600000" step="1000" value={draft.maxDurationMs} onChange={(maxDurationMs) => update({ maxDurationMs })} /></div>
        {policy ? <div aria-label="当前全局调度保护" className="rounded-lg border border-indigo-100 bg-indigo-50/40 px-3 py-2 text-[11px] leading-5 text-slate-600"><div className="flex flex-wrap justify-between gap-x-3 gap-y-1"><span className="font-medium text-slate-700">当前生效上限：{Math.min(Number(draft.maxAttempts), policy.maxAttempts)} 次发送 · {formatDispatchDuration(Math.min(Number(draft.maxDurationMs), policy.maxDurationMs))}</span><span className="text-slate-400">全局配置：设置 → 调度保护</span></div><p>全局最多 {policy.maxAttempts} 次 / {formatDispatchDuration(policy.maxDurationMs)}，与方案上限取较小值；{policy.waitForRecovery ? '全部候选暂时不可用时等待恢复' : '不等待冷却恢复，候选耗尽后返回失败'}。</p><p>首次冷却 {formatDispatchDuration(policy.cooldownInitialMs)}，连续失败 ×{policy.cooldownMultiplier}，最长 {formatDispatchDuration(policy.cooldownMaxMs)}；正常响应开始后不会被调度时限中断。</p></div> : <p role="status" className="text-[11px] text-amber-700">尚未读取全局调度保护，请刷新工作区；规则内明确配置的覆盖值仍会保留。</p>}
        <Tooltip asChild content={profile.isDefault ? '当前方案正在使用，请先切换到其他方案后再停用' : undefined}>
          <button type="button" role="switch" aria-label="停用此方案" aria-checked={!draft.enabled} disabled={Boolean(profile.isDefault)} onClick={() => update({ enabled: !draft.enabled })} className="inline-flex min-h-8 items-center gap-2 rounded-lg text-xs text-slate-600 outline-none hover:text-slate-800 focus-visible:ring-2 focus-visible:ring-indigo-400 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50">
            <span aria-hidden="true" className={`inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors duration-[var(--duration-quick)] motion-reduce:transition-none ${draft.enabled ? 'bg-slate-200' : 'bg-rose-500'}`}>
              <span className={`h-4 w-4 rounded-full bg-white shadow-sm ring-1 ring-black/5 transition-transform duration-[var(--duration-quick)] motion-reduce:transition-none ${draft.enabled ? 'translate-x-0.5' : 'translate-x-4'}`} />
            </span>
            停用此方案
          </button>
        </Tooltip>
        <details className="text-[11px] leading-5 text-slate-500"><summary className="w-fit cursor-pointer hover:text-indigo-600">方案说明</summary><div className="mt-2 space-y-2 wrap-anywhere">
          <p>{profile.isDefault ? profile.id ? '当前方案处理新请求；切换到其他方案后才能停用。' : '首个方案保存后自动设为当前方案。' : '保存不会切换当前方案；停用后无法切换使用，其他方案也会跳过对它的渠道与分组引用。'}</p>
          <p>等待重试由设置页「调度保护」统一控制。</p>
          <p>策略只影响候选选择；费用倍率沿用渠道和端点设置。</p>
        </div></details>
      </Section>
      <div aria-label="调度策略说明" aria-live="polite" className={`min-w-0 rounded-lg border px-3 py-2 text-[11px] leading-5 ${strategy?.quotaBased ? 'border-amber-200 bg-amber-50/60 text-amber-800' : 'border-slate-200 bg-slate-50/70 text-slate-500'}`}>
        <p><span className="font-medium">{strategy?.label}：</span>{strategy?.description}</p>
        {strategy?.quotaBased && <p>{strategy.note}</p>}
        <details className="mt-1"><summary className="w-fit cursor-pointer text-slate-500 hover:text-indigo-600">适用范围与余额说明</summary><div className="mt-1 space-y-0.5 text-slate-500">
        {!strategy?.quotaBased && strategy?.note && <p>{strategy.note}</p>}
        <p>{draft.candidateMode === 'ordered_channels' ? '按渠道顺序时，策略在当前渠道的凭证分组内生效；需要跨渠道均衡时，选择统一候选池。' : '统一候选池中，策略在所有参与渠道的可用凭证间生效。'}轮次保持仍会优先复用本轮选择。</p>
        <p>中转站可使用手动余额估算成本并在耗尽时拦截；余额不参与上述周期额度排序。</p>
        </div></details>
      </div>
      <ProfileMembersEditor profile={draft} channels={channels} accounts={accounts} busy={busy} onChange={setDraft} />
      <Section title="匹配规则" action={<Tooltip content="模型路由与映射按精确模型、通配模型优先匹配，再处理组合条件、非流改道和意图；同类优先级小者先执行。"><span tabIndex={0} aria-label="规则匹配顺序" className="rounded text-slate-400 focus-visible:outline-indigo-500"><Info size={14} /></span></Tooltip>}>
        <div className="grid gap-2 sm:grid-cols-2">
          <div role="group" aria-label="模型路由规则" className="flex min-w-0 flex-col rounded-lg border border-indigo-100 bg-indigo-50/40 p-2.5">
            <h4 className="flex items-center gap-1.5 text-xs font-semibold text-slate-700"><GitBranch size={14} className="shrink-0 text-indigo-500" />模型路由</h4>
            <p className="mt-1 text-[11px] leading-5 text-slate-500">按请求模型选择处理资源；模型映射是在本方案渠道内替换模型名，也属于模型路由。</p>
            <div className="mt-auto flex flex-wrap gap-1.5 pt-2">
              <Button type="button" variant="ghost" size="sm" icon={Plus} aria-label="添加模型路由" className="border border-indigo-200/70 bg-white/80 hover:border-indigo-300 hover:bg-white hover:text-indigo-600" onClick={() => createRule('model')}>添加模型路由</Button>
              <Button type="button" variant="ghost" size="sm" icon={Replace} aria-label="添加模型映射" className="border border-teal-200/70 bg-teal-50/50 text-teal-700 hover:border-teal-300 hover:bg-teal-50 hover:text-teal-800" onClick={() => createRule('mapping')}>添加模型映射</Button>
            </div>
          </div>
          <div role="group" aria-label="非流改道规则" className="flex min-w-0 flex-col rounded-lg border border-amber-100 bg-amber-50/40 p-2.5">
            <h4 className="flex items-center gap-1.5 text-xs font-semibold text-slate-700"><Route size={14} className="shrink-0 text-amber-600" />非流改道</h4>
            <p className="mt-1 text-[11px] leading-5 text-slate-500">仅为非流式请求选择其他处理目标，保留请求原有的流式设置。</p>
            <div className="mt-auto flex flex-wrap gap-1.5 pt-2"><Button type="button" variant="ghost" size="sm" icon={Plus} aria-label="添加非流改道" className="border border-amber-200/70 bg-white/80 hover:border-amber-300 hover:bg-white hover:text-amber-700" onClick={() => createRule('non_stream')}>添加非流改道</Button></div>
          </div>
        </div>
        {visibleRules.map(({ rule, index }) => <RuleEditor key={rule.rowKey} rule={rule} index={index} intents={draft.intents} intentEnabled={intentEnabled} targetProps={targetProps} policy={policy} onChange={(patch) => updateRule(index, patch)} onDelete={() => update({ rules: draft.rules.filter((_, at) => at !== index) })} />)}
        {!visibleRules.length && <p className="py-2 text-center text-xs text-slate-400">未配置匹配规则时，使用上方的默认编排。</p>}
        {hiddenIntentRules > 0 && <p className="text-[11px] text-slate-400">{hiddenIntentRules} 条依赖意图的规则已随总开关停用；开启意图路由后可编辑。</p>}
      </Section>
      <Section title="意图路由 · 可选" action={<CheckBox label="启用意图路由" value={intentEnabled} onChange={(enabled) => setDraft((previous) => setIntentRoutingEnabled(previous, enabled))} />}>
        <p className="flex items-start gap-2 text-[11px] leading-5 text-slate-500"><Sparkles size={14} className="mt-0.5 shrink-0" /><span>{intentEnabled ? '按用户消息分类后选择目标，会产生额外模型用量。模型与非流规则仍优先执行。' : '已关闭：不分类，也不执行意图路由。已有定义和分类器设置保留，开启后再配置。'}</span></p>
        {intentEnabled && <>
      <Section title="意图定义" action={<Button type="button" variant="ghost" size="sm" icon={Plus} onClick={() => update({ intents: [...draft.intents, { rowKey: crypto.randomUUID(), id: '', name: '', description: '', enabled: true, priority: draft.intents.length, targets: [], mode: 'prefer', fallbackIntent: '', fallbackToDefault: null }] })}>添加意图</Button>}>
        <IntentStatistics statistics={statistics} intents={draft.intents} saved={Boolean(draft.id)} loaded={statisticsLoaded} error={statisticsError} busy={busy} onRefresh={onRefreshStatistics} />
        {draft.intents.map((intent, index) => <details key={intent.rowKey} open className="rounded-lg border border-slate-200 p-3"><summary className="cursor-pointer text-xs font-medium text-slate-700">{intent.name || `意图 ${index + 1}`}</summary><div className="mt-3 space-y-3"><div className="grid gap-2 sm:grid-cols-3"><Field label="意图 ID（字符串）" value={intent.id} onChange={(id) => updateIntent(index, { id })} /><Field label="意图名称" value={intent.name} onChange={(name) => updateIntent(index, { name })} /><Field label="优先级" type="number" value={intent.priority} onChange={(priority) => updateIntent(index, { priority })} /><Choice label="执行顺序" value={intent.mode} options={modes} onChange={(mode) => updateIntent(index, { mode })} /><Choice label="回退意图" value={intent.fallbackIntent} options={[{ value: '', label: '无回退意图' }, ...draft.intents.filter((item) => item.rowKey !== intent.rowKey && item.id).map((item) => ({ value: item.id, label: item.name || item.id }))]} onChange={(fallbackIntent) => updateIntent(index, { fallbackIntent })} /><Choice label="目标失败后" value={triState(intent.fallbackToDefault)} options={ruleFallbackOptions(policy)} onChange={(value) => updateIntent(index, { fallbackToDefault: fromTriState(value) })} /></div><label className="block space-y-1 text-xs text-slate-500">分类说明<textarea className={`${control} h-20 resize-y py-2`} value={intent.description} onChange={(event) => updateIntent(index, { description: event.target.value })} /></label><TargetsEditor {...targetProps} targets={intent.targets} onChange={(targets) => updateIntent(index, { targets })} /><div className="flex items-center justify-between"><CheckBox label="启用意图" value={intent.enabled} onChange={(enabled) => updateIntent(index, { enabled })} /><Button type="button" variant="ghost" size="sm" icon={Trash2} onClick={() => update({ intents: draft.intents.filter((_, at) => at !== index) })}>删除意图</Button></div></div></details>)}
        {!draft.intents.length && <p className="text-xs text-slate-400">分类默认关闭，可先配置模型和非流规则。</p>}
      </Section>
      <Section title="分类器设置"><div className="grid gap-3 sm:grid-cols-3"><Choice label="分类服务" value={classifier.provider} options={[{ value: 'claude', label: 'Claude' }, { value: 'codex', label: 'Codex' }]} onChange={(provider) => classifierChange({ provider, channelId: 0, groupId: 0 })} />{classifier.provider === draft.provider ? <><Choice label="分类渠道" value={classifier.channelId} options={channels.map((channel) => ({ value: channel.id, label: channel.name }))} onChange={(channelId) => classifierChange({ channelId, groupId: 0 })} /><Choice label="分类分组" value={classifier.groupId} options={accounts.filter((account) => account.channelId === classifier.channelId).map((account) => ({ value: account.groupId, label: account.groupName }))} onChange={(groupId) => classifierChange({ groupId })} /></> : <><Field label="分类渠道 ID" type="number" min="1" value={classifier.channelId} onChange={(channelId) => classifierChange({ channelId })} /><Field label="分类分组 ID" type="number" min="1" value={classifier.groupId} onChange={(groupId) => classifierChange({ groupId })} /></>}<Field label="分类模型" value={classifier.model} onChange={(model) => classifierChange({ model })} /><Field label="超时（毫秒）" type="number" min="1" max="30000" value={classifier.timeoutMs} onChange={(timeoutMs) => classifierChange({ timeoutMs })} /><Field label="最低可信度（0–1）" type="number" min="0" max="1" step="0.05" value={classifier.confidence} onChange={(confidence) => classifierChange({ confidence })} /><Choice label="分类失败处理" value={classifier.failureMode} options={[{ value: 'default', label: '使用默认方案' }, { value: 'previous', label: '沿用上一意图' }, { value: 'no_intent', label: '继续执行其他规则' }]} onChange={(failureMode) => classifierChange({ failureMode })} /></div><p className="text-[11px] leading-5 text-slate-400">分类输入在服务端经过隐私处理；缓存默认 10 分钟，同一轮次合并分类，失败冷却 30 秒。跨服务分类请填写对应管理页中的渠道与分组 ID。</p></Section>
        </>}
      </Section>
    </fieldset>
    </div>
    <footer className="shrink-0 space-y-2 border-t border-slate-200 bg-white pt-3">
      {error && <p role="alert" className="wrap-anywhere text-xs text-rose-600">{error}</p>}
      <div className="flex justify-end gap-2"><Button type="button" variant="secondary" size="sm" icon={X} onClick={onCancel} disabled={busy}>取消</Button><Button type="submit" size="sm" icon={draft.id ? Save : Check} loading={busy}>保存方案</Button></div>
    </footer>
  </form>;
}
