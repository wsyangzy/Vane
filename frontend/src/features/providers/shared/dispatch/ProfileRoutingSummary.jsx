import { ArrowUpRight, GitBranch, Layers, Search, Server } from 'lucide-react';
import Tooltip from '@/shared/ui/Tooltip.jsx';
import { ruleKindLabel, ruleTargetLabels } from './dispatchState.js';
import { searchVendors } from '../../codex/searchPolicy.js';

const fallback = value => value == null ? '跟随全局回退' : value ? '失败后回到默认编排' : '仅尝试指定目标';

export default function ProfileRoutingSummary({ profile, profiles, channels, accounts, policy }) {
  const resources = { profiles, channels, accounts };
  const path = item => item.mode === 'fallback' ? '默认编排失败后使用目标' : `优先使用目标 · ${fallback(item.fallbackToDefault ?? policy?.ruleFallbackToDefault)}`;
  const mode = item => item.mode === 'fallback' ? '兜底' : (item.fallbackToDefault ?? policy?.ruleFallbackToDefault) == null ? '优先 · 跟随全局' : (item.fallbackToDefault ?? policy?.ruleFallbackToDefault) ? '优先 · 可回退' : '仅指定目标';
  const search = profile.search;
  const vendor = searchVendors.find(item => item.value === search?.vendor);
  const metadata = item => <Tooltip asChild content={`${path(item)}；优先级 ${item.priority}`}><span className="dp-summary-meta" tabIndex={0}>{mode(item)}<span>P{item.priority}</span></span></Tooltip>;
  const targets = values => <ul className="dp-summary-targets" aria-label="路由目标">{values.length ? ruleTargetLabels(values, resources).map((label, index) => {
    const Icon = values[index].profileId || values[index].targetKind === 'profile' ? Layers : Server;
    return <li className="dp-route-target" key={index}><Icon size={11} aria-hidden="true" /><span>{label}</span></li>;
  }) : <li className="dp-target-empty">未配置目标</li>}</ul>;
  return <div className="dp-routing-summary" data-provider={profile.provider}>
    <section className="dp-summary-section" aria-label="匹配规则">
      <div className="dp-section-heading"><h4><GitBranch size={13} />匹配规则</h4><span>{profile.rules.length ? `${profile.rules.filter(rule => rule.enabled).length} / ${profile.rules.length} 条启用` : '未配置 · 使用默认编排'}</span></div>
      {profile.rules.length ? <ol className="dp-summary-rows">{profile.rules.map((rule, index) => {
        const condition = [rule.matchModel, rule.kind === 'non_stream' || rule.requireStream === false ? '仅非流式' : rule.requireStream ? '仅流式' : ''].filter(Boolean).join(' · ');
        return <li key={rule.id || index} className="dp-summary-row">
          <span className="dp-channel-rank">{String(index + 1).padStart(2, '0')}</span>
          <div className="dp-summary-route"><div className="dp-summary-condition"><Tooltip asChild content={ruleKindLabel(rule.kind)}><span className="dp-summary-kind">{rule.name || ruleKindLabel(rule.kind)}</span></Tooltip><strong>{condition || '无附加条件'}</strong>{!rule.enabled && <span className="dp-badge">已停用</span>}</div><span aria-hidden="true" className="text-slate-300">→</span>{targets(rule.targets)}</div>
          {metadata(rule)}
        </li>;
      })}</ol> : null}
    </section>
    {profile.provider === 'codex' && <section className="dp-summary-section dp-search-summary" data-search-mode={search?.mode || 'auto'} aria-label="搜索服务"><div className="dp-section-heading"><h4><Search size={13} />搜索服务</h4>{search?.mode === 'native' ? <span className="dp-badge dp-search-mode"><ArrowUpRight size={12} aria-hidden="true" />原生线路</span> : <span>{vendor?.label || search?.vendor || '未配置协议'} · {search?.hasAPIKey ? '凭证已配置' : search?.vendor === 'searxng' ? '凭证可选' : '未配置凭证'}</span>}</div>
      {search?.mode === 'native' ? <><dl className="dp-summary-facts"><div className="dp-fact-wide"><dt>转发目标</dt><dd>本方案参与渠道与分组</dd></div><div className="dp-fact-wide"><dt>搜索方式</dt><dd>原生协议直通</dd></div></dl><p className="dp-summary-description">仅使用本方案的 Codex 端点，支持查询、打开页面等原生操作。</p></> : <><dl className="dp-summary-facts"><div className="dp-fact-wide"><dt>接口地址</dt><dd>{search?.endpoint || '未配置'}</dd></div><div><dt>查询超时</dt><dd>{search?.timeoutMs ? `${search.timeoutMs / 1000} 秒` : '未配置'}</dd></div><div><dt>结果上限</dt><dd>{search?.maxResults ? `${search.maxResults} 条 / 查询` : '未配置'}</dd></div></dl><p className="dp-summary-description">{search?.mode === 'service' ? '使用独立搜索服务。' : '优先原生搜索，失败时回退到已配置的服务。'}{vendor?.description}</p></>}
    </section>}
  </div>;
}
