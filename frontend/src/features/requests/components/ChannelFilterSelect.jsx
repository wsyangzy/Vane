import Tooltip from '@/shared/ui/Tooltip.jsx';
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Check, ChevronDown, ChevronRight } from 'lucide-react';
import { requestFilterPath } from '../utils/filterResources.js';

export default function ChannelFilterSelect({ channels = [], provider = '', channel = 'all', group = 'all', onChange }) {
  const [isOpen, setIsOpen] = useState(false);
  const [branch, setBranch] = useState({ provider: '', channel: '' });
  const rootRef = useRef(null);
  const menuRef = useRef(null);
  const menuId = useId();
  const currentProvider = channels.find(item => item.provider === branch.provider) || channels[0];
  const currentChannel = currentProvider?.channels.find(item => item.name === branch.channel) || currentProvider?.channels[0];
  const displayLabel = requestFilterPath(channels, { provider, channel, group });

  const browseProvider = (item) => setBranch({
    provider: item.provider,
    channel: (item.provider === provider && item.channels.some(candidate => candidate.name === channel) ? channel : item.channels[0]?.name) || ''
  });
  const closeMenu = () => {
    setIsOpen(false);
    rootRef.current?.querySelector('button')?.focus();
  };
  const select = (nextProvider = '', nextChannel = '', nextGroup = '') => {
    onChange?.({ provider: nextProvider, channel: nextChannel || 'all', group: nextGroup || 'all' });
    closeMenu();
  };

  useEffect(() => {
    if (!isOpen) return undefined;
    const handleClickOutside = (event) => {
      if (!rootRef.current?.contains(event.target) && !menuRef.current?.contains(event.target)) setIsOpen(false);
    };
    const handleKeyDown = (event) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        setIsOpen(false);
        rootRef.current?.querySelector('button')?.focus();
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [isOpen]);

  useLayoutEffect(() => {
    if (!isOpen) return undefined;
    const updatePosition = () => {
      const menu = menuRef.current;
      const trigger = rootRef.current?.querySelector('button');
      if (!menu || !trigger) return;
      const rect = trigger.getBoundingClientRect();
      const below = innerHeight - rect.bottom - 20;
      const above = rect.top - 20;
      const upward = below < Math.min(menu.scrollHeight, 280) && above > below;
      menu.style.maxHeight = `${Math.max(0, Math.min(340, upward ? above : below))}px`;
      menu.style.left = `${Math.max(16, Math.min(rect.left, innerWidth - menu.offsetWidth - 16))}px`;
      menu.style.top = `${upward ? rect.top - menu.offsetHeight - 4 : rect.bottom + 4}px`;
    };
    let frameId;
    const refreshPosition = () => {
      cancelAnimationFrame(frameId);
      frameId = requestAnimationFrame(updatePosition);
    };
    const observer = new ResizeObserver(refreshPosition);
    observer.observe(menuRef.current);
    observer.observe(rootRef.current);
    updatePosition();
    window.addEventListener('resize', refreshPosition);
    window.addEventListener('scroll', refreshPosition, true);
    return () => {
      cancelAnimationFrame(frameId);
      observer.disconnect();
      window.removeEventListener('resize', refreshPosition);
      window.removeEventListener('scroll', refreshPosition, true);
    };
  }, [isOpen, currentProvider, currentChannel]);

  const itemClass = (active) => `flex w-full min-w-0 items-center justify-between gap-1.5 px-3 py-2 text-left transition-colors focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-indigo-500 ${active ? 'bg-indigo-50 font-medium text-indigo-700' : 'text-slate-600 hover:bg-slate-50'}`;

  return (
    <div ref={rootRef} className="request-filter-select min-w-0">
      <button type="button" aria-label="筛选渠道" aria-haspopup="dialog" aria-expanded={isOpen}
        aria-controls={isOpen ? menuId : undefined}
        onClick={() => {
          if (isOpen) { closeMenu(); return; }
          const nextProvider = channels.find(item => item.provider === provider) || channels[0];
          if (nextProvider) browseProvider(nextProvider);
          setIsOpen(true);
        }}
        className={`inline-flex items-center justify-between gap-1.5 rounded-lg border bg-white px-2.5 text-xs font-medium transition-[color,border-color,box-shadow] duration-200 focus-visible:outline-2 focus-visible:outline-indigo-500 ${isOpen ? 'border-indigo-500 text-indigo-700 ring-2 ring-indigo-100' : 'border-slate-200 text-slate-600 hover:border-slate-300'}`}>
        <span className="min-w-0 truncate">{displayLabel}</span>
        <ChevronDown size={14} className={`shrink-0 text-slate-400 transition-transform duration-200 ${isOpen ? 'rotate-180' : ''}`} />
      </button>

      {isOpen && createPortal(
        <div ref={menuRef} id={menuId} role="dialog" aria-label="选择渠道与凭证分组"
          onMouseDown={event => event.stopPropagation()}
          className="request-filter-channel-menu fixed z-[10010] grid overflow-hidden rounded-lg border border-slate-200 bg-white text-xs shadow-xl ring-1 ring-black/5">
          <section className="request-filter-cascade-column bg-slate-50/70" aria-label="提供商">
            <div className="request-filter-cascade-heading">提供商</div>
            <div className="request-filter-cascade-list">
              <button type="button" onClick={() => select()} className={itemClass(!provider)}>
                <span>全部</span>{!provider && <Check size={13} className="shrink-0" />}
              </button>
              {channels.map(item => (
                <button key={item.provider} type="button" aria-label={`浏览 ${item.label} 渠道`}
                  onMouseEnter={() => browseProvider(item)} onFocus={() => browseProvider(item)} onClick={() => browseProvider(item)}
                  className={itemClass(currentProvider?.provider === item.provider)}>
                  <span className="min-w-0 [overflow-wrap:anywhere]">{item.label}</span><ChevronRight size={12} className="shrink-0 text-slate-400" />
                </button>
              ))}
            </div>
          </section>

          <section className="request-filter-cascade-column" aria-label="渠道">
            <div className="request-filter-cascade-heading">{currentProvider?.label || '提供商'} · 渠道</div>
            <div className="request-filter-cascade-list">
              {currentProvider && <button type="button" onClick={() => select(currentProvider.provider)}
                className={itemClass(provider === currentProvider.provider && (!channel || channel === 'all'))}>
                <span>全部渠道</span>{provider === currentProvider.provider && (!channel || channel === 'all') && <Check size={13} className="shrink-0" />}
              </button>}
              {currentProvider?.channels.map(item => (
                <button key={item.name} type="button" aria-label={`浏览 ${item.name} 凭证分组`}
                  onMouseEnter={() => setBranch({ provider: currentProvider.provider, channel: item.name })}
                  onFocus={() => setBranch({ provider: currentProvider.provider, channel: item.name })}
                  onClick={() => setBranch({ provider: currentProvider.provider, channel: item.name })}
                  className={itemClass(currentChannel?.name === item.name)}>
                  <span className="min-w-0 [overflow-wrap:anywhere]">{item.name}</span><ChevronRight size={12} className="shrink-0 text-slate-400" />
                </button>
              ))}
              {!currentProvider?.channels.length && <p className="px-3 py-6 text-center text-slate-400">暂无渠道</p>}
            </div>
          </section>

          <section className="request-filter-cascade-column" aria-label="凭证分组">
            <Tooltip asChild content={currentChannel?.name}><div className="request-filter-cascade-heading">凭证分组</div></Tooltip>
            <div className="request-filter-cascade-list">
              {currentProvider && currentChannel ? <>
                <button type="button" onClick={() => select(currentProvider.provider, currentChannel.name)}
                  className={itemClass(provider === currentProvider.provider && channel === currentChannel.name && (!group || group === 'all'))}>
                  <span>全部分组</span>{provider === currentProvider.provider && channel === currentChannel.name && (!group || group === 'all') && <Check size={13} className="shrink-0" />}
                </button>
                {currentChannel.credentialGroups.map(name => {
                  const isSelected = provider === currentProvider.provider && channel === currentChannel.name && group === name;
                  return <button key={name} type="button" onClick={() => select(currentProvider.provider, currentChannel.name, name)} className={itemClass(isSelected)}>
                    <span className="min-w-0 [overflow-wrap:anywhere]">{name}</span>{isSelected && <Check size={13} className="shrink-0" />}
                  </button>;
                })}
                {!currentChannel.credentialGroups.length && <p className="px-3 py-6 text-center text-slate-400">暂无凭证分组</p>}
              </> : <p className="px-3 py-6 text-center text-slate-400">请先选择渠道</p>}
            </div>
          </section>
        </div>, document.body
      )}
    </div>
  );
}
