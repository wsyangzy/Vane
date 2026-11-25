import { Info, Shrink } from 'lucide-react';
import { CustomSelect, Tooltip } from '@/shared/ui/index.js';

const options = [
  { value: 'summary', label: '普通摘要', description: '同一会话的所有渠道共用文本摘要，适合跨渠道路由。' },
  { value: 'native', label: '原生压缩', description: '保留原生加密压缩状态；参与路由的上游都需要支持该协议。' },
  { value: 'preserve', label: '保留客户端配置', description: '沿用客户端当前设置，不自动恢复或切换压缩方式。' },
];

export default function CompactionSettings({ mode, onChange, disabled }) {
  const option = options.find(item => item.value === mode) || options[0];
  return <fieldset aria-label="会话压缩设置" disabled={disabled} className="min-w-0 rounded-lg border border-slate-200 p-2.5">
    <legend className="px-1 text-xs font-medium text-slate-700">会话压缩方式</legend>
    <div className="grid min-w-0 items-center gap-2 md:grid-cols-[minmax(0,14rem)_minmax(0,1fr)] md:gap-4">
      <div className="flex min-w-0 items-center gap-2">
        <Shrink size={14} className="shrink-0 text-slate-400" />
        <CustomSelect options={options} value={option.value} onChange={onChange} disabled={disabled} fullWidth ariaLabel="会话压缩方式" className="min-w-0 flex-1 [&>button]:h-9 [&>button]:text-xs" />
        <Tooltip content="压缩方式由客户端在生成请求前决定，渠道选择由 Vane 调度方案管理。已有加密状态不会被转换，原生支持需用实际请求确认。">
          <button type="button" aria-label="查看会话压缩说明" className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-600 focus-visible:outline-2 focus-visible:outline-indigo-500"><Info size={14} /></button>
        </Tooltip>
      </div>
      <p className="min-w-0 text-xs leading-5 text-slate-600 [overflow-wrap:anywhere]">{option.description}</p>
    </div>
  </fieldset>;
}
