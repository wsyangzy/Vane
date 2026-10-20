import { WailsRuntime, initWails } from './connection.js';

// ============================================
// Wails Events 适配
// ============================================

// 事件监听器映射
const eventListeners = new Map();

/**
 * 订阅 Wails 事件
 * @param {string} eventName - 事件名称
 * @param {Function} callback - 回调函数
 * @returns {Function} - 取消订阅函数
 */
const subscribeToEvent = (eventName, callback) => {
  // 异步初始化后订阅
  let unsubscribeFunc = null;
  let isUnsubscribed = false;

  console.log(`📡 [subscribeToEvent] 开始订阅事件: ${eventName}`);

  initWails().then(() => {
    if (isUnsubscribed) {
      console.log(`📡 [subscribeToEvent] 事件 ${eventName} 已取消订阅，跳过注册`);
      return;
    }

    if (!WailsRuntime) {
      console.warn(`📡 [subscribeToEvent] Wails Runtime not loaded, 无法订阅 ${eventName}`);
      return;
    }

    console.log(`📡 [subscribeToEvent] 注册事件监听: ${eventName}`);
    unsubscribeFunc = WailsRuntime.EventsOn(eventName, (data) => {
      console.log(`📡 [subscribeToEvent] 收到事件 ${eventName}:`, data);
      callback(data);
    });

    // 存储监听器以便清理
    if (!eventListeners.has(eventName)) {
      eventListeners.set(eventName, []);
    }
    eventListeners.get(eventName).push({ callback, unsubscribe: unsubscribeFunc });
  });

  // 返回取消订阅函数
  return () => {
    isUnsubscribed = true;
    if (unsubscribeFunc && typeof unsubscribeFunc === 'function') {
      unsubscribeFunc();
    }
  };
};

/**
 * 取消订阅所有事件
 */
const unsubscribeAll = () => {
  if (!WailsRuntime) return;

  eventListeners.forEach((listeners) => {
    listeners.forEach(({ unsubscribe }) => {
      if (typeof unsubscribe === 'function') {
        unsubscribe();
      }
    });
  });
  eventListeners.clear();
};

export { subscribeToEvent, unsubscribeAll };
