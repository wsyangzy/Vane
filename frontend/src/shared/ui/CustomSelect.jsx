import { useState, useRef, useEffect, useLayoutEffect, useCallback } from 'react';
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
  const [dropdownPosition, setDropdownPosition] = useState({ top: 0, left: 0, width: 0, maxHeight: 240, isUpward: false });
  const containerRef = useRef(null);
  const buttonRef = useRef(null);
  const dropdownRef = useRef(null);
  const closeTimerRef = useRef(null);

  const currentOption = options.find(opt => opt.value === value);
  const descriptive = options.some(option => option.description);

  const updatePosition = useCallback(() => {
    if (buttonRef.current && dropdownRef.current) {
      const buttonRect = buttonRef.current.getBoundingClientRect();
      const dropdown = dropdownRef.current;
      const dropdownWidth = Math.min(
        descriptive ? Math.max(360, buttonRect.width) : wrapOptions ? Math.max(320, buttonRect.width) : Math.max(buttonRect.width, dropdown.offsetWidth),
        Math.max(0, window.innerWidth - 16)
      );
      // Measure wrapped descriptions at their final width before the first
      // paint. A height measured at the portal's initial width can misplace an
      // upward-opening menu or make it overlap its trigger.
      if (descriptive || wrapOptions) dropdown.style.width = `${dropdownWidth}px`;
      else dropdown.style.minWidth = `${Math.min(buttonRect.width, dropdownWidth)}px`;
      const heightLimit = descriptive ? 400 : 240;
      const dropdownHeight = Math.min(heightLimit, (dropdown.firstElementChild?.scrollHeight || dropdown.offsetHeight) + 2);
      const viewportHeight = window.innerHeight;
      const spaceBelow = Math.max(0, viewportHeight - buttonRect.bottom - 12);
      const spaceAbove = Math.max(0, buttonRect.top - 12);
      const shouldOpenUpward = spaceBelow < dropdownHeight && spaceAbove > spaceBelow;
      const maxHeight = Math.min(heightLimit, shouldOpenUpward ? spaceAbove : spaceBelow);
      const maxLeft = Math.max(8, window.innerWidth - dropdownWidth - 8);
      const left = Math.min(Math.max(8, buttonRect.left), maxLeft);
      const top = shouldOpenUpward
        ? buttonRect.top - Math.min(dropdownHeight, maxHeight) - 4
        : buttonRect.bottom + 4;
      const next = { top, left, width: descriptive || wrapOptions ? dropdownWidth : Math.min(buttonRect.width, dropdownWidth), maxHeight, isUpward: shouldOpenUpward };
      setDropdownPosition(previous => {
        return Object.keys(next).every(key => next[key] === previous[key]) ? previous : next;
      });
    }
  }, [descriptive, wrapOptions]);

  const openDropdown = useCallback(() => {
    if (closeTimerRef.current) window.clearTimeout(closeTimerRef.current);
    setDropdownState('');
    setIsRendered(true);
    setIsOpen(true);
  }, []);

  const closeDropdown = useCallback((immediate = false) => {
    if (closeTimerRef.current) window.clearTimeout(closeTimerRef.current);
    setIsOpen(false);
    if (immediate) {
      setIsRendered(false);
      setDropdownState('');
      return;
    }
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

  // Reopening during the exit animation reuses the portal. Recalculate its
  // position even when isRendered stays true and the viewport has changed.
  useLayoutEffect(() => {
    if (!isRendered) return undefined;
    updatePosition();
    const animatedAncestor = containerRef.current?.closest('.animate-in');
    animatedAncestor?.addEventListener('animationend', updatePosition);
    return () => {
      animatedAncestor?.removeEventListener('animationend', updatePosition);
    };
  }, [isRendered, isOpen, updatePosition, options]);

  useEffect(() => {
    if (!isOpen) return undefined;
    const handleScroll = event => {
      // Scrolling the option list keeps it open. Scrolling any containing page
      // dismisses the portal, so it cannot remain over an unrelated field.
      if (!dropdownRef.current?.contains(event.target)) closeDropdown(true);
    };
    const handleResize = () => closeDropdown(true);
    window.addEventListener('scroll', handleScroll, true);
    window.addEventListener('resize', handleResize);
    return () => {
      window.removeEventListener('scroll', handleScroll, true);
      window.removeEventListener('resize', handleResize);
    };
  }, [closeDropdown, isOpen]);

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

    document.addEventListener('pointerdown', handleClickOutside);
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('pointerdown', handleClickOutside);
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
        maxHeight: `${dropdownPosition.maxHeight}px`
      }}
      role="listbox"
      inert={!isOpen}
      aria-hidden={!isOpen}
      aria-label={ariaLabel || placeholder}
    >
      <div className={descriptive ? 'overflow-y-auto overscroll-contain p-1.5' : 'overflow-y-auto overscroll-contain py-1'} style={{ maxHeight: `${Math.max(0, dropdownPosition.maxHeight - 2)}px` }}>
        {options.map((option) => {
          const isSelected = option.value === value;
          return (
            <button
              key={option.value}
              type="button"
              role="option"
              aria-selected={isSelected}
              aria-disabled={Boolean(option.disabled)}
              disabled={option.disabled}
              onClick={() => handleSelect(option.value)}
              className={`
                flex w-full min-w-0 cursor-pointer items-center justify-between gap-2 px-3 py-2 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-45
                ${descriptive ? 'rounded-md' : ''}
                ${isSelected
                  ? 'bg-indigo-50 font-medium text-indigo-600'
                  : 'text-slate-600 enabled:hover:bg-slate-50'
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
