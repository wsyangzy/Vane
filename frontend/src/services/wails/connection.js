

// 检测是否在 Wails 环境中运行
// v3: 使用 window._wails 替代 v2 的 window.go
const isWailsEnvironment = () => {
  return typeof window !== 'undefined' && (window._wails !== undefined || window.go !== undefined);
};

// 动态导入 Wails 绑定（避免在非 Wails 环境中报错）
let WailsApp = null;

let WailsRuntime = null;

const loadWailsBindings = async () => {
  if (!isWailsEnvironment()) {
    return false;
  }

  try {
    WailsApp = await import('./bindings.js');
    WailsRuntime = await import('@/services/wails/runtime.js');
    return true;
  } catch (error) {
    console.warn('Failed to load Wails bindings:', error);
    return false;
  }
};

// 初始化 Wails 绑定
let wailsInitialized = false;

let wailsInitPromise = null;

const initWails = async () => {
  if (wailsInitialized) return true;
  if (wailsInitPromise) return wailsInitPromise;

  wailsInitPromise = loadWailsBindings().then(result => {
    wailsInitialized = result;
    return result;
  });

  return wailsInitPromise;
};

// 打开外部链接（Wails 环境下优先走 BrowserOpenURL，避免 WebView 内导航）
const openExternalURL = async (url, { reportFailure = false } = {}) => {
  const target = (url || '').trim();
  if (!target) return;

  if (isWailsEnvironment()) {
    await initWails();
    try {
      if (typeof WailsRuntime?.BrowserOpenURL !== 'function') throw new Error('浏览器服务尚未就绪');
      await WailsRuntime.BrowserOpenURL(target);
      return;
    } catch (error) {
      if (reportFailure) throw new Error('无法打开系统浏览器，请复制登录链接后手动打开', { cause: error });
      console.warn('Failed to open URL via Wails runtime:', error);
    }
  }

  try {
    if (typeof window !== 'undefined' && typeof window.open === 'function') {
      if (!reportFailure) { window.open(target, '_blank', 'noopener,noreferrer'); return; }
      const opened = window.open(target, '_blank');
      if (opened) { opened.opener = null; return; }
      if (reportFailure) throw new Error('浏览器未打开，请复制登录链接后手动打开');
    }
  } catch (error) {
    if (reportFailure) throw error;
    console.warn('Failed to open URL via window.open:', error);
  }
};

export { WailsApp, initWails, isWailsEnvironment, openExternalURL, WailsRuntime };
