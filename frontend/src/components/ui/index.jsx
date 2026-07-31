// ============================================
// UI 组件库 - 基于 claude-dashboard 样式
// 2025-11-28
// ============================================

/* eslint-disable react-refresh/only-export-components */

import { useState, useRef, useEffect, useCallback } from 'react';
import { createPortal } from 'react-dom';
import {
  CheckCircle2,
  ChevronDown,
  AlertCircle,
  Loader2
} from 'lucide-react';

// 导入独立组件
import ConfirmDialog from './ConfirmDialog';
import SaveResultDialog from './SaveResultDialog';
import PortConflictDialog from './PortConflictDialog';
import { ToastProvider, useToast } from './Toast';
import Tooltip from './Tooltip';

// 导出独立组件
export { ConfirmDialog, SaveResultDialog, PortConflictDialog, ToastProvider, useToast, Tooltip };

const getSegmentedTabsSliderStyle = (activeIndex, tabCount) => {
  const safeCount = Math.max(tabCount, 1);
  const safeIndex = Math.min(Math.max(activeIndex, 0), safeCount - 1);
  const gapTotalRem = Math.max(safeCount - 1, 0) * 0.25;
  const tabWidth = `calc((100% - 0.5rem - ${gapTotalRem}rem) / ${safeCount})`;

  return {
    width: tabWidth,
    left: '0.25rem',
    transform: `translateX(calc(${safeIndex * 100}% + ${safeIndex * 0.25}rem))`
  };
};

export const SegmentedTabs = ({
  tabs = [],
  activeTab,
  onChange,
  disabled = false,
  className = 'grid-cols-2',
  buttonPaddingClass = 'px-3'
}) => {
  const activeIndex = tabs.findIndex((tab) => tab.key === activeTab);

  return (
    <div className={`relative grid gap-1 rounded-xl bg-slate-100 p-1 ${className}`}>
      <span
        className="motion-tabs-slider pointer-events-none absolute inset-y-1 rounded-lg bg-white shadow-sm ring-1 ring-indigo-100"
        style={getSegmentedTabsSliderStyle(activeIndex, tabs.length)}
      />
      {tabs.map((tab) => {
        const active = activeTab === tab.key;
        return (
          <button
            key={tab.key}
            type="button"
            onClick={() => onChange?.(tab.key)}
            className={`
              relative z-10 inline-flex h-8 items-center justify-center rounded-lg ${buttonPaddingClass}
              motion-tabs-label text-xs font-semibold
              ${active ? 'text-indigo-700' : 'text-slate-500 hover:text-slate-700'}
            `}
            disabled={disabled}
            aria-pressed={active}
          >
            {tab.label}
          </button>
        );
      })}
    </div>
  );
};

// ============================================
// KPI 卡片组件
// ============================================
export const KPICard = ({
  title,
  value,
  subText,
  icon: Icon,
  statusColor = 'bg-slate-100 text-slate-600',
  layout = 'stacked',
  size = 'md',
  wrapTitle = false,
  wrapValue = false,
  titleClassName = '',
  valueClassName = '',
  iconSize,
  iconContainerClassName = '',
  iconClassName = ''
}) => {
  if (layout === 'side') {
    const isLarge = size === 'lg';
    const resolvedIconSize = iconSize ?? (isLarge ? 19 : 18);
    return (
      <div className={`motion-kpi-card h-full bg-white rounded-2xl border border-slate-200/60 shadow-sm flex flex-col ${isLarge ? 'p-5' : 'p-4'}`}>
        <div className={`flex items-center justify-between ${isLarge ? 'gap-3' : 'gap-2.5'}`}>
          <div className={`flex flex-shrink-0 items-center ${isLarge ? 'gap-3.5' : 'gap-3'}`}>
            <div className={`motion-kpi-icon rounded-lg flex-shrink-0 ${isLarge ? 'p-2.5' : 'p-2'} ${statusColor} ${iconContainerClassName}`}>
              <Icon size={resolvedIconSize} className={iconClassName} />
            </div>
            <h3 className={`${wrapTitle ? 'whitespace-normal break-words leading-snug' : 'whitespace-nowrap'} text-slate-500 font-semibold uppercase tracking-wider ${isLarge ? 'text-[11px]' : 'text-xs'} ${titleClassName}`}>{title}</h3>
          </div>
          <div
            className={`min-w-0 flex-1 text-right font-bold text-slate-900 cursor-default ${wrapValue ? 'whitespace-normal break-all leading-snug' : 'truncate'} ${isLarge ? 'text-[1.2rem]' : 'text-lg'} ${isLarge && wrapValue ? 'text-[1.05rem]' : ''} ${!isLarge && wrapValue ? 'text-base' : ''} ${valueClassName}`}
          >
            {value}
          </div>
        </div>
        {subText && (
          <div
            className={`mt-auto truncate whitespace-nowrap pt-2 text-slate-400 font-medium ${isLarge ? 'text-[11px]' : 'text-xs'}`}
          >
            {subText}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="motion-kpi-card bg-white p-5 rounded-2xl border border-slate-200/60 shadow-sm flex flex-col justify-between h-full group relative">
      <div className="flex justify-between items-start mb-2">
        <div className={`motion-kpi-icon p-2 rounded-lg ${statusColor}`}>
          <Icon size={20} />
        </div>
      </div>
      <div className="mt-auto">
        <h3 className="text-slate-500 text-xs font-semibold uppercase tracking-wider mb-1">{title}</h3>
        <div className="text-xl font-bold text-slate-900 truncate cursor-default">
          {value}
        </div>
      </div>
      {subText && (
        <div className="absolute left-1/2 -translate-x-1/2 -bottom-1 translate-y-full opacity-0 group-hover:opacity-100 transition-opacity duration-200 z-10 pointer-events-none">
          <div className="px-3 py-1.5 bg-slate-800 text-white text-xs font-medium rounded-lg shadow-lg text-center w-max max-w-[280px]">
            {subText}
          </div>
        </div>
      )}
    </div>
  );
};

// ============================================
// 统计详情项
// ============================================
export const StatDetailItem = ({ label, value, unit, valueColor }) => (
  <div className="bg-slate-50 rounded-xl p-4 flex flex-col items-center justify-center border border-slate-100 hover:border-slate-200 transition-colors group">
    <div className={`text-2xl font-bold mb-1 ${valueColor || 'text-slate-900'} group-hover:scale-105 transition-transform`}>
      {value}<span className="text-sm font-medium text-slate-500 ml-0.5">{unit}</span>
    </div>
    <div className="text-xs text-slate-400 font-medium">{label}</div>
  </div>
);

// ============================================
// 加载 Spinner
// ============================================
export const LoadingSpinner = ({ size = 'md', text = '加载中...' }) => {
  const sizeClasses = {
    sm: 'w-4 h-4',
    md: 'w-8 h-8',
    lg: 'w-12 h-12'
  };

  return (
    <div className="flex flex-col items-center justify-center py-12">
      <div className={`${sizeClasses[size]} border-2 border-slate-200 border-t-indigo-600 rounded-full animate-spin`}></div>
      {text && <p className="mt-3 text-sm text-slate-500">{text}</p>}
    </div>
  );
};

// ============================================
// 空状态
// ============================================
export const EmptyState = ({ icon: Icon, title, description, action }) => (
  <div className="flex flex-col items-center justify-center py-16 text-center">
    {Icon && (
      <div className="p-4 bg-slate-100 rounded-full mb-4">
        <Icon size={32} className="text-slate-400" />
      </div>
    )}
    <h3 className="text-lg font-semibold text-slate-700 mb-2">{title}</h3>
    {description && <p className="text-sm text-slate-500 max-w-md mb-4">{description}</p>}
    {action}
  </div>
);

// ============================================
// 错误提示
// ============================================
export const ErrorMessage = ({ title = '加载失败', message, onRetry }) => (
  <div className="flex flex-col items-center justify-center py-12 text-center">
    <div className="p-4 bg-rose-50 rounded-full mb-4">
      <AlertCircle size={32} className="text-rose-500" />
    </div>
    <h3 className="text-lg font-semibold text-rose-700 mb-2">{title}</h3>
    {message && <p className="text-sm text-rose-600 max-w-md mb-4">{message}</p>}
    {onRetry && (
      <button
        onClick={onRetry}
        className="px-4 py-2 bg-rose-600 text-white rounded-lg text-sm font-medium hover:bg-rose-700 transition-colors"
      >
        重试
      </button>
    )}
  </div>
);

// ============================================
// 按钮组件
// ============================================
export const Button = ({
  children,
  variant = 'primary',
  size = 'md',
  icon: Icon,
  loading = false,
  disabled = false,
  className = '',
  title,
  tooltip,
  'aria-label': ariaLabel,
  ...props
}) => {
  const variants = {
    primary: 'bg-indigo-600 text-white hover:bg-indigo-700 shadow-md hover:shadow-lg',
    secondary: 'bg-slate-100 text-slate-600 hover:bg-slate-200',
    success: 'bg-emerald-600 text-white hover:bg-emerald-700',
    danger: 'bg-rose-600 text-white hover:bg-rose-700',
    dangerSoft: 'bg-rose-50 text-rose-700 hover:bg-rose-100 border border-rose-200',
    ghost: 'bg-transparent text-slate-600 hover:bg-slate-100'
  };

  const sizes = {
    sm: 'px-3 py-1.5 text-xs',
    md: 'px-4 py-2 text-sm',
    lg: 'px-6 py-3 text-base'
  };

  const tooltipContent = tooltip ?? title;
  const buttonClassName = `
        inline-flex items-center justify-center font-medium rounded-lg transition-all duration-200
        ${variants[variant]} ${sizes[size]}
        ${disabled || loading ? 'opacity-50 cursor-not-allowed' : 'hover:-translate-y-0.5'}
        ${className}
      `;

  const button = (
    <button
      disabled={disabled || loading}
      className={buttonClassName}
      aria-label={ariaLabel || (typeof tooltipContent === 'string' ? tooltipContent : undefined)}
      {...props}
    >
      {loading ? (
        <Loader2 size={16} className="animate-spin mr-2" />
      ) : Icon && (
        <Icon size={16} className="mr-2" />
      )}
      {children}
    </button>
  );

  if (!tooltipContent) return button;

  return (
    <Tooltip content={tooltipContent} className={className.includes('w-full') ? 'w-full' : ''}>
      {button}
    </Tooltip>
  );
};

// ============================================
// 输入框
// ============================================
export const Input = ({
  label,
  error,
  className = '',
  ...props
}) => (
  <div className="flex flex-col">
    {label && (
      <label className="text-sm font-medium text-slate-700 mb-1.5">{label}</label>
    )}
    <input
      className={`
        w-full px-3 py-2 border rounded-lg text-sm
        focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500
        ${error ? 'border-rose-300 focus:ring-rose-500 focus:border-rose-500' : 'border-slate-200'}
        ${className}
      `}
      {...props}
    />
    {error && (
      <span className="text-xs text-rose-500 mt-1">{error}</span>
    )}
  </div>
);

// ============================================
// 选择框（兼容旧 API，内部使用统一样式）
// ============================================
export const Select = ({
  label,
  options = [],
  className = '',
  value,
  onChange,
  disabled = false,
  placeholder = '请选择',
  size = 'md',
  fullWidth = true,
  minWidth,
  ...props
}) => {
  const handleChange = (nextValue) => {
    onChange?.({
      target: {
        value: nextValue,
        name: props.name
      }
    });
  };

  return (
    <div className="flex flex-col">
      {label && (
        <label className="text-sm font-medium text-slate-700 mb-1.5">{label}</label>
      )}
      <CustomSelect
        options={options}
        value={value}
        onChange={handleChange}
        disabled={disabled}
        placeholder={placeholder}
        size={size}
        fullWidth={fullWidth}
        minWidth={minWidth}
        className={className}
      />
    </div>
  );
};

// ============================================
// 自定义下拉选择器（统一样式）
// ============================================
export const CustomSelect = ({
  options = [],
  value,
  onChange,
  size = 'sm',
  placeholder = '请选择',
  disabled = false,
  fullWidth = false,
  minWidth = null,
  embedded = false,
  ariaLabel,
  className = ''
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const [isRendered, setIsRendered] = useState(false);
  const [dropdownState, setDropdownState] = useState('');
  const [dropdownPosition, setDropdownPosition] = useState({ top: 0, left: 0, width: 0, isUpward: false });
  const containerRef = useRef(null);
  const buttonRef = useRef(null);
  const dropdownRef = useRef(null);
  const closeTimerRef = useRef(null);

  const currentOption = options.find(opt => opt.value === value);

  const updatePosition = useCallback(() => {
    if (buttonRef.current && dropdownRef.current) {
      const buttonRect = buttonRef.current.getBoundingClientRect();
      const dropdownHeight = dropdownRef.current.offsetHeight || 200;
      const dropdownWidth = Math.min(
        Math.max(buttonRect.width, dropdownRef.current.offsetWidth),
        window.innerWidth - 16
      );
      const viewportHeight = window.innerHeight;
      const spaceBelow = viewportHeight - buttonRect.bottom;
      const spaceAbove = buttonRect.top;
      const shouldOpenUpward = spaceBelow < dropdownHeight && spaceAbove > spaceBelow;
      const maxLeft = Math.max(8, window.innerWidth - dropdownWidth - 8);
      const left = Math.min(Math.max(8, buttonRect.left), maxLeft);
      const unclampedTop = shouldOpenUpward
        ? buttonRect.top - dropdownHeight - 4
        : buttonRect.bottom + 4;
      const top = Math.min(
        Math.max(8, unclampedTop),
        Math.max(8, viewportHeight - dropdownHeight - 8)
      );

      setDropdownPosition({
        top,
        left,
        width: buttonRect.width,
        isUpward: shouldOpenUpward
      });
    }
  }, []);

  const openDropdown = useCallback(() => {
    if (closeTimerRef.current) window.clearTimeout(closeTimerRef.current);
    setDropdownState('');
    setIsRendered(true);
    setIsOpen(true);
  }, []);

  const closeDropdown = useCallback(() => {
    if (closeTimerRef.current) window.clearTimeout(closeTimerRef.current);
    setIsOpen(false);
    setDropdownState('is-closing');
    const closeDuration = parseFloat(
      getComputedStyle(document.documentElement).getPropertyValue('--dropdown-close-dur')
    ) || 150;
    closeTimerRef.current = window.setTimeout(() => {
      setIsRendered(false);
      setDropdownState('');
    }, closeDuration);
  }, []);

  useEffect(() => {
    if (!isOpen || !isRendered) return undefined;

    const frameId = window.requestAnimationFrame(() => setDropdownState('is-open'));
    return () => {
      window.cancelAnimationFrame(frameId);
    };
  }, [isOpen, isRendered]);

  useEffect(() => () => {
    if (closeTimerRef.current) window.clearTimeout(closeTimerRef.current);
  }, []);

  useEffect(() => {
    if (!isRendered) return undefined;

    let frameId;
    const refreshPosition = () => {
      updatePosition();
      window.cancelAnimationFrame(frameId);
      frameId = window.requestAnimationFrame(updatePosition);
    };
    const animatedAncestor = containerRef.current?.closest('.animate-in');
    refreshPosition();
    window.addEventListener('scroll', refreshPosition, true);
    window.addEventListener('resize', refreshPosition);
    animatedAncestor?.addEventListener('animationend', refreshPosition);
    return () => {
      window.cancelAnimationFrame(frameId);
      window.removeEventListener('scroll', refreshPosition, true);
      window.removeEventListener('resize', refreshPosition);
      animatedAncestor?.removeEventListener('animationend', refreshPosition);
    };
  }, [isRendered, updatePosition]);

  useEffect(() => {
    if (!isOpen) return undefined;

    const handleClickOutside = (event) => {
      if (
        containerRef.current
        && !containerRef.current.contains(event.target)
        && !dropdownRef.current?.contains(event.target)
      ) {
        closeDropdown();
      }
    };
    const handleKeyDown = (event) => {
      if (event.key === 'Escape') closeDropdown();
    };

    document.addEventListener('mousedown', handleClickOutside);
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [closeDropdown, isOpen]);

  const handleSelect = (optValue) => {
    onChange?.(optValue);
    closeDropdown();
  };

  const sizeClasses = {
    xs: 'px-2 py-1 text-xs min-w-[80px]',
    sm: 'px-2.5 py-1.5 text-xs min-w-[100px]',
    md: 'px-3 py-2 text-sm min-w-[120px]'
  };
  const embeddedSizeClasses = {
    xs: 'h-full px-2 text-xs min-w-[56px]',
    sm: 'h-full px-2.5 text-xs min-w-[64px]',
    md: 'h-full px-3 text-sm min-w-[72px]'
  };

  const dropdownSizeClasses = {
    xs: 'text-xs',
    sm: 'text-xs',
    md: 'text-sm'
  };

  const dropdown = isRendered && createPortal(
    <div
      ref={dropdownRef}
      data-origin={dropdownPosition.isUpward ? 'bottom-left' : 'top-left'}
      onMouseDown={(event) => event.stopPropagation()}
      className={`
        t-dropdown ${dropdownState} fixed z-[9999] overflow-hidden rounded-lg
        border border-slate-200 bg-white shadow-lg ring-1 ring-black/5
        ${dropdownSizeClasses[size]}
      `}
      style={{
        top: `${dropdownPosition.top}px`,
        left: `${dropdownPosition.left}px`,
        minWidth: `${dropdownPosition.width}px`,
        maxWidth: 'calc(100vw - 16px)',
        maxHeight: '240px'
      }}
      role="listbox"
      aria-label={ariaLabel || placeholder}
    >
      <div className="max-h-[240px] overflow-y-auto py-1">
        {options.map((option) => {
          const isSelected = option.value === value;
          return (
            <button
              key={option.value}
              type="button"
              role="option"
              aria-selected={isSelected}
              onClick={() => handleSelect(option.value)}
              className={`
                flex w-full cursor-pointer items-center justify-between whitespace-nowrap px-3 py-2 text-left transition-colors
                ${isSelected
                  ? 'bg-indigo-50 font-medium text-indigo-600'
                  : 'text-slate-600 hover:bg-slate-50'
                }
              `}
            >
              <span>{option.label}</span>
              {isSelected && (
                <CheckCircle2 size={14} className="ml-2 flex-shrink-0 text-indigo-500" />
              )}
            </button>
          );
        })}
      </div>
    </div>,
    document.body
  );

  return (
    <div className={`relative ${fullWidth || className ? 'block' : 'inline-block'} ${embedded ? 'h-full' : ''} ${className}`} ref={containerRef}>
      <button
        ref={buttonRef}
        type="button"
        onClick={() => {
          if (disabled) return;
          if (isOpen) closeDropdown();
          else openDropdown();
        }}
        disabled={disabled}
        style={minWidth ? { minWidth: `${minWidth}px` } : undefined}
        aria-haspopup="listbox"
        aria-expanded={isOpen}
        aria-label={ariaLabel}
        className={`
          group inline-flex items-center justify-between font-medium focus:outline-none
          ${embedded
            ? 'rounded-none border-0 bg-transparent transition-colors'
            : 'rounded-lg border bg-white transition-[color,border-color,box-shadow] duration-200 focus-visible:ring-2 focus-visible:ring-indigo-100'
          }
          ${fullWidth ? 'w-full' : ''}
          ${embedded ? embeddedSizeClasses[size] : sizeClasses[size]}
          ${embedded
            ? disabled
              ? 'cursor-not-allowed text-slate-400'
              : isOpen
                ? 'text-indigo-600'
                : 'text-slate-600 hover:text-slate-800'
            : disabled
              ? 'cursor-not-allowed border-slate-200 text-slate-400 opacity-50'
              : isOpen
                ? 'border-indigo-500 text-indigo-600 ring-2 ring-indigo-100'
                : 'border-slate-200 text-slate-600 hover:border-slate-300 hover:text-slate-700'
          }
        `}
      >
        <span className={`${embedded ? 'flex-1 text-center' : ''} truncate`}>
          {currentOption?.label || placeholder}
        </span>
        <ChevronDown
          size={size === 'xs' ? 12 : 14}
          className={`ml-1.5 flex-shrink-0 transition-transform duration-200 ${
            isOpen ? 'rotate-180' : ''
          } ${disabled ? 'text-slate-300' : 'text-slate-400'}`}
        />
      </button>
      {dropdown}
    </div>
  );
};
