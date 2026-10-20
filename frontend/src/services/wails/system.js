import { WailsApp, initWails } from './connection.js';

// ============================================
// 系统状态 API
// ============================================

const getSystemStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const status = await WailsApp.GetSystemStatus();

  // 转换为前端期望的格式
  return {
    status: status.proxy_running ? 'running' : 'stopped',
    version: status.version,
    uptime: status.uptime_seconds,
    start_time: status.start_time, // ISO8601 格式的启动时间
    proxy_running: status.proxy_running,
    proxy_port: status.proxy_port,
    proxy_host: status.proxy_host,
    active_group: status.active_group,
    config_path: status.config_path,
    auth_enabled: status.auth_enabled,
    codex_enabled: status.codex_proxy_enabled
  };
};

/**
 * 获取 Claude Code CLI 版本
 * @returns {Promise<string>} 版本号，如 "1.0.17" 或 "未安装"
 */
const getClaudeCodeVersion = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.GetClaudeCodeVersion();
};

/**
 * 获取 Codex CLI 版本
 * @returns {Promise<string>} 版本号，如 "0.1.0" 或 "未安装"
 */
const getCodexVersion = async () => {
  await initWails()
  if (!WailsApp) throw new Error('Wails not available')
  return await WailsApp.GetCodexVersion()
}

const getConfig = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetConfig();
};

// ============================================
// 代理信息
// ============================================

const getProxyURL = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.GetProxyURL();
};

const isProxyRunning = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  return await WailsApp.IsProxyRunning();
};

// ============================================
// 系统操作 API
// ============================================

/**
 * 打开 Claude 配置目录
 */
const openClaudeConfigDir = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.OpenClaudeConfigDir();
};

/**
 * 打开 Codex 配置目录
 */
const openCodexConfigDir = async () => {
  await initWails()
  if (!WailsApp) throw new Error('Wails not available')
  return await WailsApp.OpenCodexConfigDir()
}

/**
 * 打开日志目录
 */
const openLogDir = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.OpenLogDir();
};

const openDataDir = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.OpenDataDir();
};

/**
 * 请求退出应用
 */
const requestQuit = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return await WailsApp.RequestQuit();
};

/**
 * 复制文本到剪贴板
 */
const copyToClipboard = async (text) => {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // Fallback for older browsers
    const textArea = document.createElement('textarea');
    textArea.value = text;
    document.body.appendChild(textArea);
    textArea.select();
    document.execCommand('copy');
    document.body.removeChild(textArea);
    return true;
  }
};

export { getSystemStatus, getClaudeCodeVersion, getCodexVersion, getConfig, getProxyURL, isProxyRunning, openClaudeConfigDir, openCodexConfigDir, openLogDir, openDataDir, requestQuit, copyToClipboard };
