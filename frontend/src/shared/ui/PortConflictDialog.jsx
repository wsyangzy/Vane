// ============================================
// PortConflictDialog - 端口冲突处理对话框
// (2026-02-09) - 处理代理服务器端口冲突
// ============================================

import { useState } from 'react';
import { AlertTriangle, X, RefreshCw, Shuffle, Copy, Check } from 'lucide-react';
import Tooltip from './Tooltip.jsx';

const PortConflictDialog = ({
  isOpen,
  conflictPort,
  errorMessage,
  onRetry,
  onSwitch,
  onDismiss
}) => {
  const [loading, setLoading] = useState(false);
  const [action, setAction] = useState(null); // 'retry' | 'switch'
  const [success, setSuccess] = useState(false);
  const [successAddress, setSuccessAddress] = useState('');
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState('');

  if (!isOpen) return null;

  const handleRetry = async () => {
    setLoading(true);
    setAction('retry');
    setError('');
    try {
      const result = await onRetry();
      if (result.success) {
        setSuccess(true);
        setSuccessAddress(result.proxy_address);
      } else {
        setError(result.error_message || '重试失败');
      }
    } catch (e) {
      setError(e.message || '重试失败');
    } finally {
      setLoading(false);
    }
  };

  const handleSwitch = async () => {
    setLoading(true);
    setAction('switch');
    setError('');
    try {
      const result = await onSwitch();
      if (result.success) {
        setSuccess(true);
        setSuccessAddress(result.proxy_address);
      } else {
        setError(result.error_message || '切换失败');
      }
    } catch (e) {
      setError(e.message || '切换失败');
    } finally {
      setLoading(false);
    }
  };

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(successAddress);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Fallback for older browsers
      const textArea = document.createElement('textarea');
      textArea.value = successAddress;
      document.body.appendChild(textArea);
      textArea.select();
      document.execCommand('copy');
      document.body.removeChild(textArea);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const handleClose = () => {
    if (success) {
      onDismiss();
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50 backdrop-blur-sm"
        onClick={success ? handleClose : undefined}
      />

      {/* Dialog */}
      <div className="relative bg-white rounded-2xl shadow-2xl w-full max-w-md mx-4 overflow-hidden animate-in zoom-in-95 duration-200">
        {/* Header */}
        <div className="px-6 py-4 border-b border-slate-100 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <div className={`p-2 rounded-lg ${success ? 'bg-emerald-100' : 'bg-amber-100'}`}>
              {success ? (
                <Check size={20} className="text-emerald-600" />
              ) : (
                <AlertTriangle size={20} className="text-amber-600" />
              )}
            </div>
            <h3 className="text-lg font-semibold text-slate-900">
              {success ? '端口切换成功' : '端口冲突'}
            </h3>
          </div>
          {success && (
            <button
              onClick={handleClose}
              className="p-2 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-lg transition-colors"
            >
              <X size={20} />
            </button>
          )}
        </div>

        {/* Content */}
        <div className="px-6 py-5">
          {success ? (
            <div className="space-y-4">
              <p className="text-sm text-slate-600">
                代理服务器已成功启动，请更新您的 <code className="px-1.5 py-0.5 bg-slate-100 rounded text-slate-700 font-mono text-xs">ANTHROPIC_BASE_URL</code> 配置：
              </p>
              <div className="flex items-center gap-2 p-3 bg-slate-50 rounded-lg border border-slate-200">
                <code className="flex-1 text-sm font-mono text-slate-800 break-all">
                  {successAddress}
                </code>
                <Tooltip content={copied ? '已复制' : '复制地址'}>
                  <button
                    onClick={handleCopy}
                    className={`shrink-0 p-2 rounded-lg transition-colors ${
                      copied
                        ? 'bg-emerald-100 text-emerald-600'
                        : 'bg-white text-slate-500 hover:text-slate-700 hover:bg-slate-100 border border-slate-200'
                    }`}
                    aria-label={copied ? '已复制' : '复制地址'}
                  >
                    {copied ? <Check size={16} /> : <Copy size={16} />}
                  </button>
                </Tooltip>
              </div>
            </div>
          ) : (
            <div className="space-y-3">
              <p className="text-sm text-slate-600">
                首选端口 <span className="font-semibold text-slate-800">{conflictPort}</span> 被其他程序占用。
              </p>
              {errorMessage && (
                <p className="text-xs text-slate-500 font-mono bg-slate-50 p-2 rounded">
                  {errorMessage}
                </p>
              )}
              {error && (
                <p className="text-sm text-rose-600 bg-rose-50 p-2 rounded">
                  {error}
                </p>
              )}
              <p className="text-sm text-slate-500">
                请选择处理方式：
              </p>
            </div>
          )}
        </div>

        {/* Footer */}
        {!success && (
          <div className="px-6 py-4 bg-slate-50 border-t border-slate-100 flex flex-col gap-3">
            <button
              onClick={handleRetry}
              disabled={loading}
              className="w-full flex items-center justify-center gap-2 px-4 py-2.5 text-sm font-medium text-slate-700 bg-white border border-slate-200 rounded-lg hover:bg-slate-50 hover:border-slate-300 transition-colors disabled:opacity-50"
            >
              <RefreshCw size={16} className={loading && action === 'retry' ? 'animate-spin' : ''} />
              重试首选端口
              <span className="text-xs text-slate-400 ml-1">(端口 {conflictPort})</span>
            </button>
            <button
              onClick={handleSwitch}
              disabled={loading}
              className="w-full flex items-center justify-center gap-2 px-4 py-2.5 text-sm font-medium text-white bg-indigo-600 rounded-lg hover:bg-indigo-700 transition-colors disabled:opacity-50"
            >
              <Shuffle size={16} className={loading && action === 'switch' ? 'animate-spin' : ''} />
              切换到可用端口
            </button>
          </div>
        )}

        {/* Success Footer */}
        {success && (
          <div className="px-6 py-4 bg-slate-50 border-t border-slate-100 flex justify-end">
            <button
              onClick={handleClose}
              className="px-4 py-2 text-sm font-medium text-white bg-indigo-600 rounded-lg hover:bg-indigo-700 transition-colors"
            >
              完成
            </button>
          </div>
        )}
      </div>
    </div>
  );
};

export default PortConflictDialog;
