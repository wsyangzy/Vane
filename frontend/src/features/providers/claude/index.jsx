import { checkPoolEndpoint } from '@/services/wails/pools.js';
import DispatchWorkspace from '@/features/providers/shared/dispatch/DispatchWorkspace.jsx';
import ChannelResourceGrid from '@/features/providers/shared/pools/ChannelResourceGrid.jsx';
import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { Plus, Database, AlertTriangle } from 'lucide-react';
import { Button, LoadingSpinner, ErrorMessage, useToast } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import ModalPortalScope from '@/shared/ui/ModalPortalScope.jsx';
import useEndpointsData from '@/features/providers/claude/hooks/useEndpointsData.js';
import { EndpointForm, DeleteConfirmDialog } from './components/index.js';
import SubscriptionManagement from '@/features/providers/shared/subscriptions/SubscriptionManagement.jsx';
import ClientVersionBadge from '@/features/providers/shared/ClientVersionBadge.jsx';
import { ClaudeIcon } from '@/features/requests/components/ModelTag.jsx';
import CredentialGroupDetailModal from '@/features/providers/shared/credentials/CredentialGroupDetailModal.jsx';
import { getErrorMessage, getFailoverToggleErrorMessage } from '@/features/providers/shared/formatters.js';
import { selectPersistedEndpoints } from '@/features/providers/shared/endpoints/endpointActions.js';
import { getCredentialGroupForEndpoint } from './utils/credentialGroups.js';
import { buildClaudeEndpointCopyPayload } from './utils/endpointPayload.js';
import { createChannelSetupCommitter } from '../shared/channelSetup.js';
import { getEndpointStorageStatus, getEndpointRecords, createEndpointRecord, updateEndpointRecordByID, deleteEndpointRecordByID, activateEndpointRecordByID, setEndpointFailoverEnabledByID, setEndpointSkipHealthCheckByID, getChannels, createChannel, updateChannel, deleteChannel, getGroupsRaw, activateGroup, pauseGroup, resumeGroup, getNextAvailableChannel, getConfig, getSettingsByCategory, isInterChannelFailoverEnabled, isWailsEnvironment, subscribeToEvent, getSuspendedRequestsCount, getSuspendedEndpointCounts, getManualRetryRequestsCount, getManualRetryChannelCounts, createClaudeCredentialGroup, updateClaudeCredentialGroupByID, deleteClaudeCredentialGroupByID, setDefaultClaudeCredentialGroup } from '@/services/wails/index.js';
import { hasClaudeCredential, getClaudeCredentialGroups, getDefaultClaudeCredentialGroup, buildClaudeEndpointCopyOptions, parseSettingsBool, buildSuspendedEndpointKey } from './model.js';
import { DeleteChannelConfirmDialog } from './components/DeleteChannelConfirmDialog.jsx';
import { DeleteCredentialGroupConfirmDialog } from './components/DeleteCredentialGroupConfirmDialog.jsx';
import { PauseActiveChannelConfirmDialog } from './components/PauseActiveChannelConfirmDialog.jsx';
import { EndpointDetailModal } from './components/EndpointDetailModal.jsx';
import { CreateChannelModal } from './components/CreateChannelModal.jsx';
import { ClaudeCredentialGroupFormModal } from './components/ClaudeCredentialGroupFormModal.jsx';
import { ChannelCard } from './components/ChannelCard.jsx';

// ============================================
// Channels 页面
// ============================================

const ChannelsPage = ({ clientVersion, onViewRequests }) => {
  const toast = useToast();

  // 存储模式状态
  const [storageStatus, setStorageStatus] = useState(null);
  const [storageEndpoints, setStorageEndpoints] = useState([]);
  const [storageLoading, setStorageLoading] = useState(false);

  // 端点存储统一使用 SQLite；只检查 storageType，不依赖 enabled。
  const isSqliteMode = storageStatus?.storageType === 'sqlite';

  // SQLite 端点由专用 API 加载；保留旧 Hook 仅供兼容，不自动订阅。
  const {
    endpoints,
    loading,
    error,
    refresh,
    lastUpdate
  } = useEndpointsData({ enabled: storageStatus ? !isSqliteMode : false });

  // 渠道（组）状态
  const [groups, setGroups] = useState([]);
  const [channelActionLoading, setChannelActionLoading] = useState(false);
  const [channelFailoverEnabled, setChannelFailoverEnabled] = useState(true);
  const [globalTimeoutSeconds, setGlobalTimeoutSeconds] = useState(300);
  // failoverDefaultCooldownSeconds 已移除
  const [channelsMeta, setChannelsMeta] = useState([]);
  const [channelsLastUpdate, setChannelsLastUpdate] = useState('');

  // 挂起请求计数（用于显示等待提示）
  const [suspendedRequestsCount, setSuspendedRequestsCount] = useState(0);
  const [suspendedEndpointCounts, setSuspendedEndpointCounts] = useState({});
  const [, setManualRetryRequestsCount] = useState(0);
  const [manualRetryChannelCounts, setManualRetryChannelCounts] = useState({});

  // 批量检测状态
  const [batchCheckLoading, setBatchCheckLoading] = useState(false);

  // 表单状态
  const [showForm, setShowForm] = useState(false);
  const [showCreateChannel, setShowCreateChannel] = useState(false);
  const [createChannelError, setCreateChannelError] = useState('');
  const [showEditChannel, setShowEditChannel] = useState(false);
  const [editChannelError, setEditChannelError] = useState('');
  const [editingChannel, setEditingChannel] = useState(null);
  const [editingEndpoint, setEditingEndpoint] = useState(null);
  const [defaultChannel, setDefaultChannel] = useState('');
  const [defaultCredentialGroupId, setDefaultCredentialGroupId] = useState(0);
  const [lockChannel, setLockChannel] = useState(false);
  const [formLoading, setFormLoading] = useState(false);
  const [channelFormLoading, setChannelFormLoading] = useState(false);
  const [showCredentialGroupForm, setShowCredentialGroupForm] = useState(false);
  const [credentialGroupFormMode, setCredentialGroupFormMode] = useState('create');
  const [credentialGroupFormChannel, setCredentialGroupFormChannel] = useState('');
  const [editingCredentialGroup, setEditingCredentialGroup] = useState(null);
  const [credentialGroupFormLoading, setCredentialGroupFormLoading] = useState(false);
  const [credentialGroupFormError, setCredentialGroupFormError] = useState('');
  const [channelSetupDraft, setChannelSetupDraft] = useState(null);
  const [commitChannelSetup] = useState(createChannelSetupCommitter);
	const [credentialGroupDetailTarget, setCredentialGroupDetailTarget] = useState(null);

  // 删除确认状态
  const [deleteTarget, setDeleteTarget] = useState(null);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deleteChannelTarget, setDeleteChannelTarget] = useState(null);
  const [deleteChannelLoading, setDeleteChannelLoading] = useState(false);
  const [deleteCredentialGroupTarget, setDeleteCredentialGroupTarget] = useState(null);
  const [deleteCredentialGroupLoading, setDeleteCredentialGroupLoading] = useState(false);
  const [pauseActiveChannelTarget, setPauseActiveChannelTarget] = useState(null);
  const [actionError, setActionError] = useState('');

  // 端点详情弹窗
  const [detailTarget, setDetailTarget] = useState(null);
  const [detailOpen, setDetailOpen] = useState(false);

  const storageLoadSeqRef = useRef(0);
  const groupsLoadSeqRef = useRef(0);
  const channelsMetaLoadSeqRef = useRef(0);
  const configLoadSeqRef = useRef(0);

  const openEndpointDetail = useCallback((endpoint) => {
    setDetailTarget(endpoint);
    setDetailOpen(true);
  }, []);

  const closeEndpointDetail = useCallback(() => {
    setDetailOpen(false);
    setDetailTarget(null);
  }, []);

  // 加载存储状态
  const loadStorageStatus = useCallback(async ({ throwOnError = false } = {}) => {
    const seq = ++storageLoadSeqRef.current;
    setStorageLoading(true);
    try {
      const status = await getEndpointStorageStatus();
      if (seq !== storageLoadSeqRef.current) return;
      setStorageStatus(status);

      // SQLite 模式下加载存储的端点。
      if (status.storageType === 'sqlite') {
        const records = await getEndpointRecords();
        if (seq !== storageLoadSeqRef.current) return;
        setStorageEndpoints(records);
      }
    } catch (err) {
      console.error('获取存储状态失败:', err);
      if (seq !== storageLoadSeqRef.current) return;
      // 存储后端固定为 SQLite，即使状态接口暂时失败也不要回退到旧模式。
      setStorageStatus({ enabled: false, storageType: 'sqlite' });
      if (throwOnError) throw err;
    } finally {
      if (seq === storageLoadSeqRef.current) {
        setStorageLoading(false);
      }
    }
  }, []);

  // 初始化加载存储状态
  useEffect(() => {
    loadStorageStatus();
  }, [loadStorageStatus]);

  // 加载渠道（组）状态
  const loadGroups = useCallback(async ({ throwOnError = false } = {}) => {
    const seq = ++groupsLoadSeqRef.current;
    try {
      const data = await getGroupsRaw();
      if (seq !== groupsLoadSeqRef.current) return;
      setGroups(Array.isArray(data) ? data : []);
    } catch (err) {
      console.error('获取渠道状态失败:', err);
      if (seq !== groupsLoadSeqRef.current) return;
      setGroups([]);
      if (throwOnError) throw err;
    }
  }, []);

  const loadChannelsMeta = useCallback(async ({ throwOnError = false } = {}) => {
    const seq = ++channelsMetaLoadSeqRef.current;
    // 只检查 storageType
    const sqliteEnabled = storageStatus?.storageType === 'sqlite';
    if (!sqliteEnabled) {
      if (seq === channelsMetaLoadSeqRef.current) {
        setChannelsMeta([]);
        setChannelsLastUpdate('');
      }
      if (throwOnError) {
        throw new Error('SQLite 渠道存储尚未就绪');
      }
      return [];
    }
    try {
      const list = await getChannels();
      const normalizedList = Array.isArray(list) ? list : [];
      if (seq === channelsMetaLoadSeqRef.current) {
        setChannelsMeta(normalizedList);
        setChannelsLastUpdate(new Date().toLocaleTimeString());
      }
      return normalizedList;
    } catch (err) {
      console.error('获取渠道列表失败:', err);
      // 避免瞬时失败导致 UI “清空”，保留上一次结果
      if (throwOnError) throw err;
      return [];
    }
  }, [storageStatus]);

  const loadConfig = useCallback(async () => {
    const seq = ++configLoadSeqRef.current;
    try {
      const [cfg, interChannelEnabled, failoverSettings] = await Promise.all([
        getConfig(),
        isInterChannelFailoverEnabled().catch(() => undefined),
        getSettingsByCategory('failover').catch(() => [])
      ]);
      if (seq !== configLoadSeqRef.current) return;
      const settingsFailoverEnabled = parseSettingsBool(
        (failoverSettings || []).find(setting => setting?.key === 'enabled')?.value
      );
      const configFailoverEnabled = cfg?.failover_enabled ?? cfg?.failoverEnabled;
      const resolvedFailoverEnabled = typeof settingsFailoverEnabled === 'boolean'
        ? settingsFailoverEnabled
        : (typeof interChannelEnabled === 'boolean'
            ? interChannelEnabled
            : (typeof configFailoverEnabled === 'boolean' ? configFailoverEnabled : false));
      setChannelFailoverEnabled(resolvedFailoverEnabled);
      setGlobalTimeoutSeconds(Number(cfg?.global_timeout_seconds ?? cfg?.globalTimeoutSeconds ?? 300) || 300);
      // cooldownSeconds 加载已移除
    } catch (err) {
      console.error('获取配置失败:', err);
      if (seq !== configLoadSeqRef.current) return;
      setChannelFailoverEnabled(false);
      setGlobalTimeoutSeconds(300);
    }
  }, []);

  // 加载挂起请求计数
  const loadSuspendedCount = useCallback(async () => {
    try {
      const [count, endpointCounts, manualCount, manualChannelCounts] = await Promise.all([
        getSuspendedRequestsCount(),
        getSuspendedEndpointCounts(),
        getManualRetryRequestsCount(),
        getManualRetryChannelCounts()
      ]);
      setSuspendedRequestsCount(count || 0);
      setSuspendedEndpointCounts(
        endpointCounts && !Array.isArray(endpointCounts) ? endpointCounts : {}
      );
      setManualRetryRequestsCount(manualCount || 0);
      setManualRetryChannelCounts(
        manualChannelCounts && !Array.isArray(manualChannelCounts) ? manualChannelCounts : {}
      );
    } catch (err) {
      // 静默处理错误，不影响页面正常使用
      console.debug('获取挂起请求数量失败:', err);
    }
  }, []);

  const handleToggleEndpointFailover = useCallback(async (endpoint, enabled) => {
    if (!endpoint?.id) return;
    try {
      setChannelActionLoading(true);
      await setEndpointFailoverEnabledByID(endpoint.id, enabled);
      await loadStorageStatus();
      await loadGroups();
    } catch (err) {
      console.error('切换故障转移参与状态失败:', err);
      setActionError(getFailoverToggleErrorMessage(err, '操作失败'));
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadGroups, loadStorageStatus]);

  const handleCheckEndpointHealth = async (endpoint) => {
    setChannelActionLoading(true);
    try {
      await checkPoolEndpoint('claude', endpoint.id);
      await Promise.all([loadStorageStatus({ throwOnError: true }), loadGroups(), loadChannelsMeta()]);
    } finally {
      setChannelActionLoading(false);
    }
  };

  const handleSetGroupEndpointFailover = useCallback(async (targetEndpoints = [], enabled) => {
    const targets = selectPersistedEndpoints(targetEndpoints);
    if (targets.length === 0) return;
    try {
      setChannelActionLoading(true);
      setActionError('');
      for (const endpoint of targets) {
        await setEndpointFailoverEnabledByID(endpoint.id, enabled);
      }
      await loadStorageStatus();
      await loadGroups();
    } catch (err) {
      console.error('批量切换分组端点转移失败:', err);
      setActionError(getFailoverToggleErrorMessage(err, '批量操作失败'));
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadGroups, loadStorageStatus]);

  const handleSetGroupEndpointSkipHealthCheck = useCallback(async (targetEndpoints = [], skip) => {
    const targets = selectPersistedEndpoints(targetEndpoints);
    if (targets.length === 0) return;
    try {
      setChannelActionLoading(true);
      setActionError('');
      for (const endpoint of targets) {
        await setEndpointSkipHealthCheckByID(endpoint.id, skip);
      }
      await loadStorageStatus();
      await loadGroups();
    } catch (err) {
      console.error('批量切换分组端点跳过检测失败:', err);
      setActionError(`批量操作失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadGroups, loadStorageStatus]);

  const handleActivateEndpoint = useCallback(async (endpoint) => {
    if (!endpoint?.id) return;
    try {
      setChannelActionLoading(true);
      setActionError('');
      await activateEndpointRecordByID(endpoint.id);
      await loadStorageStatus();
      await loadGroups();
      await loadChannelsMeta();
    } catch (err) {
      console.error('切换当前路由端点失败:', err);
      setActionError(`切换失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadChannelsMeta, loadGroups, loadStorageStatus]);

  // 跳过健康检测快捷开关处理函数
  const handleToggleEndpointSkipHealthCheck = useCallback(async (endpoint, skip) => {
    if (!endpoint?.id) return;
    try {
      setChannelActionLoading(true);
      await setEndpointSkipHealthCheckByID(endpoint.id, skip);
      await loadStorageStatus();
      await loadGroups();
    } catch (err) {
      console.error('切换跳过健康检测状态失败:', err);
      setActionError(`操作失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadGroups, loadStorageStatus]);

  useEffect(() => {
    loadGroups();
  }, [loadGroups]);

  useEffect(() => {
    loadConfig();
  }, [loadConfig]);

  // 定时获取挂起请求数量（页面加载时立即获取，之后每2秒轮询）
  useEffect(() => {
    loadSuspendedCount();
    const interval = setInterval(loadSuspendedCount, 2000);
    return () => clearInterval(interval);
  }, [loadSuspendedCount]);

  useEffect(() => {
    loadChannelsMeta();
  }, [loadChannelsMeta]);

  useEffect(() => {
    if (showCreateChannel) {
      setCreateChannelError('');
    }
  }, [showCreateChannel]);

  useEffect(() => {
    if (showEditChannel) {
      setEditChannelError('');
    }
  }, [showEditChannel]);

  // SQLite 模式下监听 Wails 事件，实时刷新端点数据
  const isSqliteModeRef = useRef(false);
  useEffect(() => {
    // 只检查 storageType
    isSqliteModeRef.current = storageStatus?.storageType === 'sqlite';
  }, [storageStatus]);

  useEffect(() => {
    if (!isWailsEnvironment()) return;

    // 订阅端点更新事件
    const unsubscribe = subscribeToEvent('endpoint:update', () => {
      // 只在 SQLite 模式下刷新数据
      if (isSqliteModeRef.current) {
        console.log('📡 [Channels] 收到端点更新事件，刷新 SQLite 数据');
        loadConfig();
        loadStorageStatus();
        loadGroups();
        loadChannelsMeta();
      }
    });

    return () => {
      if (typeof unsubscribe === 'function') {
        unsubscribe();
      }
    };
  }, [loadChannelsMeta, loadConfig, loadGroups, loadStorageStatus]);

  const handleRefresh = async () => {
    setBatchCheckLoading(true);
    try {
      if (isSqliteMode) {
        await loadStorageStatus();
      } else {
        await refresh();
      }
      await Promise.all([loadGroups(), loadChannelsMeta()]);
    } catch (err) {
      console.error('刷新失败:', err);
      setActionError(`刷新失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setBatchCheckLoading(false);
    }
  };

  // 获取要显示的端点列表
  const displayEndpoints = isSqliteMode ? storageEndpoints : endpoints;

  // 渠道卡片“显示全部/收起”：只允许展开一个渠道，避免同一行两列联动展开导致误触
  const [expandedChannelName, setExpandedChannelName] = useState('');

  // 点击渠道卡片外部区域时自动收起（排除弹窗区域）
  useEffect(() => {
    if (!expandedChannelName) return;
    if (typeof document === 'undefined') return;

    const onPointerDown = (event) => {
      const target = event?.target;

      // 如果点击在弹窗内（固定定位的遮罩层或其内部），不收缩渠道
      const inModal = target?.closest?.('.fixed.inset-0');
      if (inModal) return;

      const card = target?.closest?.('[data-channel-card]');
      const clickedChannelName = card?.getAttribute?.('data-channel-card') || '';
      if (clickedChannelName !== expandedChannelName) {
        setExpandedChannelName('');
      }
    };

    document.addEventListener('pointerdown', onPointerDown, true);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown, true);
    };
  }, [expandedChannelName]);

  const activeChannel = useMemo(() => {
    const activeGroup = groups.find(g => g.active);
    if (activeGroup?.name) return activeGroup.name;

    if (isSqliteMode) {
      return storageEndpoints.find(e => e.enabled)?.channel || '';
    }

    // 兜底：从端点数据推断（避免 groups 加载失败时 UI 空白）
    const inferred = displayEndpoints.find(e => e.group_is_active)?.group
      || displayEndpoints.find(e => e.group_is_active)?.channel
      || '';
    return inferred;
  }, [displayEndpoints, groups, isSqliteMode, storageEndpoints]);

  const channelOptions = useMemo(() => {
    const ordered = [];
    const seen = new Set();
    const add = (name) => {
      if (!name) return;
      if (seen.has(name)) return;
      seen.add(name);
      ordered.push(name);
    };
    channelsMeta.forEach((c) => add(c?.name || ''));
    displayEndpoints.forEach((e) => add(e.group || e.channel || ''));
    return ordered;
  }, [channelsMeta, displayEndpoints]);

  const credentialGroupsByChannel = useMemo(() => {
    const map = {};
    channelsMeta.forEach((channel) => {
      const name = channel?.name || '';
      if (name) {
        map[name] = getClaudeCredentialGroups(channel);
      }
    });
    return map;
  }, [channelsMeta]);
	const selectedCredentialGroupDetail = credentialGroupDetailTarget ? {
	  channelName: credentialGroupDetailTarget.channelName,
	  group: (credentialGroupsByChannel[credentialGroupDetailTarget.channelName] || []).find(
		(group) => Number(group.id) === Number(credentialGroupDetailTarget.group?.id)
	  ) || credentialGroupDetailTarget.group
	} : null;
  const currentDetailTarget = detailTarget ? displayEndpoints.find((endpoint) => (
    detailTarget.id ? Number(endpoint.id) === Number(detailTarget.id) : endpoint.name === detailTarget.name &&
      (endpoint.channel || endpoint.group) === (detailTarget.channel || detailTarget.group)
  )) || detailTarget : null;
  const detailChannelName = currentDetailTarget?.channel || currentDetailTarget?.group || '';
  const detailChannelMeta = channelsMeta.find((item) => item?.name === detailChannelName) || null;
  const detailCredentialGroup = getCredentialGroupForEndpoint(
    credentialGroupsByChannel[detailChannelName] || [],
    currentDetailTarget
  );

  const groupInfoMap = useMemo(() => {
    const map = new Map();
    groups.forEach(g => {
      if (g?.name) map.set(g.name, g);
    });
    return map;
  }, [groups]);

  const isEndpointInHealthScope = useCallback((ep) => {
    return (ep?.health_state || ep?.healthState) !== 'disabled';
  }, []);

  const scopedDisplayEndpoints = useMemo(() => {
    return (displayEndpoints || []).map((ep) => ({
      ...ep,
      suspendedCount: Number.isFinite(Number(suspendedEndpointCounts?.[buildSuspendedEndpointKey(ep)] ?? 0))
        ? Number(suspendedEndpointCounts?.[buildSuspendedEndpointKey(ep)] ?? 0)
        : 0,
      _health_scope: isEndpointInHealthScope(ep),
      _channel_paused: Boolean(groupInfoMap.get(ep?.group || ep?.channel || ep?.name || 'default')?.paused)
    }));
  }, [displayEndpoints, groupInfoMap, isEndpointInHealthScope, suspendedEndpointCounts]);

  const channelSections = useMemo(() => {
    const getChannelKey = (ep) => ep.group || ep.channel || ep.name || 'default';
    const map = new Map(channelOptions.map((name) => [name, []]));
    scopedDisplayEndpoints.forEach((ep) => {
      const key = getChannelKey(ep);
      if (!map.has(key)) map.set(key, []);
      map.get(key).push(ep);
    });

    const metaMap = new Map();
    channelsMeta.forEach((c) => {
      if (c?.name) metaMap.set(c.name, c);
    });

    const normalizePriority = (p) => {
      const parsed = Number(p);
      return Number.isFinite(parsed) && parsed >= 0 ? parsed : 999;
    };
    const compareDescString = (a, b) => (b || '').localeCompare(a || '');

    const sections = Array.from(map.entries()).map(([name, eps]) => {
      const meta = metaMap.get(name) || null;
      const gi = groupInfoMap.get(name) || null;
      const computedPriority = eps.length > 0 ? Math.min(...eps.map(e => normalizePriority(e.priority))) : 999;
      const sortPriority = normalizePriority(meta?.priority ?? gi?.priority ?? computedPriority);
      const sortCreatedAt = meta?.createdAt || '';

      const sortedEndpoints = [...eps].sort((a, b) => {
        const pa = normalizePriority(a?.priority);
        const pb = normalizePriority(b?.priority);
        if (pa !== pb) return pa - pb;
        const ca = a?.createdAt || '';
        const cb = b?.createdAt || '';
        const byCreated = compareDescString(ca, cb);
        if (byCreated !== 0) return byCreated;
        return (a?.name || '').localeCompare(b?.name || '');
      });

      return {
        id: meta?.id,
        name,
        website: meta?.website || '',
        channelPriority: meta?.priority ?? null,
        endpoints: sortedEndpoints,
        groupInfo: gi,
        sortPriority,
        sortCreatedAt
      };
    });

    return sections.sort((a, b) => {
      if (activeChannel) {
        const aIsActive = a.name === activeChannel;
        const bIsActive = b.name === activeChannel;
        if (aIsActive && !bIsActive) return -1;
        if (bIsActive && !aIsActive) return 1;
      }
      if (a.sortPriority !== b.sortPriority) return a.sortPriority - b.sortPriority;
      const byCreated = compareDescString(a.sortCreatedAt, b.sortCreatedAt);
      if (byCreated !== 0) return byCreated;
      return (a.name || '').localeCompare(b.name || '');
    });
  }, [activeChannel, channelOptions, channelsMeta, groupInfoMap, scopedDisplayEndpoints]);

  const headerLastUpdate = isSqliteMode ? channelsLastUpdate : lastUpdate;

  const credentialGroupEndpointCopyOptions = useMemo(() => {
    if (credentialGroupFormMode !== 'create' || !credentialGroupFormChannel) return [];
    const sourceGroups = credentialGroupsByChannel[credentialGroupFormChannel] || [];
    const sourceEndpoints = channelSections.find((section) => section.name === credentialGroupFormChannel)?.endpoints || [];
    return buildClaudeEndpointCopyOptions(sourceGroups, sourceEndpoints);
  }, [channelSections, credentialGroupFormChannel, credentialGroupFormMode, credentialGroupsByChannel]);

  // 渠道列表变化时重置展开状态，避免展开指向已不存在的渠道
  useEffect(() => {
    setExpandedChannelName('');
  }, [channelSections.length]);

  // ============================================
  // CRUD 操作处理
  // ============================================

  const openCreateEndpointForm = useCallback((channelName, credentialGroup = null) => {
    const name = String(channelName || '').trim();
    if (!name || !hasClaudeCredential(credentialGroup)) return false;
    setEditingEndpoint(null);
    setDefaultChannel(name);
    setDefaultCredentialGroupId(Number(credentialGroup?.id || 0) || 0);
    setLockChannel(true);
    setShowForm(true);
    return true;
  }, []);

  const handleCreateChannel = useCallback(async (payload) => {
    if (channelsMeta.some((channel) => channel.name === payload.name)) {
      setCreateChannelError('渠道名称已存在');
      return;
    }
    setCreateChannelError('');
    setChannelSetupDraft({ channel: payload });
    setShowCreateChannel(false);
    setCredentialGroupFormMode('create');
    setCredentialGroupFormChannel(payload.name);
    setEditingCredentialGroup({ name: '默认分组', isDefault: true });
    setCredentialGroupFormError('');
    setShowCredentialGroupForm(true);
  }, [channelsMeta]);

  const handleOpenEditChannel = useCallback((channelName) => {
    const name = (channelName || '').trim();
    if (!name) return;
    const meta = channelsMeta.find((c) => c?.name === name) || null;
    setEditingChannel({
      id: meta?.id || 0,
      name,
      website: meta?.website || '',
      priority: meta?.priority ?? 0,
      modelMapping: meta?.modelMapping || {},
      nonStreamRoute: meta?.nonStreamRoute || null
    });
    setShowEditChannel(true);
  }, [channelsMeta]);

  const handleUpdateChannel = useCallback(async (payload) => {
    try {
      setChannelFormLoading(true);
      setEditChannelError('');

      await updateChannel(payload);
      setShowEditChannel(false);
      setEditingChannel(null);
      await loadChannelsMeta();
      await loadGroups();
    } catch (err) {
      console.error('更新渠道失败:', err);
      setEditChannelError(getErrorMessage(err, '更新渠道失败'));
    } finally {
      setChannelFormLoading(false);
    }
  }, [loadChannelsMeta, loadGroups]);

  const openCreateCredentialGroupForm = useCallback((channelName) => {
    const name = String(channelName || '').trim();
    if (!name) return;
    setChannelSetupDraft(null);
    setCredentialGroupFormMode('create');
    setCredentialGroupFormChannel(name);
    setEditingCredentialGroup(null);
    setCredentialGroupFormError('');
    setShowCredentialGroupForm(true);
  }, []);

  const openEditCredentialGroupForm = useCallback((channelName, group) => {
    const name = String(channelName || group?.channel || '').trim();
    if (!name || !group) return;
    setChannelSetupDraft(null);
    setCredentialGroupFormMode('edit');
    setCredentialGroupFormChannel(name);
    setEditingCredentialGroup(group);
    setCredentialGroupFormError('');
    setShowCredentialGroupForm(true);
  }, []);

  const closeCredentialGroupForm = useCallback(() => {
    setShowCredentialGroupForm(false);
    setCredentialGroupFormChannel('');
    setEditingCredentialGroup(null);
    setCredentialGroupFormError('');
    setChannelSetupDraft(null);
  }, []);

  const handleSaveCredentialGroup = useCallback(async (payload) => {
    const { copyEndpoints = [], ...groupPayload } = payload || {};
    if (channelSetupDraft) {
      const draftGroup = { ...groupPayload, id: -1, isDefault: true, modelPolicy: 'inherit' };
      setChannelSetupDraft({ ...channelSetupDraft, group: draftGroup });
      setShowCredentialGroupForm(false);
      setCredentialGroupFormChannel('');
      setEditingCredentialGroup(null);
      setCredentialGroupFormError('');
      // Draft groups also support client passthrough, which has no stored credential.
      setEditingEndpoint(null);
      setDefaultChannel(channelSetupDraft.channel.name);
      setDefaultCredentialGroupId(draftGroup.id);
      setLockChannel(true);
      setShowForm(true);
      return;
    }
    let savedGroup = null;
    try {
      setCredentialGroupFormLoading(true);
      setCredentialGroupFormError('');
      if (credentialGroupFormMode === 'edit' && editingCredentialGroup?.id) {
        savedGroup = await updateClaudeCredentialGroupByID(editingCredentialGroup.id, groupPayload);
      } else {
        savedGroup = await createClaudeCredentialGroup({
          ...groupPayload,
          channel: credentialGroupFormChannel
        });
        const targetGroupID = Number(savedGroup?.id || 0);
        if (targetGroupID > 0) {
          for (const endpoint of copyEndpoints) {
            await createEndpointRecord(buildClaudeEndpointCopyPayload(endpoint, credentialGroupFormChannel, targetGroupID));
          }
        }
      }
    } catch (err) {
      console.error('保存凭证分组失败:', err);
      setCredentialGroupFormError(getErrorMessage(err, '保存凭证分组失败'));
      return;
    } finally {
      setCredentialGroupFormLoading(false);
    }

    closeCredentialGroupForm();

    try {
      await loadStorageStatus({ throwOnError: true });
      await loadChannelsMeta({ throwOnError: true });
      await loadGroups({ throwOnError: true });
    } catch (err) {
      console.error('刷新凭证分组列表失败:', err);
      setActionError(`凭证分组已保存，但列表刷新失败，请点击页面刷新查看最新数据。\n${getErrorMessage(err, '未知错误')}`);
    }
  }, [
    closeCredentialGroupForm,
    credentialGroupFormChannel,
    credentialGroupFormMode,
    editingCredentialGroup,
    channelSetupDraft,
    loadChannelsMeta,
    loadGroups,
    loadStorageStatus
  ]);

  const handleSetDefaultCredentialGroup = useCallback(async (_channelName, group) => {
    if (!group?.id) return;
    try {
      setChannelActionLoading(true);
      await setDefaultClaudeCredentialGroup(group.id);
      await loadStorageStatus();
      await loadChannelsMeta();
      await loadGroups();
    } catch (err) {
      console.error('设置默认凭证分组失败:', err);
      setActionError(`设置默认分组失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setChannelActionLoading(false);
    }
  }, [loadChannelsMeta, loadGroups, loadStorageStatus]);

  const handleDeleteCredentialGroup = useCallback((channelName, group) => {
    if (!group?.id) return;
    setDeleteCredentialGroupTarget({
      channelName,
      group
    });
  }, []);

  const handleConfirmDeleteCredentialGroup = useCallback(async () => {
    const target = deleteCredentialGroupTarget;
    if (!target?.group?.id) return;
    try {
      setDeleteCredentialGroupLoading(true);
      await deleteClaudeCredentialGroupByID(target.group.id);
      setDeleteCredentialGroupTarget(null);
      await loadStorageStatus();
      await loadChannelsMeta();
      await loadGroups();
    } catch (err) {
      console.error('删除凭证分组失败:', err);
      setActionError(`删除凭证分组失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setDeleteCredentialGroupLoading(false);
    }
  }, [deleteCredentialGroupTarget, loadChannelsMeta, loadGroups, loadStorageStatus]);

  // 编辑端点
  const handleEdit = (endpoint) => {
    setEditingEndpoint(endpoint);
    setShowForm(true);
  };

  // 删除端点
  const handleDelete = (endpoint) => {
    setDeleteTarget(endpoint);
  };

  const handleDeleteChannel = useCallback((channelName) => {
    const section = channelSections.find((s) => s.name === channelName);
    setDeleteChannelTarget({
      name: channelName,
      endpointCount: section?.endpoints?.length || 0
    });
  }, [channelSections]);

  // 保存端点
  const handleSave = async (formData) => {
    if (formLoading) return;
    setFormLoading(true);
    try {
      if (channelSetupDraft) {
        const { channel, group } = channelSetupDraft;
        const groupPayload = { ...group };
        delete groupPayload.id;
        await commitChannelSetup({
          create: () => createChannel(channel),
          find: async () => (await getChannels()).find((item) => item.name === channel.name),
          saveGroup: async (createdChannel) => {
            const defaultGroup = getDefaultClaudeCredentialGroup(createdChannel);
            return defaultGroup
              ? updateClaudeCredentialGroupByID(defaultGroup.id, groupPayload)
              : createClaudeCredentialGroup({ ...groupPayload, channel: channel.name });
          },
          saveEndpoint: (_channel, savedGroup) => createEndpointRecord({
            ...formData, channel: channel.name, credentialGroupId: savedGroup.id
          }),
          remove: () => deleteChannel(channel.name, true)
        });
      } else if (editingEndpoint) {
        // 编辑模式
        await updateEndpointRecordByID(editingEndpoint.id, formData);
      } else {
        // 新建模式
        await createEndpointRecord(formData);
      }
      setShowForm(false);
      setEditingEndpoint(null);
      setDefaultChannel('');
      setDefaultCredentialGroupId(0);
      setLockChannel(false);
      const savedChannelSetup = Boolean(channelSetupDraft);
      setChannelSetupDraft(null);
      try {
        await loadStorageStatus({ throwOnError: true });
        await loadChannelsMeta({ throwOnError: true });
        await loadGroups({ throwOnError: true });
      } catch (err) {
        setActionError(`${savedChannelSetup ? '渠道已创建' : '端点已保存'}，但列表刷新失败，请点击页面刷新查看最新数据。\n${getErrorMessage(err, '未知错误')}`);
      }
    } catch (err) {
      console.error('保存失败:', err);
      throw err;
    } finally {
      setFormLoading(false);
    }
  };

  // 确认删除
  const handleConfirmDelete = async () => {
    if (!deleteTarget) return;

    setDeleteLoading(true);
    try {
      await deleteEndpointRecordByID(deleteTarget.id);
      setDeleteTarget(null);
      // 刷新列表
      await loadStorageStatus();
      await loadGroups();
    } catch (err) {
      console.error('删除失败:', err);
      setActionError(`删除失败: ${getErrorMessage(err, '未知错误')}`);
    } finally {
      setDeleteLoading(false);
    }
  };

  // 错误状态
  if (error && !isSqliteMode) {
    return (
      <ErrorMessage
        title="端点数据加载失败"
        message={error}
        onRetry={refresh}
      />
    );
  }

  // 加载状态
  if (!storageStatus) {
    return <LoadingSpinner text="加载渠道数据..." />;
  }

  if ((storageLoading || (!isSqliteMode && loading)) && channelSections.length === 0) {
    return <LoadingSpinner text="加载渠道数据..." />;
  }

  return (
    <ModalPortalScope><div className="flex min-h-0 flex-1 flex-col animate-fade-in">
      {/* 页面标题 */}
      <div className="mb-3 flex min-h-9 shrink-0 flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="shrink-0 rounded-lg bg-slate-900 p-1.5 text-white shadow-sm">
            <ClaudeIcon className="h-5 w-5" />
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
              <h1 className="text-xl font-bold text-slate-900">Claude 管理</h1>
              <ClientVersionBadge provider="claude" version={clientVersion} />
              {headerLastUpdate && (
                <span className="hidden text-[11px] text-slate-400 min-[75rem]:inline">更新于 {headerLastUpdate}</span>
              )}
          </div>
        </div>

      </div>

      <DispatchWorkspace provider="claude" refreshToken={headerLastUpdate} onViewRequests={onViewRequests}
        resourcesBusy={loading || batchCheckLoading}
        onRefreshResources={handleRefresh}
        resourceActions={<>
          <SubscriptionManagement provider="claude" channels={channelsMeta} onUpdated={handleRefresh} className="dispatch-resource-action" disabled={loading || batchCheckLoading} />
          {isSqliteMode && channelSections.length > 0 && <Button type="button" size="sm" icon={Plus} className="dispatch-resource-action" disabled={loading || batchCheckLoading} onClick={() => setShowCreateChannel(true)}>添加渠道</Button>}
        </>} endpoints={displayEndpoints.map((endpoint) => {
        const channel = channelsMeta.find((channel) => channel.name === endpoint.channel);
        const group = getClaudeCredentialGroups(channel).find((group) => Number(group.id) === Number(endpoint.credentialGroupId)) || getDefaultClaudeCredentialGroup(channel);
        return {
          channelId: channel?.id, groupId: group?.id || 0,
          endpointId: endpoint.credentialGroupId > 0 ? `${endpoint.channel}::g${endpoint.credentialGroupId}::${endpoint.name}` : buildSuspendedEndpointKey(endpoint),
          name: endpoint.name, address: endpoint.url,
        };
      })} onError={(message) => toast.error(message)} onSuccess={(message) => toast.success(message)}>
      {/* 渠道分块列表 */}
      {channelSections.length === 0 ? (
        <div className="bg-white rounded-2xl border border-slate-200/60 shadow-sm p-10 text-center text-slate-500">
          {isSqliteMode ? (
            <div className="flex flex-col items-center gap-3">
              <Database size={40} className="text-slate-300" />
              <p>暂无渠道配置</p>
              <Button
                icon={Plus}
                onClick={() => {
                  setShowCreateChannel(true);
                }}
              >
                添加第一个渠道
              </Button>
            </div>
          ) : (
            '暂无端点数据'
          )}
        </div>
       ) : (
        <ChannelResourceGrid provider="claude" label="Claude 渠道列表">
          {channelSections.map((section) => (
            <ChannelCard
              key={section.id ? `channel:${section.id}` : `legacy:${section.name}`}
              channelName={section.name}
              endpoints={section.endpoints}
              channelWebsite={section.website}
              channelPriority={section.channelPriority}
              groupInfo={section.groupInfo}
              activeChannelName={activeChannel}
              isSqliteMode={isSqliteMode}
              channelFailoverEnabled={channelFailoverEnabled}
              suspendedRequestsCount={suspendedRequestsCount}
              manualRetryCount={manualRetryChannelCounts?.[section.name] ?? 0}
              credentialGroups={credentialGroupsByChannel[section.name] || []}
              expanded={expandedChannelName === section.name}
              onToggleExpanded={() => {
                setExpandedChannelName((prev) => (prev === section.name ? '' : section.name));
              }}
              loading={channelActionLoading}
              onOpenEndpoint={openEndpointDetail}
              onActivateEndpoint={isSqliteMode ? handleActivateEndpoint : undefined}
              onToggleEndpointFailover={isSqliteMode ? handleToggleEndpointFailover : undefined}
              onSetGroupEndpointFailover={isSqliteMode ? handleSetGroupEndpointFailover : undefined}
              onSetGroupEndpointSkipHealthCheck={isSqliteMode ? handleSetGroupEndpointSkipHealthCheck : undefined}
              onToggleEndpointSkipHealthCheck={isSqliteMode ? handleToggleEndpointSkipHealthCheck : undefined}
              onHealthEndpoint={handleCheckEndpointHealth}
              onActivate={async (channelName) => {
                if (channelFailoverEnabled && section.groupInfo?.paused) {
                  setActionError(`激活失败：渠道「${channelName}」已暂停，无法激活。\n请先点击「恢复」后再激活该渠道。`);
                  return;
                }
                try {
                  setChannelActionLoading(true);
                  await activateGroup(channelName);
                  if (isSqliteMode) {
                    await loadStorageStatus();
                  } else {
                    await refresh();
                  }
                  await loadGroups();
                } catch (err) {
                  console.error('激活渠道失败:', err);
                  setActionError(`激活失败: ${getErrorMessage(err, '未知错误')}`);
                } finally {
                  setChannelActionLoading(false);
                }
              }}
              onPause={async (channelName) => {
                if (channelFailoverEnabled && section.groupInfo?.active) {
                  try {
                    const next = await getNextAvailableChannel(channelName);
                    if (!next) {
                      setActionError(`暂停失败：渠道「${channelName}」为当前活跃渠道，但没有可切换的渠道。\n请检查是否其他渠道已全部暂停或无可用端点。`);
                      return;
                    }
                    setPauseActiveChannelTarget({ name: channelName, next });
                    return;
                  } catch (err) {
                    console.error('获取下一个可用渠道失败:', err);
                    setActionError(`暂停失败: ${getErrorMessage(err, '无法获取可切换渠道')}`);
                    return;
                  }
                }
                try {
                  setChannelActionLoading(true);
                  await pauseGroup(channelName);
                  await loadGroups();
                } catch (err) {
                  console.error('暂停渠道失败:', err);
                  setActionError(`暂停失败: ${getErrorMessage(err, '未知错误')}`);
                } finally {
                  setChannelActionLoading(false);
                }
              }}
              onResume={async (channelName) => {
                try {
                  setChannelActionLoading(true);
                  await resumeGroup(channelName);
                  await loadGroups();
                } catch (err) {
                  console.error('恢复渠道失败:', err);
                  setActionError(`恢复失败: ${getErrorMessage(err, '未知错误')}`);
                } finally {
                  setChannelActionLoading(false);
                }
              }}
              onAddCredentialGroup={openCreateCredentialGroupForm}
			  onOpenCredentialGroup={(channelName, group) => setCredentialGroupDetailTarget({ channelName, group })}
              onEditCredentialGroup={openEditCredentialGroupForm}
              onDeleteCredentialGroup={handleDeleteCredentialGroup}
              onSetDefaultCredentialGroup={handleSetDefaultCredentialGroup}
              onAddEndpoint={openCreateEndpointForm}
              onEditChannel={handleOpenEditChannel}
              onDeleteChannel={handleDeleteChannel}
              onEditEndpoint={(ep) => {
                closeEndpointDetail();
                setDefaultChannel('');
                setDefaultCredentialGroupId(0);
                setLockChannel(false);
                handleEdit(ep);
              }}
              onDeleteEndpoint={(ep) => {
                closeEndpointDetail();
                handleDelete(ep);
              }}
            />
          ))}
        </ChannelResourceGrid>
      )}

      </DispatchWorkspace>

      {/* 端点表单弹窗 */}
      {showForm && (
        <EndpointForm
          endpoint={editingEndpoint}
          channels={channelOptions}
          existingEndpoints={isSqliteMode ? storageEndpoints : []}
          credentialGroupsByChannel={channelSetupDraft?.group ? {
            ...credentialGroupsByChannel,
            [channelSetupDraft.channel.name]: [channelSetupDraft.group]
          } : credentialGroupsByChannel}
          defaultChannel={defaultChannel}
          defaultCredentialGroupId={defaultCredentialGroupId}
          lockChannel={lockChannel}
          initialSetup={Boolean(channelSetupDraft)}
          onSave={handleSave}
          onCancel={() => {
            setShowForm(false);
            setEditingEndpoint(null);
            setDefaultChannel('');
            setDefaultCredentialGroupId(0);
            setLockChannel(false);
            setChannelSetupDraft(null);
          }}
          loading={formLoading}
        />
      )}

      {showCreateChannel && isSqliteMode && (
        <CreateChannelModal
          open
          loading={channelFormLoading}
          serverError={createChannelError}
          channelFailoverEnabled={channelFailoverEnabled}
          onClose={() => setShowCreateChannel(false)}
          onSubmit={handleCreateChannel}
        />
      )}

      {showEditChannel && isSqliteMode && (
        <CreateChannelModal
          open
          loading={channelFormLoading}
          serverError={editChannelError}
          mode="edit"
          initialValue={editingChannel}
          channelFailoverEnabled={channelFailoverEnabled}
          onClose={() => {
            setShowEditChannel(false);
            setEditingChannel(null);
          }}
          onSubmit={handleUpdateChannel}
        />
      )}

      {showCredentialGroupForm && isSqliteMode && (
        <ClaudeCredentialGroupFormModal
          open
          mode={credentialGroupFormMode}
          channelName={credentialGroupFormChannel}
          group={editingCredentialGroup}
          continueToEndpoint={Boolean(channelSetupDraft)}
          loading={credentialGroupFormLoading}
          serverError={credentialGroupFormError}
          endpointCopyOptions={credentialGroupEndpointCopyOptions}
          channels={channelsMeta}
          onClose={closeCredentialGroupForm}
          onSubmit={handleSaveCredentialGroup}
        />
      )}

	  {selectedCredentialGroupDetail?.group && (
		<CredentialGroupDetailModal
		  provider="claude"
		  channelName={selectedCredentialGroupDetail.channelName}
		  group={selectedCredentialGroupDetail.group}
		  onClose={() => setCredentialGroupDetailTarget(null)}
		  onEdit={(group) => {
			setCredentialGroupDetailTarget(null);
			openEditCredentialGroupForm(selectedCredentialGroupDetail.channelName, group);
		  }}
		/>
	  )}

      {/* 删除确认弹窗 */}
      {deleteTarget && (
        <DeleteConfirmDialog
          endpoint={deleteTarget}
          onConfirm={handleConfirmDelete}
          onCancel={() => setDeleteTarget(null)}
          loading={deleteLoading}
        />
      )}

      {deleteCredentialGroupTarget && (
        <DeleteCredentialGroupConfirmDialog
          channelName={deleteCredentialGroupTarget.channelName}
          group={deleteCredentialGroupTarget.group}
          loading={deleteCredentialGroupLoading}
          onCancel={() => setDeleteCredentialGroupTarget(null)}
          onConfirm={handleConfirmDeleteCredentialGroup}
        />
      )}

      {deleteChannelTarget && (
        <DeleteChannelConfirmDialog
          channelName={deleteChannelTarget.name}
          endpointCount={deleteChannelTarget.endpointCount}
          loading={deleteChannelLoading}
          onCancel={() => setDeleteChannelTarget(null)}
          onConfirm={async () => {
            try {
              setDeleteChannelLoading(true);
              await deleteChannel(deleteChannelTarget.name, true);
              setDeleteChannelTarget(null);
              await loadStorageStatus();
              await loadGroups();
              await loadChannelsMeta();
            } catch (err) {
              console.error('删除渠道失败:', err);
              setActionError(`删除渠道失败: ${getErrorMessage(err, '未知错误')}`);
            } finally {
              setDeleteChannelLoading(false);
            }
          }}
        />
      )}

      {pauseActiveChannelTarget?.name && (
        <PauseActiveChannelConfirmDialog
          channelName={pauseActiveChannelTarget.name}
          nextChannel={pauseActiveChannelTarget.next}
          loading={channelActionLoading}
          onCancel={() => setPauseActiveChannelTarget(null)}
          onConfirm={async () => {
            const name = pauseActiveChannelTarget.name;
            setPauseActiveChannelTarget(null);
            try {
              setChannelActionLoading(true);
              await pauseGroup(name);
              if (isSqliteMode) {
                await loadStorageStatus();
              } else {
                await refresh();
              }
              await loadGroups();
            } catch (err) {
              console.error('暂停活跃渠道失败:', err);
              setActionError(`暂停失败: ${getErrorMessage(err, '未知错误')}`);
            } finally {
              setChannelActionLoading(false);
            }
          }}
        />
      )}

      {actionError && renderModalPortal(
        <div className="fixed inset-0 bg-black/50 flex items-start justify-center z-50 animate-fade-in pt-[20vh]">
          <div className="bg-white rounded-2xl shadow-xl w-full max-w-md p-6">
            <div className="flex items-center gap-3 mb-4">
              <div className="p-3 bg-rose-100 rounded-full">
                <AlertTriangle className="text-rose-600" size={24} />
              </div>
              <div>
                <h3 className="text-lg font-semibold text-slate-900">操作失败</h3>
                <p className="text-sm text-slate-500">请检查配置或稍后重试</p>
              </div>
            </div>
            <p className="text-slate-700 mb-4 whitespace-pre-wrap">{actionError}</p>
            <div className="flex justify-end gap-3">
              <Button variant="ghost" onClick={() => setActionError('')}>
                关闭
              </Button>
            </div>
          </div>
        </div>
      )}

      <EndpointDetailModal
        onRestore={async (ep) => { await handleToggleEndpointFailover(ep, true); closeEndpointDetail(); }}
        onHealth={handleCheckEndpointHealth}
        onToggleHealth={isSqliteMode ? handleToggleEndpointSkipHealthCheck : undefined}
        busy={channelActionLoading}
        endpoint={currentDetailTarget}
        credentialGroup={detailCredentialGroup}
        isOpen={detailOpen}
        isSqliteMode={isSqliteMode}
        channelFailoverEnabled={channelFailoverEnabled}
        globalTimeoutSeconds={globalTimeoutSeconds}
        channelMeta={detailChannelMeta}
        onClose={closeEndpointDetail}
        onEdit={(ep) => {
          closeEndpointDetail();
          setDefaultChannel('');
          setDefaultCredentialGroupId(0);
          setLockChannel(false);
          handleEdit(ep);
        }}
        onDelete={(ep) => {
          closeEndpointDetail();
          handleDelete(ep);
        }}
      />
    </div></ModalPortalScope>
  );
};

export default ChannelsPage;
