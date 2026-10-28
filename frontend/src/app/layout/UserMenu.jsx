import Tooltip from '@/shared/ui/Tooltip.jsx';
import { useState, useEffect, useRef, useCallback } from 'react';
import { AlertCircle, Check, ChevronRight, Clock, Copy, Cpu, Database, FileText, FolderOpen, Loader2, LogOut, SlidersHorizontal } from 'lucide-react';
import { isWailsEnvironment, getSystemStatus, getProxyURL, openClaudeConfigDir, openCodexConfigDir, openLogDir, openDataDir, requestQuit, copyToClipboard } from '@/services/wails/index.js';
import { useToast } from '@/shared/ui/index.js';
import releaseNotes from '@/app/release-notes.json';
import { getReleaseAnnouncement, showReleaseToastSummary } from '@/app/updates/releaseAnnouncement.js';
import './userMenu.css';
import { usePresence } from '@/shared/hooks/usePresence.js';

const InfoItem = ({ icon: Icon, label, value, onValueClick, valueAriaLabel }) => <div className="app-menu-info-item">
  <span className="app-menu-info-label"><Icon size={12} />{label}</span>
  {onValueClick ? <Tooltip asChild content="查看当前版本更新内容"><button type="button" onClick={onValueClick} aria-label={valueAriaLabel} className="app-menu-version">{value}<ChevronRight size={12} aria-hidden="true" /></button></Tooltip> : <span className="app-menu-info-value">{value}</span>}
</div>;

const formatUptime = (seconds) => {
  if (seconds == null) return '—';
  const duration = Math.max(0, seconds);
  const days = Math.floor(duration / 86400);
  const hours = Math.floor(duration % 86400 / 3600);
  const minutes = Math.floor(duration % 3600 / 60);
  const secs = Math.floor(duration % 60);
  const time = [hours, minutes, secs].map((value) => String(value).padStart(2, '0')).join(':');
  return days ? `${days}天 ${time}` : time;
};

export default function UserMenu() {
  const [isOpen, setIsOpen] = useState(false);
  const presence = usePresence(isOpen);
  const [systemInfo, setSystemInfo] = useState(null);
  const [proxyAddress, setProxyAddress] = useState('');
  const [infoError, setInfoError] = useState('');
  const [loading, setLoading] = useState(false);
  const [copied, setCopied] = useState(false);
  const [copying, setCopying] = useState(false);
  const [receivedAt, setReceivedAt] = useState(0);
  const [now, setNow] = useState(0);
  const menuRef = useRef(null);
  const triggerRef = useRef(null);
  const copyTimerRef = useRef(null);
  const releaseToastCleanupRef = useRef(null);
  const toast = useToast();
  const isWails = isWailsEnvironment();

  const handleShowReleaseNotes = useCallback(() => {
    const announcement = getReleaseAnnouncement(systemInfo?.version, releaseNotes);
    if (!announcement) { toast.info('当前版本暂无更新说明', '版本更新'); return; }
    setIsOpen(false);
    releaseToastCleanupRef.current?.();
    releaseToastCleanupRef.current = showReleaseToastSummary({ announcement, showToast: toast.show, dismissToast: toast.dismiss });
  }, [systemInfo?.version, toast]);

  useEffect(() => {
    if (!isOpen) return;
    let active = true;
    setCopied(false);
    setInfoError('');
    if (isWails) {
      setLoading(true);
      Promise.allSettled([getSystemStatus(), getProxyURL()]).then(([status, address]) => {
        if (!active) return;
        const timestamp = Date.now();
        if (status.status === 'fulfilled') { setSystemInfo(status.value); setReceivedAt(timestamp); setNow(timestamp); }
        setProxyAddress(address.status === 'fulfilled' ? address.value : '');
        if (status.status === 'rejected' || address.status === 'rejected') setInfoError('部分应用信息读取失败，请重新打开菜单重试。');
        setLoading(false);
      });
    }
    // Keep the clock moving locally instead of querying the desktop every second.
    const timer = setInterval(() => setNow(Date.now()), 1000);
    const outside = (event) => { if (!menuRef.current?.contains(event.target)) setIsOpen(false); };
    const keyDown = (event) => {
      if (event.key !== 'Escape') return;
      setIsOpen(false);
      triggerRef.current?.focus();
    };
    document.addEventListener('mousedown', outside);
    document.addEventListener('keydown', keyDown);
    return () => {
      active = false;
      clearInterval(timer);
      document.removeEventListener('mousedown', outside);
      document.removeEventListener('keydown', keyDown);
    };
  }, [isOpen, isWails]);

  useEffect(() => () => { clearTimeout(copyTimerRef.current); releaseToastCleanupRef.current?.(); }, []);

  const handleCopyAddress = async () => {
    if (!proxyAddress || copying) return;
    setCopying(true);
    try {
      await copyToClipboard(proxyAddress);
      setCopied(true);
      clearTimeout(copyTimerRef.current);
      copyTimerRef.current = setTimeout(() => setCopied(false), 2000);
    } catch (error) { toast.error(error?.message || '复制代理地址失败', '复制失败'); }
    finally { setCopying(false); }
  };

  const openDirectory = async (open, label) => {
    try { await open(); setIsOpen(false); }
    catch (error) { toast.error(error?.message || `打开${label}失败`, '打开失败'); }
  };
  const handleQuit = async () => {
    try { await requestQuit(); }
    catch (error) { toast.error(error?.message || '退出应用失败', '退出失败'); }
  };
  const uptime = systemInfo?.uptime == null ? null : Number(systemInfo.uptime) + Math.max(0, Math.floor((now - receivedAt) / 1000));
  const running = systemInfo?.proxy_running;

  return <div className="app-menu" ref={menuRef}>
    <button type="button" ref={triggerRef} className="app-menu-trigger motion-control" data-active={isOpen} onClick={() => setIsOpen((previous) => !previous)} aria-label="应用菜单" aria-expanded={isOpen} aria-controls="application-menu"><SlidersHorizontal size={16} /></button>
    {presence.present && <section id="application-menu" aria-label="应用菜单" inert={presence.closing} aria-hidden={presence.closing} data-origin="top-right" className={`app-menu-panel t-dropdown ${presence.className}`}>
      <header className="app-menu-heading"><h2>应用菜单</h2><span className="app-menu-status" data-running={Boolean(running)}><i aria-hidden="true" />{loading ? '读取中' : !systemInfo ? '状态未知' : running ? '代理运行中' : '代理已停止'}</span></header>
      <div className="app-menu-info">
        <InfoItem icon={Cpu} label="程序版本" value={systemInfo?.version ? `v${systemInfo.version}` : '—'} onValueClick={systemInfo?.version ? handleShowReleaseNotes : undefined} valueAriaLabel={systemInfo?.version ? `查看 v${systemInfo.version} 更新内容` : undefined} />
        <InfoItem icon={Clock} label="运行时长" value={formatUptime(uptime)} />
      </div>
      <div className="app-menu-address"><div className="app-menu-section-title">代理地址<span>Claude / Codex</span></div><div className="app-menu-address-field"><code>{proxyAddress || (loading ? '正在读取…' : '代理地址暂不可用')}</code><Tooltip asChild content={copied ? '已复制' : '复制代理地址'}><button type="button" onClick={handleCopyAddress} disabled={!proxyAddress || copying} aria-label={copied ? '已复制' : '复制代理地址'} data-copied={copied}>{copying ? <Loader2 size={14} className="animate-spin" /> : copied ? <Check size={14} /> : <Copy size={14} />}</button></Tooltip></div></div>
      {infoError && <p role="alert" className="app-menu-error"><AlertCircle size={13} /><span>{infoError}</span></p>}
      {/* 操作按钮 */}
      <div className="app-menu-directories">
        <button type="button" disabled={!isWails} onClick={() => openDirectory(openClaudeConfigDir, ' Claude 配置目录')} aria-label="打开 Claude 配置目录" className="app-menu-directory transition-colors duration-150"><FolderOpen size={15} /><span>Claude 配置<small>打开配置目录</small></span><ChevronRight size={12} /></button>
        <button type="button" disabled={!isWails} onClick={() => openDirectory(openCodexConfigDir, ' Codex 配置目录')} aria-label="打开 Codex 配置目录" className="app-menu-directory transition-colors duration-150"><FolderOpen size={15} /><span>Codex 配置<small>打开配置目录</small></span><ChevronRight size={12} /></button>
        <button type="button" disabled={!isWails} onClick={() => openDirectory(openLogDir, '日志目录')} aria-label="打开日志目录" className="app-menu-directory transition-colors duration-150"><FileText size={15} /><span>运行日志<small>打开日志目录</small></span><ChevronRight size={12} /></button>
        <button type="button" disabled={!isWails} onClick={() => openDirectory(openDataDir, '数据目录')} aria-label="打开数据目录" className="app-menu-directory transition-colors duration-150"><Database size={15} /><span>应用数据<small>打开数据目录</small></span><ChevronRight size={12} /></button>
      </div>
      {/* 退出按钮 */}
      <footer className="app-menu-footer"><button type="button" disabled={!isWails} onClick={handleQuit}><LogOut size={14} />退出应用</button></footer>
    </section>}
  </div>;
}
