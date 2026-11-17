import { useRef, useState } from 'react';
import ChannelSetupDialog from '../../shared/ChannelSetupDialog.jsx';
import ChannelSetupReview from '../../shared/ChannelSetupReview.jsx';
import useChannelSetupNavigation from '../../shared/useChannelSetupNavigation.js';
import { CreateChannelModal } from './CreateChannelModal.jsx';
import { ClaudeCredentialGroupFormModal } from './ClaudeCredentialGroupFormModal.jsx';
import EndpointForm from './EndpointForm.jsx';

const initialGroup = { id: -1, name: '默认分组', isDefault: true };

export default function ChannelSetupWizard({ channels, loading, onCancel, onSave }) {
  const [channel, setChannel] = useState(null);
  const [group, setGroup] = useState(null);
  const [endpoint, setEndpoint] = useState(null);
  const [error, setError] = useState('');
  const saving = useRef(false);
  const { id, step, furthestStep, formId, advance, navigate, next, cancelPendingNavigation } = useChannelSetupNavigation({ loading, saving, setError });
  const save = async () => {
    if (loading || saving.current) return;
    saving.current = true;
    setError('');
    try { await onSave({ channel, group, endpoint }); }
    catch (cause) { setError(cause?.message || '创建失败，请检查配置后重试。'); }
    finally { saving.current = false; }
  };
  return <ChannelSetupDialog provider="claude" step={step} furthestStep={furthestStep} onStepChange={navigate} onNext={next} onDraftChange={cancelPendingNavigation} onClose={onCancel} onComplete={save}
    loading={loading} error={error} formId={formId} panels={[
      { steps: [0], content: <CreateChannelModal key="channel" open embedded formId={`${id}-0`} loading={loading} onSubmit={payload => {
        if (channels.some(item => item.name === payload.name)) { setError('渠道名称已存在'); return; }
        setChannel(payload); advance();
      }} /> },
      { steps: [1, 2], content: channel && <ClaudeCredentialGroupFormModal key="group" open embedded formId={`${id}-1`} channelName={channel.name} group={initialGroup} continueToEndpoint loading={loading}
        setupSection={step === 1 ? 'basic' : 'billing'} onCredentialsReady={advance}
        onSubmit={payload => { setGroup({ ...payload, id: -1, isDefault: true, modelPolicy: 'inherit' }); advance(); }} /> },
      { steps: [3], content: group && <EndpointForm key="endpoint" embedded formId={`${id}-3`} initialSetup defaultChannel={channel.name} defaultCredentialGroupId={group.id}
        channels={[channel.name]} credentialGroupsByChannel={{ [channel.name]: [group] }} lockChannel loading={loading}
        onSave={payload => { setEndpoint(payload); advance(); }} /> },
      { steps: [4], content: endpoint && <ChannelSetupReview key="review" channel={channel} group={group} endpoint={endpoint} onEdit={navigate} loading={loading}>
        <p className="channel-setup-note">Claude 端点沿用现有接口路径规则。健康检查按全局设置执行，调用结果会记录在请求追踪中。</p>
      </ChannelSetupReview> },
    ]} />;
}
