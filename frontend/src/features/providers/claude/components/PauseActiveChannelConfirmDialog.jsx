import { AlertTriangle, Pause } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';

const PauseActiveChannelConfirmDialog = ({ channelName, onConfirm, onCancel, loading }) => {
  const confirmDisabled = loading;

  return renderModalPortal(
    <div className="fixed inset-0 bg-black/50 flex items-start justify-center z-50 animate-fade-in pt-[20vh]">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-md p-6">
        <div className="flex items-center gap-3 mb-4">
          <div className="p-3 bg-amber-100 rounded-full">
            <AlertTriangle className="text-amber-700" size={24} />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-slate-900">确认暂停活跃渠道</h3>
            <p className="text-sm text-slate-500">暂停后将按路由策略切换渠道</p>
          </div>
        </div>

        <p className="text-slate-700 mb-4">
          确定要暂停当前活跃渠道 <span className="font-semibold">“{channelName}”</span> 吗？
          暂停后将自动切换到可用渠道。
        </p>

        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button
            variant="danger"
            icon={Pause}
            onClick={() => onConfirm?.()}
            loading={loading}
            disabled={confirmDisabled}
          >
            确认暂停
          </Button>
        </div>
      </div>
    </div>
  );
};

export { PauseActiveChannelConfirmDialog };
