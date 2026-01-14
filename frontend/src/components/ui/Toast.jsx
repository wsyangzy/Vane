// ============================================
// Toast - 应用内消息提示组件
// v6.2 (2026-01-13) - 替代浏览器原生 alert
// ============================================

import { useState, useCallback, createContext, useContext, useRef, useEffect } from 'react';
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
    titleColor: 'text-emerald-800'
  },
  error: {
    icon: XCircle,
    bgColor: 'bg-rose-50',
    borderColor: 'border-rose-200',
    iconColor: 'text-rose-500',
    titleColor: 'text-rose-800'
  },
  warning: {
    icon: AlertCircle,
    bgColor: 'bg-amber-50',
    borderColor: 'border-amber-200',
    iconColor: 'text-amber-500',
    titleColor: 'text-amber-800'
  },
  info: {
    icon: Info,
    bgColor: 'bg-blue-50',
    borderColor: 'border-blue-200',
    iconColor: 'text-blue-500',
    titleColor: 'text-blue-800'
  }
};

// 单个 Toast 组件
const ToastItem = ({ id, type = 'info', title, message, onClose }) => {
  const variant = TOAST_VARIANTS[type] || TOAST_VARIANTS.info;
  const IconComponent = variant.icon;

  return (
    <div
      className={`
        flex items-start gap-3 p-4 rounded-xl border shadow-lg
        ${variant.bgColor} ${variant.borderColor}
        animate-in slide-in-from-right-full duration-300
        max-w-sm w-full
      `}
    >
      <IconComponent size={20} className={`flex-shrink-0 ${variant.iconColor}`} />
      <div className="flex-1 min-w-0">
        {title && (
          <p className={`text-sm font-semibold ${variant.titleColor}`}>{title}</p>
        )}
        {message && (
          <p className="text-sm text-slate-600 mt-0.5 break-words">{message}</p>
        )}
      </div>
      <button
        onClick={() => onClose(id)}
        className="flex-shrink-0 p-1 text-slate-400 hover:text-slate-600 rounded transition-colors"
      >
        <X size={16} />
      </button>
    </div>
  );
};

// Toast 容器
const ToastContainer = ({ toasts, removeToast }) => {
  if (toasts.length === 0) return null;

  return (
    <div className="fixed top-4 right-4 z-[9999] flex flex-col gap-3">
      {toasts.map((toast) => (
        <ToastItem
          key={toast.id}
          {...toast}
          onClose={removeToast}
        />
      ))}
    </div>
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

    // 自动关闭
    if (toast.duration > 0) {
      setTimeout(() => {
        removeToast(id);
      }, toast.duration);
    }

    return id;
  }, [removeToast]);

  // 便捷方法
  const toast = useCallback((message, options = {}) => {
    return addToast({ message, ...options });
  }, [addToast]);

  toast.success = useCallback((message, title) => {
    return addToast({ type: 'success', message, title: title || '成功' });
  }, [addToast]);

  toast.error = useCallback((message, title) => {
    return addToast({ type: 'error', message, title: title || '错误' });
  }, [addToast]);

  toast.warning = useCallback((message, title) => {
    return addToast({ type: 'warning', message, title: title || '警告' });
  }, [addToast]);

  toast.info = useCallback((message, title) => {
    return addToast({ type: 'info', message, title: title || '提示' });
  }, [addToast]);

  return (
    <ToastContext.Provider value={toast}>
      {children}
      <ToastContainer toasts={toasts} removeToast={removeToast} />
    </ToastContext.Provider>
  );
};

// Hook
export const useToast = () => {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error('useToast must be used within a ToastProvider');
  }
  return context;
};

export default ToastProvider;
