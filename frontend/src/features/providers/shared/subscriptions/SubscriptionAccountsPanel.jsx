import { useCallback, useEffect, useRef, useState } from 'react';
import { Check, ChevronDown, ChevronUp, Copy, ExternalLink, KeyRound, LoaderCircle, LogIn, Pencil, Power, RefreshCw, ShieldCheck, Trash2, Upload, X } from 'lucide-react';
import { Button, CustomSelect } from '@/shared/ui/index.js';
import { copyToClipboard, manageSubscriptionAccounts, openExternalURL } from '@/services/wails/index.js';

const FIELD = 'min-w-0 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20';
const ICON_BUTTON = 'rounded-lg p-2 text-slate-500 hover:bg-slate-100 hover:text-indigo-600 disabled:opacity-40';
const statusLabel = (account) => !account.enabled ? '已停用' : ({ ready: '凭证已保存', sign_in_required: '需要重新登录' }[account.status] || '状态未知');
const errorText = (error) => typeof error === 'string' ? error : error?.message || '操作失败，请重试';

export default function SubscriptionAccountsPanel({ provider, embedded = false, onBusyChange }) {
  const [accounts, setAccounts] = useState([]);
  const [expanded, setExpanded] = useState(false);
  const [busy, setBusy] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [name, setName] = useState('');
  const [loginMethod, setLoginMethod] = useState('browser');
  const [editing, setEditing] = useState(null);
  const [deleting, setDeleting] = useState(null);
  const [login, setLogin] = useState(null);
  const [loginCode, setLoginCode] = useState('');
  const [mode, setMode] = useState('');
  const [credentialJSON, setCredentialJSON] = useState('');
  const [accountEmail, setAccountEmail] = useState('');
  const [fileName, setFileName] = useState('');
  const [openError, setOpenError] = useState('');
  const [copied, setCopied] = useState(false);
  const openedLogin = useRef('');
  const fileInput = useRef(null);
  const mounted = useRef(true);
  const title = provider === 'codex' ? 'Codex' : 'Claude';

  useEffect(() => { onBusyChange?.(Boolean(busy)); }, [busy, onBusyChange]);

  const openLoginPage = useCallback(async (url) => {
    setOpenError('');
    try { await openExternalURL(url, { reportFailure: true }); }
    catch (reason) { if (mounted.current) setOpenError(errorText(reason)); }
  }, []);

  useEffect(() => {
    if (login?.state !== 'pending' || !login.url) return;
    const key = `${login.id}:${login.url}`;
    if (openedLogin.current === key) return;
    openedLogin.current = key;
    void openLoginPage(login.url);
  }, [login?.id, login?.state, login?.url, openLoginPage]);

  const reload = useCallback(async () => {
    const result = await manageSubscriptionAccounts(provider, 'list');
    if (mounted.current) { setAccounts(result.accounts); if (result.login) setLogin(result.login); }
  }, [provider]);

  useEffect(() => {
    mounted.current = true;
    reload().catch((reason) => { if (mounted.current) setError(errorText(reason)); });
    return () => { mounted.current = false; };
  }, [reload]);

  useEffect(() => {
    if (!login?.id || login.state !== 'pending') return;
    let active = true;
    let inflight = false;
    const timer = setInterval(async () => {
      if (inflight) return;
      inflight = true;
      try {
        const result = await manageSubscriptionAccounts(provider, 'login_status', { id: login.id });
        if (!active || !mounted.current) return;
        setLogin(result.login);
        if (result.login?.state === 'complete') { setNotice('账号已登录并保存，可在订阅分组中选择。'); await reload(); }
      } catch (reason) { if (active && mounted.current) setError(errorText(reason)); }
      finally { inflight = false; }
    }, 1500);
    return () => { active = false; clearInterval(timer); };
  }, [provider, login?.id, login?.state, reload]);

  const execute = async (action, input = {}, key = action) => {
    setBusy(key); setError(''); setNotice('');
    try {
      const result = await manageSubscriptionAccounts(provider, action, input);
      if (!mounted.current) return null;
      if (result.login) { setLogin(result.login); setExpanded(true); }
      if (!action.startsWith('login_') || result.login?.state === 'complete') await reload();
      if (action === 'login_complete' && result.login?.state === 'complete') setNotice('账号已登录并保存，可在订阅分组中选择。');
      return result;
    } catch (reason) { if (mounted.current) setError(errorText(reason)); return null; }
    finally { if (mounted.current) setBusy(''); }
  };

  const importFile = async (event) => {
    const file = event.target.files?.[0]; event.target.value = '';
    if (!file) return;
    if (file.size > 1024 * 1024) { setError('凭证文件不能超过 1 MiB'); return; }
    try {
      const raw = await file.text();
      if (mounted.current) { setCredentialJSON(raw); setFileName(file.name); setError(''); }
    } catch { if (mounted.current) setError('凭证文件读取失败，请重新选择文件'); }
  };

  const cancelLogin = async () => {
    const result = await execute('login_cancel', { id: login.id });
    if (result) { setLogin(null); setLoginCode(''); }
  };

  const beginLogin = async () => {
    setMode(''); setLoginCode(''); setOpenError(''); setCopied(false);
    setCredentialJSON(''); setAccountEmail(''); setFileName('');
    await execute('login_start', { name, login_method: loginMethod });
  };

  const startImport = async () => {
    if (login?.state === 'pending') {
      const result = await execute('login_cancel', { id: login.id });
      if (!result) return;
    }
    setLogin(null); setLoginCode(''); setMode('import'); setExpanded(true); setError(''); setNotice('');
  };

  const closeImport = () => { setMode(''); setCredentialJSON(''); setAccountEmail(''); setFileName(''); };
  const importCredential = async () => {
    if (new TextEncoder().encode(credentialJSON).length > 1024 * 1024) { setError('凭证不能超过 1 MiB'); return; }
    const result = await execute('import', { name, credential_json: credentialJSON, account_email: accountEmail });
    if (result) { closeImport(); setName(''); setNotice('凭证已加密导入，可在订阅分组中绑定该账号。'); }
  };

  return <section aria-label={`${title} 订阅账号`} className={embedded ? 'min-w-0' : 'mb-6 min-w-0 rounded-xl border border-slate-200 bg-white shadow-sm'}>
    <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
      {embedded ? <div className="flex items-center gap-2 text-sm font-semibold text-slate-800"><KeyRound size={17} className="text-indigo-600" />订阅账号<span className="rounded bg-slate-100 px-2 py-0.5 text-xs text-slate-500">{accounts.length}</span></div> : <button type="button" onClick={() => setExpanded(!expanded)} aria-expanded={expanded} className="flex items-center gap-2 rounded-lg text-sm font-semibold text-slate-800">
        <KeyRound size={17} className="text-indigo-600" />订阅账号<span className="rounded bg-slate-100 px-2 py-0.5 text-xs text-slate-500">{accounts.length}</span>{expanded ? <ChevronUp size={15} /> : <ChevronDown size={15} />}
      </button>}
      <div className="flex flex-wrap items-center gap-2">
        {provider === 'codex' && <div className="w-32"><CustomSelect value={loginMethod} options={[{ value: 'browser', label: '浏览器登录' }, { value: 'device', label: '设备码登录' }]} onChange={setLoginMethod} disabled={Boolean(busy) || login?.state === 'pending'} /></div>}
        <Button type="button" size="sm" variant="secondary" icon={LogIn} disabled={Boolean(busy) || login?.state === 'pending'} onClick={() => { if (provider === 'claude') { setMode('confirm'); setExpanded(true); } else void beginLogin(); }}>登录 {title}</Button>
        <Button type="button" size="sm" variant="secondary" icon={Upload} disabled={Boolean(busy)} onClick={startImport}>导入凭证</Button>
        <button type="button" aria-label="刷新订阅账号列表" disabled={Boolean(busy)} onClick={() => execute('list')} className={ICON_BUTTON}><RefreshCw size={15} /></button>
        <input ref={fileInput} type="file" accept=".json,application/json" className="hidden" aria-label="订阅凭证文件" onChange={importFile} />
      </div>
    </div>
    {(error || notice) && <div role={error ? 'alert' : 'status'} className={`mx-4 mb-3 break-words rounded-lg px-3 py-2 text-xs [overflow-wrap:anywhere] ${error ? 'bg-rose-50 text-rose-700' : 'bg-emerald-50 text-emerald-700'}`}>{error || notice}</div>}
    {(embedded || expanded) && <div className="space-y-3 border-t border-slate-100 p-4">
      <div className="flex flex-wrap items-center gap-3"><input aria-label="新订阅账号名称" value={name} onChange={(event) => setName(event.target.value)} disabled={Boolean(busy) || login?.state === 'pending'} placeholder="新账号名称（可选）" className={`${FIELD} w-full sm:w-64`} /><span className="text-xs text-slate-500">凭证以当前 Windows 用户加密保存；浏览器授权完成后自动添加账号。</span></div>
      {mode === 'confirm' && <div className="flex flex-wrap items-start gap-3 rounded-lg border border-indigo-100 bg-indigo-50/50 p-3">
        <ShieldCheck size={19} className="mt-0.5 shrink-0 text-indigo-600" /><div className="min-w-0 flex-1"><p className="text-sm font-medium text-slate-800">确认登录 Claude 订阅账号</p><p className="mt-1 text-xs leading-5 text-slate-500">Vane 使用官方 Claude Code 完成登录，并在本机加密保存凭证。确认后将在浏览器中打开官方授权页面。</p></div>
        <div className="flex flex-wrap gap-2"><Button size="sm" variant="ghost" icon={X} onClick={() => setMode('')}>取消</Button><Button size="sm" variant="secondary" icon={Upload} onClick={startImport}>改用导入</Button><Button size="sm" icon={LogIn} disabled={Boolean(busy)} onClick={beginLogin}>继续登录</Button></div>
      </div>}
      {mode === 'import' && <div className="space-y-3 rounded-lg border border-slate-200 bg-slate-50/60 p-3">
        <div className="flex flex-wrap items-center justify-between gap-2"><p className="flex items-center gap-2 text-sm font-medium text-slate-800"><Upload size={16} />导入 {title} 账号</p><Button size="sm" variant="ghost" icon={X} disabled={Boolean(busy)} onClick={closeImport}>取消</Button></div>
        <p className="text-xs leading-5 text-slate-500">选择或粘贴 {provider === 'codex' ? 'Codex 的 auth.json（ChatGPT 登录）' : 'Claude Code 的 .credentials.json'}，或 CLIProxyAPI 的对应账号 JSON。导入会保存文件中的凭证，后续由 Vane 自动刷新；与原工具共用凭证可能需要重新登录。</p>
        <div className="flex min-w-0 flex-wrap items-center gap-2"><Button size="sm" variant="secondary" icon={Upload} disabled={Boolean(busy)} onClick={() => fileInput.current?.click()}>选择文件</Button>{fileName && <span className="min-w-0 break-all text-xs text-slate-500">{fileName}</span>}</div>
        <textarea aria-label="订阅凭证 JSON" autoComplete="off" spellCheck={false} value={credentialJSON} onChange={(event) => { setCredentialJSON(event.target.value); setFileName(''); }} disabled={Boolean(busy)} placeholder="或将凭证 JSON 粘贴到这里" className={`${FIELD} min-h-32 w-full resize-y font-mono text-xs [overflow-wrap:anywhere]`} />
        {provider === 'claude' && <div className="space-y-1"><input aria-label="Claude 账号邮箱" type="email" value={accountEmail} onChange={(event) => setAccountEmail(event.target.value)} disabled={Boolean(busy)} placeholder="账号邮箱（原生凭证缺少身份时填写）" className={`${FIELD} w-full`} /><p className="text-xs leading-5 text-slate-500">文件没有账号 UUID 或邮箱时，请填写该凭证对应的邮箱，用于识别同一账号。</p></div>}
        <div className="flex justify-end"><Button size="sm" icon={Upload} disabled={Boolean(busy) || !credentialJSON.trim()} onClick={importCredential}>导入账号</Button></div>
      </div>}
      {login && <div className="min-w-0 space-y-2 rounded-lg border border-indigo-100 bg-indigo-50/50 p-3 text-xs text-slate-600">
        <div className="flex flex-wrap items-center justify-between gap-2"><span className="flex items-center gap-2">{login.state === 'pending' && <LoaderCircle size={15} className="animate-spin text-indigo-600" />}{login.state === 'pending' ? `请在浏览器中完成 ${title} 登录` : login.state === 'complete' ? '登录完成' : login.state === 'cancelled' ? '登录已取消' : '登录失败'}</span>
          {login.state === 'pending' ? <Button type="button" size="sm" variant="ghost" icon={X} disabled={Boolean(busy)} onClick={cancelLogin}>取消登录</Button> : <button type="button" aria-label="关闭登录状态" className={ICON_BUTTON} onClick={() => setLogin(null)}><X size={14} /></button>}
        </div>
        {login.error && <p className="text-rose-600">{login.error}</p>}
        {openError && login.state === 'pending' && <p role="alert" className="text-amber-700">{openError}</p>}
        {login.url && <><div className="flex min-w-0 items-center gap-2"><input readOnly aria-label="官方登录链接" value={login.url} onFocus={(event) => event.target.select()} className={`${FIELD} w-full flex-1 font-mono text-xs`} /><button type="button" className={ICON_BUTTON} aria-label={copied ? '登录链接已复制' : '复制登录链接'} onClick={async () => { try { if (await copyToClipboard(login.url)) setCopied(true); else setOpenError('复制失败，请选中上方链接自行复制'); } catch { setOpenError('复制失败，请选中上方链接自行复制'); } }}>{copied ? <Check size={16} /> : <Copy size={16} />}</button></div><div className="flex flex-wrap items-center gap-2"><Button type="button" size="sm" variant="ghost" icon={ExternalLink} onClick={() => openLoginPage(login.url)}>重新打开</Button><span className="text-slate-500">连接：{login.proxy || '直连'}（浏览器使用自身网络设置）</span>{login.state === 'pending' && <Button size="sm" variant="ghost" icon={Upload} disabled={Boolean(busy)} onClick={startImport}>改用导入</Button>}</div></>}
        {login.code && <p>设备验证码：<code className="select-all font-semibold text-indigo-700">{login.code}</code></p>}
        {login.state === 'pending' && (login.pasteCallback || login.pasteCode) && <><p className="leading-5 text-slate-500">{login.callbackWarning || (login.pasteCallback ? '若浏览器最后停在打不开的页面，请复制地址栏中的完整回调 URL，粘贴到下方完成登录。' : '请将官方页面显示的完整授权码粘贴到下方。')}</p><div className="flex flex-wrap gap-2"><input type="text" autoComplete="off" spellCheck={false} aria-label={login.pasteCallback ? '回调 URL' : '官方登录授权码'} value={loginCode} onChange={(event) => setLoginCode(event.target.value)} placeholder={login.pasteCallback ? '回调 URL' : '完整授权码（code#state）'} className={`${FIELD} flex-1`} /><Button type="button" size="sm" icon={Check} disabled={Boolean(busy) || !loginCode.trim()} onClick={async () => { const result = await execute('login_complete', { id: login.id, code: loginCode }); if (result) setLoginCode(''); }}>完成登录</Button></div></>}
      </div>}
      {accounts.length === 0 ? <p className="text-sm text-slate-500">还没有保存的订阅账号。可通过浏览器登录或导入凭证；客户端凭证透传可直接在分组中配置。</p> : <div className="space-y-2">{accounts.map((account) => <div key={account.id} className="flex min-w-0 flex-wrap items-center gap-3 rounded-lg border border-slate-200 p-3">
        <div className="min-w-0 flex-1">
          {editing?.id === account.id ? <div className="flex gap-2"><input aria-label="订阅账号名称" className={`${FIELD} w-full`} value={editing.name} onChange={(event) => setEditing({ ...editing, name: event.target.value })} /><button type="button" aria-label="保存账号名称" className={ICON_BUTTON} disabled={Boolean(busy) || !editing.name.trim()} onClick={async () => { if (await execute('update', { id: account.id, name: editing.name, enabled: account.enabled })) setEditing(null); }}><Check size={16} /></button><button type="button" aria-label="取消重命名" className={ICON_BUTTON} onClick={() => setEditing(null)}><X size={16} /></button></div> : <div className="flex flex-wrap items-center gap-2"><span className="break-words text-sm font-medium text-slate-800 [overflow-wrap:anywhere]">{account.name}</span><span className={`rounded px-1.5 py-0.5 text-[11px] ${account.enabled && account.status === 'ready' ? 'bg-emerald-50 text-emerald-700' : 'bg-amber-50 text-amber-700'}`}>{statusLabel(account)}</span>{account.plan && <span className="text-xs text-slate-500">{account.plan}</span>}</div>}
          <p className="mt-1 break-words text-xs text-slate-500 [overflow-wrap:anywhere]">{account.email || '凭证未提供账号邮箱'}{account.accountId && ` · ${account.accountId}`}</p>
          {account.expiresAt && !account.expiresAt.startsWith('0001-') && <p className="mt-1 text-xs text-slate-400">访问凭证到期：{new Date(account.expiresAt).toLocaleString()}，请求前自动刷新</p>}
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <button type="button" aria-label={`重命名 ${account.name}`} className={ICON_BUTTON} disabled={Boolean(busy)} onClick={() => setEditing({ id: account.id, name: account.name })}><Pencil size={15} /></button>
          <button type="button" aria-label={`刷新 ${account.name} 凭证`} className={ICON_BUTTON} disabled={Boolean(busy) || !account.enabled} onClick={async () => { if (await execute('refresh', { id: account.id }, account.id)) setNotice('凭证已刷新；额度暂停仍以真实请求验证结果为准。'); }}><RefreshCw size={15} /></button>
          <button type="button" aria-label={`${account.enabled ? '停用' : '启用'} ${account.name}`} className={`${ICON_BUTTON} ${account.enabled ? 'text-emerald-600' : ''}`} disabled={Boolean(busy)} onClick={() => execute('update', { id: account.id, name: account.name, enabled: !account.enabled })}><Power size={15} /></button>
          <button type="button" aria-label={`删除 ${account.name}`} className={`${ICON_BUTTON} hover:text-rose-600`} disabled={Boolean(busy)} onClick={() => setDeleting(account)}><Trash2 size={15} /></button>
        </div>
      </div>)}</div>}
      {deleting && <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-rose-200 bg-rose-50 p-3 text-xs text-rose-700"><p className="min-w-0 break-words">删除「{deleting.name}」的本地凭证后，引用它的订阅分组需重新选择账号。保留请求与额度历史。</p><div className="flex gap-2"><Button type="button" size="sm" variant="ghost" icon={X} onClick={() => setDeleting(null)}>取消</Button><Button type="button" size="sm" icon={Trash2} disabled={Boolean(busy)} onClick={async () => { if (await execute('delete', { id: deleting.id })) setDeleting(null); }}>确认删除</Button></div></div>}
      <p className="text-xs text-slate-500">Codex 浏览器登录由 Vane 接收官方回调；Claude 登录使用独立的官方 CLI 配置目录。设备码登录需要安装 Codex CLI。</p>
      <p className="text-xs text-slate-400">保存的账号在凭证分组中选择和切换；一个订阅账号对应一个独立调用方。此处刷新凭证不代表额度已恢复。</p>
    </div>}
  </section>;
}
