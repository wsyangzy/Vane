import { getPrivacyActionLabel } from './privacyActivityLabels.js';
import './privacyActivityDetails.css';

const REPLACEMENTS = {
  reversible: '可还原替身', reversible_key: '可还原密钥格式替身',
  mask_only: '替身（仅脱敏）', mask_only_key: '密钥格式替身（仅脱敏）',
  placeholder: '占位符', pseudonym: '假名',
};

function getReplacementLabel(item, hit) {
  if (item.state !== 'redacted' || hit.action !== 'redact') return getPrivacyActionLabel(item, hit);
  const mode = item.restoreSkipped === 'restoration_disabled'
    ? hit.replacementMode.replace(/^reversible/, 'mask_only')
    : hit.replacementMode;
  return REPLACEMENTS[mode] || '已脱敏';
}

export function PrivacyActivityDetails({ item }) {
  return <div className="privacy-activity-expanded">
    {item.fieldHits.length ? <>
      <ul aria-label="字段处理明细">{item.fieldHits.map(hit => <li key={`${hit.path}/${hit.ruleId}`}>
        <div><strong>{hit.ruleName || '未命名规则'}</strong><code title={hit.path}>{hit.path}</code></div>
        <span>{getReplacementLabel(item, hit)}</span>
        <span>{hit.count} 处</span>
      </li>)}</ul>
      {item.fieldHits.some(hit => hit.path.split('.').includes('*')) && <p>路径中的 * 代表自定义字段。</p>}
      {item.fieldHitsLimited && <p>字段明细仅保留前 64 条，命中总数见记录摘要。</p>}
    </> : item.ruleHits.length ? <>
      <ul aria-label="规则命中明细">{item.ruleHits.map(hit => <li key={`${hit.ruleId}/${hit.ruleName}`}>
        <strong>{hit.ruleName || '未命名规则'}</strong><span>{getPrivacyActionLabel(item, hit)}</span><span>{hit.count} 处</span>
      </li>)}</ul>
      <p>此记录仅保留规则摘要，未记录命中的字段位置。</p>
    </> : <p>这条记录没有字段命中明细。</p>}
  </div>;
}
