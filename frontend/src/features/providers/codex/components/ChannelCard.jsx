import ChannelResourceRow from '@/features/providers/shared/pools/ChannelResourceRow.jsx';
import { getCredentialGroups, hasCodexCredential } from '../model.js';

const ChannelCard = ({ channel, actionLoading, onEditChannel, onDeleteChannel, onAddCredentialGroup, onOpenCredentialGroup, onEditCredentialGroup, onDeleteCredentialGroup, onAddEndpoint, onOpenEndpoint, onEditEndpoint, onDeleteEndpoint }) => <ChannelResourceRow
  provider="codex"
  name={channel.name} website={channel.website} busy={Boolean(actionLoading)} groups={getCredentialGroups(channel).map((group) => ({ ...group, canAddEndpoint: hasCodexCredential(group, channel) }))}
  endpoints={(channel.endpoints || []).map((endpoint) => ({ id: endpoint.id, name: endpoint.name, groupId: Number(endpoint.credentialGroupId || endpoint.credential_group_id || 0), url: endpoint.baseURL || endpoint.base_url, priority: endpoint.priority, original: endpoint }))}
  onEdit={() => onEditChannel?.(channel)} onDelete={() => onDeleteChannel?.(channel)} onAddGroup={() => onAddCredentialGroup?.(channel)}
  onOpenGroup={(group) => onOpenCredentialGroup?.(channel, group)} onEditGroup={(group) => onEditCredentialGroup?.(channel, group)} onDeleteGroup={(group) => onDeleteCredentialGroup?.(channel, group)} onAddEndpoint={(group) => onAddEndpoint?.(channel, group)}
  onOpenEndpoint={(endpoint) => onOpenEndpoint?.(endpoint, channel)} onEditEndpoint={(endpoint) => onEditEndpoint?.(endpoint, channel)} onDeleteEndpoint={(endpoint) => onDeleteEndpoint?.(endpoint, channel)}
/>;
export { ChannelCard };
