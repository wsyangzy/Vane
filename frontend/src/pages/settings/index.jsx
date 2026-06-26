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
  Map
} from 'lucide-react';
import { Button, ErrorMessage, ConfirmDialog } from '@components/ui';
import { SettingItem, SettingsSection, PortInfo } from './components';
import {
  isWailsEnvironment,
  getSettingCategories,
  getAllSettings,
  batchUpdateSettings,
  resetCategorySettings,
  getPortInfo
} from '@utils/wailsApi.js';
import {
  fetchSettingCategories,
  fetchAllSettings,
  updateSettings as apiUpdateSettings,
  resetSettings as apiResetSettings,
  fetchPortInfo
} from '@utils/api.js';

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
  timezone: Map
};

const CATEGORY_ORDER = ['auth', 'health', 'request', 'retry', 'failover', 'strategy'];

const getCategoryOrder = (categoryName) => {
  const index = CATEGORY_ORDER.indexOf(categoryName);
  return index === -1 ? CATEGORY_ORDER.length : index;
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

  // 加载设置数据
  const loadSettings = useCallback(async () => {
    let willRetry = false;
    try {
      setLoading(true);
      setError(null);
      setSaveMessage(null);

      let categoriesData, settingsData, portData;

      if (isWails) {
        [categoriesData, settingsData, portData] = await Promise.all([
          getSettingCategories(),
          getAllSettings(),
          getPortInfo()
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
    const changeKey = `${setting.category}.${setting.key}`;
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
    const changeKey = `${category}.${key}`;
    setChanges(prev => ({
      ...prev,
      [changeKey]: value
    }));
  }, []);

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
        const [category, settingKey] = key.split('.');
        return { category, key: settingKey, value: String(value) };
      });

      if (isWails) {
        await batchUpdateSettings({ settings: updateData });
      } else {
        await apiUpdateSettings(updateData);
      }

      // 检查是否有需要重启的设置
      const requiresRestart = updateData.some(item => {
        const setting = settings.find(s => s.category === item.category && s.key === item.key);
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

      // 清除该分类相关的未保存变更
      setChanges(prev => {
        const newChanges = { ...prev };
        for (const key of Object.keys(newChanges)) {
          if (key.startsWith(`${category}.`)) {
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
  }, [resetConfirm, isWails, setAutoHideMessage]);

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
    const changeKey = `${category}.${key}`;
    const newChanges = { ...changes };
    delete newChanges[changeKey];
    setChanges(newChanges);
    setAutoHideMessage({ type: 'info', text: '已撤销该项修改' });
  }, [changes, setAutoHideMessage]);

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
              const changeKey = `${setting.category}.${setting.key}`;
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

      {/* 其他设置分类 (两列布局)
          🔧 [P5.5 调整] 字段数量 > 4 的分类自动跨 2 列，避免与小分类同行产生大片空白 */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {categories
          .filter(c => c.name !== 'server')
          .sort((a, b) => (
            getCategoryOrder(a.name) - getCategoryOrder(b.name) ||
            (a.display_order ?? 0) - (b.display_order ?? 0)
          ))
          .map(category => {
            const categorySettings = settingsByCategory[category.name]?.filter(shouldShowSetting) || [];
            const isWideCategory = categorySettings.length > 4;
            return (
            <SettingsSection
              key={category.name}
              title={category.label}
              icon={CATEGORY_ICONS[category.name] || Settings}
              description={category.description}
              onReset={() => handleResetCategory(category.name, category.label)}
              resetDisabled={saving}
              className={isWideCategory ? 'lg:col-span-2' : ''}
            >
              {settingsByCategory[category.name]?.filter(shouldShowSetting).map(setting => {
                const changeKey = `${setting.category}.${setting.key}`;
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
              {(!settingsByCategory[category.name] || settingsByCategory[category.name].filter(shouldShowSetting).length === 0) && (
                <div className="text-sm text-slate-400 text-center py-4">
                  暂无设置项
                </div>
              )}
            </SettingsSection>
            );
          })}
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
