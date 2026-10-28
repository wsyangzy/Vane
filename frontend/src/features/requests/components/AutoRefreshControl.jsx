// ============================================
// AutoRefreshControl - 自动刷新控制组件
// 2025-12-01 12:56:37
// ============================================

import { useEffect, useId, useState, useRef } from 'react';
import { ChevronDown, Check, CirclePause, Loader2, RefreshCw, Timer } from 'lucide-react';

const intervalOptions = [
  { label: '仅手动刷新', value: 0 },
  { label: '5 秒', value: 5 },
  { label: '10 秒', value: 10 },
  { label: '30 秒', value: 30 },
  { label: '1 分钟', value: 60 },
];

/**
 * AutoRefreshControl - 自动刷新间隔与手动刷新
 *
 * 功能:
 * 1. 连体按钮设计（自动刷新 + 手动刷新）
 * 2. 下拉菜单直接选择间隔（包括 Off）
 * 3. 显示当前间隔，手动刷新期间显示加载反馈
 *
 * @param {Object} props
 * @param {number} props.interval - 刷新间隔(秒)
 * @param {Function} props.onIntervalChange - 更改刷新间隔
 * @param {Function} props.onManualRefresh - 手动刷新回调
 * @param {boolean} props.loading - 页面数据正在加载
 */
const AutoRefreshControl = ({
  interval = 5,
  onIntervalChange,
  onManualRefresh,
  loading = false
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const busy = loading || refreshing;
  const dropdownRef = useRef(null);
  const triggerRef = useRef(null);
  const menuId = useId();
  const enabled = interval > 0;
  const intervalLabel = enabled ? interval < 60 ? `${interval} 秒` : `${interval / 60} 分钟` : '手动';
  const IntervalIcon = enabled ? Timer : CirclePause;

  useEffect(() => {
    if (!isOpen) return;
    dropdownRef.current?.querySelector('[role="menuitemradio"][aria-checked="true"]')?.focus();
    const outside = (event) => {
      if (!dropdownRef.current?.contains(event.target)) setIsOpen(false);
    };
    const keyDown = (event) => {
      if (event.key !== 'Escape') return;
      setIsOpen(false);
      triggerRef.current?.focus();
    };
    document.addEventListener('mousedown', outside);
    document.addEventListener('keydown', keyDown);
    return () => {
      document.removeEventListener('mousedown', outside);
      document.removeEventListener('keydown', keyDown);
    };
  }, [isOpen]);

  const manualRefresh = async () => {
    if (busy) return;
    setRefreshing(true);
    try { await onManualRefresh?.(); }
    finally { setRefreshing(false); }
  };

  const menuKeyDown = (event) => {
    if (event.key === 'Tab') {
      setIsOpen(false);
      triggerRef.current?.focus();
      return;
    }
    if (!['ArrowDown', 'ArrowUp', 'ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const choices = [...event.currentTarget.querySelectorAll('[role="menuitemradio"]')];
    const index = choices.indexOf(document.activeElement);
    const next = event.key === 'Home' ? 0 : event.key === 'End' ? choices.length - 1
      : (index + (['ArrowDown', 'ArrowRight'].includes(event.key) ? 1 : -1) + choices.length) % choices.length;
    choices[next]?.focus();
  };

  const renderOption = (option) => (
    <button type="button" key={option.value} role="menuitemradio" tabIndex={-1}
      aria-checked={interval === option.value} className="requests-refresh-option" data-manual={option.value === 0}
      onClick={() => { onIntervalChange?.(option.value); setIsOpen(false); triggerRef.current?.focus(); }}>
      {option.value === 0 && <CirclePause size={14} aria-hidden="true" />}
      <span>{option.label}</span>
      <span className="requests-refresh-choice" aria-hidden="true">{interval === option.value && <Check size={10} strokeWidth={3} />}</span>
    </button>
  );

  return (
    <div className="requests-refresh-control" ref={dropdownRef}>
      {/* 主切换/显示按钮 */}
      <button
        type="button"
        ref={triggerRef}
        aria-label="自动刷新间隔"
        aria-expanded={isOpen}
        aria-haspopup="menu"
        aria-controls={isOpen ? menuId : undefined}
        data-enabled={enabled}
        onClick={() => setIsOpen(!isOpen)}
        onKeyDown={(event) => { if (['ArrowDown', 'ArrowUp'].includes(event.key)) { event.preventDefault(); setIsOpen(true); } }}
        className="requests-refresh-interval"
      >
        <IntervalIcon className="requests-refresh-symbol" aria-hidden="true" />
        <span className="requests-refresh-value">{intervalLabel}</span>
        <ChevronDown className={`requests-tool-chevron ${isOpen ? 'rotate-180' : ''}`} />
      </button>

      {/* 手动刷新按钮 - 连体右侧 */}
      <button
        type="button"
        aria-label="立即刷新"
        onClick={manualRefresh}
        disabled={busy}
        aria-busy={busy}
        className="requests-refresh-manual"
      >
        {busy ? <Loader2 className="animate-spin" /> : <RefreshCw />}
      </button>

      {/* 下拉菜单 */}
      {isOpen && (
          <div id={menuId} className="requests-refresh-menu" role="menu" aria-label="选择自动刷新间隔" onKeyDown={menuKeyDown}>
            <div className="requests-menu-heading"><span>自动刷新</span><small>{enabled ? '已开启' : '已关闭'}</small></div>
            <div className="requests-refresh-options">
              {renderOption(intervalOptions[0])}
              <div role="group" aria-label="自动刷新频率" className="requests-refresh-presets">{intervalOptions.slice(1).map(renderOption)}</div>
            </div>
          </div>
      )}
    </div>
  );
};

export default AutoRefreshControl;
