import { useCallback, useEffect, useState } from 'react';
import { AlertCircle, Loader2, Settings, XCircle } from 'lucide-react';
import { Button, Tooltip } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { getErrorMessage } from '@/features/providers/shared/formatters.js';
import { getCodexGlobalConfig, isWailsEnvironment, updateCodexGlobalConfig } from '@/services/wails/index.js';
import { DEFAULT_CONFIG } from '../model.js';

const CodexGlobalConfigModal = ({ onClose, onSaved }) => {
  const [config, setConfig] = useState(DEFAULT_CONFIG);
  const [initialConfig, setInitialConfig] = useState(DEFAULT_CONFIG);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const isWails = isWailsEnvironment();
  const modalTopPadding = isWails ? 48 : 16;
  const modalBottomPadding = 16;
  const modalMaxHeight = `calc(100vh - ${modalTopPadding + modalBottomPadding}px)`;

  useEffect(() => {
    let canceled = false;
    setLoading(true);
    getCodexGlobalConfig()
      .then((data) => {
        if (canceled) return;
        const loaded = {
          model: data.model || '',
          enable_1m_context: Boolean(data.enable_1m_context)
        };
        setConfig(loaded);
        setInitialConfig(loaded);
      })
      .catch((err) => {
        if (canceled) return;
        setError(getErrorMessage(err, '加载 Codex 全局配置失败'));
      })
      .finally(() => {
        if (!canceled) setLoading(false);
      });
    return () => {
      canceled = true;
    };
  }, []);

  const updateField = useCallback((key, value) => {
    setConfig((prev) => ({ ...prev, [key]: value }));
    setError('');
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setError('');
    try {
      await updateCodexGlobalConfig({
        model: config.model,
        enable_1m_context: config.enable_1m_context
      });
      await onSaved?.();
      onClose?.();
    } catch (err) {
      setError(getErrorMessage(err, '保存失败'));
    } finally {
      setSaving(false);
    }
  };

  const hasChanges =
    config.model !== initialConfig.model ||
    config.enable_1m_context !== initialConfig.enable_1m_context;

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto bg-slate-900/30 px-4 pb-4 backdrop-blur-sm animate-fade-in"
      style={{ paddingTop: `${modalTopPadding}px` }}
    >
      <div
        className="flex w-full max-w-xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl"
        style={{ maxHeight: modalMaxHeight }}
      >
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-6 py-4">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-slate-900 p-2 text-white shadow-sm">
              <Settings size={18} />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-slate-900">Codex 全局配置</h2>
              <p className="mt-0.5 text-xs text-slate-500">直接读写 ~/.codex/config.toml；代理地址自动同步自服务端口</p>
            </div>
          </div>
          <Tooltip content="关闭">
            <button
              type="button"
              onClick={onClose}
              disabled={saving}
              className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700 disabled:opacity-50"
            >
              <XCircle size={18} />
            </button>
          </Tooltip>
        </div>

        <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-6 py-6">
          {loading ? (
            <div className="flex items-center gap-2 px-2 py-3 text-sm text-slate-500">
              <Loader2 size={14} className="animate-spin" />
              正在加载 config.toml...
            </div>
          ) : (
            <div className="space-y-5">
              {error && (
                <div className="flex items-start gap-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">
                  <AlertCircle size={14} className="mt-0.5 flex-shrink-0" />
                  <span>{error}</span>
                </div>
              )}

              <div>
                <label className="mb-1.5 block text-sm font-medium text-slate-700">默认模型</label>
                <input
                  type="text"
                  value={config.model}
                  onChange={(e) => updateField('model', e.target.value)}
                  placeholder="例如：gpt-5.4"
                  className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                />
                <p className="mt-1 text-xs text-slate-400">作为所有渠道的默认值；某个渠道可在其编辑弹窗里单独覆盖</p>
              </div>

              <div className="flex items-start justify-between gap-4 rounded-2xl border border-slate-200 bg-slate-50/70 px-4 py-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <label className="text-sm font-medium text-slate-700">1M 上下文窗口</label>
                    <span
                      className={`rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.12em] ${
                        config.enable_1m_context
                          ? 'bg-emerald-100 text-emerald-700'
                          : 'bg-slate-200 text-slate-500'
                      }`}
                    >
                      {config.enable_1m_context ? '1M' : '默认'}
                    </span>
                  </div>
                  <p className="mt-1 text-xs leading-5 text-slate-500">
                    开启后写入 model_context_window = 1000000；关闭则移除该字段。
                  </p>
                </div>
                <Tooltip content={config.enable_1m_context ? '关闭 1M 上下文窗口' : '开启 1M 上下文窗口'}>
                  <button
                    type="button"
                    role="switch"
                    aria-checked={config.enable_1m_context}
                    onClick={() => updateField('enable_1m_context', !config.enable_1m_context)}
                    className={`relative inline-flex h-6 w-11 flex-shrink-0 items-center rounded-full transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500 focus-visible:ring-offset-2 ${
                      config.enable_1m_context ? 'bg-emerald-500' : 'bg-slate-300 hover:bg-slate-400'
                    }`}
                  >
                    <span
                      className={`absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow transition-transform ${
                        config.enable_1m_context ? 'translate-x-5' : 'translate-x-0'
                      }`}
                    />
                  </button>
                </Tooltip>
              </div>
            </div>
          )}
        </div>

        <div className="flex flex-shrink-0 items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/40 px-6 py-3">
          <Button variant="secondary" onClick={onClose} disabled={saving}>
            取消
          </Button>
          <Button
            variant="primary"
            onClick={handleSave}
            disabled={loading || saving || !hasChanges}
            loading={saving}
          >
            保存
          </Button>
        </div>
      </div>
    </div>
  );
};

export { CodexGlobalConfigModal };
