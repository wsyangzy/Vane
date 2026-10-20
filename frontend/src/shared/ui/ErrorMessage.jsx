import { AlertCircle } from 'lucide-react';

export const ErrorMessage = ({ title = '加载失败', message, onRetry }) => (
  <div className="flex flex-col items-center justify-center py-12 text-center">
    <div className="p-4 bg-rose-50 rounded-full mb-4">
      <AlertCircle size={32} className="text-rose-500" />
    </div>
    <h3 className="text-lg font-semibold text-rose-700 mb-2">{title}</h3>
    {message && <p className="text-sm text-rose-600 max-w-md mb-4">{message}</p>}
    {onRetry && (
      <button
        onClick={onRetry}
        className="px-4 py-2 bg-rose-600 text-white rounded-lg text-sm font-medium hover:bg-rose-700 transition-colors"
      >
        重试
      </button>
    )}
  </div>
);
