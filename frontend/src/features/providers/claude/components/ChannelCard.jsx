import { hasClaudeCredential } from '../model.js';
import ChannelResourceRow from '@/features/providers/shared/pools/ChannelResourceRow.jsx';

const ChannelCard = ({ channelName, channelWebsite, endpoints = [], credentialGroups = [], loading, onEditChannel, onDeleteChannel, onAddCredentialGroup, onOpenCredentialGroup, onEditCredentialGroup, onDeleteCredentialGroup, onAddEndpoint, onOpenEndpoint, onEditEndpoint, onDeleteEndpoint }) => <ChannelResourceRow
  provider="claude"
  name={channelName} website={channelWebsite} busy={loading} groups={credentialGroups.map((group) => ({ ...group, canAddEndpoint: hasClaudeCredential(group) }))}
  endpoints={endpoints.map((endpoint) => ({ id: endpoint.id || endpoint.name, name: endpoint.name, groupId: Number(endpoint.credentialGroupId || endpoint.credential_group_id || 0), url: endpoint.url, priority: endpoint.priority, excluded: endpoint.failoverEnabled === false, original: endpoint }))}
  onEdit={() => onEditChannel?.(channelName)} onDelete={() => onDeleteChannel?.(channelName)} onAddGroup={() => onAddCredentialGroup?.(channelName)}
  onOpenGroup={(group) => onOpenCredentialGroup?.(channelName, group)} onEditGroup={(group) => onEditCredentialGroup?.(channelName, group)} onDeleteGroup={(group) => onDeleteCredentialGroup?.(channelName, group)} onAddEndpoint={(group) => onAddEndpoint?.(channelName, group)}
  onOpenEndpoint={onOpenEndpoint} onEditEndpoint={onEditEndpoint} onDeleteEndpoint={onDeleteEndpoint}
/>;
export { ChannelCard };
