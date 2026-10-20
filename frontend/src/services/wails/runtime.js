// Wails v2 → v3 Runtime Compatibility Shim
// v2 API surface backed by @wailsio/runtime (v3)
//
// v2 EventsOn callback: (data) => void
// v3 Events.On callback: (WailsEvent) => void  where WailsEvent = { name, data }

import { Events, Browser, Window } from '@wailsio/runtime';

// ============================================================
// Events
// ============================================================

/**
 * EventsOn - v2 compatible event listener
 * Wraps v3 Events.On, unwrapping WailsEvent.data for v2 callback signature
 * @param {string} eventName
 * @param {Function} callback - receives data directly (not WailsEvent)
 * @returns {Function} unsubscribe function
 */
export function EventsOn(eventName, callback) {
  return Events.On(eventName, (event) => {
    // v2 passes data directly to callback; v3 wraps in WailsEvent
    callback(event?.data);
  });
}

/**
 * EventsOnMultiple - v2 compatible (limited invocation count)
 * v3 doesn't have native equivalent, so we manually track call count
 */
export function EventsOnMultiple(eventName, callback, maxCallbacks) {
  if (maxCallbacks === -1) {
    // Unlimited = same as EventsOn
    return EventsOn(eventName, callback);
  }

  let count = 0;
  let unsubscribe = null;

  unsubscribe = Events.On(eventName, (event) => {
    count++;
    callback(event?.data);
    if (count >= maxCallbacks && unsubscribe) {
      unsubscribe();
    }
  });

  return unsubscribe;
}

/**
 * EventsOnce - v2 compatible (fires once)
 */
export function EventsOnce(eventName, callback) {
  return EventsOnMultiple(eventName, callback, 1);
}

/**
 * EventsOff - v2 compatible event unsubscription
 * @param {string} eventName
 * @param {...string} additionalEventNames
 */
export function EventsOff(eventName, ...additionalEventNames) {
  Events.Off(eventName, ...additionalEventNames);
}

/**
 * EventsOffAll - unregister all event listeners
 */
export function EventsOffAll() {
  // v3 doesn't have a direct equivalent; this is a best-effort no-op
  console.warn('[Wails Shim] EventsOffAll is not supported in v3');
}

/**
 * EventsEmit - emit event from frontend
 */
export function EventsEmit(eventName, ...data) {
  // v3 Emit signature: Emit(name, data)
  Events.Emit(eventName, data.length === 1 ? data[0] : data);
}

// ============================================================
// Browser
// ============================================================

export function BrowserOpenURL(url) {
  Browser.OpenURL(url);
}

// ============================================================
// Window
// ============================================================

export function WindowCenter() {
  Window.Center();
}

export function WindowSetTitle(title) {
  Window.SetTitle(title);
}

export function WindowFullscreen() {
  Window.Fullscreen();
}

export function WindowUnfullscreen() {
  Window.UnFullscreen();
}

export function WindowIsFullscreen() {
  return Window.IsFullscreen();
}

export function WindowGetSize() {
  return Window.GetSize();
}

export function WindowSetSize(width, height) {
  Window.SetSize(width, height);
}

export function WindowSetMaxSize(width, height) {
  Window.SetMaxSize(width, height);
}

export function WindowSetMinSize(width, height) {
  Window.SetMinSize(width, height);
}

export function WindowSetPosition(x, y) {
  Window.SetPosition(x, y);
}

export function WindowGetPosition() {
  return Window.GetPosition();
}

export function WindowHide() {
  Window.Hide();
}

export function WindowShow() {
  Window.Show();
}

export function WindowMaximise() {
  Window.Maximise();
}

export function WindowToggleMaximise() {
  Window.ToggleMaximise();
}

export function WindowUnmaximise() {
  Window.UnMaximise();
}

export function WindowIsMaximised() {
  return Window.IsMaximised();
}

export function WindowMinimise() {
  Window.Minimise();
}

export function WindowUnminimise() {
  Window.UnMinimise();
}

export function WindowIsMinimised() {
  return Window.IsMinimised();
}

export function WindowIsNormal() {
  return Window.IsNormal();
}

export function WindowSetBackgroundColour(R, G, B, A) {
  Window.SetBackgroundColour(R, G, B, A);
}

export function WindowReload() {
  window.location.reload();
}

export function WindowReloadApp() {
  window.location.reload();
}

export function WindowSetAlwaysOnTop(b) {
  Window.SetAlwaysOnTop(b);
}

// ============================================================
// Log (v3 has no frontend logging API - use console)
// ============================================================

export function LogPrint(message) { console.log(message); }
export function LogTrace(message) { console.trace(message); }
export function LogDebug(message) { console.debug(message); }
export function LogInfo(message) { console.info(message); }
export function LogWarning(message) { console.warn(message); }
export function LogError(message) { console.error(message); }
export function LogFatal(message) { console.error('[FATAL]', message); }

// ============================================================
// Theme (v3 uses different API - stubs)
// ============================================================

export function WindowSetSystemDefaultTheme() {}
export function WindowSetLightTheme() {}
export function WindowSetDarkTheme() {}

// ============================================================
// Screen
// ============================================================

export function ScreenGetAll() {
  return Promise.resolve([]);
}

// ============================================================
// App lifecycle
// ============================================================

export function Quit() {
  // v3: use backend RequestQuit instead
  console.warn('[Wails Shim] Quit() called - use RequestQuit() binding instead');
}

export function Hide() {
  Window.Hide();
}

export function Show() {
  Window.Show();
}

// ============================================================
// Clipboard
// ============================================================

export function ClipboardGetText() {
  return navigator.clipboard.readText();
}

export function ClipboardSetText(text) {
  return navigator.clipboard.writeText(text);
}

// ============================================================
// File Drop (v3 uses different API - stubs)
// ============================================================

export function OnFileDrop(_callback, _useDropTarget) {
  console.warn('[Wails Shim] OnFileDrop is not yet implemented for v3');
}

export function OnFileDropOff() {}

export function CanResolveFilePaths() {
  return false;
}

export function ResolveFilePaths(files) {
  return Promise.resolve(files);
}

// ============================================================
// Environment
// ============================================================

export function Environment() {
  return Promise.resolve({
    buildType: 'production',
    platform: navigator.platform,
    arch: ''
  });
}
