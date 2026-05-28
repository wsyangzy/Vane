// ============================================
// 布局组件 - Header 导航栏
// 2025-11-28 (Updated 2026-02-09 for user menu)
// ============================================

import { Command } from 'lucide-react';
import { isWailsEnvironment } from '@utils/wailsApi.js';
import UserMenu from './UserMenu.jsx';

// 导航标签项
const NavItem = ({ label, active, onClick }) => (
  <button
    onClick={onClick}
    className={`px-4 py-2 text-sm font-medium rounded-full transition-all duration-200 ${
      active
        ? 'bg-slate-900 text-white shadow-md'
        : 'text-slate-500 hover:text-slate-900 hover:bg-slate-100'
    }`}
  >
    {label}
  </button>
);

const Header = ({ activeTab, onTabChange, connectionStatus = 'connected', proxyStatus = {} }) => {
  const tabs = [
    { name: 'overview', label: '概览' },
    { name: 'channels', label: 'Claude 管理' },
    { name: 'codex', label: 'Codex 管理' },
    // 组管理入口已移除，配置简化后不再需要独立的组管理页面
    // { name: 'groups', label: '组管理' },
    { name: 'requests', label: '请求追踪' },
    { name: 'pricing', label: '基础定价' },
    // 新增日志页面
    { name: 'logs', label: '系统日志' },
    // 配置改为设置
    { name: 'settings', label: '设置' }
  ];

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

  // Wails 环境下为窗口按钮预留顶部空间
  const isWails = isWailsEnvironment();
  const titlebarPadding = isWails ? 'pt-7' : '';

  return (
    <nav className={`sticky top-0 z-50 bg-white/80 backdrop-blur-xl border-b border-slate-200/60 supports-[backdrop-filter]:bg-white/60 ${titlebarPadding}`}
         style={isWails ? { WebkitAppRegion: 'drag' } : {}}>
      <div className="max-w-7xl mx-auto px-6 h-16 flex items-center justify-between"
           style={isWails ? { WebkitAppRegion: 'no-drag' } : {}}>
        {/* 左侧：Logo + 导航 */}
        <div className="flex items-center space-x-8">
          {/* Logo */}
          <div
            className="flex items-center space-x-2.5 group cursor-pointer"
            onClick={() => onTabChange('overview')}
          >
            <div className="w-8 h-8 bg-slate-900 rounded-lg flex items-center justify-center text-white shadow-lg">
              <Command size={16} strokeWidth={3} />
            </div>
            <span className="font-bold text-lg tracking-tight text-slate-900">
              AI Relay Forwarder
            </span>
          </div>

          {/* 导航标签 */}
          <div className="hidden md:flex items-center bg-slate-100/50 p-1 rounded-full border border-slate-200/60">
            {tabs.map(tab => (
              <NavItem
                key={tab.name}
                label={tab.label}
                active={activeTab === tab.name}
                onClick={() => onTabChange(tab.name)}
              />
            ))}
          </div>
        </div>

        {/* 右侧：状态指示器 + 头像 */}
        <div className="flex items-center space-x-4">
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
      </div>

      {/* 移动端导航 */}
      <div className="md:hidden flex items-center space-x-2 px-4 pb-3 overflow-x-auto">
        {tabs.map(tab => (
          <NavItem
            key={tab.name}
            label={tab.label}
            active={activeTab === tab.name}
            onClick={() => onTabChange(tab.name)}
          />
        ))}
      </div>
    </nav>
  );
};

export default Header;

