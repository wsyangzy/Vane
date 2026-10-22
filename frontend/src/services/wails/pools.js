import { WailsApp, initWails } from './connection.js';

const targetFromDTO = (target) => ({ poolId: Number(target.pool_id || 0), channelId: Number(target.channel_id || 0), groupId: Number(target.group_id || 0), upstreamModel: target.upstream_model || '' });
const targetToDTO = (target) => ({ pool_id: Number(target.poolId || 0), channel_id: Number(target.channelId || 0), group_id: Number(target.groupId || 0), upstream_model: target.upstreamModel?.trim() || '' });
const memberFromDTO = (member) => ({ channelId: Number(member.channel_id), groupId: Number(member.group_id), tier: member.tier });
const memberToDTO = (member) => ({ channel_id: Number(member.channelId), group_id: Number(member.groupId), tier: member.tier });

export const mapAccountPool = (pool) => ({
  id: Number(pool.id), provider: pool.provider, name: pool.name, strategy: pool.strategy,
  isDefault: Boolean(pool.is_default), hasKey: Boolean(pool.has_key), revision: Number(pool.revision),
  members: (pool.members || []).map(memberFromDTO),
  // ModelRule retains its exported Go field name for Targets in the Wails DTO.
  rules: (pool.rules || []).map((rule, index) => ({ id: Number(rule.id || index + 1), name: rule.name || '', disabled: Boolean(rule.disabled), priority: Number(rule.priority || 0), matchModel: rule.match_model || '', mode: rule.mode || 'prefer', targets: (rule.Targets || []).map(targetFromDTO) })),
  nonStream: pool.non_stream ? { mode: pool.non_stream.mode, targets: (pool.non_stream.targets || []).map(targetFromDTO) } : null
});
export const buildAccountPoolPayload = (pool) => ({
  id: Number(pool.id || 0), provider: pool.provider, name: pool.name.trim(), strategy: pool.strategy,
  is_default: Boolean(pool.isDefault), members: pool.members.map(memberToDTO),
  rules: (pool.rules || []).map((rule, index) => ({ id: Number(rule.id || index + 1), name: rule.name.trim(), disabled: Boolean(rule.disabled), priority: Number(rule.priority ?? index), match_model: rule.matchModel.trim(), mode: rule.mode, Targets: rule.targets.map(targetToDTO) })),
  non_stream: pool.nonStream ? { mode: pool.nonStream.mode, targets: pool.nonStream.targets.map(targetToDTO) } : null
});

async function bridge() { await initWails(); if (!WailsApp) throw new Error('桌面服务尚未就绪'); return WailsApp; }
export async function listAccountPools(provider) {
  const api = await bridge(); const data = await api.ListAccountPools(provider);
  return {
    pools: (data.pools || []).map(mapAccountPool),
    accounts: (data.accounts || []).map((account) => ({ channelId: Number(account.channel_id), channelName: account.channel_name, groupId: Number(account.group_id), groupName: account.group_name, enabled: Boolean(account.enabled), managed: Boolean(account.managed), authMode: account.auth_mode })),
    cooldowns: (data.cooldowns || []).map((state) => ({ channelId: Number(state.channel_id), groupId: Number(state.group_id), endpoint: state.endpoint, model: state.model, reason: state.reason, until: state.until, repair: Boolean(state.repair), probing: Boolean(state.probing) }))
  };
}
export async function saveAccountPool(pool) { const api = await bridge(); return Number(await api.SaveAccountPool(buildAccountPoolPayload(pool))); }
export async function deleteAccountPool(provider, id) { const api = await bridge(); await api.DeleteAccountPool(provider, Number(id)); }
export async function rotateAccountPoolKey(provider, id) { const api = await bridge(); const result = await api.RotateAccountPoolKey(provider, Number(id)); return { key: result.key, header: result.header }; }
export async function setPoolAccountEnabled(provider, channelId, groupId, enabled) { const api = await bridge(); await api.SetPoolAccountEnabled(provider, Number(channelId), Number(groupId), Boolean(enabled)); }

export async function checkPoolEndpoint(provider, id) { const api = await bridge(); await api.CheckPoolEndpoint(provider, Number(id)); }
