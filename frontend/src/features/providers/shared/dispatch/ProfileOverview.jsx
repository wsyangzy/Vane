import Tooltip from '@/shared/ui/Tooltip.jsx';
import { Ban, CheckCircle2, ChevronRight, Clock3, GitBranch, Layers, Pencil, Server, SlidersHorizontal, Sparkles, Star, Trash2 } from 'lucide-react';
import { affinityOptions, candidateModeOptions, profileChannelIds, strategyOptions } from './dispatchState.js';
import './profileDetails.css';

const optionLabel = (options, value) => options.find((option) => option.value === value)?.label || value;

export default function ProfileOverview({ profile, channels, busy, onSetDefault, onEdit, onDelete }) {
  const channelIds = profileChannelIds(profile);
  const activeGroups = profile.members.filter((member) => member.enabled).length;
  const strategy = strategyOptions.find((option) => option.value === profile.strategy);
  const settings = [
    { label: '调度策略', value: optionLabel(strategyOptions, profile.strategy), icon: SlidersHorizontal },
    { label: '候选编排', value: optionLabel(candidateModeOptions, profile.candidateMode), icon: Layers },
    { label: '保持方式', value: optionLabel(affinityOptions, profile.affinityMode), icon: GitBranch },
    { label: '请求预算', value: `${profile.maxAttempts} 次 / ${profile.maxDurationMs / 1000} 秒`, icon: Clock3 },
  ];

  return <section className="dp-card" aria-label="方案概况">
    <header className="dp-profile-header">
      <div className="dp-profile-title">
        <div className="dp-profile-name"><h3>{profile.name}</h3><span className="dp-badge" data-tone={profile.isDefault ? 'success' : profile.enabled ? 'muted' : 'danger'}>{profile.isDefault ? <><CheckCircle2 size={11} />当前使用</> : profile.enabled ? '可切换' : <><Ban size={11} />已停用</>}</span></div>
        <p>方案概况<span>修订 {profile.revision}</span></p>
      </div>
      <div className="dp-profile-actions" aria-label="方案操作">
        <Tooltip asChild content={profile.isDefault ? undefined : !profile.enabled ? '此方案已停用，请先编辑并恢复启用' : '切换至此调度方案'}><button type="button" className="dp-icon-button" aria-label={profile.isDefault ? '当前调度方案' : '切换至此调度方案'} disabled={busy || profile.isDefault || !profile.enabled} onClick={onSetDefault}><Star size={14} className={profile.isDefault ? 'fill-amber-400 text-amber-500' : ''} /></button></Tooltip>
        <Tooltip asChild content="编辑调度方案"><button type="button" className="dp-icon-button" aria-label="编辑调度方案" disabled={busy} onClick={onEdit}><Pencil size={14} /></button></Tooltip>
        <Tooltip asChild content={profile.isDefault ? '当前方案不能删除，请先切换到其他方案' : '删除调度方案'}><button type="button" className="dp-icon-button" data-danger aria-label="删除调度方案" disabled={busy || profile.isDefault} onClick={onDelete}><Trash2 size={14} /></button></Tooltip>
      </div>
    </header>

    <dl className="dp-profile-settings">{settings.map(({ label, value, icon: Icon }) => <div key={label}><dt><Icon size={12} />{label}</dt><dd>{value}</dd></div>)}</dl>
    {strategy?.quotaBased && <p className="mx-3 mb-3 rounded-lg border border-amber-100 bg-amber-50/60 px-3 py-2 text-[11px] leading-5 text-amber-800"><span className="font-medium">{strategy.badge}：</span>{strategy.note}</p>}

    <div className="dp-channel-section">
      <div className="dp-section-heading dp-channel-heading"><h4><Server size={13} />参与渠道</h4><span>{channelIds.length} 个渠道 · {activeGroups} 个参与分组</span></div>
      {channelIds.length ? <ol className="dp-channel-list">{channelIds.map((id, index) => {
        const channel = channels.find((item) => item.id === id);
        const groups = profile.members.filter((member) => member.channelId === id && member.enabled).length;
        return <li key={id}><div className="dp-channel-chip"><span className="dp-channel-rank" aria-label={`渠道序号 ${index + 1}`}>{String(index + 1).padStart(2, '0')}</span><span className="dp-channel-name">{channel?.name || `渠道 ${id}`}</span><span className="dp-channel-note" data-warning={!groups || !channel}>{!channel ? '资源已不存在' : groups ? `${groups} 分组` : '无参与分组'}</span></div>{profile.candidateMode === 'ordered_channels' && index < channelIds.length - 1 && <ChevronRight size={12} className="dp-channel-connector" aria-hidden="true" />}</li>;
      })}</ol> : <p className="dp-empty">尚未添加参与渠道，可通过编辑方案选择。</p>}
    </div>

    <footer className="dp-profile-footer"><span>{profile.rules.length ? `${profile.rules.filter((rule) => rule.enabled).length} / ${profile.rules.length} 条规则启用` : '未配置规则'}</span><span>{profile.intents.length ? `${profile.intents.filter((intent) => intent.enabled).length} / ${profile.intents.length} 种意图启用` : '未配置意图'}</span><span data-active={Boolean(profile.classifier?.enabled)}><Sparkles size={11} />分类{profile.classifier?.enabled ? '已开启' : '未开启'}</span></footer>
  </section>;
}
