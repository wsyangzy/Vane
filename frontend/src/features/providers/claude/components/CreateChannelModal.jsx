import { useState } from 'react';
import { CheckCircle2, XCircle } from 'lucide-react';
import { Button, Tooltip } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';

// ============================================
// 新建渠道弹窗（SQLite 模式）
// ============================================

const CreateChannelModal = ({
  open,
  onClose,
  onSubmit,
  loading = false,
  serverError = '',
  mode = 'create',
  initialValue = null,
  embedded = false,
  formId
}) => {
  const isEdit = mode === 'edit';
  const [name, setName] = useState(() => (isEdit ? (initialValue?.name || '') : ''));
  const [website, setWebsite] = useState(() => (isEdit ? (initialValue?.website || '') : ''));
  const [error, setError] = useState('');

  if (!open) return null;

  return (embedded ? content => content : renderModalPortal)(
    <div className={embedded ? 'contents' : 'fixed inset-0 bg-slate-900/30 backdrop-blur-sm flex items-center justify-center p-4 z-[60]'}>
      <div className={embedded ? 'channel-setup-embedded' : 'bg-white rounded-2xl shadow-xl w-full max-w-2xl max-h-[calc(100vh-2rem)] flex flex-col overflow-hidden'}>
        {!embedded && <div className="flex items-start justify-between px-6 py-4 border-b border-slate-100">
          <div>
            <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑渠道' : '添加渠道 · 1/3'}</h2>
            <p className="text-xs text-slate-500 mt-1">
              {isEdit ? '修改渠道官网' : '填写渠道信息，完成凭证和端点配置后统一创建；中途退出不保存'}
            </p>
          </div>
          <Tooltip content="关闭">
            <button
              onClick={onClose}
              className="p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700 rounded-lg transition-colors"
              disabled={loading}
              aria-label="关闭"
            >
              <XCircle size={18} />
            </button>
          </Tooltip>
        </div>}

        <form
          id={formId}
          onSubmit={(e) => {
            e.preventDefault();
            const trimmedName = name.trim();
            if (!trimmedName) {
              setError('请输入渠道名称');
              return;
            }
            setError('');
            onSubmit?.({
              name: trimmedName,
              website: website.trim(),
              // Keep legacy resource order out of the channel form.
              priority: isEdit ? (initialValue?.priority ?? 1) : 0,
              // Saving basic channel fields must preserve stored model mappings.
              modelMapping: isEdit ? { ...(initialValue?.modelMapping || {}) } : {},
            });
          }}
          className="flex flex-1 flex-col overflow-hidden"
          autoComplete="off"
        >
          <div className={`flex-1 overflow-y-auto p-6 ${embedded ? 'channel-setup-fields' : ''}`}>
            {(error || serverError) && (
              <div role="alert" className="mb-4 p-3 bg-rose-50 border border-rose-200 rounded-lg text-rose-700 text-sm">
                {error || serverError}
              </div>
            )}

          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-slate-700">
                渠道名称 <span className="text-rose-500">*</span>
              </label>
              <input
                aria-label="渠道名称"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例如：official / backup / openai"
                autoComplete="off"
                data-form-type="other"
                className={`mt-1 w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 ${
                  isEdit ? 'disabled:bg-slate-100 disabled:text-slate-500 disabled:cursor-not-allowed' : ''
                }`}
                disabled={loading || isEdit}
              />
              {isEdit && (
                <p className="text-xs text-slate-400 mt-1">渠道名称用于关联端点，不支持修改</p>
              )}
            </div>

            <div>
              <label className="block text-sm font-medium text-slate-700">渠道官网</label>
              <input
                aria-label="渠道官网"
                value={website}
                onChange={(e) => setWebsite(e.target.value)}
                placeholder="https://example.com (可选)"
                autoComplete="off"
                data-form-type="other"
                className="mt-1 w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500"
                disabled={loading}
              />
              <p className="text-xs text-slate-400 mt-1">用于渠道信息展示，非必填</p>
            </div>

          </div>
          </div>

          {!embedded && <div className="flex items-center justify-end gap-2 border-t border-slate-100 bg-white px-6 py-4">
            <Button variant="ghost" type="button" onClick={onClose} disabled={loading}>
              取消
            </Button>
            <Button icon={isEdit ? CheckCircle2 : undefined} type="submit" loading={loading}>
              {isEdit ? '保存修改' : '下一步'}
            </Button>
          </div>}
        </form>
      </div>
    </div>
  );
};

export { CreateChannelModal };
