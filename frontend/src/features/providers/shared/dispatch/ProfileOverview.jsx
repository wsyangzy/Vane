import { useRef, useState } from 'react';
import Tooltip from '@/shared/ui/Tooltip.jsx';
import { Ban, CheckCircle2, Clock3, FlaskConical, GitBranch, Layers, Pencil, Search, SlidersHorizontal, Star, Trash2 } from 'lucide-react';
import { affinityOptions, candidateModeOptions, profileSwitchBlockReason, strategyOptions } from './dispatchState.js';
import { formatDispatchDuration } from '@/shared/lib/dispatchPolicy.js';
import { Button } from '@/shared/ui/index.js';
import ProfileChannels from './ProfileChannels.jsx';
import ProfileRoutingSummary from './ProfileRoutingSummary.jsx';
import DispatchPreviewDialog from './DispatchPreviewDialog.jsx';
import './profileDetails.css';

const optionLabel = (options, value) => options.find((option) => option.value === value)?.label || value;

export default function ProfileOverview({ profile, profiles, channels, accounts, policy, busy, perform, onSetDefault, onEdit, onDelete }) {
  const [previewOpen, setPreviewOpen] = useState(false);
  const previewTrigger = useRef(null);
  const strategy = strategyOptions.find((option) => option.value === profile.strategy);
  const switchBlockReason = profileSwitchBlockReason(profile);
  const effectiveAttempts = policy ? Math.min(profile.maxAttempts, policy.maxAttempts) : profile.maxAttempts;
  const effectiveDuration = policy ? Math.min(profile.maxDurationMs, policy.maxDurationMs) : profile.maxDurationMs;
  const settings = [
    { label: '调度策略', value: optionLabel(strategyOptions, profile.strategy).split(' · ')[0], icon: SlidersHorizontal, help: strategy?.quotaBased ? strategy.note : strategy?.description },
    { label: '候选编排', value: optionLabel(candidateModeOptions, profile.candidateMode), icon: Layers },
    { label: '保持方式', value: optionLabel(affinityOptions, profile.affinityMode), icon: GitBranch },
    { label: '生效预算', value: `${effectiveAttempts} 次 / ${formatDispatchDuration(effectiveDuration)}`, icon: Clock3, help: policy ? `方案上限 ${profile.maxAttempts} 次 / ${formatDispatchDuration(profile.maxDurationMs)}，全局上限 ${policy.maxAttempts} 次 / ${formatDispatchDuration(policy.maxDurationMs)}，取较小值。${policy.waitForRecovery ? '等待候选恢复' : '不等待候选恢复'}；冷却 ${formatDispatchDuration(policy.cooldownInitialMs)} 起，最长 ${formatDispatchDuration(policy.cooldownMaxMs)}。` : undefined },
  ];
  if (profile.provider === 'codex') settings.push({ label: '搜索线路', value: ({ auto: '自动选择', native: '原生协议转发', service: '搜索服务适配' })[profile.search?.mode || 'auto'], icon: Search });

  return <section className="dp-card dp-profile-card" aria-label="方案概况">
    <header className="dp-profile-header">
      <div className="dp-profile-title">
        <div className="dp-profile-name"><h3>{profile.name}</h3><span className="dp-badge" data-tone={profile.isDefault ? 'success' : profile.enabled ? 'muted' : 'danger'}>{profile.isDefault ? <><CheckCircle2 size={11} />当前使用</> : !profile.enabled ? <><Ban size={11} />已停用</> : switchBlockReason || '可切换'}</span><span className="dp-profile-revision">修订 {profile.revision}</span></div>
      </div>
      <div className="dp-profile-toolbar"><Button ref={previewTrigger} type="button" size="sm" variant="secondary" icon={FlaskConical} disabled={busy} aria-haspopup="dialog" aria-expanded={previewOpen} onClick={() => setPreviewOpen(true)}>模拟调度</Button><div className="dp-profile-actions" aria-label="方案操作">
        <Tooltip asChild content={profile.isDefault ? undefined : !profile.enabled ? '此方案已停用，请先编辑并恢复启用' : switchBlockReason ? '无参与分组，请先编辑方案，添加渠道并启用分组' : '切换至此调度方案'}><button type="button" className="dp-icon-button" aria-label={profile.isDefault ? '当前调度方案' : '切换至此调度方案'} disabled={busy || profile.isDefault || Boolean(switchBlockReason)} onClick={onSetDefault}><Star size={14} className={profile.isDefault ? 'fill-amber-400 text-amber-500' : ''} /></button></Tooltip>
        <Tooltip asChild content="编辑调度方案"><button type="button" className="dp-icon-button" aria-label="编辑调度方案" disabled={busy} onClick={onEdit}><Pencil size={14} /></button></Tooltip>
        <Tooltip asChild content={profile.isDefault ? '当前方案不能删除，请先切换到其他方案' : '删除调度方案'}><button type="button" className="dp-icon-button" data-danger aria-label="删除调度方案" disabled={busy || profile.isDefault} onClick={onDelete}><Trash2 size={14} /></button></Tooltip>
      </div></div>
    </header>

    <div className="dp-profile-body scrollbar-visible">
    <dl className="dp-profile-settings" style={{ '--profile-setting-count': settings.length }}>{settings.map(({ label, value, icon: Icon, help }) => <Tooltip key={label} asChild content={help}><div tabIndex={help ? 0 : undefined} className={help ? 'dp-setting-help' : undefined}><dt><Icon size={12} />{label}</dt><dd>{value}</dd></div></Tooltip>)}</dl>

    <ProfileChannels key={profile.id} profile={profile} channels={channels} accounts={accounts} />
    <ProfileRoutingSummary profile={profile} profiles={profiles} channels={channels} accounts={accounts} policy={policy} />
    </div>

    {previewOpen && <DispatchPreviewDialog profile={profile} channels={channels} accounts={accounts} busy={busy} perform={perform} triggerRef={previewTrigger} onClose={() => setPreviewOpen(false)} />}
  </section>;
}
