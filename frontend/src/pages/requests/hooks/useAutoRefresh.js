// ============================================
// useAutoRefresh Hook - 自动刷新状态管理
// 添加页面可见性检测
// ============================================

import { useState, useEffect, useRef, useCallback } from 'react';

/**
 * useAutoRefresh Hook - 管理自动刷新逻辑（优化版）
 *
 * 设计理念 (by Dan Abramov):
 * 1. 正确的副作用清理 - 避免内存泄漏
 * 2. 使用 useRef 存储定时器引用,避免闭包陷阱
 * 3. 使用 useRef 存储回调函数,避免依赖项变化导致定时器重建
 * 4. 通过设置 interval=0 来停止自动刷新
 * 5. 页面隐藏时自动暂停刷新，可见时恢复，节省后端资源
 *
 * @param {Function} onRefresh - 刷新回调函数
 * @returns {Object}
 */
export const useAutoRefresh = (onRefresh) => {
  // 自动刷新间隔状态 (0 = 关闭)
  // 默认开启5秒自动刷新
  const [refreshInterval, setRefreshInterval] = useState(5);

  // 页面可见性状态
  const [isPageVisible, setIsPageVisible] = useState(!document.hidden);

  // 使用 useRef 存储定时器,避免闭包问题
  const timerRef = useRef(null);

  // 使用 useRef 存储最新的 onRefresh 回调,避免依赖项变化
  const onRefreshRef = useRef(onRefresh);

  // 每次渲染时更新 ref 中的回调
  useEffect(() => {
    onRefreshRef.current = onRefresh;
  }, [onRefresh]);

  // 清理定时器的辅助函数
  const clearTimer = useCallback(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  // 更改刷新间隔
  const changeInterval = useCallback((newInterval) => {
    setRefreshInterval(newInterval);
  }, []);

  // 监听页面可见性变化
  useEffect(() => {
    const handleVisibilityChange = () => {
      const visible = !document.hidden;
      setIsPageVisible(visible);

      // 页面重新可见时立即刷新一次（获取最新数据）
      if (visible && refreshInterval > 0) {
        onRefreshRef.current?.(true);
      }
    };

    document.addEventListener('visibilitychange', handleVisibilityChange);

    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, [refreshInterval]);

  // 当间隔变化或页面可见性变化时,重新设置定时器
  useEffect(() => {
    clearTimer();

    // 只有在页面可见且间隔大于0时才启动定时器
    if (refreshInterval > 0 && isPageVisible) {
      timerRef.current = window.setInterval(() => {
        // 调用 ref 中存储的最新回调，传入 silent=true 参数实现静默刷新
        onRefreshRef.current?.(true);
      }, refreshInterval * 1000);
    }

    // 清理函数
    return () => {
      clearTimer();
    };
  }, [refreshInterval, isPageVisible, clearTimer]);

  return {
    isEnabled: refreshInterval > 0,
    interval: refreshInterval,
    isPageVisible, // 暴露给外部用于调试
    changeInterval
  };
};

export default useAutoRefresh;
