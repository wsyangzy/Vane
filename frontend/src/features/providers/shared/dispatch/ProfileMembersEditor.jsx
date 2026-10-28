import Tooltip from '@/shared/ui/Tooltip.jsx';
import { useId, useRef, useState } from 'react';
import { ArrowDown, ArrowUp, Check, KeyRound, Layers, Plus, Trash2, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { moveProfileChannel, profileChannelIds, selectProfileChannels, updateProfileMembers } from './dispatchState.js';
import ChannelPickerDialog from './ChannelPickerDialog.jsx';
import './profileMembers.css';

const iconButton = 'inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-700 focus-visible:outline-2 focus-visible:outline-indigo-500 disabled:opacity-30';

function GroupCard({ account, member, channelName, groupId, weighted, busy, onChange }) {
  const selected = Boolean(member?.enabled);
  const name = account?.groupName || (groupId === 0 ? '全部凭证分组' : `分组 ${groupId}（资源不存在）`);
  return <div className="profile-group-card" data-selected={selected}>
    <label className="profile-group-select">
      <span className="profile-group-icon"><KeyRound size={14} /></span>
      <span className="profile-group-identity"><span className="profile-group-name">{name}</span><span className="profile-group-kind">{groupId === 0 ? '包含当前及后续新增分组' : account?.managed ? '托管订阅' : '分组凭证'}</span></span>
      <span className="profile-group-state"><input type="checkbox" aria-label={`${channelName} / ${name} 参与调度`} checked={selected} disabled={busy} onChange={(event) => onChange({ enabled: event.target.checked })} />{selected ? '参与' : '未参与'}</span>
    </label>
    {weighted && <div className="profile-group-fields"><label className="profile-group-field"><span>调度权重</span><input aria-label={`${channelName} / ${name} 权重`} type="number" min="1" max="10000" value={member?.weight ?? 1} disabled={busy || !selected} onChange={(event) => onChange({ weight: event.target.value })} /></label></div>}
  </div>;
}

export default function ProfileMembersEditor({ profile, channels, accounts, busy, onChange }) {
  const ids = profileChannelIds(profile);
  const [pickerOpen, setPickerOpen] = useState(false);
  const headingId = useId();
  const addButton = useRef(null);
  const enabledCount = profile.members.filter((member) => member.enabled).length;
  const weighted = profile.strategy === 'weighted';
  const updateGroup = (channelId, groupId, patch) => onChange(updateProfileMembers(profile, channelId, [groupId], patch));
  const confirmChannels = (selectedIds) => {
    onChange(selectProfileChannels(profile, selectedIds, accounts));
    setPickerOpen(false);
  };
  const removeChannel = (channelId) => {
    onChange({ ...profile, members: profile.members.filter((member) => member.channelId !== channelId) });
    addButton.current?.focus();
  };

  return <section className="profile-members" aria-labelledby={headingId}>
    <div className="profile-members-heading">
      <div className="profile-members-heading-copy"><h3 id={headingId}><Layers size={15} />渠道与凭证编排</h3><span className="profile-members-total">{ids.length} 个渠道 · {enabledCount} 个参与分组</span></div>
      <button ref={addButton} type="button" className="profile-add-channel" aria-haspopup="dialog" disabled={busy} onClick={() => setPickerOpen(true)}><Plus size={13} />添加渠道</button>
    </div>
    {pickerOpen && <ChannelPickerDialog channels={channels} accounts={accounts} selectedIds={ids} busy={busy} onConfirm={confirmChannels} onClose={() => setPickerOpen(false)} returnFocusRef={addButton} />}
    <div className="profile-members-body">
      <p className="profile-members-hint">{profile.candidateMode === 'ordered_channels' ? '按序号依次调度渠道；在每个渠道中选择参与的凭证分组。' : '已选分组汇入统一候选池；当前策略在所有候选中选择。'}{weighted && ' 权重越高，获得的调度份额越多。'}</p>
      {!ids.length && <div className="profile-members-empty"><Layers size={22} /><span>选择渠道，开始编排</span><small>添加后可调整渠道顺序及参与的凭证分组。</small><Button type="button" size="sm" variant="secondary" icon={Plus} disabled={busy} onClick={() => setPickerOpen(true)}>选择渠道</Button></div>}
      <div className="profile-selected-channels">{ids.map((channelId, index) => {
        const channel = channels.find((item) => item.id === channelId);
        const name = channel?.name || `渠道 ${channelId}（资源不存在）`;
        const members = profile.members.filter((item) => item.channelId === channelId);
        const groups = accounts.filter((account) => account.channelId === channelId);
        const groupIds = [...new Set([...groups.map((group) => group.groupId), ...members.map((member) => member.groupId)])];
        const participating = members.filter((member) => member.enabled).length;
        const allSelected = groupIds.every((groupId) => members.some((member) => member.groupId === groupId && member.enabled));
        return <section key={channelId} className="profile-channel-block" aria-label={`编排渠道 ${name}`}>
          <div className="profile-channel-header"><Tooltip asChild content="渠道顺序"><span className="profile-channel-order">{String(index + 1).padStart(2, '0')}</span></Tooltip><div className="profile-channel-heading"><h4>{name}</h4><span>{participating} / {groupIds.length} 个分组参与</span></div><div className="profile-channel-tools"><button type="button" className="profile-channel-select-all" disabled={busy} aria-label={`${name} ${allSelected ? '取消全选分组' : '全选分组'}`} onClick={() => onChange(updateProfileMembers(profile, channelId, groupIds, { enabled: !allSelected }))}>{allSelected ? <X size={12} /> : <Check size={12} />}{allSelected ? '取消全选' : '全选'}</button><div className="profile-channel-actions"><Tooltip asChild content="上移渠道"><button type="button" className={iconButton} aria-label={`上移渠道 ${name}`} disabled={busy || index === 0} onClick={() => onChange(moveProfileChannel(profile, channelId, -1))}><ArrowUp size={13} /></button></Tooltip><Tooltip asChild content="下移渠道"><button type="button" className={iconButton} aria-label={`下移渠道 ${name}`} disabled={busy || index === ids.length - 1} onClick={() => onChange(moveProfileChannel(profile, channelId, 1))}><ArrowDown size={13} /></button></Tooltip><Tooltip asChild content="从方案移除渠道"><button type="button" className={`${iconButton} hover:!bg-rose-50 hover:!text-rose-600`} aria-label={`移除渠道 ${name}`} disabled={busy} onClick={() => removeChannel(channelId)}><Trash2 size={13} /></button></Tooltip></div></div></div>
          <div className="profile-channel-members">
            <div className="profile-group-grid">{groupIds.map((groupId) => <GroupCard key={groupId} channelName={name} groupId={groupId} weighted={weighted} busy={busy} account={groups.find((group) => group.groupId === groupId)} member={members.find((member) => member.groupId === groupId)} onChange={(patch) => updateGroup(channelId, groupId, patch)} />)}</div>
          </div>
        </section>;
      })}</div>
    </div>
  </section>;
}
