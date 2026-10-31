import { DeleteConfirmDialog } from './DeleteConfirmDialog.jsx';
import CredentialGroupRoutingImpact from '../../shared/credentials/CredentialGroupRoutingImpact.jsx';
import useCredentialGroupRoutingImpact from '../../shared/credentials/useCredentialGroupRoutingImpact.js';

export function DeleteCredentialGroupConfirmDialog({ channel, group, endpointCount, loading, onCancel, onConfirm }) {
  const impact = useCredentialGroupRoutingImpact('codex', group?.id);
  return <DeleteConfirmDialog
    title="删除凭证分组"
    description={`即将从渠道「${channel?.name}」中删除凭证分组「${group?.name}」。`}
    hint={<>
      <p>{endpointCount > 0
        ? `将同时删除组内 ${endpointCount} 个端点。仅当这些端点全部不参与转移且未被激活时才能删除；历史请求记录会保留。`
        : '该分组当前没有端点，确认后将删除此分组。默认分组不能删除。'}</p>
      <CredentialGroupRoutingImpact impact={impact} />
    </>}
    loading={loading}
    confirmDisabled={impact.loading || Boolean(impact.error)}
    onCancel={onCancel}
    onConfirm={onConfirm}
  />;
}
