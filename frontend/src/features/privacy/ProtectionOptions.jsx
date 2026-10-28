import { useState } from 'react';
import { Check, LockKeyhole, ShieldCheck, SlidersHorizontal, Undo2, X } from 'lucide-react';
import { Button, CustomSelect } from '@/shared/ui/index.js';

export function ProtectionOptions({ settings, Dialog, saving, onSave, onClose }) {
  const [draft, setDraft] = useState(() => ({ ...settings }));
  const change = (key, value) => setDraft(current => ({ ...current, [key]: value }));
  const submit = async () => {
    if (await onSave(draft)) onClose();
  };
  return <Dialog title="运行设置" icon={SlidersHorizontal} busy={saving} onClose={onClose} footer={<>
    <Button variant="secondary" size="sm" icon={X} disabled={saving} onClick={onClose}>取消</Button>
    <Button size="sm" icon={Check} loading={saving} onClick={submit}>保存设置</Button>
  </>}>
    <div className="protection-options">
      <p className="protection-options-note"><ShieldCheck size={14} /><span>匹配内容、处理动作和例外放行统一在「防护规则」中管理。</span></p>
      <section><div className="privacy-section-heading"><h2><Undo2 size={14} />回复还原</h2><span>默认关闭</span></div>
        <label className="protection-check"><input type="checkbox" checked={draft.restoreResponses} disabled={saving} onChange={event => change('restoreResponses', event.target.checked)} /><span>在客户端回复和工具参数中还原原值</span></label>
        <p>使用会话内稳定替身，映射仅存内存，闲置 1 小时或重启后清空。没有会话标识时仅在本次请求内还原。</p>
        <label className="protection-check"><input type="checkbox" checked={draft.keepKeyFormat} disabled={saving || !draft.restoreResponses} onChange={event => change('keepKeyFormat', event.target.checked)} /><span><LockKeyhole size={12} />密钥替身尽量保留前缀、长度和分隔符</span></label>
        <p>适用于需要密钥格式的代码生成，需开启回复还原。签名、加密内容和思考原文保持原样。</p>
      </section>
      <section><div className="privacy-section-heading"><h2><SlidersHorizontal size={14} />扫描异常</h2><span>扫描上限 {Math.round(draft.scanMaxBytes / 1024 / 1024)} MB</span></div>
        <div className="privacy-policy-fields">
          {[
            { key: 'onError', label: '扫描出错时', options: [{ value: 'fail_open', label: '放行并记录' }, { value: 'fail_closed', label: '拒发请求' }] },
            { key: 'unsupportedPathAction', label: '路径未覆盖时', options: [{ value: 'warn', label: '放行并记录' }, { value: 'fail_closed', label: '拒发请求' }] },
            { key: 'overLimitAction', label: '超出扫描上限时', options: [{ value: 'scan_prefix', label: '扫描前缀，其余放行' }, { value: 'fail_closed', label: '拒发请求' }] }
          ].map(field => <div key={field.key}><span>{field.label}</span><CustomSelect ariaLabel={field.label} fullWidth disabled={saving} value={draft[field.key]} options={field.options} onChange={value => change(field.key, value)} /></div>)}
        </div>
        <p>放行会在防护记录中标明未扫描或截断；拒发在本机返回错误，不调用上游。</p>
      </section>
    </div>
  </Dialog>;
}
