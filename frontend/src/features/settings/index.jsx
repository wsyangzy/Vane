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
  Shuffle,
  RotateCcw,
  HeartPulse,
  ArrowLeftRight,
  Clock,
  Lock,
  Hash,
  Archive,
  X,
  FileText,
  Map as MapIcon,
  Gauge,
  Trash2
} from 'lucide-react';
import { Button, ErrorMessage, ConfirmDialog } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { SettingItem, SettingsSection, PortInfo } from './components/index.js';
import {
  isWailsEnvironment,
  getSettingCategories,
  getAllSettings,
  batchUpdateSettings,
  resetCategorySettings,
  resetSettingsByScope,
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
  strategy: Shuffle,
  retry: RotateCcw,
  health: HeartPulse,
  failover: ArrowLeftRight,
  request: Clock,
  auth: Lock,
  token_counting: Hash,
  retention: Archive,
  // 🔧 [P5.5.5 新增] Logging / Timezone 分类图标
  logging: FileText,
  timezone: MapIcon,
  cache: Gauge
};

const BALANCED_CATEGORY_ORDER = [
  'token_counting',
  'strategy',
  'failover',
  'cache',
  'retention'
];

const CATEGORY_ORDER = [
  'auth',
  'health',
  'request',
  'retry',
  ...BALANCED_CATEGORY_ORDER
];

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
      <div className="mt-4 rounded-xl bg-slate-50/80 px-3 py-2.5">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <span className={`inline-flex h-7 items-center gap-1.5 rounded-full px-2.5 text-xs font-semibold ring-1 ring-inset ${
                status?.enabled
                  ? 'bg-emerald-50 text-emerald-700 ring-emerald-200'
                  : 'bg-slate-100 text-slate-500 ring-slate-200'
              }`}>
                {status?.enabled ? <CheckCircle2 size={13} /> : <AlertCircle size={13} />}
                {status?.enabled ? '缓存已启用' : '缓存已禁用'}
              </span>
              <span className="text-xs font-medium text-slate-500">
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
            className="flex max-h-[calc(100vh-2rem)] w-full max-w-6xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl"
            role="dialog"
            aria-modal="true"
            aria-labelledby="cache-status-title"
            onMouseDown={(event) => event.stopPropagation()}
          >
            <div className="flex flex-col gap-4 border-b border-slate-100 px-6 py-4 lg:flex-row lg:items-center lg:justify-between">
              <div className="flex min-w-0 items-start gap-3">
                <div className="rounded-lg bg-indigo-50 p-2 text-indigo-600">
                  <Gauge size={20} />
                </div>
                <div className="min-w-0">
                  <h2 id="cache-status-title" className="text-lg font-semibold text-slate-900">
                    缓存运行状态
                  </h2>
                  <p className="mt-1 text-sm text-slate-500">
                    {summaryText} · 占用/上限表示当前 key 数 / 自动淘汰上限
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
                  className="inline-flex h-9 w-9 items-center justify-center rounded-lg text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-600"
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

            <div className="overflow-auto p-6">
              <table className="w-full min-w-[760px] text-left text-sm">
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
                      <span className="block max-w-[220px] truncate font-mono text-xs font-semibold text-slate-700">
                        {cache.name}
                      </span>
                    </td>
                    <td className="px-3 py-2">
                      <span
                        className={`inline-flex rounded-full px-2 py-0.5 text-xs font-semibold ${
                          cache.enabled
                            ? 'bg-emerald-50 text-emerald-700'
                            : 'bg-slate-100 text-slate-500'
                        }`}
                        title={cache.fixed ? '运行时状态缓存，不受「启用缓存」开关控制' : undefined}
                      >
                        {cache.fixed ? '常驻' : cache.enabled ? '启用' : '禁用'}
                      </span>
                    </td>
                    <td className="px-3 py-2 font-mono text-xs text-slate-500">{formatCacheTTL(cache.ttl)}</td>
                    <td className="px-3 py-2 font-mono text-xs text-slate-500">
                      {formatCacheCapacity(cache)}
                    </td>
                    <td className="px-3 py-2 font-mono text-xs text-slate-500">
                      {formatCacheHitRate(cache.hits, cache.misses)}
                      {cache.staleServed > 0 && (
                        <span
                          className="ml-1.5 text-amber-600"
                          title="数据库查询失败时返回旧值的次数"
                        >
                          · 旧值 {cache.staleServed}
                        </span>
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
  const [failoverProvider, setFailoverProvider] = useState('claude');
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
      const provider = setting.provider || (setting.category === 'failover' ? 'claude' : 'global');
      if (setting.category === 'failover' && provider !== failoverProvider) continue;
      if (setting.category !== 'failover' && provider !== 'global') continue;
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
  }, [settings, failoverProvider]);

  // 获取当前值（优先使用变更值）
  const getCurrentValue = useCallback((setting) => {
    const changeKey = `${setting.provider || 'global'}.${setting.category}.${setting.key}`;
    if (changeKey in changes) {
      return changes[changeKey];
    }
    return setting.value;
  }, [changes]);

  // 获取渠道间故障转移开关状态
  const getChannelFailoverEnabled = useCallback(() => {
    const failoverSettings = settingsByCategory['failover'];
    if (!failoverSettings) return true; // 默认开启
    // 设置项 key 是 "enabled"，category 是 "failover"
    const channelFailoverSetting = failoverSettings.find(s => s.key === 'enabled');
    if (!channelFailoverSetting) return true;
    const value = getCurrentValue(channelFailoverSetting);
    return value === 'true' || value === true;
  }, [settingsByCategory, getCurrentValue]);

  // 获取路由策略类型
  const getStrategyType = useCallback(() => {
    const strategySettings = settingsByCategory['strategy'];
    if (!strategySettings) return 'priority';
    const typeSetting = strategySettings.find(s => s.key === 'type');
    if (!typeSetting) return 'priority';
    return getCurrentValue(typeSetting);
  }, [settingsByCategory, getCurrentValue]);

  // 获取循环重试开关状态
  const getLoopRetryEnabled = useCallback(() => {
    const failoverSettings = settingsByCategory['failover'];
    if (!failoverSettings) return true;
    const loopRetrySetting = failoverSettings.find(s => s.key === 'loop_retry');
    if (!loopRetrySetting) return true;
    const value = getCurrentValue(loopRetrySetting);
    return value === 'true' || value === true;
  }, [settingsByCategory, getCurrentValue]);

  // 获取跳过健康检测设置值
  const getSkipHealthCheck = useCallback(() => {
    const failoverSettings = settingsByCategory['failover'];
    if (!failoverSettings) return false;
    const skipHealthCheckSetting = failoverSettings.find(s => s.key === 'skip_health_check');
    if (!skipHealthCheckSetting) return false;
    const value = getCurrentValue(skipHealthCheckSetting);
    return value === 'true' || value === true;
  }, [settingsByCategory, getCurrentValue]);

  // 判断设置项是否应该显示（条件显示逻辑）
  const shouldShowSetting = useCallback((setting) => {
    // 快速测试相关设置仅在 strategy.type === 'fastest' 时显示
    if (setting.category === 'strategy' &&
        ['fast_test_enabled', 'fast_test_cache_ttl', 'fast_test_timeout', 'fast_test_path'].includes(setting.key)) {
      return getStrategyType() === 'fastest';
    }

    // loop_retry 和 max_loops 仅在渠道间故障转移开启时显示
    if (setting.category === 'failover' && setting.key === 'loop_retry') {
      return getChannelFailoverEnabled();
    }

    // max_loops 仅在 loop_retry === true 时显示
    if (setting.category === 'failover' && setting.key === 'max_loops') {
      return getChannelFailoverEnabled() && getLoopRetryEnabled();
    }

    // loop_retry_wait_timeout 仅在 loop_retry === true 时显示
    if (setting.category === 'failover' && setting.key === 'loop_retry_wait_timeout') {
      return getChannelFailoverEnabled() && getLoopRetryEnabled();
    }

    // skip_health_check 不再作为单独设置项显示，改为在循环重试弹窗内控制
    if (setting.category === 'failover' && setting.key === 'skip_health_check') {
      return false; // 始终隐藏，通过 loop_retry 弹窗控制
    }

    return true;
  }, [getStrategyType, getChannelFailoverEnabled, getLoopRetryEnabled]);

  // 判断设置项是否应该禁用（条件禁用逻辑）- 已通过 shouldShowSetting 处理
  const shouldDisableSetting = useCallback(() => {
    return false;
  }, []);

  // 获取禁用提示文案
  const getDisabledReason = useCallback(() => {
    return null;
  }, []);

  // 处理设置变更
  const handleSettingChange = useCallback((category, key, value) => {
    const provider = category === 'failover' ? failoverProvider : 'global';
    const changeKey = `${provider}.${category}.${key}`;
    setChanges(prev => ({
      ...prev,
      [changeKey]: value
    }));
  }, [failoverProvider]);

  // 处理跳过健康检测变更（通过弹窗触发）
  const handleSkipHealthCheckChange = useCallback((newValue) => {
    handleSettingChange('failover', 'skip_health_check', newValue ? 'true' : 'false');
  }, [handleSettingChange]);

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
      const updateData = Object.entries(changes).map(([key, value]) => {
        const [provider, category, settingKey] = key.split('./index.jsx');
        return { provider, category, key: settingKey, value: String(value) };
      });

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

      setAutoHideMessage({
        type: 'success',
        text: requiresRestart
          ? '设置已保存。部分设置需要重启应用后生效。'
          : '设置已保存并生效'
      });

      // 重新加载设置
      await loadSettings();
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
        if (category === 'failover') {
          await resetSettingsByScope(failoverProvider, category);
        } else {
          await resetCategorySettings(category);
        }
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
          if (key.startsWith(`${category === 'failover' ? failoverProvider : 'global'}.${category}.`)) {
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
  }, [resetConfirm, isWails, setAutoHideMessage, loadCacheStatus, failoverProvider]);

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
    const provider = category === 'failover' ? failoverProvider : 'global';
    const changeKey = `${provider}.${category}.${key}`;
    const newChanges = { ...changes };
    delete newChanges[changeKey];
    setChanges(newChanges);
    setAutoHideMessage({ type: 'info', text: '已撤销该项修改' });
  }, [changes, setAutoHideMessage, failoverProvider]);

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
    .filter(category => category.name !== 'server')
    .sort((a, b) => (
      getCategoryOrder(a.name) - getCategoryOrder(b.name) ||
      (a.display_order ?? 0) - (b.display_order ?? 0)
    ));
  const settingCategoriesByName = new Map(
    orderedSettingCategories.map(category => [category.name, category])
  );
  const balancedCategoryStart = getCategoryOrder(BALANCED_CATEGORY_ORDER[0]);
  const balancedCategoryEnd = getCategoryOrder(BALANCED_CATEGORY_ORDER.at(-1));
  const leadingSettingCategories = orderedSettingCategories.filter(
    category => getCategoryOrder(category.name) < balancedCategoryStart
  );
  const trailingSettingCategories = orderedSettingCategories.filter(
    category => getCategoryOrder(category.name) > balancedCategoryEnd
  );

  const renderSettingCategory = (category, className = '') => {
    if (!category) return null;
    const categorySettings = settingsByCategory[category.name]?.filter(shouldShowSetting) || [];
    return (
      <SettingsSection
        key={category.name}
        title={category.label}
        icon={CATEGORY_ICONS[category.name] || Settings}
        description={category.description}
        onReset={() => handleResetCategory(category.name, category.label)}
        resetDisabled={saving}
        className={className}
      >
        {category.name === 'failover' && (
          <div className="mb-3 inline-flex rounded-lg bg-slate-100 p-1">
            {['claude', 'codex'].map(provider => (
              <button
                key={provider}
                type="button"
                onClick={() => setFailoverProvider(provider)}
                className={`rounded-md px-4 py-1.5 text-sm font-medium transition-colors ${failoverProvider === provider ? 'bg-white text-indigo-600 shadow-sm' : 'text-slate-500 hover:text-slate-700'}`}
              >
                {provider === 'claude' ? 'Claude' : 'Codex'}
              </button>
            ))}
          </div>
        )}
        {categorySettings.map(setting => {
          const changeKey = `${setting.provider || 'global'}.${setting.category}.${setting.key}`;
          const isSettingDisabled = saving || shouldDisableSetting(setting);
          const disabledReason = getDisabledReason(setting);
          // 为 loop_retry 传递跳过健康检测相关 props
          const isLoopRetrySetting = setting.category === 'failover' && setting.key === 'loop_retry';
          return (
            <SettingItem
              key={changeKey}
              setting={setting}
              value={getCurrentValue(setting)}
              onChange={handleSettingChange}
              disabled={isSettingDisabled}
              disabledReason={disabledReason}
              hasChange={changeKey in changes}
              onRevert={handleRevertSetting}
              {...(isLoopRetrySetting && {
                skipHealthCheck: getSkipHealthCheck(),
                onSkipHealthCheckChange: handleSkipHealthCheckChange
              })}
            />
          );
        })}
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
    <div className="space-y-8 animate-in fade-in slide-in-from-bottom-2 duration-500">
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
      <div className="flex justify-between items-end">
        <div>
          <h1 className="text-2xl font-bold text-slate-900 flex items-center gap-3">
            <div className="p-2 bg-slate-900 rounded-lg text-white shadow-lg">
              <Settings size={20} />
            </div>
            <div>
              <div>系统设置</div>
              <p className="text-sm text-slate-500 font-normal mt-1">
                配置系统运行参数，大部分设置即时生效
              </p>
            </div>
          </h1>
        </div>
        <div className="flex items-center gap-3">
          <Button
            icon={RefreshCw}
            variant="secondary"
            onClick={loadSettings}
            disabled={saving}
          >
            刷新
          </Button>
          <Button
            icon={Save}
            variant="primary"
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
          flex items-center gap-3 px-4 py-3 rounded-lg text-sm relative overflow-hidden
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
        <div className="flex items-center justify-between px-4 py-3 rounded-lg bg-amber-50 border border-amber-200">
          <div className="flex items-center gap-2 text-sm text-amber-700">
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

      {/* 端口信息 (服务端口分类的特殊处理) */}
      {categories.find(c => c.name === 'server') && (
        <SettingsSection
          title="服务端口"
          icon={Server}
          description="API 服务端口配置"
          onReset={() => handleResetCategory('server', '服务端口')}
          resetDisabled={saving}
        >
          <PortInfo portInfo={portInfo} loading={loading} />
          <div className="mt-4 pt-4 border-t border-slate-100">
            {settingsByCategory['server']?.filter(shouldShowSetting).map(setting => {
              const changeKey = `${setting.provider || 'global'}.${setting.category}.${setting.key}`;
              return (
                <SettingItem
                  key={changeKey}
                  setting={setting}
                  value={getCurrentValue(setting)}
                  onChange={handleSettingChange}
                  disabled={saving}
                  hasChange={changeKey in changes}
                  onRevert={handleRevertSetting}
                />
              );
            })}
          </div>
        </SettingsSection>
      )}

      {/* 其他设置分类：普通卡片成对对齐，仅对中间高低差明显的卡片做组合布局。 */}
      <div className="space-y-6">
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          {leadingSettingCategories.map(category => renderSettingCategory(category))}
        </div>

        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          {BALANCED_CATEGORY_ORDER.slice(0, 2)
            .map(name => settingCategoriesByName.get(name))
            .filter(Boolean)
            .map(category => renderSettingCategory(category))}
        </div>

        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2 lg:items-stretch">
          <div className="contents lg:flex lg:h-full lg:min-w-0 lg:flex-col lg:gap-6">
            <div className="order-0 min-w-0 lg:flex-1">
              {renderSettingCategory(settingCategoriesByName.get('failover'), 'lg:h-full')}
            </div>
            <div className="order-2 min-w-0 lg:flex-1">
              {renderSettingCategory(settingCategoriesByName.get('retention'), 'lg:h-full')}
            </div>
          </div>
          <div className="order-1 min-w-0 lg:h-full lg:order-none">
            {renderSettingCategory(settingCategoriesByName.get('cache'), 'lg:h-full')}
          </div>
        </div>

        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          {trailingSettingCategories.map(category => renderSettingCategory(category))}
        </div>
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
