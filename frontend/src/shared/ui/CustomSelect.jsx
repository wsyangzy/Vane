import { useState, useRef, useEffect, useCallback } from 'react';
import { createPortal } from 'react-dom';
import { CheckCircle2, ChevronDown } from 'lucide-react';
import { motionDuration } from '@/shared/hooks/usePresence.js';

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
  wrapOptions = false,
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
  const descriptive = options.some(option => option.description);

  const updatePosition = useCallback(() => {
    if (buttonRef.current && dropdownRef.current) {
      const buttonRect = buttonRef.current.getBoundingClientRect();
      const dropdownHeight = dropdownRef.current.offsetHeight || 200;
      const dropdownWidth = Math.min(
        descriptive ? 360 : wrapOptions ? Math.max(320, buttonRect.width) : Math.max(buttonRect.width, dropdownRef.current.offsetWidth),
        window.innerWidth - 16
      );
      const viewportHeight = window.innerHeight;
      const spaceBelow = viewportHeight - buttonRect.bottom;
      const spaceAbove = buttonRect.top;
      const shouldOpenUpward = spaceBelow < dropdownHeight && spaceAbove > spaceBelow;
      const maxLeft = Math.max(8, window.innerWidth - dropdownWidth - 8);
      const left = Math.min(Math.max(8, descriptive ? buttonRect.right - dropdownWidth : buttonRect.left), maxLeft);
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
        width: descriptive || wrapOptions ? dropdownWidth : buttonRect.width,
        isUpward: shouldOpenUpward
      });
    }
  }, [descriptive, wrapOptions]);

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
    const closeDuration = motionDuration('--dropdown-close-dur', 150);
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

  // 下拉浮层挂在 document.body 上，层级必须高于弹窗层（弹窗统一 z-[10000]），
  // 否则弹窗内的 CustomSelect 展开后会被弹窗面板整个盖住，表现为"点了没反应"。
  const dropdown = isRendered && createPortal(
    <div
      ref={dropdownRef}
      data-origin={dropdownPosition.isUpward ? 'bottom-left' : 'top-left'}
      onMouseDown={(event) => event.stopPropagation()}
      className={`
        t-dropdown ${dropdownState} fixed z-[10010] overflow-hidden rounded-lg
        border border-slate-200 bg-white shadow-lg ring-1 ring-black/5
        ${dropdownSizeClasses[size]}
      `}
      style={{
        top: `${dropdownPosition.top}px`,
        left: `${dropdownPosition.left}px`,
        ...(descriptive || wrapOptions ? { width: `${dropdownPosition.width}px` } : { minWidth: `${dropdownPosition.width}px` }),
        maxWidth: 'calc(100vw - 16px)',
        maxHeight: descriptive ? 'min(400px, calc(100dvh - 16px))' : '240px'
      }}
      role="listbox"
      inert={!isOpen}
      aria-hidden={!isOpen}
      aria-label={ariaLabel || placeholder}
    >
      <div className={descriptive ? 'max-h-[min(400px,calc(100dvh-16px))] overflow-y-auto p-1.5' : 'max-h-[240px] overflow-y-auto py-1'}>
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
                flex w-full min-w-0 cursor-pointer items-center justify-between gap-2 px-3 py-2 text-left transition-colors
                ${descriptive ? 'rounded-md' : ''}
                ${isSelected
                  ? 'bg-indigo-50 font-medium text-indigo-600'
                  : 'text-slate-600 hover:bg-slate-50'
                }
              `}
            >
              <span className="min-w-0">
                <span className={`flex items-center gap-2 ${descriptive ? 'flex-wrap text-xs' : wrapOptions ? 'whitespace-normal [overflow-wrap:anywhere]' : 'whitespace-nowrap'}`}>
                  <span>{option.label}</span>
                  {option.badge && <span className="rounded border border-slate-200 bg-slate-50 px-1.5 py-0.5 text-[10px] font-normal text-slate-500">{option.badge}</span>}
                </span>
                {option.description && <span className="mt-0.5 block whitespace-normal text-[11px] font-normal leading-4 text-slate-500 [overflow-wrap:anywhere]">{option.menuDescription || option.description}</span>}
              </span>
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
