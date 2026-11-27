import { ArrowUpRight, CheckCircle2, Eye, EyeOff, LoaderCircle, Search, Trash2, WandSparkles } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { CustomSelect } from '@/shared/ui/index.js';
import { getCodexSearchAPIKey } from '@/services/wails/dispatch.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';
import { defaultSearchPolicy, searchVendors } from '../searchPolicy.js';

const choices = [
  { value: 'auto', label: '自动选择', icon: WandSparkles, description: '优先使用原生搜索，失败后从本方案已配置的服务中回退。', path: '/v1/alpha/search', badge: '默认' },
  { value: 'native', label: '原生协议转发', icon: ArrowUpRight, description: '仅使用本方案的 Codex 端点。支持查询与打开页面等原生操作。', path: '/v1/alpha/search/native' },
  { value: 'service', label: '搜索服务适配', icon: Search, description: '仅使用独立搜索服务。查询能力依所选协议提供。', path: '/v1/alpha/search/service' }
];
const control = 'h-8 w-full min-w-0 rounded-lg border border-slate-200 bg-white px-2.5 text-xs outline-none focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100 disabled:opacity-50';

export default function ProfileSearchEditor({ profileId = 0, savedSearch, value, onChange, disabled = false }) {
  const search = { ...defaultSearchPolicy(), ...value };
  const vendor = searchVendors.find(item => item.value === search.vendor);
  const storedKeyExists = id => search.configuredVendors?.includes(id) || (savedSearch?.vendor === id && savedSearch.hasAPIKey);
  const draftKeyFor = id => search.apiKeys?.[id] ?? (search.vendor === id ? search.apiKey : undefined);
  const vendorOptions = searchVendors.map(option => {
    const id = option.value;
    const savedEndpoint = (savedSearch?.vendor === id ? savedSearch.endpoint : savedSearch?.serviceEndpoints?.[id]) ?? option.endpoint;
    const endpoint = (search.vendor === id ? search.endpoint : search.serviceEndpoints?.[id] ?? option.endpoint).trim();
    const draftKey = draftKeyFor(id);
    const keyRequired = id !== 'searxng';
    let badge = '未配置';
    if (keyRequired && draftKey === '') badge = '待删除';
    else if (endpoint && (!keyRequired || draftKey || storedKeyExists(id))) {
      badge = draftKey !== undefined || endpoint !== savedEndpoint ? '待保存' : '已配置';
    } else if (!keyRequired && savedEndpoint) badge = '待移除';
    return { ...option, badge };
  });
  const hasSavedKey = storedKeyExists(search.vendor);
  const savedKeyLength = search.apiKeyLengths?.[search.vendor] ?? savedSearch?.apiKeyLengths?.[search.vendor];
  const savedKeyPlaceholder = savedKeyLength > 0 ? '•'.repeat(savedKeyLength) : '已保存，点击查看';
  const apiKey = draftKeyFor(search.vendor);
  const pendingDeletion = apiKey === '';
  const [showKey, setShowKey] = useState(false);
  const [revealedKey, setRevealedKey] = useState(null);
  const [loadingKey, setLoadingKey] = useState(false);
  const [keyError, setKeyError] = useState('');
  const keyRequest = useRef(0);
  useEffect(() => () => { keyRequest.current++; }, []);
  const update = patch => onChange({ ...search, ...patch });
  const updateKey = next => {
    const apiKeys = { ...search.apiKeys };
    if (next === undefined) delete apiKeys[search.vendor];
    else apiKeys[search.vendor] = next;
    update({ apiKey: undefined, apiKeys });
  };
  const clearKeyView = () => {
    keyRequest.current++;
    setRevealedKey(null); setShowKey(false); setLoadingKey(false); setKeyError('');
  };
  const toggleKey = async () => {
    setKeyError('');
    if (showKey) { clearKeyView(); return; }
    if (apiKey !== undefined) { setShowKey(true); return; }
    const request = ++keyRequest.current;
    setLoadingKey(true);
    try {
      const key = await getCodexSearchAPIKey(profileId, search.vendor);
      if (request !== keyRequest.current) return;
      if (!key) throw new Error('当前协议未保存 API Key，请填写后保存');
      // Revealing a stored credential must not turn it into a replacement draft.
      setRevealedKey(key); setShowKey(true);
    } catch (error) {
      if (request === keyRequest.current) setKeyError(getErrorMessage(error, '读取搜索密钥失败'));
    } finally {
      if (request === keyRequest.current) setLoadingKey(false);
    }
  };
  const selectVendor = next => {
    const preset = searchVendors.find(item => item.value === next);
    const serviceEndpoints = { ...search.serviceEndpoints, [search.vendor]: search.endpoint };
    const endpoint = serviceEndpoints[next] ?? preset.endpoint;
    delete serviceEndpoints[next];
    clearKeyView();
    update({ vendor: next, endpoint, serviceEndpoints, apiKey: undefined, hasAPIKey: Boolean(storedKeyExists(next)) });
  };
  return <section className="min-w-0 rounded-xl border border-slate-200 bg-white" aria-label="Codex 搜索配置">
    <div className="flex min-h-10 items-center gap-2 border-b border-slate-100 px-3"><Search size={14} className="text-indigo-500" /><h3 className="text-xs font-semibold text-slate-700">Codex 搜索</h3><span className="ml-auto text-[11px] text-slate-400">随本方案保存</span></div>
    <div className="space-y-3 p-3">
      <div className="grid min-w-0 gap-2 sm:grid-cols-3" role="group" aria-label="搜索线路">
        {choices.map(({ value: mode, label, icon: Icon, description, path, badge }) => <button key={mode} type="button" aria-pressed={search.mode === mode} aria-label={label} disabled={disabled} onClick={() => { clearKeyView(); update({ mode }); }} className={`min-w-0 rounded-lg border p-3 text-left outline-none transition-colors focus-visible:ring-2 focus-visible:ring-indigo-400 disabled:opacity-50 ${search.mode === mode ? 'border-indigo-300 bg-indigo-50/60' : 'border-slate-200 bg-slate-50/40 hover:border-indigo-200'}`}>
          <span className="mb-1.5 flex items-center gap-1.5 text-xs font-medium text-slate-700"><Icon size={14} className="shrink-0 text-indigo-500" />{label}{badge && <span className="rounded bg-white px-1 text-[10px] text-slate-400">{badge}</span>}{search.mode === mode && <CheckCircle2 size={13} className="ml-auto shrink-0 text-indigo-500" />}</span>
          <span className="block text-[11px] leading-5 text-slate-500">{description}</span><code className="mt-2 block text-[10px] text-slate-400 [overflow-wrap:anywhere]">{path}</code>
        </button>)}
      </div>
      {search.mode !== 'native' && <div className="space-y-3 rounded-lg border border-slate-200 bg-slate-50/40 p-3">
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="min-w-0 space-y-1"><span className="text-[11px] text-slate-500">搜索服务协议</span><CustomSelect fullWidth ariaLabel="搜索服务协议" options={vendorOptions} value={search.vendor} onChange={selectVendor} disabled={disabled} /></div>
          <label className="min-w-0 space-y-1 text-[11px] text-slate-500"><span>完整接口地址</span><input aria-label="搜索服务接口地址" className={`${control} font-mono`} value={search.endpoint} placeholder={vendor?.endpoint || 'https://search.example.com/search'} disabled={disabled} onChange={event => update({ endpoint: event.target.value })} /></label>
          <label className="min-w-0 space-y-1 text-[11px] text-slate-500">
            <span>API Key{search.vendor === 'searxng' ? '（可选）' : ''}</span>
            <span className="relative block">
              <input aria-label="搜索服务 API Key" type={showKey ? 'text' : 'password'}
                className={`${control} pr-16 font-mono placeholder:text-slate-500`} autoComplete="new-password"
                value={apiKey ?? (showKey ? revealedKey : null) ?? ''}
                placeholder={pendingDeletion ? '保存后删除凭证' : hasSavedKey ? savedKeyPlaceholder : '填写搜索服务凭证'}
                disabled={disabled}
                onChange={event => { keyRequest.current++; setRevealedKey(null); setLoadingKey(false); setKeyError(''); updateKey(event.target.value || undefined); }} />
              <button type="button" aria-label={loadingKey ? '读取搜索密钥' : showKey ? '隐藏搜索密钥' : '显示搜索密钥'}
                aria-busy={loadingKey} disabled={disabled || loadingKey || pendingDeletion || (!apiKey && (!hasSavedKey || !profileId))}
                onClick={toggleKey} className="absolute right-8 top-1 inline-flex h-6 w-6 items-center justify-center rounded text-slate-400 hover:bg-slate-100 disabled:opacity-40">
                {loadingKey ? <LoaderCircle size={14} className="animate-spin" /> : showKey ? <EyeOff size={14} /> : <Eye size={14} />}
              </button>
              <button type="button" aria-label="删除已保存搜索密钥" title="保存时删除此协议的凭证"
                disabled={disabled || pendingDeletion || (!hasSavedKey && !apiKey)}
                onClick={() => { clearKeyView(); updateKey(hasSavedKey ? '' : undefined); }}
                className="absolute right-1 top-1 inline-flex h-6 w-6 items-center justify-center rounded text-slate-400 hover:bg-slate-100 disabled:opacity-40">
                <Trash2 size={13} />
              </button>
            </span>
            {keyError && <span role="alert" className="block text-rose-600 [overflow-wrap:anywhere]">{keyError}</span>}
          </label>
          <div className="grid grid-cols-2 gap-2"><label className="space-y-1 text-[11px] text-slate-500"><span>单次超时（秒）</span><input aria-label="搜索超时秒数" className={control} type="number" min="1" max="120" value={search.timeoutMs / 1000} disabled={disabled} onChange={event => update({ timeoutMs: event.target.value * 1000 })} /></label><label className="space-y-1 text-[11px] text-slate-500"><span>每条查询结果数</span><input aria-label="每条查询结果数" className={control} type="number" min="1" max="20" value={search.maxResults} disabled={disabled} onChange={event => update({ maxResults: event.target.value })} /></label></div>
        </div>
        <p className="text-[11px] leading-5 text-slate-500">{vendor?.description}</p>
        <p className="text-[11px] leading-5 text-slate-500">自动选择只尝试已保存配置且支持当前操作的服务，取到可用结果即停止。凭证留空则保留，修改或删除后需保存。</p>
        <p className="text-[11px] leading-5 text-slate-500">允许实时联网时，Tavily 和 Exa 可打开明确的网页地址。点击链接与页内查找由原生线路处理。</p>
      </div>}
      <p className="text-[11px] leading-5 text-slate-400">标准入口按本方案选择处理；显式原生／适配入口始终固定线路。搜索不执行文本模型映射或非流改道。</p>
    </div>
  </section>;
}
