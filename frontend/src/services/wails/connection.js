

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
const openExternalURL = async (url) => {
  const target = (url || '').trim();
  if (!target) return;

  if (isWailsEnvironment()) {
    await initWails();
    try {
      WailsRuntime?.BrowserOpenURL?.(target);
      return;
    } catch (error) {
      console.warn('Failed to open URL via Wails runtime:', error);
    }
  }

  try {
    if (typeof window !== 'undefined' && typeof window.open === 'function') {
      window.open(target, '_blank', 'noopener,noreferrer');
    }
  } catch (error) {
    console.warn('Failed to open URL via window.open:', error);
  }
};

export { WailsApp, initWails, isWailsEnvironment, openExternalURL, WailsRuntime };
