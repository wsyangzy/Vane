// ============================================
// UserMenu - 用户菜单下拉组件
// (2026-02-09)
// ============================================

import { useState, useEffect, useRef, useCallback } from 'react';
import {
  Copy,
  FolderOpen,
  FileText,
  LogOut,
  Check,
  Clock,
  Cpu,
  Database,
  ChevronRight
} from 'lucide-react';
import {
  isWailsEnvironment,
  getSystemStatus,
  getProxyURL,
  openClaudeConfigDir,
  openCodexConfigDir,
  openLogDir,
  openDataDir,
  requestQuit,
  copyToClipboard
} from '@/services/wails/index.js';
import { useToast } from '@/shared/ui/index.js';
import releaseNotes from '@/app/release-notes.json';
import { getReleaseAnnouncement, showReleaseToastSummary } from '@/app/updates/releaseAnnouncement.js';

// 信息卡片项目组件（移到外部避免 lint 警告）
const InfoItem = ({
  icon: Icon,
  iconBg,
  iconColor,
  label,
  value,
  delay,
  isLoaded,
  extra,
  valueClass,
  onValueClick,
  valueAriaLabel
}) => (
  <div
    className="flex items-center justify-between transition-all duration-300 ease-out"
    style={{
      opacity: isLoaded ? 1 : 0,
      transform: isLoaded ? 'translateY(0)' : 'translateY(8px)',
      transitionDelay: `${delay}ms`
    }}
  >
    <div className="flex items-center gap-2">
      <div className={`p-1.5 ${iconBg} rounded-lg transition-transform duration-300 hover:scale-110`}>
        <Icon size={14} className={iconColor} />
      </div>
      <span className="text-xs font-medium text-slate-500">{label}</span>
    </div>
    <div className="flex items-center gap-2">
      {onValueClick ? (
        <button
          type="button"
          onClick={onValueClick}
          className={`inline-flex items-center gap-0.5 rounded-md px-1.5 py-0.5 text-sm font-semibold transition-colors hover:bg-indigo-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300 ${valueClass || 'text-slate-800'}`}
          aria-label={valueAriaLabel}
          title="查看当前版本更新内容"
        >
          {value}
          <ChevronRight size={12} aria-hidden="true" />
        </button>
      ) : (
        <span className={`text-sm font-semibold ${valueClass || 'text-slate-800'}`}>{value}</span>
      )}
      {extra}
    </div>
  </div>
);

const UserMenu = () => {
  const [isOpen, setIsOpen] = useState(false);
  const [systemInfo, setSystemInfo] = useState(null);
  const [proxyAddress, setProxyAddress] = useState('');
  const [copied, setCopied] = useState(false);
  const [isLoaded, setIsLoaded] = useState(false);
  const menuRef = useRef(null);
  const releaseToastCleanupRef = useRef(null);
  const isWails = isWailsEnvironment();
  const toast = useToast();

  // 加载系统信息
  const loadSystemInfo = useCallback(async () => {
    if (!isWails) return;
    try {
      const [status, url] = await Promise.all([
        getSystemStatus(),
        getProxyURL()
      ]);
      setSystemInfo(status);
      setProxyAddress(url);
    } catch (error) {
      console.warn('Failed to load system info:', error);
    }
  }, [isWails]);

  const handleShowReleaseNotes = useCallback(() => {
    const announcement = getReleaseAnnouncement(systemInfo?.version, releaseNotes);
    if (!announcement) {
      toast.info('当前版本暂无更新说明', '版本更新');
      return;
    }

    setIsOpen(false);
    releaseToastCleanupRef.current?.();
    releaseToastCleanupRef.current = showReleaseToastSummary({
      announcement,
      showToast: toast.show,
      dismissToast: toast.dismiss
    });
  }, [systemInfo?.version, toast]);

  // 打开菜单时加载数据并触发动画
  useEffect(() => {
    if (isOpen) {
      // 延迟触发动画（等待下一帧渲染完成）
      const frameId = requestAnimationFrame(() => {
        void loadSystemInfo();
        requestAnimationFrame(() => {
          setIsLoaded(true);
        });
      });
      return () => cancelAnimationFrame(frameId);
    }
  }, [isOpen, loadSystemInfo]);

  // 定时刷新运行时长（菜单打开时）
  useEffect(() => {
    if (!isOpen) return;
    const timer = setInterval(loadSystemInfo, 1000);
    return () => clearInterval(timer);
  }, [isOpen, loadSystemInfo]);

  useEffect(() => () => {
    releaseToastCleanupRef.current?.();
  }, []);

  // 点击外部关闭菜单
  useEffect(() => {
    const handleClickOutside = (event) => {
      if (menuRef.current && !menuRef.current.contains(event.target)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // 复制代理地址
  const handleCopyAddress = async () => {
    if (!proxyAddress) return;
    await copyToClipboard(proxyAddress);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  // 打开 Claude 配置目录
  const handleOpenConfigDir = async () => {
    try {
      await openClaudeConfigDir();
      setIsOpen(false);
    } catch (error) {
      toast.error(error?.message || '打开 Claude 配置目录失败', '打开失败');
    }
  };

  // 打开 Codex 配置目录
  const handleOpenCodexConfigDir = async () => {
    try {
      await openCodexConfigDir();
      setIsOpen(false);
    } catch (error) {
      toast.error(error?.message || '打开 Codex 配置目录失败', '打开失败');
    }
  };

  // 打开日志目录
  const handleOpenLogDir = async () => {
    try {
      await openLogDir();
      setIsOpen(false);
    } catch (error) {
      toast.error(error?.message || '打开日志目录失败', '打开失败');
    }
  };

  const handleOpenDataDir = async () => {
    try {
      await openDataDir();
      setIsOpen(false);
    } catch (error) {
      toast.error(error?.message || '打开数据目录失败', '打开失败');
    }
  };

  // 退出应用
  const handleQuit = async () => {
    try {
      await requestQuit();
    } catch (error) {
      console.error('Failed to quit:', error);
    }
  };

  // 格式化运行时长（dd:HH:mm:ss 格式，按需显示）
  const formatUptime = (seconds) => {
    if (!seconds || seconds < 0) return '00:00';

    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = Math.floor(seconds % 60);

    const pad = (n) => String(n).padStart(2, '0');

    if (days > 0) {
      return `${pad(days)}:${pad(hours)}:${pad(minutes)}:${pad(secs)}`;
    }
    if (hours > 0) {
      return `${pad(hours)}:${pad(minutes)}:${pad(secs)}`;
    }
    return `${pad(minutes)}:${pad(secs)}`;
  };

  return (
    <div className="relative" ref={menuRef}>
      {/* 头像按钮 */}
      <button
        onClick={() => { setIsLoaded(false); setIsOpen(!isOpen); }}
        className={`w-8 h-8 rounded-full bg-gradient-to-tr from-indigo-500 to-purple-500 border-2 border-white shadow-md cursor-pointer transition-all duration-200 ${
          isOpen ? 'ring-2 ring-indigo-300 scale-105' : 'hover:ring-2 ring-indigo-200 hover:scale-105'
        }`}
        aria-label="用户菜单"
      />

      {/* 下拉菜单 */}
      {isOpen && (
        <div
          className="absolute right-0 top-full mt-2 w-72 bg-white rounded-xl shadow-xl border border-slate-200/80 overflow-hidden z-50"
          style={{
            animation: 'menuSlideIn 0.2s ease-out forwards'
          }}
        >
          <style>
            {`
              @keyframes menuSlideIn {
                from {
                  opacity: 0;
                  transform: translateY(-8px) scale(0.96);
                }
                to {
                  opacity: 1;
                  transform: translateY(0) scale(1);
                }
              }
            `}
          </style>

          {/* 信息卡片 */}
          <div className="p-4 bg-gradient-to-br from-slate-50 to-indigo-50/30 border-b border-slate-100 space-y-3">
            <InfoItem
              icon={Cpu}
              iconBg="bg-indigo-100"
              iconColor="text-indigo-600"
              label="程序版本"
              value={`v${systemInfo?.version || '—'}`}
              valueClass="text-indigo-600 font-mono"
              delay={0}
              isLoaded={isLoaded}
              onValueClick={systemInfo?.version ? handleShowReleaseNotes : undefined}
              valueAriaLabel={systemInfo?.version ? `查看 v${systemInfo.version} 更新内容` : undefined}
            />

            <InfoItem
              icon={Clock}
              iconBg="bg-emerald-100"
              iconColor="text-emerald-600"
              label="运行时长"
              value={formatUptime(systemInfo?.uptime)}
              valueClass="text-emerald-600 font-mono tabular-nums"
              delay={100}
              isLoaded={isLoaded}
            />
          </div>

          {/* 代理地址复制 */}
          <div
            className="p-3 border-b border-slate-100 transition-all duration-300 ease-out"
            style={{
              opacity: isLoaded ? 1 : 0,
              transform: isLoaded ? 'translateY(0)' : 'translateY(8px)',
              transitionDelay: '150ms'
            }}
          >
            <div className="flex items-center gap-2">
              <div className="flex-1 px-3 py-2 bg-slate-50 rounded-lg border border-slate-200 overflow-hidden">
                <code className="text-xs font-mono text-slate-600 truncate block">
                  {proxyAddress || 'http://127.0.0.1:8087'}
                </code>
              </div>
              <button
                onClick={handleCopyAddress}
                className={`shrink-0 p-2 rounded-lg transition-all duration-200 ${
                  copied
                    ? 'bg-emerald-100 text-emerald-600 scale-105'
                    : 'bg-slate-100 text-slate-500 hover:bg-slate-200 hover:text-slate-700 hover:scale-105'
                }`}
                aria-label={copied ? '已复制' : '复制代理地址'}
              >
                {copied ? <Check size={16} /> : <Copy size={16} />}
              </button>
            </div>
            <div className="flex items-center gap-2 mt-2 px-1">
              <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-blue-50 text-blue-600">Claude</span>
              <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-green-50 text-green-600">Codex</span>
            </div>
          </div>

          {/* 操作按钮 */}
          <div
            className="p-2 transition-all duration-300 ease-out"
            style={{
              opacity: isLoaded ? 1 : 0,
              transform: isLoaded ? 'translateY(0)' : 'translateY(8px)',
              transitionDelay: '200ms'
            }}
          >
            <button
              onClick={handleOpenConfigDir}
              className="w-full flex items-center gap-3 px-3 py-2 text-sm text-slate-600 hover:bg-slate-50 rounded-lg transition-colors duration-150"
            >
              <FolderOpen size={16} className="text-slate-400" />
              <span>打开 Claude 配置目录</span>
            </button>
            <button
              onClick={handleOpenCodexConfigDir}
              className="w-full flex items-center gap-3 px-3 py-2 text-sm text-slate-600 hover:bg-slate-50 rounded-lg transition-colors duration-150"
            >
              <FolderOpen size={16} className="text-slate-400" />
              <span>打开 Codex 配置目录</span>
            </button>
            <button
              onClick={handleOpenLogDir}
              className="w-full flex items-center gap-3 px-3 py-2 text-sm text-slate-600 hover:bg-slate-50 rounded-lg transition-colors duration-150"
            >
              <FileText size={16} className="text-slate-400" />
              <span>打开日志目录</span>
            </button>
            <button
              onClick={handleOpenDataDir}
              className="w-full flex items-center gap-3 px-3 py-2 text-sm text-slate-600 hover:bg-slate-50 rounded-lg transition-colors duration-150"
            >
              <Database size={16} className="text-slate-400" />
              <span>打开数据目录</span>
            </button>
          </div>

          {/* 退出按钮 */}
          <div
            className="p-2 border-t border-slate-100 transition-all duration-300 ease-out"
            style={{
              opacity: isLoaded ? 1 : 0,
              transform: isLoaded ? 'translateY(0)' : 'translateY(8px)',
              transitionDelay: '250ms'
            }}
          >
            <button
              onClick={handleQuit}
              className="w-full flex items-center gap-3 px-3 py-2 text-sm text-rose-600 hover:bg-rose-50 rounded-lg transition-all duration-150 hover:translate-x-1"
            >
              <LogOut size={16} />
              <span>退出应用</span>
            </button>
          </div>
        </div>
      )}
    </div>
  );
};

export default UserMenu;
