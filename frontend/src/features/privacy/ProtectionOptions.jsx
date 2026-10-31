import { useState } from 'react';
import { Check, ShieldCheck, SlidersHorizontal, Undo2, X } from 'lucide-react';
import { Button, CustomSelect } from '@/shared/ui/index.js';

export function ProtectionOptions({ settings, Dialog, saving, onSave, onClose }) {
  const [draft, setDraft] = useState(() => ({ ...settings }));
  const [scanLimitMB, setScanLimitMB] = useState(() => String(settings.scanMaxBytes / 1024 / 1024));
  const [error, setError] = useState('');
  const change = (key, value) => setDraft(current => ({ ...current, [key]: value }));
  const submit = async () => {
    const scanMaxBytes = Math.round(Number(scanLimitMB) * 1024 * 1024);
    if (!Number.isSafeInteger(scanMaxBytes) || scanMaxBytes <= 0) {
      setError('扫描上限必须为大于 0 的有效数值。');
      return;
    }
    setError('');
    if (await onSave({ ...draft, scanMaxBytes })) onClose();
  };
  return <Dialog title="运行设置" icon={SlidersHorizontal} busy={saving} onClose={onClose} footer={<>
    <Button variant="secondary" size="sm" icon={X} disabled={saving} onClick={onClose}>取消</Button>
    <Button size="sm" icon={Check} loading={saving} onClick={submit}>保存设置</Button>
  </>}>
    <div className="protection-options">
      <p className="protection-options-note"><ShieldCheck size={14} /><span>所有渠道统一受顶部模式控制；脱敏规则可选择是否还原回复，额外放行规则不参与脱敏和还原。</span></p>
      <section><div className="privacy-section-heading"><h2><Undo2 size={14} />回复还原</h2><span>仅在脱敏模式下生效</span></div>
        <label className="protection-check"><input type="checkbox" className="h-3.5 w-3.5 accent-indigo-600" checked={draft.restoreResponses} disabled={saving} onChange={event => change('restoreResponses', event.target.checked)} /><span>自动还原回复中的敏感内容</span></label>
        <p>开启后，可在各规则中选择是否还原回复；关闭后均不还原。额外放行不参与还原。</p>
        <p>映射仅存本机内存，闲置 1 小时或重启后清空；没有会话标识时仅在本次请求内还原。</p>
      </section>
      <section><div className="privacy-section-heading"><h2><SlidersHorizontal size={14} />扫描处理</h2><span>不主动拒发请求</span></div>
        <label className="protection-scan-limit"><span>单次请求扫描上限</span><div><input aria-label="单次请求扫描上限（MB）" aria-invalid={Boolean(error)} type="number" min="0.000001" step="any" className="privacy-input rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-xs focus:border-indigo-400 focus:outline-none focus:ring-2 focus:ring-indigo-100" value={scanLimitMB} disabled={saving} onChange={event => { setScanLimitMB(event.target.value); setError(''); }} /><span>MB</span></div></label>
        {error && <p role="alert" className="protection-options-error">{error}</p>}
        <p>按单次请求中可扫描文本的累计大小计算，支持小数；默认 4 MB。</p>
        <div className="protection-scan-policies">
          {[
            { key: 'onError', label: '扫描出错时', options: [{ value: 'fail_open', label: '原文放行并记录' }, { value: 'passthrough', label: '放行，不记防护记录' }] },
            { key: 'unsupportedPathAction', label: '路径未覆盖时', options: [{ value: 'warn', label: '原文放行并记录' }, { value: 'passthrough', label: '放行，不记防护记录' }] },
            { key: 'overLimitAction', label: '超过扫描上限时', options: [{ value: 'scan_prefix', label: '扫描前缀，其余放行' }, { value: 'skip_scan', label: '跳过整次扫描并放行' }] }
          ].map(field => <div key={field.key}><span>{field.label}</span><CustomSelect ariaLabel={field.label} fullWidth size="sm" disabled={saving} value={draft[field.key]} options={field.options} onChange={value => change(field.key, value)} /></div>)}
        </div>
        <p>选择不记防护记录仅影响对应异常，请求追踪仍照常记录。还原映射超限时仍会记录并原文放行。</p>
      </section>
    </div>
  </Dialog>;
}
