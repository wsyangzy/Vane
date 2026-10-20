import { TriangleAlert } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';

const ActiveChannelPauseDialog = ({ channelName, nextChannelName, loading, onCancel, onConfirm }) => renderModalPortal(
  <div className="fixed inset-0 z-[100] flex items-start justify-center bg-slate-900/30 px-4 pt-[20vh] backdrop-blur-sm animate-fade-in">
    <div className="w-full max-w-md overflow-hidden rounded-2xl bg-white shadow-xl">
      <div className="flex items-start gap-3 px-6 py-5">
        <div className="rounded-xl bg-amber-100 p-2 text-amber-700"><TriangleAlert size={18} /></div>
        <div>
          <h2 className="text-lg font-semibold text-slate-900">确认暂停活跃渠道</h2>
          <p className="mt-1 text-sm leading-6 text-slate-500">
            确定要暂停当前接管渠道“{channelName}”吗？暂停后将自动切换到“{nextChannelName}”。
          </p>
        </div>
      </div>
      <div className="flex justify-end gap-2 border-t border-slate-100 bg-slate-50/40 px-6 py-3">
        <Button variant="secondary" onClick={onCancel} disabled={loading}>取消</Button>
        <Button variant="danger" loading={loading} onClick={onConfirm}>确认暂停</Button>
      </div>
    </div>
  </div>
);

export { ActiveChannelPauseDialog };
