import { Trash2, AlertTriangle } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';

const DeleteChannelConfirmDialog = ({ channelName, endpointCount = 0, onConfirm, onCancel, loading }) => {
  const confirmDisabled = loading;

  return renderModalPortal(
    <div className="fixed inset-0 bg-black/50 flex items-start justify-center z-50 animate-fade-in pt-[20vh]">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-md p-6">
        <div className="flex items-center gap-3 mb-4">
          <div className="p-3 bg-rose-100 rounded-full">
            <AlertTriangle className="text-rose-600" size={24} />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-slate-900">确认删除渠道</h3>
            <p className="text-sm text-slate-500">此操作不可撤销</p>
          </div>
        </div>

        <p className="text-slate-700 mb-4">
          确定要删除渠道 <span className="font-semibold">“{channelName}”</span> 吗？
        </p>

        {endpointCount > 0 && (
          <div className="p-3 bg-amber-50 border border-amber-200 rounded-lg text-amber-800 text-sm mb-4">
            <div className="font-medium">该渠道下仍有 {endpointCount} 个端点</div>
            <div className="text-xs text-amber-700 mt-2">确认删除将同时删除该渠道下的所有端点并删除该渠道。</div>
          </div>
        )}

        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button
            variant="danger"
            icon={Trash2}
            onClick={() => onConfirm?.()}
            loading={loading}
            disabled={confirmDisabled}
          >
            确认删除
          </Button>
        </div>
      </div>
    </div>
  );
};

export { DeleteChannelConfirmDialog };
