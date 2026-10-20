import { Trash2, TriangleAlert } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { isWailsEnvironment } from '@/services/wails/index.js';

// ============================================================================
// 删除确认弹窗（渠道 / 端点 共用）
// ============================================================================
const DeleteConfirmDialog = ({ title, description, hint, loading, onCancel, onConfirm }) => {
  const isWails = isWailsEnvironment();
  const modalTopPadding = isWails ? 48 : 16;

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto bg-slate-900/30 px-4 pb-4 backdrop-blur-sm animate-fade-in"
      style={{ paddingTop: `${modalTopPadding}px` }}
    >
      <div className="flex w-full max-w-md flex-col overflow-hidden rounded-2xl bg-white shadow-xl">
        <div className="flex items-start gap-3 border-b border-slate-100 px-6 py-4">
          <div className="rounded-xl bg-rose-100 p-2 text-rose-600 shadow-sm">
            <TriangleAlert size={18} />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-slate-900">{title}</h2>
            {description && <p className="mt-1 text-sm text-slate-500">{description}</p>}
          </div>
        </div>
        {hint && <div className="px-6 py-4 text-sm text-slate-600">{hint}</div>}
        <div className="flex items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/40 px-6 py-3">
          <Button variant="secondary" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button variant="danger" icon={Trash2} loading={loading} onClick={onConfirm}>
            确认删除
          </Button>
        </div>
      </div>
    </div>
  );
};

export { DeleteConfirmDialog };
