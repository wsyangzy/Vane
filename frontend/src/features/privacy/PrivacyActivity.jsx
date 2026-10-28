import { useState } from 'react';
import { Activity, Filter, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';

const STATES = { redacted: '已脱敏', detected: '仅检测到', blocked: '已拒发', clean: '扫描无命中', disabled: '防护关闭', skipped: '未完整扫描', truncated: '扫描已截断', unknown: '无扫描记录' };
const REASONS = { skipped_unsupported_path: '路径未覆盖', skipped_empty_body: '空正文', skipped_non_text_body: '非文本正文', skipped_no_rules: '没有启用的防护规则', skipped_no_text: '没有可扫描的文本字段', scan_error: '扫描失败，按设置放行', scan_truncated: '超过扫描上限', privacy_scan_failed: '扫描失败', encoded_response: '压缩响应未还原', response_too_large: '响应过大，停止还原', large_stream_event: '超大流事件仅还原完整替身', mapping_capacity: '映射容量已满，新增值使用固定占位符' };
const formatTime = value => { const date = new Date(value); return Number.isNaN(date.getTime()) ? '—' : date.toLocaleTimeString('zh-CN', { hour12: false }); };

export function PrivacyActivity({ activity, Dialog, onClose }) {
  const [onlyIssues, setOnlyIssues] = useState(false);
  const visible = activity.filter(item => !onlyIssues || ['blocked', 'skipped', 'truncated'].includes(item.state) || item.truncated || item.restoreSkipped);
  return <Dialog title="最近防护记录" icon={Activity} onClose={onClose} footer={<Button variant="secondary" size="sm" icon={X} onClick={onClose}>关闭</Button>}>
    <div className="privacy-activity-heading"><p>最近 100 次请求 · 摘要最多保留 30 天 / 5,000 条，不含正文和命中原文。</p><Button size="sm" variant={onlyIssues ? 'secondary' : 'ghost'} icon={Filter} onClick={() => setOnlyIssues(!onlyIssues)}>{onlyIssues ? '显示全部' : '只看异常'}</Button></div>
    <div className="privacy-activity-counts">{[['redacted', '已脱敏'], ['detected', '仅检测到'], ['blocked', '已拒发'], ['issues', '扫描 / 还原异常']].map(([state, label]) => <div key={state}><span>{label}</span><strong>{activity.filter(item => state === 'issues' ? ['skipped', 'truncated'].includes(item.state) || item.truncated || item.restoreSkipped : item.state === state).length}</strong></div>)}</div>
    <div className="privacy-activity-list">{visible.length ? visible.map(item => <article key={item.id}>
      <div className="privacy-activity-id"><time>{formatTime(item.createdAt)}</time><strong>{item.provider === 'claude' ? 'Claude' : item.provider === 'codex' ? 'Codex' : item.provider || '—'}</strong><code>{item.requestId || '—'}</code></div>
      <div className="privacy-activity-result"><span className="privacy-activity-state" data-state={item.state}>{STATES[item.state] || STATES.unknown}</span><span>命中 {item.hitCount} 处</span><span>{item.scanDurationMs.toFixed(1)} ms</span>{item.restoredCount > 0 && <span>还原 {item.restoredCount} 处</span>}{item.truncated && item.state !== 'truncated' && <span className="privacy-activity-warning">仅扫描前缀</span>}</div>
      <div className="privacy-activity-detail"><span>{item.path}</span><span>{item.ruleHits.map(hit => `${hit.ruleName} ×${hit.count}`).join(' · ')}</span></div>
      {(item.skippedReason || item.restoreSkipped) && <p className="privacy-activity-warning">{[item.skippedReason, item.restoreSkipped].filter(Boolean).map(reason => REASONS[reason] || (reason.startsWith('scan_error:') ? '扫描失败，按设置放行' : reason)).join(' · ')}</p>}
    </article>) : <p className="privacy-activity-empty">{onlyIssues ? '最近记录中暂无异常' : '尚无防护记录，发起请求后点击页面刷新即可查看'}</p>}</div>
  </Dialog>;
}
