import { WailsApp, initWails } from './connection.js';

// ============================================
// 挂起请求计数 API
// ============================================

/**
 * 获取当前挂起等待端点恢复的请求数量
 * 用于渠道管理页面显示等待提示
 * @returns {Promise<number>} - 挂起请求数量
 */
const getSuspendedRequestsCount = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetSuspendedRequestsCount();
};

/**
 * 获取当前挂起请求的端点计数
 * @returns {Promise<Object>} - { [endpointName]: count }
 */
const getSuspendedEndpointCounts = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetSuspendedEndpointCounts();
};

/**
 * 获取等待用户操作的挂起请求数量（达到最大循环轮数）
 * @returns {Promise<number>} - 挂起请求数量
 */
const getManualRetryRequestsCount = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetManualRetryRequestsCount();
};

/**
 * 获取等待用户操作的渠道计数
 * @returns {Promise<Object>} - { [channelName]: count }
 */
const getManualRetryChannelCounts = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetManualRetryChannelCounts();
};

// ============================================
// 端口冲突处理 API
// ============================================

/**
 * 获取端口冲突状态
 * @returns {Promise<Object>} - {has_conflict, conflict_port, error_message, proxy_address, proxy_running}
 */
const getPortConflictStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetPortConflictStatus();
};

/**
 * 解决端口冲突
 * @param {string} action - 处理动作: "retry" (重试首选端口) | "switch" (切换到可用端口)
 * @returns {Promise<Object>} - {success, proxy_address, error_message}
 */
const resolvePortConflict = async (action) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.ResolvePortConflict(action);
};

export { getSuspendedRequestsCount, getSuspendedEndpointCounts, getManualRetryRequestsCount, getManualRetryChannelCounts, getPortConflictStatus, resolvePortConflict };
