import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Check, ChevronDown, RotateCcw, Search, Square, SquareCheck } from 'lucide-react';
import { requestModelOptions } from '../utils/modelFilter.js';

export default function ModelFilterSelect({ models = [], value = [], onChange }) {
  const [isOpen, setIsOpen] = useState(false);
  const [search, setSearch] = useState('');
  const rootRef = useRef(null);
  const menuRef = useRef(null);
  const searchRef = useRef(null);
  const menuId = useId();
  const options = useMemo(() => requestModelOptions(models, value), [models, value]);
  const matching = options.filter(name => name.toLowerCase().includes(search.trim().toLowerCase()));
  const label = value.length === 0 ? '所有模型' : value.length === 1 ? value[0] : `已选 ${value.length} 个模型`;

  const closeMenu = () => {
    setIsOpen(false);
    rootRef.current?.querySelector('button')?.focus({ preventScroll: true });
  };

  useEffect(() => {
    if (!isOpen) return undefined;
    const handleOutside = event => {
      if (!rootRef.current?.contains(event.target) && !menuRef.current?.contains(event.target)) setIsOpen(false);
    };
    const handleKeyDown = event => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      setIsOpen(false);
      rootRef.current?.querySelector('button')?.focus({ preventScroll: true });
    };
    const handleScroll = event => {
      if (!menuRef.current?.contains(event.target)) setIsOpen(false);
    };
    const handleResize = () => setIsOpen(false);
    document.addEventListener('pointerdown', handleOutside);
    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('scroll', handleScroll, true);
    window.addEventListener('resize', handleResize);
    return () => {
      document.removeEventListener('pointerdown', handleOutside);
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('scroll', handleScroll, true);
      window.removeEventListener('resize', handleResize);
    };
  }, [isOpen]);

  useLayoutEffect(() => {
    if (!isOpen) return undefined;
    const updatePosition = () => {
      const menu = menuRef.current;
      const trigger = rootRef.current?.querySelector('button');
      if (!menu || !trigger) return;
      const rect = trigger.getBoundingClientRect();
      const width = Math.min(Math.max(320, rect.width), window.innerWidth - 32);
      menu.style.width = `${width}px`;
      const below = Math.max(0, window.innerHeight - rect.bottom - 20);
      const above = Math.max(0, rect.top - 20);
      const desiredHeight = Math.min(360, menu.scrollHeight);
      const upward = below < desiredHeight && above > below;
      menu.style.maxHeight = `${Math.min(360, upward ? above : below)}px`;
      menu.style.left = `${Math.max(16, Math.min(rect.left, window.innerWidth - width - 16))}px`;
      menu.style.top = `${upward ? rect.top - menu.offsetHeight - 4 : rect.bottom + 4}px`;
    };
    updatePosition();
    searchRef.current?.focus({ preventScroll: true });
    const observer = new ResizeObserver(updatePosition);
    observer.observe(menuRef.current);
    observer.observe(rootRef.current);
    const animatedAncestor = rootRef.current?.closest('.animate-in');
    animatedAncestor?.addEventListener('animationend', updatePosition);
    return () => {
      observer.disconnect();
      animatedAncestor?.removeEventListener('animationend', updatePosition);
    };
  }, [isOpen]);

  const toggleModel = name => onChange?.(value.includes(name) ? value.filter(item => item !== name) : [...value, name]);
  const handleNavigation = event => {
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
    if (event.target === searchRef.current && ['Home', 'End'].includes(event.key)) return;
    const items = [...menuRef.current.querySelectorAll('[role="option"]')];
    const current = items.indexOf(document.activeElement);
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1
      : event.key === 'ArrowDown' ? (current + 1) % items.length : (current <= 0 ? items.length : current) - 1;
    event.preventDefault();
    items[next]?.focus({ preventScroll: true });
    items[next]?.scrollIntoView({ block: 'nearest' });
  };
  const itemClass = selected => `flex w-full items-start gap-2 rounded-md px-2.5 py-2 text-left text-xs transition-colors focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-indigo-500 ${selected ? 'bg-indigo-50 text-indigo-700' : 'text-slate-600 hover:bg-slate-50'}`;

  return (
    <div ref={rootRef} className="request-filter-select min-w-0">
      <button type="button" aria-label="筛选模型" aria-haspopup="dialog" aria-expanded={isOpen}
        aria-controls={isOpen ? menuId : undefined} title={value.length ? value.join('、') : undefined}
        onClick={() => {
          if (isOpen) { closeMenu(); return; }
          setSearch('');
          setIsOpen(true);
        }}
        className={`inline-flex items-center justify-between gap-1.5 rounded-lg border bg-white px-2.5 text-xs font-medium transition-colors focus-visible:outline-2 focus-visible:outline-indigo-500 ${isOpen ? 'border-indigo-500 text-indigo-700 ring-2 ring-indigo-100' : 'border-slate-200 text-slate-600 hover:border-slate-300'}`}>
        <span className="min-w-0 truncate">{label}</span>
        <ChevronDown size={14} className={`shrink-0 text-slate-400 transition-transform duration-200 ${isOpen ? 'rotate-180' : ''}`} />
      </button>
      {isOpen && createPortal(
        <div ref={menuRef} id={menuId} role="dialog" aria-label="选择模型"
          onMouseDown={event => event.stopPropagation()}
          onKeyDown={handleNavigation}
          className="request-filter-model-menu fixed z-[10010] flex flex-col overflow-hidden rounded-lg border border-slate-200 bg-white text-xs shadow-xl ring-1 ring-black/5">
          <div className="shrink-0 border-b border-slate-100 p-2">
            <div className="flex items-center gap-2 rounded-md border border-slate-200 px-2.5 focus-within:border-indigo-500 focus-within:ring-2 focus-within:ring-indigo-100">
              <Search size={14} className="shrink-0 text-slate-400" />
              <input ref={searchRef} type="search" aria-label="搜索模型" placeholder="输入模型名称搜索"
                value={search} onChange={event => setSearch(event.target.value)}
                className="h-8 w-full min-w-0 bg-transparent text-xs text-slate-700 outline-none placeholder:text-slate-400" />
            </div>
          </div>
          <div role="listbox" aria-label="模型" aria-multiselectable="true" className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-1.5">
            <button type="button" role="option" aria-selected={value.length === 0} onClick={() => onChange?.([])} className={itemClass(value.length === 0)}>
              {value.length === 0 ? <SquareCheck size={14} className="mt-0.5 shrink-0" /> : <Square size={14} className="mt-0.5 shrink-0 text-slate-300" />}
              <span>所有模型</span>
            </button>
            {matching.map(name => {
              const selected = value.includes(name);
              return <button key={name} type="button" role="option" aria-selected={selected} onClick={() => toggleModel(name)} className={itemClass(selected)}>
                {selected ? <SquareCheck size={14} className="mt-0.5 shrink-0" /> : <Square size={14} className="mt-0.5 shrink-0 text-slate-300" />}
                <span className="min-w-0 [overflow-wrap:anywhere]">{name}</span>
              </button>;
            })}
            {matching.length === 0 && <p role="status" className="px-3 py-5 text-center text-slate-400">{options.length ? '没有匹配的模型' : '暂无可选模型'}</p>}
          </div>
          <div className="flex shrink-0 items-center justify-between gap-2 border-t border-slate-100 px-2.5 py-2">
            <span className="text-slate-400" aria-live="polite">{value.length ? `已选 ${value.length} 个模型` : '不限模型'}</span>
            <div className="flex items-center gap-1">
              <button type="button" disabled={!value.length} onClick={() => onChange?.([])} className="inline-flex items-center gap-1 rounded-md px-2 py-1.5 text-slate-500 hover:bg-slate-50 disabled:opacity-40"><RotateCcw size={12} />清空</button>
              <button type="button" onClick={closeMenu} className="inline-flex items-center gap-1 rounded-md bg-indigo-50 px-2 py-1.5 text-indigo-600 hover:bg-indigo-100"><Check size={12} />完成</button>
            </div>
          </div>
        </div>, document.body
      )}
    </div>
  );
}
