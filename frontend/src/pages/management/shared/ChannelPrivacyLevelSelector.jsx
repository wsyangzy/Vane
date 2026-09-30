// ============================================
// 渠道隐私级别（Claude / Codex 渠道编辑表单共用）
// 覆盖全局隐私模式在本渠道上的效果；全局模式为「关闭」时一律不生效。
// ============================================

import { Check } from 'lucide-react';

const CHANNEL_PRIVACY_LEVELS = [
  {
    value: '',
    label: '跟随全局',
    description: '按「隐私防护」页的全局模式处理。'
  },
  {
    value: 'third_party',
    label: '三方中转',
    description: '全局为「仅检测」时也强制脱敏。经第三方中转的请求，命中内容一律不以原文外发。'
  },
  {
    value: 'trusted',
    label: '可信渠道',
    description: '只检测、不改写，请求按原样发出。适用于官方直连等可信上游。'
  }
];

const ChannelPrivacyLevelSelector = ({ value, onChange, disabled = false }) => (
  <div>
    <span className="mb-1.5 block text-sm font-medium text-slate-700">隐私级别</span>
    <div className="grid gap-2 sm:grid-cols-3">
      {CHANNEL_PRIVACY_LEVELS.map((level) => {
        const active = (value || '') === level.value;
        return (
          <button
            key={level.value || 'default'}
            type="button"
            onClick={() => onChange(level.value)}
            disabled={disabled}
            aria-pressed={active}
            className={`rounded-lg border p-2.5 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-60 ${
              active ? 'border-emerald-300 bg-emerald-50 ring-1 ring-emerald-200' : 'border-slate-200 bg-white hover:border-slate-300'
            }`}
          >
            <div className="flex items-center gap-1.5">
              <span className={`text-sm font-semibold ${active ? 'text-emerald-800' : 'text-slate-700'}`}>{level.label}</span>
              {active && <Check className="ml-auto h-3.5 w-3.5 text-emerald-600" />}
            </div>
            <p className="mt-1 text-xs leading-relaxed text-slate-500">{level.description}</p>
          </button>
        );
      })}
    </div>
    <p className="mt-1.5 text-xs text-slate-400">全局模式为「关闭」时，各渠道的隐私级别都不生效。</p>
  </div>
);

export default ChannelPrivacyLevelSelector;
