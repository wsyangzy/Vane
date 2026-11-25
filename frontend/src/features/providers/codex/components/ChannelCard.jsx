import ChannelResourceRow from '@/features/providers/shared/pools/ChannelResourceRow.jsx';
import { Cpu, Shuffle } from 'lucide-react';
import { DetailMetricCard } from '@/features/providers/shared/endpoints/detailParts.jsx';
import { getCredentialGroups, hasCodexCredential } from '../model.js';

const ChannelCard = ({ channel, actionLoading, channelFailoverEnabled, onEditChannel, onDeleteChannel, onAddCredentialGroup, onOpenCredentialGroup, onEditCredentialGroup, onDeleteCredentialGroup, onSetDefaultCredentialGroup, onAddEndpoint, onOpenEndpoint, onEditEndpoint, onDeleteEndpoint, onHealthEndpoint, onToggleEndpointSkipHealthCheck }) => <ChannelResourceRow
  provider="codex"
  details={<>
    <DetailMetricCard label="模型" icon={Cpu} density="compact">
      <span className="text-sm font-medium text-slate-800 [overflow-wrap:anywhere]">{channel.model?.trim() || '跟随 Codex 全局默认模型'}</span>
    </DetailMetricCard>
    {channelFailoverEnabled && <DetailMetricCard label="参与渠道间故障转移" icon={Shuffle} density="compact">
      <span className="text-sm font-medium text-slate-800">{channel.failoverEnabled !== false ? '已开启' : '已关闭'}</span>
    </DetailMetricCard>}
  </>}
  name={channel.name} website={channel.website} busy={Boolean(actionLoading)} groups={getCredentialGroups(channel).map((group) => ({ ...group, canAddEndpoint: hasCodexCredential(group, channel) }))}
  endpoints={(channel.endpoints || []).map((endpoint) => ({ id: endpoint.id, name: endpoint.name, groupId: Number(endpoint.credentialGroupId || endpoint.credential_group_id || 0), url: endpoint.baseURL || endpoint.base_url, priority: endpoint.priority, latencyMs: endpoint.responseTimeMs ?? endpoint.response_time_ms, skipHealthCheck: endpoint.skipHealthCheck ?? endpoint.skip_health_check, inScope: endpoint.healthInScope !== false && (endpoint.healthState || endpoint.health_state) !== 'disabled', original: endpoint }))}
  onEdit={() => onEditChannel?.(channel)} onDelete={() => onDeleteChannel?.(channel)} onAddGroup={() => onAddCredentialGroup?.(channel)}
  onOpenGroup={(group) => onOpenCredentialGroup?.(channel, group)} onEditGroup={(group) => onEditCredentialGroup?.(channel, group)} onDeleteGroup={(group) => onDeleteCredentialGroup?.(channel, group)} onAddEndpoint={(group) => onAddEndpoint?.(channel, group)}
  onSetDefaultGroup={onSetDefaultCredentialGroup && ((group) => onSetDefaultCredentialGroup(channel, group))}
  onOpenEndpoint={(endpoint) => onOpenEndpoint?.(endpoint, channel)} onEditEndpoint={(endpoint) => onEditEndpoint?.(endpoint, channel)} onDeleteEndpoint={(endpoint) => onDeleteEndpoint?.(endpoint, channel)}
  onHealthEndpoint={onHealthEndpoint} onToggleEndpointSkipHealthCheck={onToggleEndpointSkipHealthCheck}
/>;
export { ChannelCard };
