import { checkPoolEndpoint } from '@/services/wails/pools.js';
import DispatchWorkspace from '@/features/providers/shared/dispatch/DispatchWorkspace.jsx';
import { useCallback, useEffect, useState } from 'react';
import { Database, Plus } from 'lucide-react';
import { Button, EmptyState, ErrorMessage, LoadingSpinner, useToast } from '@/shared/ui/index.js';
import SubscriptionManagement from '@/features/providers/shared/subscriptions/SubscriptionManagement.jsx';
import ClientVersionBadge from '@/features/providers/shared/ClientVersionBadge.jsx';
import { getErrorMessage, getFailoverToggleErrorMessage } from '@/features/providers/shared/formatters.js';
import CredentialGroupDetailModal from '@/features/providers/shared/credentials/CredentialGroupDetailModal.jsx';
import { buildCodexEndpointMutationPayload, selectPersistedEndpoints } from '@/features/providers/shared/endpoints/endpointActions.js';
import { activateCodexChannel, activateCodexEndpoint, createCodexChannel, createCodexCredentialGroup, createCodexEndpoint, deleteCodexChannelByID, deleteCodexCredentialGroupByID, deleteCodexEndpointByID, getCodexChannels, getCodexGlobalConfig, getNextAvailableCodexChannel, getSettingsByScope, getCodexSuspendedCount, subscribeToEvent, setDefaultCodexCredentialGroup, setChannelPrivacyLevel, setCodexChannelFailoverEnabled, updateCodexChannelByID, updateCodexCredentialGroupByID, updateCodexEndpointByID } from '@/services/wails/index.js';
import { getCredentialGroups, hasCodexCredential, getDefaultCredentialGroup, buildCodexEndpointCopyOptions, buildCodexEndpointCopyPayload } from './model.js';
import { CodexLogo } from './components/CodexLogo.jsx';
import { ChannelFormModal } from './components/ChannelFormModal.jsx';
import { CredentialGroupFormModal } from './components/CredentialGroupFormModal.jsx';
import { EndpointFormModal } from './components/EndpointFormModal.jsx';
import { DeleteConfirmDialog } from './components/DeleteConfirmDialog.jsx';
import { ActiveChannelPauseDialog } from './components/ActiveChannelPauseDialog.jsx';
import { CodexEndpointDetailModal } from './components/CodexEndpointDetailModal.jsx';
import { ChannelCard } from './components/ChannelCard.jsx';
import ModalPortalScope from '@/shared/ui/ModalPortalScope.jsx';

// ============================================================================
// 主页面
// ============================================================================
const CodexPage = ({ clientVersion }) => {
  const toast = useToast();
  const [channels, setChannels] = useState([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState('');
  const [error, setError] = useState('');
  const [lastUpdate, setLastUpdate] = useState('');
  // Codex 挂起请求数（>0 时触发渠道卡片/端点 toggle 的 amber 警示色）
  const [suspendedCount, setSuspendedCount] = useState(0);
  const [channelFailoverEnabled, setChannelFailoverEnabled] = useState(false);
  // Codex 全局配置（来自 ~/.codex/config.toml，所有渠道共用）
  const [globalConfig, setGlobalConfig] = useState({
    model: '',
    enable_1m_context: false,
    effective_model: '',
    effective_enable_1m_context: false,
    global_timeout_seconds: 300
  });

  // 页面加载时读取全局配置，供端点详情展示。
  const loadGlobalConfig = useCallback(async () => {
    try {
      const data = await getCodexGlobalConfig();
      setGlobalConfig({
        model: data.model || '',
        enable_1m_context: Boolean(data.enable_1m_context),
        effective_model: data.effective_model || data.model || '',
        effective_enable_1m_context: Boolean(data.effective_enable_1m_context ?? data.enable_1m_context),
        global_timeout_seconds: data.global_timeout_seconds || data.globalTimeoutSeconds || 300
      });
    } catch {
      // 静默失败：全局配置加载失败仅影响展示，不影响渠道管理
    }
  }, []);

  // 渠道弹窗
  const [showChannelForm, setShowChannelForm] = useState(false);
  const [channelFormMode, setChannelFormMode] = useState('create');
  const [editingChannel, setEditingChannel] = useState(null);
  const [channelFormError, setChannelFormError] = useState('');
  const [deleteChannelTarget, setDeleteChannelTarget] = useState(null);
  const [activeChannelPauseTarget, setActiveChannelPauseTarget] = useState(null);

  // 凭证分组弹窗
  const [showCredentialGroupForm, setShowCredentialGroupForm] = useState(false);
  const [credentialGroupFormMode, setCredentialGroupFormMode] = useState('create');
  const [credentialGroupFormChannel, setCredentialGroupFormChannel] = useState(null);
  const [editingCredentialGroup, setEditingCredentialGroup] = useState(null);
  const [credentialGroupFormError, setCredentialGroupFormError] = useState('');
  const [initialSetupChannelID, setInitialSetupChannelID] = useState(null);
  const [deleteCredentialGroupTarget, setDeleteCredentialGroupTarget] = useState(null);
	const [credentialGroupDetailTarget, setCredentialGroupDetailTarget] = useState(null);

  // 端点弹窗（创建 + 编辑共用 EndpointFormModal）
  const [showEndpointForm, setShowEndpointForm] = useState(false);
  const [endpointFormMode, setEndpointFormMode] = useState('create'); // 'create' | 'edit'
  const [endpointFormChannel, setEndpointFormChannel] = useState(null);
  const [endpointFormCredentialGroup, setEndpointFormCredentialGroup] = useState(null);
  const [editingEndpoint, setEditingEndpoint] = useState(null);
  const [endpointFormError, setEndpointFormError] = useState('');
  const [deleteEndpointTarget, setDeleteEndpointTarget] = useState(null);
  const [endpointDetailTarget, setEndpointDetailTarget] = useState(null);

  const selectedEndpointDetail = endpointDetailTarget ? (() => {
    const latestChannel = channels.find((channel) => channel.id === endpointDetailTarget.channelOwner?.id) ||
      endpointDetailTarget.channelOwner;
    const latestEndpoint = latestChannel?.endpoints?.find((endpoint) => endpoint.id === endpointDetailTarget.endpoint?.id) ||
      endpointDetailTarget.endpoint;
    return { channelOwner: latestChannel, endpoint: latestEndpoint };
  })() : null;
	const selectedCredentialGroupDetail = credentialGroupDetailTarget ? (() => {
	  const latestChannel = channels.find((channel) => channel.id === credentialGroupDetailTarget.channel?.id) ||
		credentialGroupDetailTarget.channel;
	  const latestGroup = getCredentialGroups(latestChannel).find(
		(group) => Number(group.id) === Number(credentialGroupDetailTarget.group?.id)
	  ) || credentialGroupDetailTarget.group;
	  return { channel: latestChannel, group: latestGroup };
	})() : null;
  const fetchData = useCallback(async ({ silent = false, throwOnError = false } = {}) => {
    if (!silent) setLoading(true);
    setError('');
    try {
      const [list, failoverSettings] = await Promise.all([
        getCodexChannels(),
        getSettingsByScope('codex', 'failover').catch(() => [])
      ]);
      const normalizedList = Array.isArray(list) ? list : [];
      setChannels(normalizedList);
      setChannelFailoverEnabled((failoverSettings || []).find((setting) => setting.key === 'enabled')?.value === 'true');
      setLastUpdate(new Date().toLocaleTimeString());
      return normalizedList;
    } catch (err) {
      if (throwOnError) throw err;
      const message = getErrorMessage(err, '加载 Codex 渠道失败');
      if (silent) {
        toast.error(message, '刷新失败');
      } else {
        setError(message);
      }
      return [];
    } finally {
      if (!silent) setLoading(false);
    }
  }, [toast]);

  useEffect(() => {
    fetchData();
    // 初始拉取一次挂起数
    getCodexSuspendedCount().then(setSuspendedCount).catch(() => {});
    // 初始拉取一次全局配置
    loadGlobalConfig();

    const unsubscribe = subscribeToEvent('codex-provider:update', () => {
      fetchData({ silent: true });
    });
    // 订阅挂起数变化事件（后端 SuspensionManager 在挂起开始/结束时推送）
    const unsubscribeSuspension = subscribeToEvent('codex-provider:suspension', (data) => {
      const count = Number(data?.suspended_count) || 0;
      setSuspendedCount(count);
    });
    const refreshTimer = window.setInterval(() => {
      fetchData({ silent: true });
    }, 30000);
    return () => {
      window.clearInterval(refreshTimer);
      if (typeof unsubscribe === 'function') unsubscribe();
      if (typeof unsubscribeSuspension === 'function') unsubscribeSuspension();
    };
  }, [fetchData, loadGlobalConfig]);

  // ===== 渠道动作 =====
  const handleActivateChannel = async (channel) => {
    if (channel.isActive) return;
    if (channelFailoverEnabled && channel.failoverEnabled === false) {
      toast.error('请先恢复此渠道，使其重新参与渠道间转移', '无法设为当前');
      return;
    }
    setActionLoading(`activate-channel-${channel.id}`);
    try {
      await activateCodexChannel(channel.id);
      await fetchData({ silent: true });
    } catch (err) {
      toast.error(getErrorMessage(err, '切换渠道失败'), '切换失败');
    } finally {
      setActionLoading('');
    }
  };

  const handleToggleChannelFailover = async (channel, enabled) => {
    if (channel.isActive && !enabled) {
      setActionLoading(`prepare-channel-pause-${channel.id}`);
      try {
        const nextChannel = await getNextAvailableCodexChannel(channel.id);
        setActiveChannelPauseTarget({ channel, nextChannel });
      } catch (err) {
        toast.error(getFailoverToggleErrorMessage(err, '没有其他可用于接管的 Codex 渠道'), '无法暂停');
      } finally {
        setActionLoading('');
      }
      return;
    }
    setActionLoading(`toggle-channel-failover-${channel.id}`);
    try {
      await setCodexChannelFailoverEnabled(channel.id, enabled);
      await fetchData({ silent: true });
    } catch (err) {
      toast.error(getFailoverToggleErrorMessage(err, '更新渠道故障转移状态失败'), '操作失败');
    } finally {
      setActionLoading('');
    }
  };

  const handleConfirmActiveChannelPause = async () => {
    const channel = activeChannelPauseTarget?.channel;
    if (!channel) return;
    setActionLoading(`toggle-channel-failover-${channel.id}`);
    try {
      await setCodexChannelFailoverEnabled(channel.id, false);
      setActiveChannelPauseTarget(null);
      await fetchData({ silent: true });
    } catch (err) {
      toast.error(getFailoverToggleErrorMessage(err, '暂停当前接管渠道失败'), '操作失败');
    } finally {
      setActionLoading('');
    }
  };

  const openCreateChannelForm = () => {
    setChannelFormMode('create');
    setEditingChannel(null);
    setChannelFormError('');
    setShowChannelForm(true);
  };

  const openEditChannelForm = (channel) => {
    setChannelFormMode('edit');
    setEditingChannel(channel);
    setChannelFormError('');
    setShowChannelForm(true);
  };

  const closeChannelForm = () => {
    setShowChannelForm(false);
    setEditingChannel(null);
    setChannelFormError('');
  };

  const handleSaveChannel = async (payload) => {
    const isCreating = channelFormMode !== 'edit' || !editingChannel;
    const { privacyLevel, ...channelPayload } = payload;
    let channelCreated = false;
    setActionLoading(channelFormMode === 'edit' ? 'update-channel' : 'create-channel');
    setChannelFormError('');
    try {
      if (!isCreating) {
        await updateCodexChannelByID(editingChannel.id, channelPayload);
        if ('privacyLevel' in payload && (privacyLevel || '') !== (editingChannel.privacyLevel || '')) {
          await setChannelPrivacyLevel('codex', editingChannel.id, privacyLevel);
        }
      } else {
        await createCodexChannel(channelPayload);
        channelCreated = true;
      }
      const refreshedChannels = await fetchData({ silent: true, throwOnError: true });
      if (isCreating) {
        const createdChannel = refreshedChannels.find((channel) => channel?.name === payload.name);
        if (!createdChannel) {
          throw new Error('渠道已创建，但刷新后未找到该渠道，请刷新列表后继续配置');
        }
        closeChannelForm();
        openCreateCredentialGroupForm(createdChannel, true);
      } else {
        closeChannelForm();
      }
    } catch (err) {
      const message = getFailoverToggleErrorMessage(err, '保存渠道失败');
      if (channelCreated) {
        closeChannelForm();
        toast.error(`渠道已创建，但刷新列表失败，请点击刷新后继续配置凭证分组和端点。\n${message}`, '刷新失败');
      } else {
        setChannelFormError(message);
        toast.error(message, '保存失败');
      }
    } finally {
      setActionLoading('');
    }
  };

  const handleDeleteChannel = async () => {
    if (!deleteChannelTarget) return;
    setActionLoading(`delete-channel-${deleteChannelTarget.id}`);
    try {
      await deleteCodexChannelByID(deleteChannelTarget.id);
      await fetchData({ silent: true });
      setDeleteChannelTarget(null);
    } catch (err) {
      toast.error(getErrorMessage(err, '删除渠道失败'), '删除渠道失败');
    } finally {
      setActionLoading('');
    }
  };

  // ===== 凭证分组动作 =====
  const openCreateCredentialGroupForm = (channel, isInitialSetup = false) => {
    setInitialSetupChannelID(isInitialSetup ? channel?.id ?? null : null);
    setCredentialGroupFormMode('create');
    setCredentialGroupFormChannel(channel);
    setEditingCredentialGroup(null);
    setCredentialGroupFormError('');
    setShowCredentialGroupForm(true);
  };

  const openEditCredentialGroupForm = (channel, group) => {
    setInitialSetupChannelID(null);
    setCredentialGroupFormMode('edit');
    setCredentialGroupFormChannel(channel);
    setEditingCredentialGroup(group);
    setCredentialGroupFormError('');
    setShowCredentialGroupForm(true);
  };

  const closeCredentialGroupForm = () => {
    setShowCredentialGroupForm(false);
    setCredentialGroupFormChannel(null);
    setEditingCredentialGroup(null);
    setCredentialGroupFormError('');
    setInitialSetupChannelID(null);
  };

  const handleSaveCredentialGroup = async (payload) => {
    if (!credentialGroupFormChannel) return;
    const { copyEndpoints = [], ...groupPayload } = payload || {};
    delete groupPayload.copyEndpointsFromGroupId;
    const targetChannel = credentialGroupFormChannel;
    const shouldContinueInitialSetup = initialSetupChannelID === targetChannel.id;
    let savedGroup = null;
    const loadingKey = credentialGroupFormMode === 'edit'
      ? `save-credential-group-${editingCredentialGroup?.id}`
      : 'create-credential-group';
    setActionLoading(loadingKey);
    setCredentialGroupFormError('');
    try {
      if (credentialGroupFormMode === 'edit' && editingCredentialGroup) {
        savedGroup = await updateCodexCredentialGroupByID(editingCredentialGroup.id, groupPayload);
      } else {
        savedGroup = await createCodexCredentialGroup({
          ...groupPayload,
          channelId: targetChannel.id
        });
        const targetGroupID = Number(savedGroup?.id || 0);
        if (targetGroupID > 0) {
          for (const endpoint of copyEndpoints) {
            await createCodexEndpoint(buildCodexEndpointCopyPayload(
              endpoint,
              targetChannel.id,
              targetGroupID
            ));
          }
        }
      }
    } catch (err) {
      setCredentialGroupFormError(getErrorMessage(err, '保存凭证分组失败'));
      return;
    } finally {
      setActionLoading('');
    }

    closeCredentialGroupForm();
    if (shouldContinueInitialSetup && savedGroup?.id && copyEndpoints.length === 0) {
      openCreateEndpointForm(targetChannel, savedGroup);
    }

    try {
      await fetchData({ silent: true, throwOnError: true });
    } catch (err) {
      toast.error(`凭证分组已保存，但列表刷新失败，请点击刷新查看最新数据。\n${getErrorMessage(err, '未知错误')}`, '刷新失败');
    }
  };

  const handleDeleteCredentialGroup = async () => {
    const target = deleteCredentialGroupTarget;
    if (!target?.group) return;
    setActionLoading(`delete-credential-group-${target.group.id}`);
    try {
      await deleteCodexCredentialGroupByID(target.group.id);
      await fetchData({ silent: true });
      setDeleteCredentialGroupTarget(null);
    } catch (err) {
      toast.error(getErrorMessage(err, '删除凭证分组失败'), '删除凭证分组失败');
    } finally {
      setActionLoading('');
    }
  };

  const handleSetDefaultCredentialGroup = async (_channel, group) => {
    if (!group?.id) return;
    setActionLoading(`default-credential-group-${group.id}`);
    try {
      await setDefaultCodexCredentialGroup(group.id);
      await fetchData({ silent: true });
    } catch (err) {
      toast.error(getErrorMessage(err, '设置默认分组失败'), '设置默认分组失败');
    } finally {
      setActionLoading('');
    }
  };

  // ===== 端点动作 =====
  const openCreateEndpointForm = (channel, credentialGroup = null) => {
    const targetCredentialGroup = credentialGroup || getDefaultCredentialGroup(channel);
    if (!channel || !targetCredentialGroup || !hasCodexCredential(targetCredentialGroup, channel)) return;
    setEndpointFormMode('create');
    setEndpointFormChannel(channel);
    setEndpointFormCredentialGroup(targetCredentialGroup);
    setEditingEndpoint(null);
    setEndpointFormError('');
    setShowEndpointForm(true);
  };

  const openEditEndpointForm = (endpoint) => {
    const channel = channels.find((c) => c.id === endpoint.channelId) || null;
    const endpointGroupID = endpoint.credentialGroupId || endpoint.credential_group_id || 0;
    const endpointGroup = (channel?.credentialGroups || []).find((group) => Number(group.id) === Number(endpointGroupID)) ||
      getDefaultCredentialGroup(channel);
    setEndpointFormMode('edit');
    setEndpointFormChannel(channel);
    setEndpointFormCredentialGroup(endpointGroup);
    setEditingEndpoint(endpoint);
    setEndpointFormError('');
    setShowEndpointForm(true);
  };

  const closeEndpointForm = () => {
    setShowEndpointForm(false);
    setEndpointFormChannel(null);
    setEndpointFormCredentialGroup(null);
    setEditingEndpoint(null);
    setEndpointFormError('');
  };

  const handleSaveEndpoint = async ({ name, baseURL, credentialGroupId, failoverEnabled, skipHealthCheck, priority, timeoutSeconds }) => {
    if (endpointFormMode === 'edit') {
      if (!editingEndpoint) return;
      setActionLoading(`save-endpoint-${editingEndpoint.id}`);
      setEndpointFormError('');
      try {
        await updateCodexEndpointByID(editingEndpoint.id, {
          name,
          baseURL,
          credentialGroupId,
          failoverEnabled,
          skipHealthCheck,
          priority,
          timeoutSeconds
        });
      } catch (err) {
        setEndpointFormError(getErrorMessage(err, '更新端点失败'));
        return;
      } finally {
        setActionLoading('');
      }
      closeEndpointForm();
      try {
        await fetchData({ silent: true, throwOnError: true });
      } catch (err) {
        toast.error(`端点已保存，但列表刷新失败，请点击刷新查看最新数据。\n${getErrorMessage(err, '未知错误')}`, '刷新失败');
      }
      return;
    }

    if (!endpointFormChannel) return;
    setActionLoading('create-endpoint');
    setEndpointFormError('');
    try {
      await createCodexEndpoint({
        channelId: endpointFormChannel.id,
        credentialGroupId,
        name,
        baseURL,
        failoverEnabled,
        skipHealthCheck,
        priority,
        timeoutSeconds
      });
    } catch (err) {
      setEndpointFormError(getErrorMessage(err, '保存端点失败'));
      return;
    } finally {
      setActionLoading('');
    }
    closeEndpointForm();
    try {
      await fetchData({ silent: true, throwOnError: true });
    } catch (err) {
      toast.error(`端点已保存，但列表刷新失败，请点击刷新查看最新数据。\n${getErrorMessage(err, '未知错误')}`, '刷新失败');
    }
  };

  const handleActivateEndpoint = async (endpoint) => {
    if (endpoint.isActive) return;
    setActionLoading(`activate-endpoint-${endpoint.id}`);
    try {
      await activateCodexEndpoint(endpoint.id);
      await fetchData({ silent: true });
    } catch (err) {
      toast.error(getErrorMessage(err, '激活端点失败'), '激活端点失败');
    } finally {
      setActionLoading('');
    }
  };

  const handleToggleEndpointFailover = async (endpoint, enabled) => {
    try {
      await updateCodexEndpointByID(endpoint.id, {
        name: endpoint.name,
        baseURL: endpoint.baseURL || endpoint.base_url,
        credentialGroupId: endpoint.credentialGroupId || endpoint.credential_group_id || 0,
        failoverEnabled: enabled,
        priority: endpoint.priority,
        timeoutSeconds: endpoint.timeoutSeconds ?? endpoint.timeout_seconds ?? 0
      });
      await fetchData({ silent: true });
    } catch (err) {
      toast.error(getFailoverToggleErrorMessage(err), '操作失败');
    }
  };

  const handleSetGroupEndpointFailover = async (targetEndpoints = [], enabled, loadingKey = 'set-group-failover') => {
    const targets = selectPersistedEndpoints(targetEndpoints);
    if (targets.length === 0) return;
    setError('');
    setActionLoading(loadingKey);
    try {
      for (const endpoint of targets) {
        await updateCodexEndpointByID(endpoint.id, buildCodexEndpointMutationPayload(endpoint, {
          failoverEnabled: enabled
        }));
      }
    } catch (err) {
      toast.error(getFailoverToggleErrorMessage(err, '批量操作失败'), '批量操作失败');
    } finally {
      // Some endpoints may have been saved before a later update failed.
      await fetchData({ silent: true });
      setActionLoading('');
    }
  };

  const handleSetGroupEndpointSkipHealthCheck = async (targetEndpoints = [], skipHealthCheck, loadingKey = 'set-group-health') => {
    const targets = selectPersistedEndpoints(targetEndpoints);
    if (targets.length === 0) return;
    setActionLoading(loadingKey);
    try {
      for (const endpoint of targets) {
        await updateCodexEndpointByID(endpoint.id, buildCodexEndpointMutationPayload(endpoint, {
          skipHealthCheck
        }));
      }
    } catch (err) {
      toast.error(getErrorMessage(err, '批量切换跳过检测失败'), '批量操作失败');
    } finally {
      await fetchData({ silent: true });
      setActionLoading('');
    }
  };

  const handleToggleEndpointSkipHealthCheck = async (endpoint, skipHealthCheck) => {
    setActionLoading(`skip-health-endpoint-${endpoint.id}`);
    try {
      await updateCodexEndpointByID(endpoint.id, {
        name: endpoint.name,
        baseURL: endpoint.baseURL || endpoint.base_url,
        credentialGroupId: endpoint.credentialGroupId || endpoint.credential_group_id || 0,
        skipHealthCheck,
        priority: endpoint.priority,
        timeoutSeconds: endpoint.timeoutSeconds ?? endpoint.timeout_seconds ?? 0
      });
      await fetchData({ silent: true });
    } catch (err) {
      toast.error(getErrorMessage(err, '切换跳过检测失败'), '操作失败');
    } finally {
      setActionLoading('');
    }
  };

  const handleDeleteEndpoint = async () => {
    if (!deleteEndpointTarget) return;
    const epId = deleteEndpointTarget.endpoint.id;
    setActionLoading(`delete-endpoint-${epId}`);
    try {
      await deleteCodexEndpointByID(epId);
      await fetchData({ silent: true });
      setDeleteEndpointTarget(null);
    } catch (err) {
      toast.error(getErrorMessage(err, '删除端点失败'), '删除端点失败');
    } finally {
      setActionLoading('');
    }
  };

  const credentialGroupEndpointCopyOptions =
    credentialGroupFormMode === 'create' && credentialGroupFormChannel
      ? buildCodexEndpointCopyOptions(credentialGroupFormChannel)
      : [];

  const deletingCredentialGroupChannel = channels.find(
    (channel) => channel.id === deleteCredentialGroupTarget?.channel?.id
  ) || deleteCredentialGroupTarget?.channel;
  const deletingCredentialGroupEndpointCount = (deletingCredentialGroupChannel?.endpoints || []).filter(
    (endpoint) => Number(endpoint.credentialGroupId ?? endpoint.credential_group_id ?? 0) ===
      Number(deleteCredentialGroupTarget?.group?.id)
  ).length;

  return (
    <ModalPortalScope><div className="flex min-h-0 flex-1 flex-col animate-fade-in">
      {/* 页面标题 */}
      <div className="mb-3 flex min-h-9 shrink-0 flex-wrap items-center justify-between gap-3">
        <div className="flex min-h-9 min-w-0 items-center gap-2.5">
          <div className="shrink-0 rounded-lg bg-slate-900 p-1.5 text-white shadow-sm">
            <CodexLogo className="h-5 w-5" />
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
              <h1 className="text-xl font-bold text-slate-900">Codex 管理</h1>
              <ClientVersionBadge provider="codex" version={clientVersion} />
              {lastUpdate && (
                <span className="hidden text-[11px] text-slate-400 min-[75rem]:inline">更新于 {lastUpdate}</span>
              )}
          </div>
        </div>
      </div>

      <DispatchWorkspace provider="codex" refreshToken={lastUpdate}
        resourcesBusy={loading || Boolean(actionLoading)}
        onRefreshResources={() => fetchData({ silent: true, throwOnError: true })}
        resourceActions={<>
          <SubscriptionManagement provider="codex" channels={channels} onUpdated={() => fetchData({ silent: true, throwOnError: true })} className="dispatch-resource-action" disabled={loading || Boolean(actionLoading)} />
          {channels.length > 0 && <Button type="button" size="sm" icon={Plus} className="dispatch-resource-action" disabled={loading || Boolean(actionLoading)} onClick={openCreateChannelForm}>添加渠道</Button>}
        </>} endpoints={channels.flatMap((channel) => channel.endpoints.map((endpoint) => ({
        channelId: channel.id, groupId: endpoint.credentialGroupId || getDefaultCredentialGroup(channel)?.id || 0, endpointId: String(endpoint.id),
        name: endpoint.name, address: endpoint.baseURL,
      })))} onError={(message) => toast.error(message)} onSuccess={(message) => toast.success(message)}>
      {error && (
        <div className="mb-4">
          <ErrorMessage message={error} onClose={() => setError('')} />
        </div>
      )}

      {loading ? (
        <div className="flex items-center justify-center py-16">
          <LoadingSpinner />
        </div>
      ) : channels.length === 0 ? (
        <EmptyState
          icon={Database}
          title="暂无 Codex 渠道"
          description="创建渠道和凭证分组后，在调度方案中选择参与成员。"
          action={
            <Button variant="primary" icon={Plus} onClick={openCreateChannelForm}>
              添加渠道
            </Button>
          }
        />
      ) : (
        <div aria-label="Codex 渠道列表" className="channel-resource-grid">
          {channels.map((channel) => (
            <ChannelCard
              key={channel.id}
              channel={channel}
              actionLoading={actionLoading}
              suspendedCount={suspendedCount}
              globalConfig={globalConfig}
              channelFailoverEnabled={channelFailoverEnabled}
              onActivateChannel={handleActivateChannel}
              onToggleChannelFailover={handleToggleChannelFailover}
              onEditChannel={openEditChannelForm}
              onDeleteChannel={(c) => setDeleteChannelTarget(c)}
              onAddCredentialGroup={openCreateCredentialGroupForm}
			  onOpenCredentialGroup={(channelOwner, group) => setCredentialGroupDetailTarget({ channel: channelOwner, group })}
              onEditCredentialGroup={openEditCredentialGroupForm}
              onSetDefaultCredentialGroup={handleSetDefaultCredentialGroup}
              onDeleteCredentialGroup={(channelOwner, group) =>
                setDeleteCredentialGroupTarget({ channel: channelOwner, group })
              }
              onAddEndpoint={openCreateEndpointForm}
              onActivateEndpoint={handleActivateEndpoint}
              onToggleEndpointFailover={handleToggleEndpointFailover}
              onSetGroupEndpointFailover={handleSetGroupEndpointFailover}
              onSetGroupEndpointSkipHealthCheck={handleSetGroupEndpointSkipHealthCheck}
              onToggleEndpointSkipHealthCheck={handleToggleEndpointSkipHealthCheck}
              onEditEndpoint={openEditEndpointForm}
              onOpenEndpoint={(endpoint, channelOwner) =>
                setEndpointDetailTarget({ endpoint, channelOwner })
              }
              onDeleteEndpoint={(endpoint, channelOwner) =>
                setDeleteEndpointTarget({ endpoint, channelOwner })
              }
            />
          ))}
        </div>
      )}

      </DispatchWorkspace>

      {showChannelForm && (
        <ChannelFormModal
          mode={channelFormMode}
          channel={editingChannel}
          channelFailoverEnabled={channelFailoverEnabled}
          loading={actionLoading === 'create-channel' || actionLoading === 'update-channel'}
          serverError={channelFormError}
          onCancel={closeChannelForm}
          onSave={handleSaveChannel}
        />
      )}

      {showCredentialGroupForm && (
        <CredentialGroupFormModal
          channels={channels}
          mode={credentialGroupFormMode}
          channel={credentialGroupFormChannel}
          group={editingCredentialGroup}
          continueToEndpoint={initialSetupChannelID === credentialGroupFormChannel?.id}
          loading={
            credentialGroupFormMode === 'edit'
              ? actionLoading === `save-credential-group-${editingCredentialGroup?.id}`
              : actionLoading === 'create-credential-group'
          }
          serverError={credentialGroupFormError}
          endpointCopyOptions={credentialGroupEndpointCopyOptions}
          onCancel={closeCredentialGroupForm}
          onSave={handleSaveCredentialGroup}
        />
      )}

	  {selectedCredentialGroupDetail?.group && (
		<CredentialGroupDetailModal
		  provider="codex"
		  channel={selectedCredentialGroupDetail.channel}
		  group={selectedCredentialGroupDetail.group}
		  onClose={() => setCredentialGroupDetailTarget(null)}
		  onEdit={(group) => {
			setCredentialGroupDetailTarget(null);
			openEditCredentialGroupForm(selectedCredentialGroupDetail.channel, group);
		  }}
		/>
	  )}

      {showEndpointForm && (
        <EndpointFormModal
          mode={endpointFormMode}
          channel={endpointFormChannel}
          endpoint={editingEndpoint}
          credentialGroup={endpointFormCredentialGroup}
          loading={
            endpointFormMode === 'edit'
              ? actionLoading === `save-endpoint-${editingEndpoint?.id}`
              : actionLoading === 'create-endpoint'
          }
          serverError={endpointFormError}
          onCancel={closeEndpointForm}
          onSave={handleSaveEndpoint}
        />
      )}

      {selectedEndpointDetail?.endpoint && (
        <CodexEndpointDetailModal
          onRestore={async (ep) => { await handleToggleEndpointFailover(ep, true); setEndpointDetailTarget(null); }}
          onHealth={async (ep) => { try { await checkPoolEndpoint('codex', ep.id); setEndpointDetailTarget(null); await fetchData({ silent: true }); } catch (error) { toast.error(getErrorMessage(error)); } }}
          endpoint={selectedEndpointDetail.endpoint}
          channel={selectedEndpointDetail.channelOwner}
          globalConfig={globalConfig}
          onClose={() => setEndpointDetailTarget(null)}
          onEdit={(endpoint) => {
            setEndpointDetailTarget(null);
            openEditEndpointForm(endpoint);
          }}
        />
      )}

      {deleteChannelTarget && (
        <DeleteConfirmDialog
          title={`删除渠道「${deleteChannelTarget.name}」`}
          description="该操作会级联删除其下所有端点，且无法撤销。"
          hint={
            deleteChannelTarget.isActive
              ? '该渠道是当前接管渠道，删除后系统会尝试激活下一个渠道并同步 config.toml。'
              : '删除后端点也会一并消失。若需保留端点请先迁移到其他渠道。'
          }
          loading={actionLoading === `delete-channel-${deleteChannelTarget.id}`}
          onCancel={() => setDeleteChannelTarget(null)}
          onConfirm={handleDeleteChannel}
        />
      )}

      {activeChannelPauseTarget && (
        <ActiveChannelPauseDialog
          channelName={activeChannelPauseTarget.channel?.name}
          nextChannelName={activeChannelPauseTarget.nextChannel?.name}
          loading={actionLoading === `toggle-channel-failover-${activeChannelPauseTarget.channel?.id}`}
          onCancel={() => setActiveChannelPauseTarget(null)}
          onConfirm={handleConfirmActiveChannelPause}
        />
      )}

      {deleteCredentialGroupTarget && (
        <DeleteConfirmDialog
          title="删除凭证分组"
          description={`即将从渠道「${deleteCredentialGroupTarget.channel?.name}」中删除凭证分组「${deleteCredentialGroupTarget.group?.name}」。`}
          hint={deletingCredentialGroupEndpointCount > 0
            ? `将同时删除组内 ${deletingCredentialGroupEndpointCount} 个端点。仅当这些端点全部不参与转移且未被激活时才能删除；历史请求记录会保留。`
            : '该分组当前没有端点，确认后将删除此分组。默认分组不能删除。'}
          loading={actionLoading === `delete-credential-group-${deleteCredentialGroupTarget.group.id}`}
          onCancel={() => setDeleteCredentialGroupTarget(null)}
          onConfirm={handleDeleteCredentialGroup}
        />
      )}

      {deleteEndpointTarget && (
        <DeleteConfirmDialog
          title="删除端点"
          description={`即将从渠道「${deleteEndpointTarget.channelOwner?.name}」中删除端点「${deleteEndpointTarget.endpoint?.name || deleteEndpointTarget.endpoint?.baseURL || '未命名端点'}」。`}
          hint={
            deleteEndpointTarget.endpoint.isActive
              ? '这是该渠道的当前活跃端点；删除后系统会尝试切换到同渠道下一个可用端点。'
              : '该端点未激活，删除后只会从该渠道的反代候选中移除。'
          }
          loading={actionLoading === `delete-endpoint-${deleteEndpointTarget.endpoint.id}`}
          onCancel={() => setDeleteEndpointTarget(null)}
          onConfirm={handleDeleteEndpoint}
        />
      )}

    </div></ModalPortalScope>
  );
};

export default CodexPage;
