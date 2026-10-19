import { useEffect, useState } from 'react';
import { Copy, KeyRound, Plus, RefreshCw, Trash2 } from 'lucide-react';
import { Button, CustomSelect } from '@components/ui';
import { manageSubscriptionAccounts } from '@utils/wailsApi.js';
import { generateLocalRelayKey, subscriptionStateLabel } from './subscriptionSettings.js';

const FIELD = 'w-full min-w-0 rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20';

export function SubscriptionStatus({ group }) {
  const pauses = group.subscriptionStatus || [];
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (!pauses.length) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [pauses.length]);
  return <div className="min-w-0 space-y-1 rounded-lg border border-indigo-100 bg-indigo-50/50 p-3 text-xs text-slate-600">
    <div className="flex flex-wrap items-center gap-2">
      <span className="font-medium text-indigo-700">官方订阅 · 绑定 v{group.subscription?.version || 1}</span>
      <span>{group.subscription?.accountId ? '托管账号' : '客户端凭证透传'}</span>
      <span>{group.subscription?.enabled ? '默认官方优先，按模型规则与备用授权选路' : '官方已手动暂停，仅使用已授权备用'}</span>
      <span className="rounded bg-amber-50 px-1.5 py-0.5 text-amber-700">官方验证待完成</span>
    </div>
    {pauses.map((pause, index) => <div key={`${pause.model}:${index}`} className="break-words [overflow-wrap:anywhere]">
      {pause.model === '*' ? '账号额度' : pause.model}：{subscriptionStateLabel(pause.state === 'paused' && new Date(pause.resumeNotBefore).getTime() <= now && group.subscription?.enabled ? 'due_unverified' : pause.state)}
      {pause.resumeNotBefore && ` · ${pause.resetKnown ? '上游重置时间' : '下次允许验证'} ${new Date(pause.resumeNotBefore).toLocaleString()}`}
      {new Date(pause.resumeNotBefore).getTime() > now && `（约 ${Math.ceil((new Date(pause.resumeNotBefore).getTime() - now) / 60000)} 分钟后）`}
    </div>)}
    {pauses.length === 0 && <div>当前没有已知额度暂停；实际目标以请求详情为准。</div>}
  </div>;
}

export default function SubscriptionSettings({ provider = 'claude', value, onChange, channels = [], localKeyConfigured, acknowledged, onAcknowledge, disabled }) {
  const [key, setKey] = useState('');
  const [accounts, setAccounts] = useState([]);
  const [accountsError, setAccountsError] = useState('');
  useEffect(() => { let active = true; manageSubscriptionAccounts(provider, 'list').then((result) => { if (active) setAccounts(result.accounts); }).catch(() => { if (active) setAccountsError('账号列表读取失败，可关闭表单后在订阅账号区域重试'); }); return () => { active = false; }; }, [provider]);
  const [error, setError] = useState('');
  const [generating, setGenerating] = useState(false);
  const binding = value || { enabled: true, backupTargets: [] };
  const update = (patch) => onChange({ ...binding, ...patch });
  const options = channels.flatMap((channel) => (channel.credentialGroups || channel.credential_groups || [])
    .filter((group) => (group.authMode || group.auth_mode) !== 'client_passthrough' && (group.token || group.apiKey || group.api_key || group.tokenMasked || group.apiKeyMasked || group.token_masked || group.api_key_masked))
    .map((group) => ({ value: `${channel.id}/${group.id}`, label: `${channel.name} / ${group.name}` })));
  const generate = async () => {
    setGenerating(true); setError('');
    try { const result = await generateLocalRelayKey(); setKey(result.key); update({ keyHash: result.hash }); onAcknowledge(false); }
    catch { setError('无法生成安全密钥，请在桌面应用中重试'); }
    finally { setGenerating(false); }
  };
  return <div className="min-w-0 space-y-4 rounded-xl border border-indigo-100 bg-indigo-50/30 p-4">
    <p className="text-xs leading-5 text-slate-600">每个分组接入一个独立订阅。可选择客户端既有登录，或绑定下方管理的独立账号。官方验证待完成。</p>
    <div className="space-y-2"><label className="text-sm font-medium text-slate-700">订阅凭证来源</label>
      <CustomSelect value={binding.accountId || ''} options={[
        { value: '', label: '客户端登录凭证透传（客户端负责刷新）' },
        ...accounts.filter((account) => account.enabled && account.status !== 'sign_in_required').map((account) => ({ value: account.id, label: `${account.name}${account.plan ? ` · ${account.plan}` : ''}` })),
        ...(binding.accountId && !accounts.some((account) => account.id === binding.accountId && account.enabled && account.status !== 'sign_in_required') ? [{ value: binding.accountId, label: '原绑定账号不可用，请重新选择' }] : [])
      ]} onChange={(accountId) => update({ accountId, action: '' })} disabled={disabled} />
      {accountsError && <p className="text-xs text-amber-700">{accountsError}</p>}
      <p className="text-xs text-slate-500">账号在页面右上方「订阅管理 → 订阅账号」中登录或导入；更换绑定后旧账号的额度状态不会沿用。</p>
    </div>
    <label className="flex items-center gap-2 text-sm text-slate-700"><input type="checkbox" checked={Boolean(binding.enabled)} onChange={(e) => update({ enabled: e.target.checked })} disabled={disabled} className="accent-indigo-600" />允许使用官方订阅</label>
    <div className="space-y-2">
      <Button type="button" variant="secondary" onClick={generate} disabled={disabled || generating}><KeyRound size={14} />{localKeyConfigured || binding.keyHash ? '轮换本地密钥' : '生成本地密钥'}</Button>
      <p className="text-xs text-slate-500">本地密钥用于识别调用方，与官方登录凭证分开配置。保存轮换配置后旧密钥失效。</p>
      {key && <div className="space-y-2">
        <div className="flex min-w-0 items-center gap-2"><code className="min-w-0 flex-1 select-all break-all rounded-lg border border-slate-200 bg-white p-2 text-xs">{key}</code><button type="button" aria-label="复制本地密钥" disabled={disabled} onClick={async () => { try { await navigator.clipboard.writeText(key); } catch { setError('复制失败，请手动复制并保存密钥'); } }} className="shrink-0 rounded-lg p-2 text-indigo-600 hover:bg-indigo-100"><Copy size={16} /></button></div>
        <label className="flex items-start gap-2 text-xs text-slate-700"><input type="checkbox" checked={Boolean(acknowledged)} onChange={(e) => onAcknowledge(e.target.checked)} className="mt-0.5 accent-indigo-600" />我已保存密钥，知道关闭弹窗后无法再次查看</label>
      </div>}
      {!key && binding.keyHash && !acknowledged && <p className="text-xs text-amber-700">新密钥尚未确认保存，请重新生成并保存。</p>}
      {error && <p role="alert" className="text-xs text-rose-600">{error}</p>}
    </div>
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2"><span className="text-sm font-medium text-slate-700">授权备用目标（按顺序）</span><button type="button" aria-label="添加备用目标" disabled={disabled || binding.backupTargets.length >= 16} onClick={() => update({ backupTargets: [...binding.backupTargets, { rowId: crypto.randomUUID(), channelId: 0, groupId: 0, upstreamModel: '' }] })} className="rounded-lg p-1.5 text-indigo-600 hover:bg-indigo-100 disabled:opacity-40"><Plus size={16} /></button></div>
      {binding.backupTargets.map((target, index) => {
        const replace = (patch) => update({ backupTargets: binding.backupTargets.map((item, i) => i === index ? { ...item, ...patch } : item) });
        return <div key={target.rowId || index} className="flex min-w-0 items-start gap-2 rounded-lg border border-slate-200 bg-white p-2">
          <div className="min-w-0 flex-1 space-y-2"><CustomSelect value={target.channelId && target.groupId ? `${target.channelId}/${target.groupId}` : ''} options={[{ value: '', label: '选择普通凭证分组' }, ...options]} onChange={(selected) => { const [channelId, groupId] = selected.split('/').map(Number); replace({ channelId, groupId }); }} disabled={disabled} /><input aria-label={`备用目标 ${index + 1} 上游模型`} className={FIELD} value={target.upstreamModel} onChange={(e) => replace({ upstreamModel: e.target.value })} placeholder="上游模型（留空沿用目标分组策略）" disabled={disabled} /></div>
          <button type="button" aria-label={`删除备用目标 ${index + 1}`} onClick={() => update({ backupTargets: binding.backupTargets.filter((_, i) => i !== index) })} disabled={disabled} className="rounded-lg p-2 text-slate-400 hover:bg-rose-50 hover:text-rose-600"><Trash2 size={14} /></button>
        </div>;
      })}
      {binding.backupTargets.length === 0 && <p className="text-xs text-slate-500">没有备用目标时，官方不可用将直接返回错误。</p>}
    </div>
    {binding.version > 0 && <div className="space-y-2 border-t border-indigo-100 pt-3">
      <div className="flex flex-wrap gap-2"><Button type="button" variant="secondary" disabled={disabled} onClick={() => update({ action: binding.action === 'verify' ? '' : 'verify' })}><RefreshCw size={13} />请求验证恢复</Button>{!binding.accountId && <Button type="button" variant="secondary" disabled={disabled} onClick={() => update({ action: binding.action === 'rebind' ? '' : 'rebind' })}><KeyRound size={13} />重新绑定订阅</Button>}</div>
      {binding.action && <p className="text-xs text-amber-700">{binding.action === 'verify' ? '保存后允许下一条真实请求验证；不会立刻发请求或标记额度恢复。' : '保存后视为新的订阅身份，原绑定的暂停与恢复事件不再适用。请确保客户端使用新的独立订阅。'}</p>}
    </div>}
    <p className="text-xs leading-5 text-slate-500">保存分组后，在页面右上方「订阅管理 → 客户端接入」选择该分组，准备官方端点并预览、复制或应用客户端配置。{provider === 'codex' ? '支持 models、responses 和原生 compact；接入配置使用 HTTP 传输。' : '可复制当前 PowerShell 窗口的临时启动命令，或应用到 settings.json。'}</p>

  </div>;
}
