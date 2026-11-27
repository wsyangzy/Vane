// useWailsLogs.js - Wails 日志流 Hook
// 提供实时日志查看功能
import { useState, useEffect, useCallback, useRef } from 'react';
import { EventsOn, EventsOff } from '@/services/wails/runtime.js';
import { normalizeLogEntry } from '../logPresentation.js';
import {
  getRecentLogs,
  startLogStream,
  stopLogStream,
  getLogStreamStatus
} from '@/services/wails/index.js';

const LOG_STREAM_USER_PAUSED_KEY = 'log_stream_user_paused';

const readUserPaused = () => {
  try {
    return window.sessionStorage.getItem(LOG_STREAM_USER_PAUSED_KEY) === '1';
  } catch {
    return false;
  }
};

const writeUserPaused = (paused) => {
  try {
    if (paused) {
      window.sessionStorage.setItem(LOG_STREAM_USER_PAUSED_KEY, '1');
    } else {
      window.sessionStorage.removeItem(LOG_STREAM_USER_PAUSED_KEY);
    }
  } catch {
    // ignore storage errors
  }
};

/**
 * useWailsLogs Hook
 * @param {Object} options 配置选项
 * @param {number} options.maxLogs 最大日志条数（默认500）
 * @param {boolean} options.autoStart 是否自动启动流（默认true）
 * @param {string} options.levelFilter 日志级别过滤（默认null，显示全部）
 * @param {boolean} options.isActive 页面是否可见（默认true）
 * @returns {Object} { logs, loading, error, isStreaming, start, stop, clear, refresh }
 */
export function useWailsLogs(options = {}) {
  const {
    maxLogs = 500,
    autoStart = true,
    levelFilter = null,
    isActive = true, // 新增：页面可见性
  } = options;

  const [logs, setLogs] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [isStreaming, setIsStreaming] = useState(false);

  const unsubscribeRef = useRef(null);
  const isMountedRef = useRef(true);
  // 防止并发调用 startStreaming
  const isStartingRef = useRef(false);
  // 每次挂载或停止都递增，旧的异步启动与订阅不能恢复当前监听。
  const instanceIdRef = useRef(0);
  // 后端启停按顺序执行，快速停止后重开时不能由旧操作覆盖新状态。
  const streamOperationRef = useRef(null);
  // 用户手动暂停标志，防止页面可见性控制自动恢复
  const userPausedRef = useRef(false);

  // 日志级别过滤
  const filterLogs = useCallback((logList) => {
    if (!levelFilter) return logList;
    return logList.filter(log => log.level === levelFilter);
  }, [levelFilter]);

  // 加载历史日志
  const loadRecentLogs = useCallback(() => {
    return getRecentLogs(maxLogs).then((recentLogs) => {
      if (isMountedRef.current) {
        setLogs(filterLogs((recentLogs || []).map(normalizeLogEntry)));
        setError(null);
      }
    }).catch((err) => {
      console.error('❌ 加载历史日志失败:', err);
      if (isMountedRef.current) {
        setError(err.message || '加载失败');
      }
    }).finally(() => {
      if (isMountedRef.current) {
        setLoading(false);
      }
    });
  }, [maxLogs, filterLogs]);

  // 启动日志流
  const startStreaming = useCallback(async (options = {}) => {
    const { userInitiated = true } = options;
    // 防止并发调用（解决 StrictMode 和异步竞态问题）
    if (isStartingRef.current) {
      console.log('📡 [日志流] 已有启动操作进行中，跳过');
      return;
    }
    isStartingRef.current = true;
    const currentInstanceId = ++instanceIdRef.current;
    // 用户主动启动时清除暂停标志
    if (userInitiated) {
      userPausedRef.current = false;
      writeUserPaused(false);
    }

    streamOperationRef.current = (streamOperationRef.current || Promise.resolve()).then(async () => {
      if (!isMountedRef.current || instanceIdRef.current !== currentInstanceId) return;
      const status = await getLogStreamStatus();
      if (!isMountedRef.current || instanceIdRef.current !== currentInstanceId) return;
      if (!status) {
        await startLogStream();
      }

      // StrictMode 重挂载、停止或卸载已经使本次启动失效。
      if (!isMountedRef.current || instanceIdRef.current !== currentInstanceId) {
        return;
      }

      // ⚠️ 关键修复：在订阅之前取消所有现有订阅
      // 放在异步操作之后，确保取消的是已经存在的订阅
      EventsOff('log:batch');
      unsubscribeRef.current = null;

      // 订阅日志事件（批量）
      const unsubscribe = EventsOn('log:batch', (batchLogs) => {
        // 检查是否仍是当前实例且组件未卸载
        if (!isMountedRef.current || instanceIdRef.current !== currentInstanceId) {
          return;
        }

        setLogs(prevLogs => {
          // 合并新日志，限制总数
          const newLogs = [...prevLogs, ...batchLogs.map(normalizeLogEntry)];
          return filterLogs(newLogs.slice(-maxLogs));
        });
      });

      unsubscribeRef.current = unsubscribe;

      if (isMountedRef.current) {
        setError(null);
        setIsStreaming(true);
      }
    }).catch((err) => {
      console.error('❌ 启动日志流失败:', err);
      if (isMountedRef.current && instanceIdRef.current === currentInstanceId) {
        setError(err.message || '启动失败');
      }
    }).finally(() => {
      if (instanceIdRef.current === currentInstanceId) isStartingRef.current = false;
    });
    return streamOperationRef.current;
  }, [maxLogs, filterLogs]);

  // 停止日志流
  const stopStreaming = useCallback(async (options = {}) => {
    const { userInitiated = true } = options;
    // 标记为用户主动暂停，防止页面可见性控制自动恢复
    if (userInitiated) {
      userPausedRef.current = true;
      writeUserPaused(true);
    }

    // 使旧的异步启动与回调失效，并允许下一次主动启动。
    const currentInstanceId = ++instanceIdRef.current;
    isStartingRef.current = false;

    // 用 EventsOff 显式取消所有 log:batch 订阅
    EventsOff('log:batch');
    unsubscribeRef.current = null;

    // 再停止后端流（检查是否正在运行）
    streamOperationRef.current = (streamOperationRef.current || Promise.resolve()).then(async () => {
      if (instanceIdRef.current !== currentInstanceId) return;
      const isRunning = await getLogStreamStatus();
      if (instanceIdRef.current !== currentInstanceId) return;
      if (isRunning) {
        await stopLogStream();
      }

      if (isMountedRef.current && instanceIdRef.current === currentInstanceId) {
        setIsStreaming(false);
      }
    }).catch((err) => {
      console.error('❌ 停止日志流失败:', err);
    });
    return streamOperationRef.current;
  }, []);

  // 清空日志
  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);

  // 刷新日志
  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    await loadRecentLogs();
  }, [loadRecentLogs]);

  // 初始化 - 只在组件挂载时执行一次
  useEffect(() => {
    isMountedRef.current = true;
    // 重置启动锁
    isStartingRef.current = false;
    instanceIdRef.current += 1;
    userPausedRef.current = readUserPaused();

    // 1. 加载历史日志
    loadRecentLogs();

    // 2. 自动启动流
    if (autoStart && !userPausedRef.current) {
      startStreaming({ userInitiated: false });
    }

    // 清理函数
    return () => {
      isMountedRef.current = false;
      // 取消所有订阅
      EventsOff('log:batch');
      if (unsubscribeRef.current) {
        unsubscribeRef.current();
        unsubscribeRef.current = null;
      }
    };
    // 注意：依赖数组为空，只在挂载/卸载时执行
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 页面可见性控制：当页面不可见时停止监听，节省资源
  useEffect(() => {
    // 跳过初始渲染
    if (!isMountedRef.current) return;

    if (!isActive && isStreaming) {
      // 页面不可见，停止监听（注意：这不是用户主动暂停，需要临时保存状态）
      const wasUserPaused = userPausedRef.current;
      console.log('📴 日志页面不可见，停止日志流');
      stopStreaming({ userInitiated: false });
      // 恢复之前的用户暂停状态（页面不可见导致的暂停不算用户主动暂停）
      userPausedRef.current = wasUserPaused;
    } else if (isActive && !isStreaming && autoStart && !isStartingRef.current && !userPausedRef.current) {
      // 页面重新可见，且用户没有主动暂停时才重新启动
      console.log('📡 日志页面可见，重新启动日志流');
      startStreaming({ userInitiated: false });
    }
  }, [isActive, isStreaming, autoStart, startStreaming, stopStreaming]);

  const start = useCallback(() => startStreaming({ userInitiated: true }), [startStreaming]);
  const stop = useCallback(() => stopStreaming({ userInitiated: true }), [stopStreaming]);

  return {
    logs,
    loading,
    error,
    isStreaming,
    start,
    stop,
    clear: clearLogs,
    refresh,
  };
}

export default useWailsLogs;
