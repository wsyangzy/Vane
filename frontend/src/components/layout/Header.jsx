// ============================================
// 布局组件 - Header 导航栏
// 2025-11-28 (Updated 2026-02-09 for user menu)
// ============================================

import { useCallback, useEffect, useRef, useState } from 'react';
import { Command } from 'lucide-react';
import { isWailsEnvironment } from '@utils/wailsApi.js';
import { useToast } from '@components/ui';
import UserMenu from './UserMenu.jsx';
import WindowControls from './WindowControls.jsx';

const NAV_TABS = [
  { name: 'overview', label: '概览' },
  { name: 'channels', label: 'Claude 管理' },
  { name: 'codex', label: 'Codex 管理' },
  // 组管理入口已移除，配置简化后不再需要独立的组管理页面
  // { name: 'groups', label: '组管理' },
  { name: 'requests', label: '请求追踪' },
  { name: 'privacy', label: '隐私防护' },
  { name: 'pricing', label: '基础定价' },
  // 新增日志页面
  { name: 'logs', label: '系统日志' },
  // 配置改为设置
  { name: 'settings', label: '设置' }
];

// 导航标签项
const NavItem = ({ label, active, onClick, buttonRef }) => (
  <button
    ref={buttonRef}
    onClick={onClick}
    className={`relative z-10 whitespace-nowrap px-3 py-2 text-sm font-medium rounded-full transition-colors duration-200 ${
      active
        ? 'text-white'
        : 'text-slate-500 hover:text-slate-900 hover:bg-slate-100'
    }`}
    aria-pressed={active}
  >
    {label}
  </button>
);

const SlidingNav = ({ tabs, activeTab, onTabChange, className = '' }) => {
  const containerRef = useRef(null);
  const itemRefs = useRef({});
  const [sliderStyle, setSliderStyle] = useState(null);

  const updateSlider = useCallback(() => {
    const container = containerRef.current;
    const activeItem = itemRefs.current[activeTab];
    if (!container || !activeItem) {
      setSliderStyle(null);
      return;
    }

    setSliderStyle({
      width: `${activeItem.offsetWidth}px`,
      transform: `translateX(${activeItem.offsetLeft}px)`
    });
  }, [activeTab]);

  useEffect(() => {
    const animationFrame = window.requestAnimationFrame(updateSlider);

    const container = containerRef.current;
    const activeItem = itemRefs.current[activeTab];
    const resizeObserver = typeof ResizeObserver !== 'undefined'
      ? new ResizeObserver(updateSlider)
      : null;

    if (resizeObserver) {
      if (container) resizeObserver.observe(container);
      if (activeItem) resizeObserver.observe(activeItem);
    }

    window.addEventListener('resize', updateSlider);

    return () => {
      window.cancelAnimationFrame(animationFrame);
      resizeObserver?.disconnect();
      window.removeEventListener('resize', updateSlider);
    };
  }, [activeTab, tabs, updateSlider]);

  return (
    <div
      ref={containerRef}
      className={`relative items-center bg-slate-100/50 p-1 rounded-full border border-slate-200/60 ${className}`}
    >
      <span
        className={`pointer-events-none absolute left-0 inset-y-1 rounded-full bg-slate-900 shadow-md transition-[transform,width,opacity] duration-200 ease-out ${
          sliderStyle ? 'opacity-100' : 'opacity-0'
        }`}
        style={sliderStyle || undefined}
      />
      {tabs.map(tab => (
        <NavItem
          key={tab.name}
          label={tab.label}
          active={activeTab === tab.name}
          onClick={() => onTabChange(tab.name)}
          buttonRef={(node) => {
            if (node) {
              itemRefs.current[tab.name] = node;
            } else {
              delete itemRefs.current[tab.name];
            }
          }}
        />
      ))}
    </div>
  );
};

const Header = ({ activeTab, onTabChange, connectionStatus = 'connected', proxyStatus = {} }) => {
  const toast = useToast();
  // 使用真实的代理网关状态
  const {
    running: proxyRunning = false,
    port: proxyPort = 0
  } = proxyStatus;

  // 连接断开/连接中时的状态（仅此时显示单个状态块）
  const isDisconnected = connectionStatus !== 'connected';
  const disconnectedStatus = isDisconnected
    ? {
        color: 'bg-amber-500',
        text: connectionStatus === 'connecting' ? '连接中...' : '事件通道断开',
        ping: connectionStatus === 'connecting'
      }
    : null;

  const isWails = isWailsEnvironment();
  const handleTitlebarDoubleClick = async (event) => {
    if (!isWails || getComputedStyle(event.target).getPropertyValue('--wails-draggable').trim() !== 'drag') return;
    try {
      const { Window } = await import('@wailsio/runtime');
      await Window.ToggleMaximise();
    } catch { toast.error('窗口操作失败，请重试'); }
  };

  return (
    <header className="relative z-50 shrink-0 border-b border-slate-200/60 bg-white/80 backdrop-blur-xl supports-[backdrop-filter]:bg-white/60">
      {isWails && (
        <div onDoubleClick={handleTitlebarDoubleClick} className="desktop-titlebar flex h-8 items-center justify-end border-b border-slate-100 bg-slate-50/70">
          <WindowControls />
        </div>
      )}

      {/* Equal side columns keep navigation centred on the whole window. */}
      <nav aria-label="主导航" className="grid h-16 grid-cols-[minmax(0,1fr)_auto] items-center gap-4 px-6 min-[1200px]:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]">
        <div className="flex min-w-0 items-center">
          <button type="button" aria-label="返回概览"
            className="group flex min-w-0 items-center gap-2.5 rounded-lg"
            onClick={() => onTabChange('overview')}
          >
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-slate-900 text-white shadow-lg">
              <Command size={16} strokeWidth={3} />
            </div>
            <span className="truncate text-lg font-bold tracking-tight text-slate-900">
              AI Relay Forwarder
            </span>
          </button>
        </div>

        <SlidingNav
          tabs={NAV_TABS}
          activeTab={activeTab}
          onTabChange={onTabChange}
          className="hidden min-[1200px]:flex"
        />

        {/* 右侧：状态指示器 + 头像 */}
        <div className="flex min-w-0 items-center justify-end gap-3">
          {/* 连接状态 */}
          {isDisconnected ? (
            <div className="hidden sm:flex items-center px-3 py-1.5 bg-white border border-slate-200 rounded-md shadow-sm text-xs text-slate-500">
              <span className="relative flex h-2 w-2 mr-2">
                {disconnectedStatus.ping && (
                  <span className={`animate-ping absolute inline-flex h-full w-full rounded-full ${disconnectedStatus.color} opacity-75`}></span>
                )}
                <span className={`relative inline-flex rounded-full h-2 w-2 ${disconnectedStatus.color}`}></span>
              </span>
              {disconnectedStatus.text}
            </div>
          ) : (
            <div className="hidden sm:flex items-center px-3 py-1.5 bg-white border border-slate-200 rounded-md shadow-sm text-xs text-slate-500">
              <span className="relative flex h-2 w-2 mr-2">
                {proxyRunning && (
                  <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-500 opacity-75"></span>
                )}
                <span className={`relative inline-flex rounded-full h-2 w-2 ${proxyRunning && proxyPort > 0 ? 'bg-emerald-500' : 'bg-rose-500'}`}></span>
              </span>
              {proxyRunning && proxyPort > 0 ? `代理端口 :${proxyPort}` : '代理未运行'}
            </div>
          )}

          {/* 用户菜单 */}
          <UserMenu />
        </div>
      </nav>

      {/* 移动端导航 */}
      <div className="px-4 pb-3 overflow-x-auto min-[1200px]:hidden">
        <SlidingNav
          tabs={NAV_TABS}
          activeTab={activeTab}
          onTabChange={onTabChange}
          className="mx-auto flex w-max"
        />
      </div>
    </header>
  );
};

export default Header;
