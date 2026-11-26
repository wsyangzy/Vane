import { useLayoutEffect, useRef, useState } from 'react';
import { ArrowUpRight, GitBranch, Layers, Search, Server } from 'lucide-react';
import Tooltip from '@/shared/ui/Tooltip.jsx';
import { ruleKindLabel, ruleTargetLabels } from './dispatchState.js';
import { searchVendors } from '../../codex/searchPolicy.js';

const fallback = value => value == null ? '跟随全局回退' : value ? '失败后回到默认编排' : '仅尝试指定目标';

function RouteTargetsSummary({ values, labels }) {
  const containerRef = useRef(null);
  const measureRef = useRef(null);
  const moreRef = useRef(null);
  const [shown, setShown] = useState(labels.length);
  useLayoutEffect(() => {
    const measure = () => {
      const widths = [...measureRef.current.children].map(item => item.getBoundingClientRect().width);
      const gap = parseFloat(getComputedStyle(containerRef.current).columnGap) || 0;
      const available = containerRef.current.clientWidth;
      const total = widths.reduce((sum, width) => sum + width, 0) + Math.max(0, widths.length - 1) * gap;
      let count = widths.length;
      if (total > available) {
        const limit = available - moreRef.current.getBoundingClientRect().width - gap;
        count = 0;
        for (let used = 0; count < widths.length; count++) {
          const next = widths[count] + (count ? gap : 0);
          if (used + next > limit) break;
          used += next;
        }
      }
      setShown(previous => previous === count ? previous : count);
    };
    const observer = new ResizeObserver(measure);
    observer.observe(containerRef.current);
    observer.observe(measureRef.current);
    return () => observer.disconnect();
  }, [labels]);
  const hidden = Math.max(0, labels.length - shown);
  const chip = (label, index) => {
    const Icon = values[index]?.profileId || values[index]?.targetKind === 'profile' ? Layers : Server;
    return <span key={index} className="dp-route-target"><Icon size={11} aria-hidden="true" /><span>{label}</span></span>;
  };
  return <Tooltip asChild disabled={!hidden} content={<ul className="space-y-1 text-left" aria-label="全部路由目标">{labels.map((label, index) => <li key={index} className="break-all">{index + 1}. {label}</li>)}</ul>}>
    <div ref={containerRef} className="dp-summary-targets" tabIndex={hidden ? 0 : undefined} aria-label="路由目标">
      <span ref={measureRef} className="dp-target-measure dp-target-list" aria-hidden="true" inert>{labels.map(chip)}</span>
      <span ref={moreRef} className="dp-target-measure dp-route-more" aria-hidden="true">… +{labels.length}</span>
      {labels.length ? <>{labels.slice(0, shown).map(chip)}{hidden > 0 && <span className="dp-route-more" aria-label={`${hidden} 个目标已省略`}>… +{hidden}</span>}</> : <span className="dp-target-empty">未配置目标</span>}
    </div>
  </Tooltip>;
}

export default function ProfileRoutingSummary({ profile, profiles, channels, accounts, policy }) {
  const resources = { profiles, channels, accounts };
  const path = item => item.mode === 'fallback' ? '默认编排失败后使用目标' : `优先使用目标 · ${fallback(item.fallbackToDefault ?? policy?.ruleFallbackToDefault)}`;
  const mode = item => item.mode === 'fallback' ? '兜底' : (item.fallbackToDefault ?? policy?.ruleFallbackToDefault) == null ? '优先 · 跟随全局' : (item.fallbackToDefault ?? policy?.ruleFallbackToDefault) ? '优先 · 可回退' : '仅指定目标';
  const search = profile.search;
  const vendor = searchVendors.find(item => item.value === search?.vendor);
  const metadata = item => <Tooltip asChild content={`${path(item)}；优先级 ${item.priority}`}><span className="dp-summary-meta" tabIndex={0}>{mode(item)}<span>P{item.priority}</span></span></Tooltip>;
  const targets = values => <RouteTargetsSummary values={values} labels={ruleTargetLabels(values, resources)} />;
  return <div className="dp-routing-summary" data-provider={profile.provider}>
    <section className="dp-summary-section" aria-label="匹配规则">
      <div className="dp-section-heading"><h4><GitBranch size={13} />匹配规则</h4><span>{profile.rules.length ? `${profile.rules.filter(rule => rule.enabled).length} / ${profile.rules.length} 条启用` : '未配置 · 使用默认编排'}</span></div>
      {profile.rules.length ? <ol className="dp-summary-rows">{profile.rules.map((rule, index) => {
        const condition = [rule.matchModel, rule.kind === 'non_stream' || rule.requireStream === false ? '仅非流式' : rule.requireStream ? '仅流式' : ''].filter(Boolean).join(' · ');
        return <li key={rule.id || index} className="dp-summary-row">
          <span className="dp-channel-rank">{String(index + 1).padStart(2, '0')}</span>
          <div className="dp-summary-route"><Tooltip asChild content={`${rule.name || ruleKindLabel(rule.kind)}（${ruleKindLabel(rule.kind)}）：${condition || '无附加条件'}${!rule.enabled ? '；已停用' : ''}`}><div className="dp-summary-condition" tabIndex={0}><span className="dp-summary-kind">{rule.name || ruleKindLabel(rule.kind)}</span><strong>{condition || '无附加条件'}</strong>{!rule.enabled && <span className="dp-badge">已停用</span>}</div></Tooltip><span aria-hidden="true" className="text-slate-300">→</span>{targets(rule.targets)}</div>
          {metadata(rule)}
        </li>;
      })}</ol> : null}
    </section>
    {profile.provider === 'codex' && <section className="dp-summary-section dp-search-summary" data-search-mode={search?.mode || 'auto'} aria-label="搜索服务"><div className="dp-section-heading"><h4><Search size={13} />搜索服务</h4>{search?.mode === 'native' ? <span className="dp-badge dp-search-mode"><ArrowUpRight size={12} aria-hidden="true" />原生线路</span> : <span>{vendor?.label || search?.vendor || '未配置协议'} · {search?.hasAPIKey ? '凭证已配置' : search?.vendor === 'searxng' ? '凭证可选' : '未配置凭证'}</span>}</div>
      {search?.mode === 'native' ? <><dl className="dp-summary-facts"><div className="dp-fact-wide"><dt>转发目标</dt><dd>本方案参与渠道与分组</dd></div><div className="dp-fact-wide"><dt>搜索方式</dt><dd>原生协议直通</dd></div></dl><p className="dp-summary-description">仅使用本方案的 Codex 端点，支持查询、打开页面等原生操作。</p></> : <><dl className="dp-summary-facts"><div className="dp-fact-wide"><dt>接口地址</dt><dd>{search?.endpoint || '未配置'}</dd></div><div><dt>查询超时</dt><dd>{search?.timeoutMs ? `${search.timeoutMs / 1000} 秒` : '未配置'}</dd></div><div><dt>结果上限</dt><dd>{search?.maxResults ? `${search.maxResults} 条 / 查询` : '未配置'}</dd></div></dl><p className="dp-summary-description">{search?.mode === 'service' ? '使用独立搜索服务。' : '优先原生搜索，失败时回退到已配置的服务。'}{vendor?.description}</p></>}
    </section>}
  </div>;
}
