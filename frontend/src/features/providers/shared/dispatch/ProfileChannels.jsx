import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { ChevronRight, Server } from 'lucide-react';
import { profileChannelIds } from './dispatchState.js';
import ChannelGroupsPopover from './ChannelGroupsPopover.jsx';

export default function ProfileChannels({ profile, channels, accounts = [] }) {
  const entries = useMemo(() => profileChannelIds(profile).map(id => {
    const channel = channels.find(channel => channel.id === id);
    const members = profile.members.filter(member => member.channelId === id);
    return { id, name: channel?.name || `渠道 ${id}`, missing: !channel, members, enabledGroups: members.filter(member => member.enabled).length };
  }), [profile, channels]);
  const [shown, setShown] = useState(0);
  const [floating, setFloating] = useState(null);
  const panelId = useId();
  const panelRef = useRef(null);
  const restoringFocus = useRef(false);
  const closeTimer = useRef(null);
  const summaryRef = useRef(null);
  const measureRef = useRef(null);
  const moreRef = useRef(null);
  const cancelClose = useCallback(() => {
    if (closeTimer.current) window.clearTimeout(closeTimer.current);
    closeTimer.current = null;
  }, []);
  const close = useCallback(() => {
    cancelClose();
    if (panelRef.current?.contains(document.activeElement)) {
      restoringFocus.current = true;
      floating?.anchor.focus({ preventScroll: true });
      restoringFocus.current = false;
    }
    setFloating(value => value ? { ...value, open: false } : null);
  }, [cancelClose, floating?.anchor]);
  const scheduleClose = useCallback(() => {
    cancelClose();
    closeTimer.current = window.setTimeout(() => {
      closeTimer.current = null;
      close();
    }, 180);
  }, [cancelClose, close]);
  useEffect(() => () => cancelClose(), [cancelClose]);
  const open = (event, focusPanel = false) => {
    const value = event.currentTarget.dataset.channelKey;
    const key = value === 'all' ? value : Number(value);
    setFloating({ key, anchor: event.currentTarget, open: true, focusPanel });
  };
  const pointerEnter = event => { if (event.pointerType !== 'touch') { cancelClose(); open(event); } };
  const pointerLeave = event => { if (event.pointerType !== 'touch') scheduleClose(); };
  const focus = event => { cancelClose(); if (!restoringFocus.current) open(event); };
  const blur = event => { if (!panelRef.current?.contains(event.relatedTarget)) close(); };
  const click = event => { cancelClose(); open(event); };
  const keyDown = event => { if (event.key === 'ArrowDown') { event.preventDefault(); open(event, true); } };
  const triggerEvents = { onPointerEnter: pointerEnter, onPointerLeave: pointerLeave, onFocus: focus, onBlur: blur, onClick: click, onKeyDown: keyDown };
  const trigger = key => {
    return {
      'data-channel-key': key,
      'aria-haspopup': 'dialog', 'aria-expanded': floating?.open && floating.key === key || false,
      'aria-controls': floating?.open && floating.key === key ? panelId : undefined,
    };
  };
  useLayoutEffect(() => {
    const measure = () => {
      const widths = [...measureRef.current.children].map(item => item.offsetWidth);
      const gap = parseFloat(getComputedStyle(measureRef.current).columnGap) || 0;
      const available = summaryRef.current.clientWidth;
      const total = widths.reduce((sum, width) => sum + width, 0) + Math.max(0, widths.length - 1) * gap;
      let count = widths.length;
      if (total > available) {
        const limit = available - moreRef.current.offsetWidth - gap;
        count = 0;
        for (let used = 0; count < widths.length; count++) {
          const next = widths[count] + (count ? gap : 0);
          if (used + next > limit) break;
          used += next;
        }
      }
      setShown(previous => previous === count ? previous : count);
    };
    const observer = new ResizeObserver(measure);
    observer.observe(summaryRef.current);
    observer.observe(measureRef.current);
    return () => observer.disconnect();
  }, [entries]);
  const chip = (entry, index, measuring = false) => <li key={entry.id}><button type="button" className="dp-channel-chip" aria-label={`${entry.name} 分组详情`} {...(measuring ? { tabIndex: -1 } : trigger(entry.id))} {...(measuring ? {} : triggerEvents)}><span className="dp-channel-rank" aria-label={`渠道序号 ${index + 1}`}>{String(index + 1).padStart(2, '0')}</span><span className="dp-channel-name">{entry.name}</span><span className="dp-channel-note" data-warning={!entry.enabledGroups || entry.missing}>{entry.missing ? '资源已不存在' : entry.enabledGroups ? `${entry.enabledGroups} 分组` : '无参与分组'}</span></button>{profile.candidateMode === 'ordered_channels' && index < entries.length - 1 && <ChevronRight size={12} className="dp-channel-connector" aria-hidden="true" />}</li>;
  return <div className="dp-channel-section">
    <div className="dp-section-heading dp-channel-heading"><h4><Server size={13} />参与渠道</h4><span>{entries.length} 个渠道 · {profile.members.filter(member => member.enabled).length} 个参与分组</span></div>
    <div ref={summaryRef} className="dp-channel-summary">
      <ol ref={measureRef} className="dp-channel-list dp-channel-measure" aria-hidden="true" inert>{entries.map((entry, index) => chip(entry, index, true))}</ol>
      <span ref={moreRef} className="dp-channel-more dp-channel-measure" aria-hidden="true">… +{entries.length}</span>
      {entries.length ? <><ol className="dp-channel-list" aria-label="参与渠道摘要">{entries.slice(0, shown).map((entry, index) => chip(entry, index))}</ol>{shown < entries.length && <button type="button" className="dp-channel-more" aria-label={`查看全部 ${entries.length} 个渠道分组`} {...trigger('all')} {...triggerEvents}>… +{entries.length - shown}</button>}</> : <p className="dp-summary-empty">尚未添加参与渠道。</p>}
    </div>
    <ChannelGroupsPopover id={panelId} floating={floating ? { ...floating, cancelClose, scheduleClose } : null} panelRef={panelRef} entries={floating?.key === 'all' ? entries : entries.filter(entry => entry.id === floating?.key)} accounts={accounts} strategy={profile.strategy} onClose={close} />
  </div>;
}
