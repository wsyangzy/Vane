// ============================================
// Wails Events Hook
// 2025-12-04
// 使用 Wails Events 替代 SSE 实时数据
// ============================================

import { useState, useRef, useCallback, useEffect } from 'react';
import { isWailsEnvironment, initWails } from '@/services/wails/index.js';

// Wails 事件名称（与后端 app_events.go 对应）
const WAILS_EVENTS = {
  SYSTEM_STATUS: 'system:status',
  ENDPOINT_UPDATE: 'endpoint:update',
  GROUP_UPDATE: 'group:update',
  USAGE_UPDATE: 'usage:update',
  ERROR: 'error',
  NOTIFICATION: 'notification'
};

// 连接状态
export const WAILS_STATUS = {
  DISCONNECTED: 'disconnected',
  CONNECTING: 'connecting',
  CONNECTED: 'connected',
  ERROR: 'error',
  NOT_AVAILABLE: 'not_available'
};

// 事件映射：前端事件名 -> Wails 事件名
const EVENT_MAPPING = {
  'status': WAILS_EVENTS.SYSTEM_STATUS,
  'endpoint': WAILS_EVENTS.ENDPOINT_UPDATE,
  'group': WAILS_EVENTS.GROUP_UPDATE,
  // Wails 侧没有独立的 connection 事件，复用 usage:update 作为连接统计推送源
  'connection': WAILS_EVENTS.USAGE_UPDATE,
  'usage': WAILS_EVENTS.USAGE_UPDATE,
  'error': WAILS_EVENTS.ERROR,
  'notification': WAILS_EVENTS.NOTIFICATION
};

/**
 * Wails Events Hook
 * 替代 useSSE，使用 Wails 事件系统
 * @param {Function} onDataUpdate - 数据更新回调函数 (data, eventType) => void
 * @param {Object} options - 配置选项
 */
const useWailsEvents = (onDataUpdate, options = {}) => {
  const {
    events = 'status,endpoint,group,connection'
  } = options;

  const [connectionStatus, setConnectionStatus] = useState(WAILS_STATUS.CONNECTING);
  const onDataUpdateRef = useRef(onDataUpdate);
  const unsubscribersRef = useRef([]);
  const isInitializedRef = useRef(false);

  // 保持回调引用最新
  useEffect(() => {
    onDataUpdateRef.current = onDataUpdate;
  }, [onDataUpdate]);

  // 订阅事件
  const subscribe = useCallback(async () => {
    // 检查是否在 Wails 环境
    if (!isWailsEnvironment()) {
      console.log('📡 [Wails Events] 非 Wails 环境，跳过事件订阅');
      setConnectionStatus(WAILS_STATUS.NOT_AVAILABLE);
      return;
    }

    try {
      console.log('📡 [Wails Events] 初始化中...');
      setConnectionStatus(WAILS_STATUS.CONNECTING);

      // 等待 Wails 初始化
      const initialized = await initWails();
      if (!initialized) {
        console.warn('⚠️ [Wails Events] Wails 初始化失败');
        setConnectionStatus(WAILS_STATUS.ERROR);
        return;
      }

      // 动态导入 runtime
      const { EventsOn } = await import('@/services/wails/runtime.js');

      // 订阅请求的事件
      const eventTypes = events.split(',').map(e => e.trim());

      eventTypes.forEach(eventType => {
        const wailsEventName = EVENT_MAPPING[eventType];
        if (!wailsEventName) {
          console.warn(`⚠️ [Wails Events] 未知事件类型: ${eventType}`);
          return;
        }

        const unsubscribe = EventsOn(wailsEventName, (data) => {
          console.log(`📡 [Wails Events] 收到 ${eventType} 事件:`, data);

          if (onDataUpdateRef.current) {
            // 兼容 useOverviewData 的连接统计字段命名：successful_requests 等
            let normalizedData = data;
            if (eventType === 'connection' && data && typeof data === 'object') {
              normalizedData = { ...data };
              if (normalizedData.successful_requests === undefined && normalizedData.success_requests !== undefined) {
                normalizedData.successful_requests = normalizedData.success_requests;
              }
              if (normalizedData.total_tokens === undefined) {
                const input = normalizedData.total_input_tokens || 0;
                const output = normalizedData.total_output_tokens || 0;
                normalizedData.total_tokens = input + output;
              }
            }

            // 转换数据格式以兼容 SSE 处理逻辑
            const wrappedData = {
              data: normalizedData,
              event: eventType,
              ...(normalizedData || {})
            };
            onDataUpdateRef.current(wrappedData, eventType);
          }
        });

        unsubscribersRef.current.push(unsubscribe);
      });

      console.log('✅ [Wails Events] 事件订阅成功');
      setConnectionStatus(WAILS_STATUS.CONNECTED);
      isInitializedRef.current = true;

    } catch (error) {
      console.error('❌ [Wails Events] 订阅失败:', error);
      setConnectionStatus(WAILS_STATUS.ERROR);
    }
  }, [events]);

  // 取消订阅
  const unsubscribe = useCallback(() => {
    unsubscribersRef.current.forEach(unsub => {
      if (typeof unsub === 'function') {
        unsub();
      }
    });
    unsubscribersRef.current = [];
    setConnectionStatus(WAILS_STATUS.DISCONNECTED);
    console.log('📡 [Wails Events] 已取消订阅');
  }, []);

  // 重新连接
  const reconnect = useCallback(() => {
    unsubscribe();
    subscribe();
  }, [subscribe, unsubscribe]);

  // 组件挂载时自动订阅
  useEffect(() => {
    const timer = setTimeout(() => {
      subscribe();
    }, 100); // 延迟初始化，等待 DOM 准备就绪

    return () => {
      clearTimeout(timer);
      unsubscribe();
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  return {
    connectionStatus,
    reconnectAttempts: 0,
    connect: subscribe,
    disconnect: unsubscribe,
    reconnect,
    isConnected: connectionStatus === WAILS_STATUS.CONNECTED,
    isWailsAvailable: isWailsEnvironment()
  };
};

export default useWailsEvents;
