import { Trash2, AlertTriangle } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import CredentialGroupRoutingImpact from '../../shared/credentials/CredentialGroupRoutingImpact.jsx';
import useCredentialGroupRoutingImpact from '../../shared/credentials/useCredentialGroupRoutingImpact.js';

const DeleteCredentialGroupConfirmDialog = ({ channelName, group, onConfirm, onCancel, loading }) => {
  const endpointCount = Number(group?.endpointCount ?? group?.endpoint_count ?? 0);
  const impact = useCredentialGroupRoutingImpact('claude', group?.id);
  const isDefault = Boolean(group?.isDefault ?? group?.is_default);

  return renderModalPortal(
    <div className="fixed inset-0 bg-black/50 flex items-start justify-center z-50 animate-fade-in pt-[20vh]">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-md p-6">
        <div className="flex items-center gap-3 mb-4">
          <div className="p-3 bg-rose-100 rounded-full">
            <AlertTriangle className="text-rose-600" size={24} />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-slate-900">确认删除凭证分组</h3>
            <p className="text-sm text-slate-500">此操作不可撤销</p>
          </div>
        </div>

        <p className="text-slate-700 mb-4">
          确定要删除渠道 <span className="font-semibold">“{channelName}”</span> 下的凭证分组{' '}
          <span className="font-semibold">“{group?.name || '未命名分组'}”</span> 吗？
        </p>

        <div className="p-3 bg-amber-50 border border-amber-200 rounded-lg text-amber-800 text-sm mb-4">
          <div className="font-medium">
            {endpointCount > 0 ? `将同时删除组内 ${endpointCount} 个端点` : '该分组当前没有端点'}
          </div>
          <div className="text-xs text-amber-700 mt-2">
            未被任何调度方案引用时可以删除，历史请求记录会保留。
          </div>
          {isDefault && <p className="mt-2 text-xs">该分组是渠道默认分组，请先将其他分组设为默认后再删除。</p>}
        </div>

        <div className="mb-4"><CredentialGroupRoutingImpact impact={impact} /></div>
        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button
            variant="danger"
            icon={Trash2}
            onClick={() => onConfirm?.()}
            loading={loading}
            disabled={loading || isDefault || impact.loading || Boolean(impact.error) || impact.profiles.length > 0}
          >
            确认删除
          </Button>
        </div>
      </div>
    </div>
  );
};

export { DeleteCredentialGroupConfirmDialog };
