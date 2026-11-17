import { useRef, useState } from 'react';
import ChannelSetupDialog from '../../shared/ChannelSetupDialog.jsx';
import ChannelSetupReview from '../../shared/ChannelSetupReview.jsx';
import useChannelSetupNavigation from '../../shared/useChannelSetupNavigation.js';
import { isSubscriptionGroup } from '../../shared/subscriptions/subscriptionSettings.js';
import { ChannelFormModal } from './ChannelFormModal.jsx';
import { CredentialGroupFormModal } from './CredentialGroupFormModal.jsx';
import { EndpointFormModal } from './EndpointFormModal.jsx';
import EndpointPathPolicy from './EndpointPathPolicy.jsx';

const initialGroup = { id: -1, name: '默认分组', isDefault: true };

export default function ChannelSetupWizard({ channels, loading, serverError, onCancel, onSave }) {
  const [channel, setChannel] = useState(null);
  const [group, setGroup] = useState(null);
  const [endpoint, setEndpoint] = useState(null);
  const [error, setError] = useState('');
  const saving = useRef(false);
  const { id, step, furthestStep, formId, advance, navigate, next, cancelPendingNavigation } = useChannelSetupNavigation({ loading, saving, setError });
  const draftChannel = channel ? { ...channel, id: -1, credentialGroups: [group || initialGroup], endpoints: [] } : null;
  const save = async () => {
    if (loading || saving.current) return;
    saving.current = true;
    setError('');
    try { await onSave({ channel: draftChannel, group, endpoint }); }
    catch (cause) { setError(cause?.message || '创建失败，请检查配置后重试。'); }
    finally { saving.current = false; }
  };
  return <ChannelSetupDialog provider="codex" step={step} furthestStep={furthestStep} onStepChange={navigate} onNext={next} onDraftChange={cancelPendingNavigation} onClose={onCancel} onComplete={save}
    loading={loading} error={error || (step === 4 ? serverError : '')} formId={formId} panels={[
      { steps: [0], content: <ChannelFormModal key="channel" mode="create" embedded formId={`${id}-0`} loading={loading} onSave={payload => {
        if (channels.some(item => item.name === payload.name)) { setError('渠道名称已存在'); return; }
        setChannel(payload); advance();
      }} /> },
      { steps: [1, 2], content: channel && <CredentialGroupFormModal key="group" mode="create" embedded formId={`${id}-1`} channel={draftChannel} group={initialGroup} continueToEndpoint loading={loading}
        setupSection={step === 1 ? 'basic' : 'billing'} onCredentialsReady={advance}
        onSave={payload => { setGroup({ ...payload, id: -1, isDefault: true, modelPolicy: 'inherit' }); advance(); }} /> },
      { steps: [3], content: group && <EndpointFormModal key="endpoint" mode="create" embedded formId={`${id}-3`} initialSetup channel={draftChannel} credentialGroup={group}
        pathStrategyOverride={endpoint?.pathStrategy} loading={loading} onSave={payload => { setEndpoint(payload); advance(); }} /> },
      { steps: [4], content: endpoint && <ChannelSetupReview key="review" channel={draftChannel} group={group} endpoint={endpoint} onEdit={navigate} loading={loading}>
        {isSubscriptionGroup(group)
          ? <p className="channel-setup-note">官方订阅使用固定服务地址，凭证与可用状态由实际请求验证。</p>
          : <EndpointPathPolicy compact channel={draftChannel} credentialGroup={group} baseURL={endpoint.baseURL} strategy={endpoint.pathStrategy}
            onChange={pathStrategy => setEndpoint(current => ({ ...current, pathStrategy }))} disabled={loading} />}
      </ChannelSetupReview> },
    ]} />;
}
