import Tooltip from '@/shared/ui/Tooltip.jsx';
// ============================================
// Settings 页面 - 系统设置管理（可编辑）
// (2025-12-08)
// ============================================

import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import {
  Settings,
  RefreshCw,
  Save,
  AlertCircle,
  CheckCircle2,
  Server,
  RotateCcw,
  HeartPulse,
  Clock,
  Lock,
  Hash,
  Archive,
  X,
  FileText,
  Gauge,
  Trash2
} from 'lucide-react';
import { Button, ErrorMessage, ConfirmDialog } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { SettingItem, SettingsSection, PortInfo } from './components/index.js';
import { buildSettingsUpdates } from './settingsUpdates.js';
import {
  isWailsEnvironment,
  getSettingCategories,
  getAllSettings,
  batchUpdateSettings,
  resetCategorySettings,
  getPortInfo,
  getCacheStatus,
  clearCache
} from '@/services/wails/index.js';
import {
  fetchSettingCategories,
  fetchAllSettings,
  updateSettings as apiUpdateSettings,
  resetSettings as apiResetSettings,
  fetchPortInfo
} from '@/services/http/api.js';

// 分类名称到 Lucide 图标的映射
const CATEGORY_ICONS = {
  server: Server,
  retry: RotateCcw,
  health: HeartPulse,
  request: Clock,
  auth: Lock,
  token_counting: Hash,
  retention: Archive,
  logging: FileText,
  cache: Gauge
};

const CATEGORY_ORDER = ['auth', 'health', 'request', 'retry', 'token_counting', 'retention', 'logging', 'cache'];
// Keep the columns independent and stable as descriptions wrap or values change.
const PRIMARY_COLUMN_CATEGORIES = new Set(['auth', 'request', 'retention', 'logging', 'token_counting']);
const LEGACY_CATEGORIES = new Set(['strategy', 'failover', 'timezone', 'streaming', 'hot_pool']);
const CURRENT_KEYS = {
  retry: ['max_total_attempts', 'max_total_duration'],
  request: ['global_timeout', 'response_header_timeout', 'default_response_header_timeout']
};

const getCategoryOrder = (categoryName) => {
  const index = CATEGORY_ORDER.indexOf(categoryName);
  return index === -1 ? CATEGORY_ORDER.length : index;
};

const getCacheClearKey = (names = []) => (names.length > 0 ? names.join('|') : '__all__');

const formatCacheHitRate = (hits = 0, misses = 0) => {
  const total = hits + misses;
  if (total <= 0) return '-';
  return `${((hits / total) * 100).toFixed(1)}%`;
};

// TTL 为 0 的缓存只靠写入失效刷新，不按时间过期
const formatCacheTTL = (ttl) => (!ttl || ttl === '0s' ? '随写入失效' : ttl);
const CACHE_DETAILS = {
  'overview:queries': { label: '概览查询结果', description: '按日期与筛选条件缓存整页结果；仅存内存，最多 24 项，有效期不超过 30 秒。' },
  'usage:local-index': { label: '本地会话索引', description: '缓存会话文件的用量元数据并保存到本机；重启可复用。清理后重新解析，不删除原始会话。' }
};

const formatCacheCapacity = (cache) => {
  const size = cache?.size ?? 0;
  const maxEntries = cache?.maxEntries ?? 0;
  return maxEntries > 0 ? `${size}/${maxEntries}` : `${size}/不限`;
};

const formatCacheTime = (value) => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', {
    timeZone: 'Asia/Shanghai',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false
  });
};

const CacheStatusPanel = ({
  status,
  loading,
  error,
  clearing,
  onRefresh,
  onClear
}) => {
  const [modalOpen, setModalOpen] = useState(false);
  const caches = status?.caches || [];
  const totalHits = caches.reduce((sum, item) => sum + (item.hits || 0), 0);
  const totalMisses = caches.reduce((sum, item) => sum + (item.misses || 0), 0);
  const totalSize = caches.reduce((sum, item) => sum + (item.size || 0), 0);
  const clearAllKey = getCacheClearKey([]);
  const summaryText = loading
    ? '正在刷新状态...'
    : `${caches.length} 组 · ${totalSize} 项 · 命中率 ${formatCacheHitRate(totalHits, totalMisses)}`;

  useEffect(() => {
    if (!modalOpen) return undefined;
    const handleKeyDown = (event) => {
      if (event.key === 'Escape') {
        setModalOpen(false);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [modalOpen]);

  return (
    <>
      <div className="my-2 rounded-lg bg-slate-50/80 px-2.5 py-2">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <span className={`inline-flex h-6 items-center gap-1.5 rounded-md px-2 text-[11px] font-medium ring-1 ring-inset ${
                status?.enabled
                  ? 'bg-emerald-50 text-emerald-700 ring-emerald-200'
                  : 'bg-slate-100 text-slate-500 ring-slate-200'
              }`}>
                {status?.enabled ? <CheckCircle2 size={13} /> : <AlertCircle size={13} />}
                {status?.enabled ? '缓存已启用' : '缓存已禁用'}
              </span>
              <span className="text-[11px] font-medium text-slate-500">
                {summaryText}
              </span>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              icon={Gauge}
              variant="secondary"
              size="sm"
              onClick={() => setModalOpen(true)}
              aria-haspopup="dialog"
            >
              查看状态明细
            </Button>
            <Button
              icon={RefreshCw}
              variant="secondary"
              size="sm"
              onClick={onRefresh}
              disabled={loading || Boolean(clearing)}
            >
              刷新状态
            </Button>
          </div>
        </div>

        {error && (
          <div className="mt-3 flex items-center gap-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">
            <AlertCircle size={15} />
            <span className="min-w-0 break-words">{error}</span>
          </div>
        )}
      </div>

      {modalOpen && renderModalPortal(
        <div
          className="fixed inset-0 z-[100] flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-sm animate-fade-in"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) setModalOpen(false);
          }}
        >
          <div
            className="flex max-h-[calc(100dvh-2rem)] w-full max-w-5xl flex-col overflow-hidden rounded-xl bg-white shadow-xl"
            role="dialog"
            aria-modal="true"
            aria-labelledby="cache-status-title"
            onMouseDown={(event) => event.stopPropagation()}
          >
            <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b border-slate-100 px-4 py-3">
              <div className="flex min-w-0 items-start gap-2.5">
                <div className="rounded-lg bg-indigo-50 p-2 text-indigo-600">
                  <Gauge size={16} />
                </div>
                <div className="min-w-0">
                  <h2 id="cache-status-title" className="text-sm font-semibold text-slate-900">
                    缓存运行状态
                  </h2>
                  <p className="mt-0.5 text-[11px] leading-4 text-slate-500">
                    {summaryText} · 占用为查询项或索引文件数，上限为自动淘汰阈值
                  </p>
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Button
                  icon={RefreshCw}
                  variant="secondary"
                  size="sm"
                  onClick={onRefresh}
                  disabled={loading || Boolean(clearing)}
                >
                  刷新状态
                </Button>
                <Button
                  icon={Trash2}
                  variant="dangerSoft"
                  size="sm"
                  onClick={() => onClear([])}
                  disabled={loading || Boolean(clearing) || caches.length === 0}
                  loading={clearing === clearAllKey}
                >
                  清理全部
                </Button>
                <button
                  type="button"
                  onClick={() => setModalOpen(false)}
                  className="inline-flex h-8 w-8 items-center justify-center rounded-lg text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-600"
                  aria-label="关闭缓存状态弹窗"
                >
                  <X size={18} />
                </button>
              </div>
            </div>

            {error && (
              <div className="mx-6 mt-4 flex items-center gap-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">
                <AlertCircle size={15} />
                <span className="min-w-0 break-words">{error}</span>
              </div>
            )}

            <div className="min-h-0 overflow-auto p-4">
              <table className="w-full min-w-[760px] text-left text-xs">
            <thead className="bg-slate-50 text-xs font-semibold uppercase text-slate-400">
              <tr>
                <th className="px-3 py-2">名称</th>
                <th className="px-3 py-2">状态</th>
                <th className="px-3 py-2">TTL</th>
                <th className="px-3 py-2">占用/上限</th>
                <th className="px-3 py-2">命中率</th>
                <th className="px-3 py-2">失效</th>
                <th className="px-3 py-2">更新时间</th>
                <th className="px-3 py-2 text-right">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 bg-white">
              {loading && (
                <tr>
                  <td colSpan={8} className="px-3 py-6 text-center text-sm text-slate-400">
                    正在加载缓存状态...
                  </td>
                </tr>
              )}
              {!loading && caches.length === 0 && (
                <tr>
                  <td colSpan={8} className="px-3 py-6 text-center text-sm text-slate-400">
                    暂无缓存状态
                  </td>
                </tr>
              )}
              {!loading && caches.map((cache) => {
                const rowClearKey = getCacheClearKey([cache.name]);
                return (
                  <tr key={cache.name} className="text-slate-600">
                    <td className="px-3 py-2">
                      <Tooltip asChild content={CACHE_DETAILS[cache.name]?.description}><div className="max-w-[260px]">
                        {CACHE_DETAILS[cache.name] && <span className="block text-xs font-semibold text-slate-700">{CACHE_DETAILS[cache.name].label}</span>}
                        <span className={`block break-all font-mono text-xs ${CACHE_DETAILS[cache.name] ? 'mt-0.5 text-slate-400' : 'font-semibold text-slate-700'}`}>{cache.name}</span>
                      </div></Tooltip>
                    </td>
                    <td className="px-3 py-2">
                      <Tooltip asChild content={cache.fixed ? '运行时状态缓存，不受「启用缓存」开关控制' : undefined}><span
                        className={`inline-flex rounded-full px-2 py-0.5 text-xs font-semibold ${
                          cache.enabled
                            ? 'bg-emerald-50 text-emerald-700'
                            : 'bg-slate-100 text-slate-500'
                        }`}
                      >
                        {cache.fixed ? '常驻' : cache.enabled ? '启用' : '禁用'}
                      </span></Tooltip>
                    </td>
                    <td className="px-3 py-2 text-xs text-slate-500">{cache.name === 'usage:local-index' ? '文件变化时重建' : formatCacheTTL(cache.ttl)}</td>
                    <td className="px-3 py-2 font-mono text-xs text-slate-500">
                      {formatCacheCapacity(cache)}
                    </td>
                    <td className="px-3 py-2 font-mono text-xs text-slate-500">
                      {formatCacheHitRate(cache.hits, cache.misses)}
                      {cache.staleServed > 0 && (
                        <Tooltip asChild content="数据库查询失败时返回旧值的次数"><span
                          className="ml-1.5 text-amber-600"
                        >
                          · 旧值 {cache.staleServed}
                        </span></Tooltip>
                      )}
                    </td>
                    <td className="px-3 py-2 font-mono text-xs text-slate-500">
                      {cache.invalidations}
                    </td>
                    <td className="px-3 py-2 text-xs text-slate-500">
                      <div className="whitespace-nowrap">{formatCacheTime(cache.lastUpdatedAt)}</div>
                      <div className="whitespace-nowrap text-slate-400">清理 {formatCacheTime(cache.lastInvalidatedAt)}</div>
                    </td>
                    <td className="px-3 py-2 text-right">
                      <Button
                        icon={Trash2}
                        variant="ghost"
                        size="sm"
                        onClick={() => onClear([cache.name])}
                        disabled={Boolean(clearing)}
                        loading={clearing === rowClearKey}
                      >
                        清理
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
            </div>
          </div>
        </div>
      )}
    </>
  );
};

// ============================================
// Settings 页面主组件
// ============================================

const SettingsPage = () => {
  const [categories, setCategories] = useState([]);
  const [settings, setSettings] = useState([]);
  const [portInfo, setPortInfo] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  const [changes, setChanges] = useState({}); // 跟踪变更: { "category.key": newValue }
  const [saveMessage, setSaveMessage] = useState(null);
  const [initRetryAttempt, setInitRetryAttempt] = useState(0); // 初始化重试次数
  const [resetConfirm, setResetConfirm] = useState({ isOpen: false, category: '', label: '' }); // 重置确认弹窗
  const [cacheStatus, setCacheStatus] = useState(null);
  const [cacheLoading, setCacheLoading] = useState(false);
  const [cacheClearing, setCacheClearing] = useState('');
  const [cacheError, setCacheError] = useState(null);

  const isWails = isWailsEnvironment();
  const initRetryRef = useRef({ count: 0, timer: null });
  const messageTimerRef = useRef(null);

  // 设置自动消失的消息
  const setAutoHideMessage = useCallback((message, duration = 3000) => {
    // 清除之前的定时器
    if (messageTimerRef.current) {
      clearTimeout(messageTimerRef.current);
      messageTimerRef.current = null;
    }

    setSaveMessage(message);

    // 对于 info 类型的消息，设置自动消失
    if (message && (message.type === 'info' || message.type === 'success')) {
      messageTimerRef.current = setTimeout(() => {
        setSaveMessage(null);
        messageTimerRef.current = null;
      }, duration);
    }
  }, []);

  const isRetriableInitError = useCallback((err) => {
    const msg = (err?.message || String(err || '')).toLowerCase();
    return msg.includes('设置服务未启用') ||
      msg.includes('database is locked') ||
      msg.includes('sqlite_busy') ||
      msg.includes('context deadline exceeded');
  }, []);

  const loadCacheStatus = useCallback(async ({ silent = false } = {}) => {
    if (!isWails) {
      setCacheStatus(null);
      setCacheError(null);
      return null;
    }

    try {
      if (!silent) {
        setCacheLoading(true);
      }
      setCacheError(null);
      const status = await getCacheStatus();
      setCacheStatus(status);
      return status;
    } catch (err) {
      const msg = err?.message || String(err) || '缓存状态加载失败';
      setCacheError(msg);
      return null;
    } finally {
      if (!silent) {
        setCacheLoading(false);
      }
    }
  }, [isWails]);

  // 加载设置数据
  const loadSettings = useCallback(async () => {
    let willRetry = false;
    try {
      setLoading(true);
      setCacheLoading(isWails);
      setError(null);
      setCacheError(null);
      setSaveMessage(null);

      let categoriesData, settingsData, portData;
      let cacheData = null;

      if (isWails) {
        [categoriesData, settingsData, portData, cacheData] = await Promise.all([
          getSettingCategories(),
          getAllSettings(),
          getPortInfo(),
          getCacheStatus().catch(err => {
            setCacheError(err?.message || String(err) || '缓存状态加载失败');
            return null;
          })
        ]);
      } else {
        [categoriesData, settingsData, portData] = await Promise.all([
          fetchSettingCategories(),
          fetchAllSettings(),
          fetchPortInfo()
        ]);
      }

      setCategories(categoriesData || []);
      setSettings(settingsData || []);
      setPortInfo(portData);
      setCacheStatus(cacheData);
      setChanges({});
      setInitRetryAttempt(0);
      initRetryRef.current.count = 0;
      if (initRetryRef.current.timer) {
        clearTimeout(initRetryRef.current.timer);
        initRetryRef.current.timer = null;
      }

    } catch (err) {
      const msg = err?.message || String(err) || '加载设置失败';
      const maxRetry = 12;
      const retryDelayMs = 500;

      if (isRetriableInitError(err) && initRetryRef.current.count < maxRetry) {
        willRetry = true;
        initRetryRef.current.count += 1;
        const attempt = initRetryRef.current.count;
        setInitRetryAttempt(attempt);

        setAutoHideMessage({ type: 'info', text: `设置服务初始化中，正在重试 (${attempt}/${maxRetry})...` }, 5000);

        if (initRetryRef.current.timer) {
          clearTimeout(initRetryRef.current.timer);
        }
        initRetryRef.current.timer = setTimeout(() => {
          loadSettings();
        }, retryDelayMs);
      } else {
        setError(msg);
        setInitRetryAttempt(0);
      }
    } finally {
      if (!willRetry) {
        setLoading(false);
        setCacheLoading(false);
        setInitRetryAttempt(0);
      }
    }
  }, [isWails, isRetriableInitError, setAutoHideMessage]);

  useEffect(() => () => {
    if (initRetryRef.current.timer) {
      clearTimeout(initRetryRef.current.timer);
      initRetryRef.current.timer = null;
    }
    if (messageTimerRef.current) {
      clearTimeout(messageTimerRef.current);
      messageTimerRef.current = null;
    }
  }, []);

  useEffect(() => {
    loadSettings();
  }, [loadSettings]);

  // 按分类组织设置
  const settingsByCategory = useMemo(() => {
    const grouped = {};
    for (const setting of settings) {
      if ((setting.provider || 'global') !== 'global' || LEGACY_CATEGORIES.has(setting.category)) continue;
      if (CURRENT_KEYS[setting.category] && !CURRENT_KEYS[setting.category].includes(setting.key)) continue;
      if (!grouped[setting.category]) {
        grouped[setting.category] = [];
      }
      grouped[setting.category].push(setting);
    }
    // 按 display_order 排序
    for (const category in grouped) {
      grouped[category].sort((a, b) => a.display_order - b.display_order);
    }
    return grouped;
  }, [settings]);

  // 获取当前值（优先使用变更值）
  const getCurrentValue = useCallback((setting) => {
    const changeKey = `${setting.provider || 'global'}.${setting.category}.${setting.key}`;
    if (changeKey in changes) {
      return changes[changeKey];
    }
    return setting.value;
  }, [changes]);

  // 处理设置变更
  const handleSettingChange = useCallback((category, key, value) => {
    const provider = 'global';
    const changeKey = `${provider}.${category}.${key}`;
    setChanges(prev => ({
      ...prev,
      [changeKey]: value
    }));
  }, []);

  // 保存所有变更
  const handleSave = useCallback(async () => {
    if (Object.keys(changes).length === 0) {
      setAutoHideMessage({ type: 'info', text: '没有需要保存的变更' });
      return;
    }

    try {
      setSaving(true);
      setSaveMessage(null);

      // 构建更新数据
      const updateData = buildSettingsUpdates(changes);

      if (isWails) {
        await batchUpdateSettings({ settings: updateData });
      } else {
        await apiUpdateSettings(updateData);
      }

      // 检查是否有需要重启的设置
      const requiresRestart = updateData.some(item => {
        const setting = settings.find(s => (s.provider || 'global') === item.provider && s.category === item.category && s.key === item.key);
        return setting?.requires_restart;
      });

      // Reload clears stale messages; show the result after it has completed.
      await loadSettings();
      setAutoHideMessage({
        type: 'success',
        text: requiresRestart
          ? '设置已保存。部分设置需要重启应用后生效。'
          : '设置已保存并生效'
      });
    } catch (err) {
      setSaveMessage({ type: 'error', text: err.message || '保存失败' });
    } finally {
      setSaving(false);
    }
  }, [changes, isWails, settings, loadSettings, setAutoHideMessage]);

  // 打开重置确认弹窗
  const handleResetCategory = useCallback((category, label) => {
    setResetConfirm({ isOpen: true, category, label: label || category });
  }, []);

  // 执行重置操作（静默刷新，不滚动页面）
  const executeResetCategory = useCallback(async () => {
    const { category, label } = resetConfirm;
    setResetConfirm({ isOpen: false, category: '', label: '' });

    try {
      setSaving(true);
      setSaveMessage(null);

      if (isWails) {
        await resetCategorySettings(category);
      } else {
        await apiResetSettings(category);
      }

      setAutoHideMessage({ type: 'success', text: `${label} 设置已重置为默认值` });

      // 静默刷新设置数据，不触发 loading 状态
      let settingsData;
      if (isWails) {
        settingsData = await getAllSettings();
      } else {
        settingsData = await fetchAllSettings();
      }
      setSettings(settingsData || []);
      if (category === 'cache') {
        await loadCacheStatus({ silent: true });
      }

      // 清除该分类相关的未保存变更
      setChanges(prev => {
        const newChanges = { ...prev };
        for (const key of Object.keys(newChanges)) {
          if (key.startsWith(`global.${category}.`)) {
            delete newChanges[key];
          }
        }
        return newChanges;
      });
    } catch (err) {
      setSaveMessage({ type: 'error', text: err.message || '重置失败' });
    } finally {
      setSaving(false);
    }
  }, [resetConfirm, isWails, setAutoHideMessage, loadCacheStatus]);

  // 取消重置操作
  const cancelResetCategory = useCallback(() => {
    setResetConfirm({ isOpen: false, category: '', label: '' });
  }, []);

  // 判断是否有未保存的变更
  const hasChanges = Object.keys(changes).length > 0;

  // 取消所有变更
  const handleCancelAllChanges = useCallback(() => {
    setChanges({});
    setAutoHideMessage({ type: 'info', text: '已取消所有变更' });
  }, [setAutoHideMessage]);

  // 撤销单项修改
  const handleRevertSetting = useCallback((category, key) => {
    const provider = 'global';
    const changeKey = `${provider}.${category}.${key}`;
    const newChanges = { ...changes };
    delete newChanges[changeKey];
    setChanges(newChanges);
    setAutoHideMessage({ type: 'info', text: '已撤销该项修改' });
  }, [changes, setAutoHideMessage]);

  const handleClearCache = useCallback(async (names = []) => {
    if (!isWails) return;

    const targetNames = Array.isArray(names) ? names : [];
    const clearKey = getCacheClearKey(targetNames);
    try {
      setCacheClearing(clearKey);
      setCacheError(null);
      const result = await clearCache(targetNames);
      setAutoHideMessage({
        type: 'success',
        text: targetNames.length > 0
          ? `已清理 ${targetNames.join(', ')}`
          : `已清理 ${result.clearedEntries} 条缓存项`
      });
      await loadCacheStatus({ silent: true });
    } catch (err) {
      const msg = err?.message || String(err) || '清理缓存失败';
      setCacheError(msg);
      setSaveMessage({ type: 'error', text: msg });
    } finally {
      setCacheClearing('');
    }
  }, [isWails, loadCacheStatus, setAutoHideMessage]);

  // 添加快捷键支持 (Ctrl+S / Cmd+S)
  useEffect(() => {
    const handleKeyDown = (e) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault();
        if (hasChanges && !saving) {
          handleSave();
        }
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [hasChanges, saving, handleSave]);

  if (error) {
    return (
      <ErrorMessage
        title="设置加载失败"
        message={error}
        onRetry={loadSettings}
      />
    );
  }

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center py-24 space-y-6">
        <div className="relative">
          <div className="w-16 h-16 border-4 border-slate-200 border-t-indigo-600 rounded-full animate-spin"></div>
          <Settings className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 text-slate-400" size={24} />
        </div>
        <div className="text-center space-y-2">
          <div className="text-lg font-medium text-slate-700">
            {initRetryAttempt > 0 ? '设置服务初始化中...' : '加载设置...'}
          </div>
          {initRetryAttempt > 0 && (
            <>
              <div className="text-sm text-slate-500">
                正在重试 ({initRetryAttempt}/12)
              </div>
              <div className="w-64 h-2 bg-slate-200 rounded-full overflow-hidden">
                <div
                  className="h-full bg-indigo-600 transition-all duration-300 ease-out"
                  style={{ width: `${(initRetryAttempt / 12) * 100}%` }}
                />
              </div>
            </>
          )}
        </div>
      </div>
    );
  }

  const orderedSettingCategories = categories
    .filter(category => category.name !== 'server' && !LEGACY_CATEGORIES.has(category.name))
    .sort((a, b) => (
      getCategoryOrder(a.name) - getCategoryOrder(b.name) ||
      (a.order ?? 0) - (b.order ?? 0)
    ));
  const categoryColumns = [
    orderedSettingCategories.filter(category => PRIMARY_COLUMN_CATEGORIES.has(category.name)),
    orderedSettingCategories.filter(category => !PRIMARY_COLUMN_CATEGORIES.has(category.name))
  ];
  const renderSettingCategory = (category, className = '') => {
    if (!category) return null;
    const categorySettings = settingsByCategory[category.name] || [];
    if (categorySettings.length === 0 && category.name !== 'cache') return null;
    const enabledKey = category.name === 'health' ? 'active_probes_enabled' : category.name === 'logging' ? 'file_enabled' : null;
    const enabledSetting = categorySettings.find(setting => setting.key === enabledKey);
    const dependentDisabled = enabledSetting && String(getCurrentValue(enabledSetting)) === 'false';
    const settingItems = categorySettings.map(setting => {
      const changeKey = `${setting.provider || 'global'}.${setting.category}.${setting.key}`;
      const dependsOnSwitch = category.name === 'health'
        ? setting.key !== enabledKey
        : category.name === 'logging' && ['max_file_size', 'max_files', 'compress_rotated'].includes(setting.key);
      return (
        <SettingItem
          key={changeKey}
          setting={setting}
          value={getCurrentValue(setting)}
          onChange={handleSettingChange}
          disabled={saving || Boolean(dependentDisabled && dependsOnSwitch)}
          hasChange={changeKey in changes}
          onRevert={handleRevertSetting}
        />
      );
    });
    return (
      <SettingsSection
        key={category.name}
        compact
        title={category.label}
        icon={CATEGORY_ICONS[category.name] || Settings}
        description={category.name === 'server' ? undefined : category.description}
        onReset={() => handleResetCategory(category.name, category.label)}
        resetDisabled={saving}
        className={className}
      >
        {category.name === 'server' ? (
          <div className="grid items-center gap-3 py-2 sm:grid-cols-2">
            <PortInfo portInfo={portInfo} loading={loading} />
            <div className="min-w-0 border-slate-100 sm:border-l sm:pl-3">{settingItems}</div>
          </div>
        ) : settingItems}
        {category.name === 'retry' && (
          <p className="my-2 rounded-lg bg-indigo-50/60 px-2.5 py-2 text-[11px] leading-5 text-indigo-700">
            候选渠道、调度策略、方案预算和等待恢复，请在 Claude 管理或 Codex 管理的调度方案中配置。
          </p>
        )}
        {category.name === 'cache' && isWails && (
          <CacheStatusPanel
            status={cacheStatus}
            loading={cacheLoading}
            error={cacheError}
            clearing={cacheClearing}
            onRefresh={() => loadCacheStatus()}
            onClear={handleClearCache}
          />
        )}
        {categorySettings.length === 0 && (
          <div className="text-sm text-slate-400 text-center py-4">
            暂无设置项
          </div>
        )}
      </SettingsSection>
    );
  };

  return (
    <div className="settings-workspace space-y-3 animate-in fade-in slide-in-from-bottom-2 duration-500">
      {/* CSS 动画定义 */}
      <style>{`
        @keyframes countdown {
          from {
            stroke-dashoffset: 0;
          }
          to {
            stroke-dashoffset: 50.27;
          }
        }
      `}</style>

      {/* 页面标题 */}
      <div className="flex min-h-9 flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="shrink-0 rounded-lg bg-slate-900 p-1.5 text-white shadow-sm">
            <Settings size={20} />
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <h1 className="text-xl font-bold text-slate-900">系统设置</h1>
            <p className="text-xs text-slate-500">大部分设置即时生效</p>
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          <Button
            icon={RefreshCw}
            variant="secondary"
            size="sm"
            onClick={loadSettings}
            disabled={saving}
          >
            刷新
          </Button>
          <Button
            icon={Save}
            variant="primary"
            size="sm"
            onClick={handleSave}
            disabled={saving || !hasChanges}
            loading={saving}
            title="Ctrl+S / Cmd+S"
          >
            <span className="flex items-center gap-2">
              {hasChanges ? `保存 (${Object.keys(changes).length})` : '保存'}
              <kbd className="hidden sm:inline-block px-1.5 py-0.5 bg-white/20 rounded text-[10px] font-mono">
                {navigator.platform.indexOf('Mac') === 0 ? '⌘S' : 'Ctrl+S'}
              </kbd>
            </span>
          </Button>
        </div>
      </div>

      {/* 保存消息提示 */}
      {saveMessage && (
        <div className={`
          flex items-center gap-2 px-3 py-2 rounded-lg text-xs relative overflow-hidden
          ${saveMessage.type === 'success' ? 'bg-emerald-50 text-emerald-700 border border-emerald-200' : ''}
          ${saveMessage.type === 'error' ? 'bg-rose-50 text-rose-700 border border-rose-200' : ''}
          ${saveMessage.type === 'info' ? 'bg-slate-50 text-slate-600 border border-slate-200' : ''}
        `}>
          <div className="flex items-center gap-2 flex-1">
            {saveMessage.type === 'success' && <CheckCircle2 size={16} />}
            {saveMessage.type === 'error' && <AlertCircle size={16} />}
            {saveMessage.text}
          </div>
          {/* 自动消失倒计时圆圈 */}
          {(saveMessage.type === 'info' || saveMessage.type === 'success') && (
            <div className="relative w-5 h-5 flex-shrink-0">
              <svg className="w-5 h-5 -rotate-90" viewBox="0 0 20 20">
                {/* 渐变定义 */}
                <defs>
                  <linearGradient id="countdown-gradient" x1="0%" y1="0%" x2="100%" y2="100%">
                    <stop offset="0%" stopColor={saveMessage.type === 'success' ? '#10b981' : '#6366f1'} />
                    <stop offset="100%" stopColor={saveMessage.type === 'success' ? '#34d399' : '#818cf8'} />
                  </linearGradient>
                </defs>
                {/* 背景圆圈 */}
                <circle
                  cx="10"
                  cy="10"
                  r="8"
                  stroke="currentColor"
                  strokeWidth="2"
                  fill="none"
                  opacity="0.2"
                />
                {/* 进度圆圈 */}
                <circle
                  cx="10"
                  cy="10"
                  r="8"
                  stroke="url(#countdown-gradient)"
                  strokeWidth="2"
                  fill="none"
                  strokeDasharray="50.27"
                  strokeDashoffset="0"
                  className="animate-countdown"
                  style={{
                    animation: 'countdown 3s linear forwards'
                  }}
                />
              </svg>
            </div>
          )}
        </div>
      )}

      {/* 未保存变更提示 */}
      {hasChanges && (
        <div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 rounded-lg bg-amber-50 border border-amber-200">
          <div className="flex items-center gap-2 text-xs text-amber-700">
            <AlertCircle size={16} />
            您有 {Object.keys(changes).length} 项未保存的变更
          </div>
          <Button
            icon={X}
            variant="ghost"
            onClick={handleCancelAllChanges}
            disabled={saving}
            size="sm"
          >
            取消所有变更
          </Button>
        </div>
      )}

      {renderSettingCategory(categories.find(category => category.name === 'server'))}

      <div className="settings-categories grid grid-cols-1 items-start gap-3 lg:grid-cols-2 lg:items-stretch">
        {categoryColumns.map((column, index) => (
          <div key={index} className="settings-column flex min-w-0 flex-col gap-3">
            {column.map(category => renderSettingCategory(category, 'lg:grow'))}
          </div>
        ))}
      </div>

      {/* 重置确认弹窗 */}
      <ConfirmDialog
        isOpen={resetConfirm.isOpen}
        title="重置设置"
        message={`确定要将「${resetConfirm.label}」分类的设置重置为默认值吗？\n\n此操作无法撤销。`}
        confirmText="重置"
        cancelText="取消"
        variant="warning"
        onConfirm={executeResetCategory}
        onCancel={cancelResetCategory}
      />
    </div>
  );
};

export default SettingsPage;
