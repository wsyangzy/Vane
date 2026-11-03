import { DeleteConfirmDialog } from './DeleteConfirmDialog.jsx';
import CredentialGroupRoutingImpact from '../../shared/credentials/CredentialGroupRoutingImpact.jsx';
import useCredentialGroupRoutingImpact from '../../shared/credentials/useCredentialGroupRoutingImpact.js';

export function DeleteCredentialGroupConfirmDialog({ channel, group, endpointCount, loading, onCancel, onConfirm }) {
  const impact = useCredentialGroupRoutingImpact('codex', group?.id);
  const isDefault = Boolean(group?.isDefault ?? group?.is_default);
  return <DeleteConfirmDialog
    title="删除凭证分组"
    description={`即将从渠道「${channel?.name}」中删除凭证分组「${group?.name}」。`}
    hint={<>
      <p>{endpointCount > 0
        ? `将同时删除组内 ${endpointCount} 个端点。未被任何调度方案引用时可以删除，历史请求记录会保留。`
        : '该分组当前没有端点，未被任何调度方案引用时可以删除。'}</p>
      {isDefault && <p className="mt-2 text-amber-700">该分组是渠道默认分组，请先将其他分组设为默认后再删除。</p>}
      <CredentialGroupRoutingImpact impact={impact} />
    </>}
    loading={loading}
    confirmDisabled={isDefault || impact.loading || Boolean(impact.error) || impact.profiles.length > 0}
    onCancel={onCancel}
    onConfirm={onConfirm}
  />;
}
