// ============================================
// SaveResultDialog - 文件保存结果弹窗
// (2026-01-11) - 优化视觉效果
// ============================================

import { CheckCircle, XCircle, FolderOpen, X, FileText, Copy, Check, Sparkles } from 'lucide-react';
import { useState } from 'react';

// 格式化文件大小
const formatFileSize = (bytes) => {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
};

/**
 * 文件保存结果弹窗
 * @param {boolean} isOpen - 是否显示
 * @param {object} result - 保存结果 { success, fileName, filePath, fileSize, error }
 * @param {function} onClose - 关闭回调
 * @param {function} onOpenFolder - 打开文件夹回调（可选）
 */
const SaveResultDialog = ({ isOpen, result, onClose, onOpenFolder }) => {
  const [copied, setCopied] = useState(false);

  if (!isOpen || !result) return null;

  const showSuccess = isOpen && !!result.success;

  const { success, fileName, filePath, fileSize, error } = result;

  // 复制路径到剪贴板
  const handleCopyPath = async () => {
    if (filePath) {
      try {
        await navigator.clipboard.writeText(filePath);
        setCopied(true);
        setTimeout(() => setCopied(false), 2000);
      } catch (err) {
        console.error('Failed to copy path:', err);
      }
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/40 backdrop-blur-sm transition-opacity"
        onClick={onClose}
      />

      {/* Dialog */}
      <div className="relative bg-white rounded-2xl shadow-2xl w-full max-w-sm mx-4 overflow-hidden animate-in zoom-in-95 duration-300">
        {success ? (
          <>
            {/* 成功状态 - 简洁设计 */}
            <div className="relative pt-8 pb-6 px-6">
              {/* 关闭按钮 */}
              <button
                onClick={onClose}
                className="absolute top-3 right-3 p-1.5 text-slate-300 hover:text-slate-500 hover:bg-slate-100 rounded-lg transition-colors"
              >
                <X size={18} />
              </button>

              {/* 成功图标 - 带动画 */}
              <div className="flex justify-center mb-5">
                <div className={`relative transition-all duration-500 ${showSuccess ? 'scale-100 opacity-100' : 'scale-50 opacity-0'}`}>
                  {/* 背景光环 */}
                  <div className="absolute inset-0 bg-emerald-400/20 rounded-full blur-xl scale-150" />
                  {/* 外圈 */}
                  <div className="relative w-16 h-16 bg-gradient-to-br from-emerald-400 to-emerald-600 rounded-full flex items-center justify-center shadow-lg shadow-emerald-500/30">
                    <CheckCircle size={32} className="text-white" strokeWidth={2.5} />
                  </div>
                  {/* 装饰粒子 */}
                  <Sparkles size={14} className={`absolute -top-1 -right-1 text-amber-400 transition-all duration-700 ${showSuccess ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-2'}`} />
                </div>
              </div>

              {/* 标题 */}
              <h3 className="text-lg font-semibold text-slate-800 text-center mb-1">
                保存成功
              </h3>
              <p className="text-sm text-slate-400 text-center mb-5">
                日志文件已保存到本地
              </p>

              {/* 文件信息卡片 */}
              <div className="bg-gradient-to-br from-slate-50 to-slate-100/50 rounded-xl p-4 border border-slate-200/60">
                <div className="flex items-center gap-3">
                  <div className="p-2.5 bg-white rounded-xl shadow-sm border border-slate-100">
                    <FileText size={20} className="text-indigo-500" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <h4 className="font-medium text-slate-800 truncate text-sm" title={fileName}>
                      {fileName}
                    </h4>
                    <p className="text-xs text-slate-400 mt-0.5 font-medium">
                      {formatFileSize(fileSize)}
                    </p>
                  </div>
                </div>

                {/* 文件路径 */}
                {filePath && (
                  <div className="mt-3 pt-3 border-t border-slate-200/60">
                    <div className="flex items-center gap-2 bg-white rounded-lg px-3 py-2 border border-slate-100">
                      <p className="text-[11px] text-slate-400 truncate flex-1 font-mono" title={filePath}>
                        {filePath}
                      </p>
                      <button
                        onClick={handleCopyPath}
                        className={`p-1.5 rounded-md transition-all flex-shrink-0 ${
                          copied
                            ? 'bg-emerald-50 text-emerald-500'
                            : 'text-slate-300 hover:text-indigo-500 hover:bg-indigo-50'
                        }`}
                        title={copied ? '已复制' : '复制路径'}
                      >
                        {copied ? <Check size={14} /> : <Copy size={14} />}
                      </button>
                    </div>
                  </div>
                )}
              </div>
            </div>

            {/* Footer */}
            <div className="px-6 pb-6 flex gap-3">
              {onOpenFolder && (
                <button
                  onClick={() => onOpenFolder(filePath)}
                  className="flex-1 flex items-center justify-center gap-2 px-4 py-2.5 text-sm font-medium text-slate-600 bg-slate-100 hover:bg-slate-200 rounded-xl transition-colors"
                >
                  <FolderOpen size={16} />
                  打开目录
                </button>
              )}
              <button
                onClick={onClose}
                className={`${onOpenFolder ? 'flex-1' : 'w-full'} px-4 py-2.5 text-sm font-medium text-white bg-gradient-to-r from-emerald-500 to-emerald-600 hover:from-emerald-600 hover:to-emerald-700 rounded-xl transition-all shadow-sm shadow-emerald-500/20`}
              >
                完成
              </button>
            </div>
          </>
        ) : (
          <>
            {/* 失败状态 */}
            <div className="relative pt-8 pb-6 px-6">
              {/* 关闭按钮 */}
              <button
                onClick={onClose}
                className="absolute top-3 right-3 p-1.5 text-slate-300 hover:text-slate-500 hover:bg-slate-100 rounded-lg transition-colors"
              >
                <X size={18} />
              </button>

              {/* 失败图标 */}
              <div className="flex justify-center mb-5">
                <div className="w-16 h-16 bg-gradient-to-br from-rose-400 to-rose-600 rounded-full flex items-center justify-center shadow-lg shadow-rose-500/30">
                  <XCircle size={32} className="text-white" strokeWidth={2.5} />
                </div>
              </div>

              {/* 标题 */}
              <h3 className="text-lg font-semibold text-slate-800 text-center mb-1">
                保存失败
              </h3>
              <p className="text-sm text-slate-400 text-center mb-5">
                无法保存日志文件
              </p>

              {/* 错误信息 */}
              <div className="bg-rose-50 rounded-xl p-4 border border-rose-100">
                <p className="text-sm text-rose-600 text-center">
                  {error === 'cancelled' ? '已取消保存操作' : error || '发生未知错误'}
                </p>
              </div>
            </div>

            {/* Footer */}
            <div className="px-6 pb-6">
              <button
                onClick={onClose}
                className="w-full px-4 py-2.5 text-sm font-medium text-white bg-slate-600 hover:bg-slate-700 rounded-xl transition-colors"
              >
                关闭
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
};

export default SaveResultDialog;
