import { useEffect, useLayoutEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { Server } from 'lucide-react';
import { usePresence } from '@/shared/hooks/usePresence.js';
import { formatMultiplierDisplayValue } from '@/shared/lib/multiplier.js';

export default function ChannelGroupsPopover({ id, floating, panelRef, entries, accounts, strategy, onClose }) {
  const open = Boolean(floating?.open);
  const presence = usePresence(open);
  const [position, setPosition] = useState({ top: 0, left: 0, maxHeight: 360, upward: false });
  useLayoutEffect(() => {
    const panel = panelRef.current;
    const anchor = floating?.anchor;
    if (!presence.present || !panel || !anchor) return undefined;
    const update = () => {
      const rect = anchor.getBoundingClientRect();
      const width = Math.min(panel.getBoundingClientRect().width, window.innerWidth - 16);
      const below = Math.max(0, window.innerHeight - rect.bottom - 12);
      const above = Math.max(0, rect.top - 12);
      const height = Math.min(360, panel.scrollHeight);
      const upward = below < height && above > below;
      const maxHeight = Math.min(360, upward ? above : below);
      const next = { maxHeight, upward, left: Math.max(8, Math.min(rect.left, window.innerWidth - width - 8)), top: upward ? rect.top - Math.min(height, maxHeight) - 4 : rect.bottom + 4 };
      setPosition(previous => Object.keys(next).every(key => next[key] === previous[key]) ? previous : next);
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(panel);
    observer.observe(anchor);
    window.addEventListener('resize', update);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', update);
    };
  }, [floating?.anchor, open, panelRef, presence.present, entries.length]);
  useEffect(() => {
    if (open && floating.focusPanel) panelRef.current?.focus({ preventScroll: true });
  }, [open, floating?.anchor, floating?.focusPanel, panelRef]);
  useEffect(() => {
    if (!open) return undefined;
    const outside = event => {
      if (!panelRef.current?.contains(event.target) && !floating.anchor.contains(event.target)) onClose();
    };
    const scroll = event => { if (!panelRef.current?.contains(event.target)) onClose(); };
    const keydown = event => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onClose(); } };
    document.addEventListener('pointerdown', outside);
    window.addEventListener('scroll', scroll, true);
    window.addEventListener('keydown', keydown);
    return () => {
      document.removeEventListener('pointerdown', outside);
      window.removeEventListener('scroll', scroll, true);
      window.removeEventListener('keydown', keydown);
    };
  }, [open, floating?.anchor, panelRef, onClose]);
  if (!open) return null;
  return createPortal(<section ref={panelRef} id={id} role="dialog" aria-label={entries.length === 1 ? `${entries[0].name} 的参与分组` : '参与渠道分组'} tabIndex={0} className={`dp-groups-popover t-dropdown ${presence.className}`} data-origin={position.upward ? 'bottom-left' : 'top-left'} style={{ top: position.top, left: position.left, maxHeight: position.maxHeight }} onPointerEnter={event => { if (event.pointerType !== 'touch') floating.cancelClose(); }} onPointerLeave={event => { if (event.pointerType !== 'touch') floating.scheduleClose(); }} onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget) && !floating.anchor.contains(event.relatedTarget)) onClose(); }}>
    <header><h4><Server size={13} />参与分组</h4></header>
    <ol className="dp-channel-groups" aria-label="渠道与分组">{entries.map(entry => <li key={entry.id}><div className="dp-group-channel"><strong>{entry.name}</strong><span>{entry.missing ? '资源已不存在' : `${entry.enabledGroups} 个参与分组`}</span></div><div className="dp-group-badges">{entry.members.map(member => {
      const account = accounts.find(item => item.channelId === entry.id && item.groupId === member.groupId);
      return <span key={member.groupId} className="dp-badge" data-tone={member.enabled ? 'accent' : 'muted'}>{account?.groupName || (member.groupId ? `分组 ${member.groupId}` : '全部分组')}{account && ` · ${formatMultiplierDisplayValue(account.costMultiplier)}`}{!member.enabled && ' · 未参与'}{member.upstreamModel && ` → ${member.upstreamModel}`}{strategy === 'weighted' && ` · 权重 ${member.weight}`}</span>;
    })}</div></li>)}</ol>
  </section>, document.body);
}
