// ============================================
// Toast - 应用内消息提示组件
// (2026-01-13) - 替代浏览器原生 alert
// (2026-03-17) - 倒计时圆圈动效关闭按钮
// ============================================

import { useState, useCallback, useEffect, createContext, useContext, useRef, useMemo } from 'react';
import { CheckCircle2, XCircle, AlertCircle, Info, X } from 'lucide-react';

// Toast 上下文
const ToastContext = createContext(null);

// Toast 消息类型配置
const TOAST_VARIANTS = {
  success: {
    icon: CheckCircle2,
    bgColor: 'bg-emerald-50',
    borderColor: 'border-emerald-200',
    iconColor: 'text-emerald-500',
    titleColor: 'text-emerald-800',
    ringFrom: '#10b981',
    ringTo: '#34d399'
  },
  error: {
    icon: XCircle,
    bgColor: 'bg-rose-50',
    borderColor: 'border-rose-200',
    iconColor: 'text-rose-500',
    titleColor: 'text-rose-800',
    ringFrom: '#f43f5e',
    ringTo: '#fb7185'
  },
  warning: {
    icon: AlertCircle,
    bgColor: 'bg-amber-50',
    borderColor: 'border-amber-200',
    iconColor: 'text-amber-500',
    titleColor: 'text-amber-800',
    ringFrom: '#f59e0b',
    ringTo: '#fbbf24'
  },
  info: {
    icon: Info,
    bgColor: 'bg-blue-50',
    borderColor: 'border-blue-200',
    iconColor: 'text-blue-500',
    titleColor: 'text-blue-800',
    ringFrom: '#6366f1',
    ringTo: '#818cf8'
  }
};

// 倒计时圆圈半径与周长
const RING_R = 8;
const RING_C = 2 * Math.PI * RING_R; // ≈ 50.27

// 单个 Toast 组件
const ToastItem = ({ id, type = 'info', title, message, duration, onClose }) => {
  const variant = TOAST_VARIANTS[type] || TOAST_VARIANTS.info;
  const IconComponent = variant.icon;
  // 每个 toast 独立的 gradient id，避免多个 toast 共享同一 id 导致渐变错乱
  const gradientId = `toast-ring-${id}`;

  // 退出动画
  const [exiting, setExiting] = useState(false);
  const [entered, setEntered] = useState(false);
  const timerRef = useRef(null);

  const handleClose = useCallback(() => {
    if (exiting) return;
    setExiting(true);
    setEntered(false);
    // 等退出动画完成再移除 DOM
    const closeDuration = parseFloat(
      getComputedStyle(document.documentElement).getPropertyValue('--toast-close')
    ) || 250;
    window.setTimeout(() => onClose(id), closeDuration);
  }, [id, onClose, exiting]);

  useEffect(() => {
    const frameId = window.requestAnimationFrame(() => setEntered(true));
    return () => window.cancelAnimationFrame(frameId);
  }, []);

  useEffect(() => {
    if (duration > 0) {
      timerRef.current = setTimeout(handleClose, duration);
    }
    return () => clearTimeout(timerRef.current);
  }, [duration, handleClose]);

  return (
    <div
      role={type === 'error' ? 'alert' : 'status'}
      data-open={entered && !exiting}
      className={`
        t-toast pointer-events-auto flex items-start gap-3 p-4 rounded-xl border shadow-lg
        ${variant.bgColor} ${variant.borderColor}
        max-w-sm w-full ${entered && !exiting ? 'is-open' : ''}
      `}
    >
      <IconComponent size={20} className={`flex-shrink-0 mt-0.5 ${variant.iconColor}`} />
      <div className="flex-1 min-w-0">
        {title && (
          <p className={`text-sm font-semibold ${variant.titleColor}`}>{title}</p>
        )}
        {Array.isArray(message) ? (
          <ul className="mt-1.5 list-disc space-y-1 pl-4 text-sm leading-5 text-slate-600 marker:text-blue-400">
            {message.map((item, index) => (
              <li key={index} className="pl-0.5 [overflow-wrap:anywhere]">{item}</li>
            ))}
          </ul>
        ) : message && (
          <p className="mt-0.5 whitespace-pre-line text-sm leading-5 text-slate-600 [overflow-wrap:anywhere]">{message}</p>
        )}
      </div>
      {/* 倒计时圆圈关闭按钮 */}
      <button
        type="button"
        onClick={handleClose}
        className="motion-control relative flex-shrink-0 w-6 h-6 flex items-center justify-center text-slate-400 hover:text-slate-600"
        aria-label="关闭"
      >
        {/* 倒计时 SVG 圈 */}
        {duration > 0 && (
          <svg className="absolute inset-0 w-6 h-6 -rotate-90" viewBox="0 0 20 20">
            <defs>
              <linearGradient id={gradientId} x1="0%" y1="0%" x2="100%" y2="100%">
                <stop offset="0%" stopColor={variant.ringFrom} />
                <stop offset="100%" stopColor={variant.ringTo} />
              </linearGradient>
            </defs>
            {/* 背景圆圈 */}
            <circle
              className="toast-countdown-ring"
              cx="10" cy="10" r={RING_R}
              stroke="currentColor"
              strokeWidth="1.5"
              fill="none"
              opacity="0.15"
            />
            {/* 进度圆圈 */}
            <circle
              cx="10" cy="10" r={RING_R}
              stroke={`url(#${gradientId})`}
              strokeWidth="1.5"
              fill="none"
              strokeLinecap="round"
              strokeDasharray={RING_C}
              strokeDashoffset="0"
              style={{
                animation: `toast-countdown ${duration}ms linear forwards`
              }}
            />
          </svg>
        )}
        {/* X 图标 */}
        <X size={12} className="relative z-10" />
      </button>
    </div>
  );
};

// Toast 容器
const ToastContainer = ({ toasts, removeToast }) => {
  if (toasts.length === 0) return null;

  return (
    <>
      {/* 倒计时动画 逆时针 keyframes */}
      <style>{`
        @keyframes toast-countdown {
          from { stroke-dashoffset: 0; }
          to   { stroke-dashoffset: ${RING_C}; }
        }
      `}</style>
      <div className="toast-viewport fixed right-4 z-[9999] flex w-96 max-w-[calc(100vw-2rem)] flex-col gap-3 overflow-y-auto overscroll-contain pointer-events-none">
        {toasts.map((toast) => (
          <ToastItem
            key={toast.id}
            {...toast}
            onClose={removeToast}
          />
        ))}
      </div>
    </>
  );
};

// Toast Provider
export const ToastProvider = ({ children }) => {
  const [toasts, setToasts] = useState([]);
  const toastIdRef = useRef(0);

  const removeToast = useCallback((id) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const addToast = useCallback((options) => {
    const id = ++toastIdRef.current;
    const toast = {
      id,
      type: options.type || 'info',
      title: options.title,
      message: options.message,
      duration: options.duration ?? 4000 // 默认 4 秒
    };

    setToasts((prev) => [...prev, toast]);

    return id;
  }, []);

  // 便捷方法
  const toast = useMemo(() => ({
    show: (message, options = {}) => addToast({ message, ...options }),
    dismiss: removeToast,
    success: (message, title) => addToast({ type: 'success', message, title: title || '成功' }),
    error: (message, title) => addToast({ type: 'error', message, title: title || '错误', duration: 6000 }),
    warning: (message, title) => addToast({ type: 'warning', message, title: title || '警告', duration: 5000 }),
    info: (message, title) => addToast({ type: 'info', message, title: title || '提示' })
  }), [addToast, removeToast]);

  return (
    <ToastContext.Provider value={toast}>
      {children}
      <ToastContainer toasts={toasts} removeToast={removeToast} />
    </ToastContext.Provider>
  );
};

// Hook
// eslint-disable-next-line react-refresh/only-export-components
export const useToast = () => {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error('useToast must be used within a ToastProvider');
  }
  return context;
};

export default ToastProvider;
