import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import {
  AlertCircle,
  Braces,
  ChevronDown,
  Check,
  Database,
  Eraser,
  Fingerprint,
  History,
  KeyRound,
  ListFilter,
  PencilLine,
  Plus,
  RefreshCw,
  Regex,
  Save,
  Search,
  ShieldCheck,
  Trash,
  X
} from 'lucide-react';
import { Button, ConfirmDialog, ErrorMessage, KPICard, LoadingSpinner, useToast } from '@components/ui';
import {
  clearPrivacySyntheticValues,
  createPrivacyRule,
  deletePrivacySyntheticValue,
  deletePrivacyRule,
  fetchPrivacySyntheticValues,
  fetchPrivacyConfig,
  updatePrivacyRule
} from '@utils/api.js';
import { REGEX_TEMPLATES } from './regexTemplates.js';

const RULE_TYPES = [
  {
    value: 'exact',
    label: '精准匹配',
    shortLabel: '精准',
    icon: KeyRound,
    tone: 'indigo',
    inputLabel: '精准值',
    placeholder: '输入需要精确替换的敏感值',
    helper: '只替换用户配置的具体值，大小写敏感。'
  },
  {
    value: 'regex',
    label: '正则匹配',
    shortLabel: '正则',
    icon: Regex,
    tone: 'amber',
    inputLabel: '正则表达式',
    placeholder: String.raw`\b(?:\+?86[-\s]?)?1[3-9]\d{9}\b`,
    helper: '使用 Go RE2 语法。内置识别以正则模板方式提供，可一键填入后按普通正则保存。'
  },
  {
    value: 'field',
    label: '字段匹配',
    shortLabel: '字段',
    icon: Braces,
    tone: 'sky',
    inputLabel: '字段名 / JSON Path',
    placeholder: 'password, access_token, $.auth.refresh_token',
    helper: '仅处理 JSON 请求体中的字符串字段值；不修改 key、数字、布尔或 null。'
  },
  {
    value: 'keyword',
    label: '关键词上下文',
    shortLabel: '关键词',
    icon: Search,
    tone: 'violet',
    inputLabel: '关键词',
    placeholder: '姓名, token, 联系人',
    helper: '匹配“关键词: 值”“关键词=值”这类文本上下文，只替换后面的值。'
  },
  {
    value: 'validator',
    label: '校验算法',
    shortLabel: '校验',
    icon: Fingerprint,
    tone: 'rose',
    inputLabel: '校验算法',
    placeholder: 'cn_id_card, bank_card_luhn, jwt',
    helper: '用格式候选 + 校验函数降低误伤，适合身份证、银行卡、JWT。'
  },
  {
    value: 'entropy',
    label: '高熵识别',
    shortLabel: '高熵',
    icon: ShieldCheck,
    tone: 'emerald',
    inputLabel: '最小长度',
    placeholder: '32',
    helper: '识别高随机性的长 token/secret，建议谨慎启用。'
  },
  {
    value: 'structured',
    label: '结构解析',
    shortLabel: '结构',
    icon: Database,
    tone: 'slate',
    inputLabel: '参数名 / Cookie 名',
    placeholder: 'access_token, sessionid, api_key',
    helper: '解析 URL query、form-urlencoded、Cookie 风格 key/value 文本，只替换 value。'
  }
];

const RULE_TYPE_MAP = RULE_TYPES.reduce((acc, item) => {
  acc[item.value] = item;
  return acc;
}, {});

const CREATABLE_RULE_TYPES = RULE_TYPES.filter((item) => item.value !== 'validator');

const RULE_TONE_ICON_CLASSES = {
  amber: 'bg-amber-50 text-amber-600',
  emerald: 'bg-emerald-50 text-emerald-600',
  indigo: 'bg-indigo-50 text-indigo-600',
  rose: 'bg-rose-50 text-rose-600',
  sky: 'bg-sky-50 text-sky-600',
  slate: 'bg-slate-100 text-slate-600',
  violet: 'bg-violet-50 text-violet-600'
};

const VALIDATOR_OPTIONS = [
  { value: 'cn_id_card', label: '身份证校验位', description: '识别 18 位中国大陆身份证号，并验证最后一位校验码。' },
  { value: 'bank_card_luhn', label: '银行卡 Luhn', description: '识别 13-19 位连续数字，并通过 Luhn 校验位后脱敏。' },
  { value: 'jwt', label: 'JWT 结构', description: '识别三段式 JWT，并校验 header 与 payload 可 Base64URL 解码。' }
];

const PSEUDONYM_REPLACEMENT_MODE = 'pseudonym';
const DEFAULT_RULE_CATEGORY = 'custom';

const PAGE_SECTIONS = [
  { value: 'rules', label: '规则配置', icon: ShieldCheck },
  { value: 'synthetic', label: '模拟值记录', icon: History }
];

const SYNTHETIC_KIND_LABELS = {
  api_key: 'API Key',
  bank_card: '银行卡',
  custom: '自定义',
  id_card: '身份证',
  name: '姓名',
  phone: '手机号',
  secret: '密钥',
  token: 'Token'
};

const createEmptyForm = (type = 'exact') => ({
  name: '',
  type,
  pattern_or_value: type === 'entropy' ? '32' : '',
  enabled: true
});

const getCategoryForForm = (sourceForm) => {
  if (sourceForm.type !== 'regex') {
    return DEFAULT_RULE_CATEGORY;
  }
  return REGEX_TEMPLATES.find((template) => template.pattern === sourceForm.pattern_or_value)?.category || DEFAULT_RULE_CATEGORY;
};

const splitRuleList = (value = '') => value
  .split(/[,，\n\r]+/)
  .map((item) => item.trim())
  .filter(Boolean);

const Toggle = ({ checked, onChange, disabled = false, label }) => (
  <button
    type="button"
    onClick={() => !disabled && onChange?.(!checked)}
    disabled={disabled}
    aria-pressed={checked}
    aria-label={label}
    className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border transition-colors ${
      checked ? 'border-emerald-500 bg-emerald-500' : 'border-slate-300 bg-slate-200'
    } ${disabled ? 'cursor-not-allowed opacity-50' : 'hover:shadow-sm'}`}
  >
    <span
      className={`inline-block h-4 w-4 rounded-full bg-white shadow-sm transition-transform ${
        checked ? 'translate-x-4' : 'translate-x-0.5'
      }`}
    />
  </button>
);

const RuleBadge = ({ children, tone = 'slate', dot = false }) => {
  const tones = {
    slate: 'bg-slate-100/70 text-slate-500 ring-slate-200',
    indigo: 'bg-indigo-50/80 text-indigo-600 ring-indigo-100',
    amber: 'bg-amber-50/80 text-amber-700 ring-amber-100',
    emerald: 'bg-emerald-50/80 text-emerald-700 ring-emerald-100',
    sky: 'bg-sky-50/80 text-sky-700 ring-sky-100',
    violet: 'bg-violet-50/80 text-violet-700 ring-violet-100',
    rose: 'bg-rose-50/80 text-rose-700 ring-rose-100'
  };
  const dotTones = {
    slate: 'bg-slate-400',
    indigo: 'bg-indigo-500',
    amber: 'bg-amber-500',
    emerald: 'bg-emerald-500',
    sky: 'bg-sky-500',
    violet: 'bg-violet-500',
    rose: 'bg-rose-500'
  };

  return (
    <span className={`inline-flex h-5 items-center gap-1 rounded-full px-2 text-[11px] font-medium leading-none ring-1 ring-inset ${tones[tone] || tones.slate}`}>
      {dot ? <span className={`h-1.5 w-1.5 rounded-full ${dotTones[tone] || dotTones.slate}`} /> : null}
      {children}
    </span>
  );
};

const getPatternDisplay = (rule) => {
  if (rule.type === 'exact') {
    return `${rule.pattern_masked || rule.patternMasked || '****'} (${rule.value_length ?? rule.valueLength ?? 0})`;
  }
  if (rule.type === 'entropy') {
    return `最小长度 ${rule.pattern_or_value || rule.pattern_masked || rule.patternMasked || '32'}`;
  }
  if (rule.type === 'validator') {
    const labels = splitRuleList(rule.pattern_or_value || rule.pattern_masked || rule.patternMasked)
      .map((value) => VALIDATOR_OPTIONS.find((item) => item.value === value)?.label || value);
    return labels.join('、') || '-';
  }
  return rule.pattern_or_value || rule.pattern_masked || rule.patternMasked || '-';
};

const RuleTypeTabs = ({ activeType, rules, onChange }) => (
  <div className="rounded-xl border border-slate-200 bg-white p-1 shadow-sm">
    <div className="grid grid-cols-2 gap-1 md:grid-cols-4 xl:grid-cols-7">
      {RULE_TYPES.map((item) => {
        const Icon = item.icon;
        const count = item.value === 'validator'
          ? new Set(
            rules
              .filter((rule) => rule.type === 'validator' && rule.enabled)
              .flatMap((rule) => splitRuleList(rule.pattern_or_value || rule.pattern_masked || rule.patternMasked))
          ).size
          : rules.filter((rule) => rule.type === item.value).length;
        const active = activeType === item.value;
        return (
          <button
            key={item.value}
            type="button"
            onClick={() => onChange(item.value)}
            className={`flex min-w-0 items-center gap-2 rounded-lg px-3 py-2 text-left transition-colors ${
              active ? 'bg-slate-900 text-white shadow-sm' : 'text-slate-600 hover:bg-slate-50 hover:text-slate-900'
            }`}
          >
            <Icon className="h-4 w-4 shrink-0" />
            <span className="min-w-0 flex-1 truncate text-xs font-semibold">{item.shortLabel}</span>
            <span className={`rounded-full px-1.5 py-0.5 text-[10px] font-semibold ${active ? 'bg-white/15 text-white' : 'bg-slate-100 text-slate-500'}`}>
              {count}
            </span>
          </button>
        );
      })}
    </div>
  </div>
);

const SectionTabs = ({ activeSection, onChange, syntheticCount }) => (
  <div className="mb-4 inline-flex rounded-xl border border-slate-200 bg-white p-1 shadow-sm">
    {PAGE_SECTIONS.map((section) => {
      const Icon = section.icon;
      const active = activeSection === section.value;
      return (
        <button
          key={section.value}
          type="button"
          onClick={() => onChange(section.value)}
          className={`inline-flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-semibold transition-colors ${
            active ? 'bg-slate-900 text-white shadow-sm' : 'text-slate-500 hover:bg-slate-50 hover:text-slate-900'
          }`}
        >
          <Icon className="h-4 w-4" />
          {section.label}
          {section.value === 'synthetic' && (
            <span className={`rounded-full px-1.5 py-0.5 text-[10px] font-semibold ${active ? 'bg-white/15 text-white' : 'bg-slate-100 text-slate-500'}`}>
              {syntheticCount}
            </span>
          )}
        </button>
      );
    })}
  </div>
);

const RuleTypeDropdown = ({ value, onChange }) => {
  const [open, setOpen] = useState(false);
  const dropdownRef = useRef(null);
  const selected = RULE_TYPE_MAP[value] || RULE_TYPES[0];
  const SelectedIcon = selected.icon;

  useEffect(() => {
    if (!open) return undefined;

    const handlePointerDown = (event) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target)) {
        setOpen(false);
      }
    };
    const handleKeyDown = (event) => {
      if (event.key === 'Escape') {
        setOpen(false);
      }
    };

    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [open]);

  const handleSelect = (type) => {
    onChange(type);
    setOpen(false);
  };

  return (
    <div ref={dropdownRef} className="relative">
      <div className="mb-1.5 flex items-center justify-between gap-3">
        <span className="text-xs font-semibold text-slate-600">规则类型</span>
        <span className="text-xs text-slate-400">选择本次新增规则的匹配方式</span>
      </div>
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        aria-haspopup="listbox"
        aria-expanded={open}
        className="flex w-full items-center gap-3 rounded-lg border border-slate-200 bg-white px-3 py-2.5 text-left shadow-sm transition focus:border-indigo-300 focus:outline-none focus:ring-2 focus:ring-indigo-100"
      >
        <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${RULE_TONE_ICON_CLASSES[selected.tone] || RULE_TONE_ICON_CLASSES.indigo}`}>
          <SelectedIcon className="h-4 w-4" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-sm font-semibold text-slate-900">{selected.label}</span>
          <span className="block truncate text-xs text-slate-500">{selected.helper}</span>
        </span>
        <ChevronDown className={`h-4 w-4 shrink-0 text-slate-400 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>

      {open && (
        <div
          role="listbox"
          className="absolute left-0 right-0 top-full z-20 mt-2 max-h-72 overflow-y-auto rounded-xl border border-slate-200 bg-white p-1.5 shadow-xl shadow-slate-900/10"
        >
          {CREATABLE_RULE_TYPES.map((item) => {
            const Icon = item.icon;
            const active = item.value === value;
            return (
              <button
                key={item.value}
                type="button"
                role="option"
                aria-selected={active}
                onClick={() => handleSelect(item.value)}
                className={`flex w-full items-start gap-3 rounded-lg px-3 py-2 text-left transition-colors ${
                  active ? 'bg-indigo-50 text-slate-900' : 'text-slate-600 hover:bg-slate-50'
                }`}
              >
                <span className={`mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg ${RULE_TONE_ICON_CLASSES[item.tone] || RULE_TONE_ICON_CLASSES.indigo}`}>
                  <Icon className="h-4 w-4" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-2 text-sm font-semibold">
                    {item.label}
                    {active && <Check className="h-3.5 w-3.5 text-indigo-600" />}
                  </span>
                  <span className="mt-0.5 line-clamp-2 text-xs leading-5 text-slate-500">{item.helper}</span>
                </span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
};

const formatRecordTime = (value) => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return String(value);
  }
  const pad = (part) => String(part).padStart(2, '0');
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
};

const getSyntheticRecordKey = (record) => `${record.kind || 'custom'}-${record.raw_hash || record.rawHash || record.replacement || record.value}`;

const SyntheticValuesPanel = ({ records, loading, deletingKey, onRefresh, onClear, onDelete }) => (
  <section className="space-y-3">
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 shadow-sm">
      <div className="flex min-w-0 items-center gap-2">
        <History className="h-4 w-4 shrink-0 text-slate-500" />
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-semibold text-slate-900">模拟值记录</h2>
            <RuleBadge tone="indigo">{records.length}</RuleBadge>
          </div>
          <p className="mt-0.5 truncate text-xs text-slate-500">记录原值与模拟值映射，用于跨会话复用和二次跳过。</p>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <Button variant="secondary" icon={RefreshCw} onClick={onRefresh} disabled={loading}>
          刷新
        </Button>
        <Button variant="danger" icon={Eraser} onClick={onClear} disabled={loading || records.length === 0}>
          清空记录
        </Button>
      </div>
    </div>

    {loading ? (
      <div className="rounded-xl border border-slate-200 bg-white py-10 shadow-sm">
        <LoadingSpinner text="加载模拟值记录..." />
      </div>
    ) : records.length === 0 ? (
      <div className="rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="flex min-h-32 flex-col items-center justify-center gap-3 px-4 py-8 text-center">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-slate-400">
            <History className="h-5 w-5" />
          </div>
          <div>
            <div className="text-sm font-semibold text-slate-500">暂无模拟值记录</div>
            <div className="mt-1 text-xs text-slate-400">请求命中规则并生成模拟值后会显示在这里</div>
          </div>
        </div>
      </div>
    ) : (
      <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="overflow-x-auto">
          <div className="min-w-[1080px]">
            <div className="grid grid-cols-[minmax(180px,1.1fr)_minmax(180px,1.1fr)_110px_145px_145px_120px_72px] gap-3 border-b border-slate-100 bg-slate-50 px-4 py-2 text-xs font-semibold text-slate-500">
              <span>原值</span>
              <span>模拟值</span>
              <span>类型</span>
              <span>创建时间</span>
              <span>最近复用</span>
              <span className="text-right">复用 / 跳过</span>
              <span className="text-right">操作</span>
            </div>
            <div className="divide-y divide-slate-100">
              {records.map((record) => {
                const recordKey = getSyntheticRecordKey(record);
                const rawValue = record.raw_value || record.rawValue || '-';
                const replacement = record.replacement || record.value || '-';
                const reuseCount = record.reuse_count ?? record.reuseCount ?? 0;
                const skipCount = record.skip_count ?? record.skipCount ?? 0;
                const deleting = deletingKey === recordKey;
                return (
                  <div
                    key={recordKey}
                    className="grid grid-cols-[minmax(180px,1.1fr)_minmax(180px,1.1fr)_110px_145px_145px_120px_72px] items-center gap-3 px-4 py-2.5 text-sm transition-colors hover:bg-slate-50"
                  >
                    <div className="min-w-0 truncate rounded-md bg-slate-50 px-2 py-1 font-mono text-xs text-slate-700" title={rawValue}>
                      {rawValue}
                    </div>
                    <div className="min-w-0 truncate rounded-md bg-indigo-50 px-2 py-1 font-mono text-xs text-indigo-700" title={replacement}>
                      {replacement}
                    </div>
                    <div>
                      <RuleBadge tone="indigo">{SYNTHETIC_KIND_LABELS[record.kind] || record.kind || '自定义'}</RuleBadge>
                    </div>
                    <div className="text-xs text-slate-500">{formatRecordTime(record.created_at || record.createdAt)}</div>
                    <div className="text-xs text-slate-500">{formatRecordTime(record.last_seen_at || record.lastSeenAt)}</div>
                    <div className="text-right text-xs font-semibold text-slate-700">{reuseCount} / {skipCount}</div>
                    <div className="flex justify-end">
                      <button
                        type="button"
                        onClick={() => onDelete(record)}
                        disabled={loading || deleting}
                        className="inline-flex h-7 w-8 items-center justify-center rounded-md text-slate-400 transition-colors hover:bg-rose-50 hover:text-rose-500 disabled:cursor-not-allowed disabled:opacity-50"
                        aria-label="删除模拟值记录"
                      >
                        <Trash className="h-4 w-4" strokeWidth={1.8} />
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      </div>
    )}
  </section>
);


const RuleGrid = ({ rules, typeMeta, onEdit, onDelete, onToggle }) => {
  const EmptyIcon = typeMeta.icon;
  const TypeIcon = typeMeta.icon;
  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 shadow-sm">
        <div className="flex min-w-0 items-center gap-2">
          <TypeIcon className="h-4 w-4 shrink-0 text-slate-500" />
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h2 className="text-sm font-semibold text-slate-900">{typeMeta.label}</h2>
              <RuleBadge tone={typeMeta.tone}>{rules.length}</RuleBadge>
            </div>
            <p className="mt-0.5 truncate text-xs text-slate-500">{typeMeta.helper}</p>
          </div>
        </div>
      </div>

      {rules.length === 0 ? (
        <div className="rounded-xl border border-slate-200 bg-white shadow-sm">
          <div className="flex min-h-32 flex-col items-center justify-center gap-3 px-4 py-8 text-center">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-slate-200 bg-slate-50 text-slate-400">
              <EmptyIcon className="h-5 w-5" />
            </div>
            <div>
              <div className="text-sm font-semibold text-slate-500">暂无{typeMeta.shortLabel}规则</div>
              <div className="mt-1 text-xs text-slate-400">点击右上角添加规则并选择类型</div>
            </div>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
          {rules.map((rule) => (
            <div key={rule.id} className="rounded-xl border border-slate-200 bg-white px-3 py-2.5 shadow-sm transition-colors hover:border-slate-300">
              <div className="flex items-center gap-3">
                <Toggle checked={rule.enabled} onChange={(checked) => onToggle(rule, checked)} label={`${rule.name} 启用状态`} />
                <div className="flex min-w-0 flex-1 items-center gap-2">
                  <span className="min-w-0 truncate text-[13px] font-medium leading-5 text-slate-800">{rule.name}</span>
                  <RuleBadge tone={rule.enabled ? 'emerald' : 'slate'} dot>{rule.enabled ? '启用' : '停用'}</RuleBadge>
                  <RuleBadge tone={typeMeta.tone}>{typeMeta.shortLabel}</RuleBadge>
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                  <button
                    type="button"
                    onClick={() => onEdit(rule)}
                    className="inline-flex h-7 w-8 items-center justify-center rounded-md text-slate-400 transition-colors hover:bg-indigo-50 hover:text-indigo-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-100"
                    aria-label="编辑规则"
                  >
                    <PencilLine className="h-4 w-4" strokeWidth={1.8} />
                  </button>
                  <button
                    type="button"
                    onClick={() => onDelete(rule)}
                    className="inline-flex h-7 w-8 items-center justify-center rounded-md text-slate-400 transition-colors hover:bg-rose-50 hover:text-rose-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-rose-100"
                    aria-label="删除规则"
                  >
                    <Trash className="h-4 w-4" strokeWidth={1.8} />
                  </button>
                </div>
              </div>
              <div className="mt-2 min-w-0 rounded-md bg-slate-50 px-2 py-1 font-mono text-[11px] leading-4 text-slate-600">
                <div className="truncate">{getPatternDisplay(rule)}</div>
              </div>
            </div>
          ))}
        </div>
      )}
    </section>
  );
};

const ValidatorRulePanel = ({ typeMeta, enabledValues, updatingValue, onToggle }) => {
  const TypeIcon = typeMeta.icon;
  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 shadow-sm">
        <div className="flex min-w-0 items-center gap-2">
          <TypeIcon className="h-4 w-4 shrink-0 text-slate-500" />
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <h2 className="text-sm font-semibold text-slate-900">校验算法</h2>
              <RuleBadge tone={typeMeta.tone}>{enabledValues.size}</RuleBadge>
            </div>
            <p className="mt-0.5 truncate text-xs text-slate-500">固定算法能力，只需要控制是否启用。</p>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
        {VALIDATOR_OPTIONS.map((option) => {
          const checked = enabledValues.has(option.value);
          const disabled = Boolean(updatingValue);
          return (
            <div key={option.value} className="rounded-xl border border-slate-200 bg-white px-3 py-3 shadow-sm transition-colors hover:border-slate-300">
              <div className="flex items-start gap-3">
                <Toggle
                  checked={checked}
                  disabled={disabled}
                  onChange={(nextChecked) => onToggle(option.value, nextChecked)}
                  label={`${option.label} 启用状态`}
                />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-[13px] font-semibold leading-5 text-slate-800">{option.label}</span>
                    <RuleBadge tone={checked ? 'emerald' : 'slate'} dot>{checked ? '启用' : '停用'}</RuleBadge>
                  </div>
                  <p className="mt-1 text-xs leading-5 text-slate-500">{option.description}</p>
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
};

const RuleModal = ({ open, mode, typeMeta, form, saving, error, onClose, onChange, onSubmit }) => {
  if (!open) return null;
  const isEdit = mode === 'edit';
  const TypeIcon = typeMeta.icon;
  const validatorValues = splitRuleList(form.pattern_or_value);
  const activeRegexTemplate = REGEX_TEMPLATES.find((template) => template.pattern === form.pattern_or_value);

  const updatePattern = (value) => onChange({ ...form, pattern_or_value: value });
  const applyRegexTemplate = (template) => {
    onChange({
      ...form,
      name: template.name,
      pattern_or_value: template.pattern
    });
  };
  const toggleValidator = (value) => {
    const values = new Set(validatorValues);
    if (values.has(value)) {
      values.delete(value);
    } else {
      values.add(value);
    }
    updatePattern(Array.from(values).join(','));
  };
  const changeRuleType = (type) => {
    if (isEdit || type === form.type) return;
    onChange({
      ...createEmptyForm(type),
      name: form.name,
      enabled: form.enabled
    });
  };

  return createPortal(
    <div className="fixed inset-0 z-[2147483647] flex items-start justify-center overflow-y-auto px-4 py-6 sm:items-center sm:py-8">
      <div className="fixed inset-0 bg-slate-900/35 backdrop-blur-sm" onClick={onClose} />
      <form
        onSubmit={onSubmit}
        className="relative flex max-h-[calc(100vh-3rem)] w-full max-w-xl flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl"
      >
        <div className="flex shrink-0 items-center justify-between border-b border-slate-100 px-5 py-4">
          <div className="flex items-center gap-3">
            <div className="rounded-lg bg-indigo-50 p-2 text-indigo-600">
              <TypeIcon className="h-5 w-5" />
            </div>
            <div>
              <h2 className="text-base font-semibold text-slate-900">{isEdit ? '编辑规则' : '添加规则'}</h2>
              <p className="text-xs text-slate-500">{typeMeta.helper}</p>
            </div>
          </div>
          <button type="button" onClick={onClose} className="rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="space-y-4 overflow-y-auto p-5">
          {error && (
            <div className="flex items-start gap-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
              <span className="min-w-0 break-words">{error}</span>
            </div>
          )}

          {!isEdit && <RuleTypeDropdown value={form.type} onChange={changeRuleType} />}

          <label className="block">
            <span className="text-xs font-semibold text-slate-600">规则名称</span>
            <input
              value={form.name}
              onChange={(event) => onChange({ ...form, name: event.target.value })}
              className="mt-1 w-full rounded-lg border border-slate-200 px-3 py-2 text-sm outline-none transition focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
              placeholder="例如：主账号 Token"
              required
            />
          </label>

          {form.type === 'regex' && (
            <div className="rounded-lg border border-slate-200 bg-slate-50 p-3">
              <div className="text-xs font-semibold text-slate-600">内置正则模板</div>
              <div className="mt-2 flex flex-wrap gap-2">
                {REGEX_TEMPLATES.map((template) => {
                  const active = activeRegexTemplate?.name === template.name;
                  return (
                    <button
                      key={template.name}
                      type="button"
                      aria-pressed={active}
                      onClick={() => applyRegexTemplate(template)}
                      className={`rounded-full border px-2.5 py-1 text-xs font-medium transition-colors ${
                        active
                          ? 'border-indigo-200 bg-indigo-50 text-indigo-700'
                          : 'border-slate-200 bg-white text-slate-600 hover:border-indigo-200 hover:bg-indigo-50 hover:text-indigo-700'
                      }`}
                    >
                      {template.name}
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {form.type === 'validator' ? (
            <div className="block">
              <span className="text-xs font-semibold text-slate-600">{typeMeta.inputLabel}</span>
              <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-3">
                {VALIDATOR_OPTIONS.map((option) => {
                  const active = validatorValues.includes(option.value);
                  return (
                    <button
                      key={option.value}
                      type="button"
                      onClick={() => toggleValidator(option.value)}
                      className={`flex items-center justify-center gap-2 rounded-lg border px-3 py-2 text-xs font-semibold transition-colors ${
                        active ? 'border-indigo-200 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-600 hover:bg-slate-50'
                      }`}
                    >
                      {active && <Check className="h-3.5 w-3.5" />}
                      {option.label}
                    </button>
                  );
                })}
              </div>
            </div>
          ) : (
            <label className="block">
              <span className="text-xs font-semibold text-slate-600">{typeMeta.inputLabel}</span>
              {form.type === 'exact' || form.type === 'entropy' ? (
                <input
                  type={form.type === 'exact' ? 'password' : 'number'}
                  min={form.type === 'entropy' ? 8 : undefined}
                  value={form.pattern_or_value}
                  onChange={(event) => updatePattern(event.target.value)}
                  className="mt-1 w-full rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm outline-none transition focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
                  placeholder={isEdit && form.type === 'exact' ? '留空保留原值' : typeMeta.placeholder}
                  required={!isEdit}
                />
              ) : (
                <textarea
                  value={form.pattern_or_value}
                  onChange={(event) => updatePattern(event.target.value)}
                  className="mt-1 min-h-24 w-full resize-y rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm outline-none transition focus:border-indigo-400 focus:ring-2 focus:ring-indigo-100"
                  placeholder={typeMeta.placeholder}
                  required={!isEdit}
                />
              )}
            </label>
          )}

          <div className="flex items-center justify-between rounded-lg border border-slate-200 bg-slate-50 px-3 py-2">
            <span className="text-sm font-medium text-slate-700">启用规则</span>
            <Toggle checked={form.enabled} onChange={(checked) => onChange({ ...form, enabled: checked })} label="启用规则" />
          </div>
        </div>

        <div className="flex shrink-0 justify-end gap-3 border-t border-slate-100 bg-slate-50 px-5 py-4">
          <button type="button" onClick={onClose} className="rounded-lg px-4 py-2 text-sm font-medium text-slate-600 hover:bg-slate-100">
            取消
          </button>
          <Button type="submit" icon={Save} loading={saving}>
            保存
          </Button>
        </div>
      </form>
    </div>,
    document.body
  );
};

const PrivacyPage = () => {
  const toast = useToast();
  const [rules, setRules] = useState([]);
  const [syntheticValues, setSyntheticValues] = useState([]);
  const [loading, setLoading] = useState(true);
  const [syntheticLoading, setSyntheticLoading] = useState(false);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [activeType, setActiveType] = useState('exact');
  const [modal, setModal] = useState({ open: false, mode: 'create', rule: null });
  const [form, setForm] = useState(createEmptyForm('exact'));
  const [formError, setFormError] = useState('');
  const [deleteTarget, setDeleteTarget] = useState(null);
  const [clearSyntheticConfirm, setClearSyntheticConfirm] = useState(false);
  const [syntheticDeleteTarget, setSyntheticDeleteTarget] = useState(null);
  const [syntheticDeletingKey, setSyntheticDeletingKey] = useState('');
  const [validatorUpdating, setValidatorUpdating] = useState('');
  const [activeSection, setActiveSection] = useState('rules');

  const loadConfig = useCallback(async () => {
    try {
      setLoading(true);
      setError('');
      const config = await fetchPrivacyConfig();
      setRules(config.rules || []);
    } catch (err) {
      setError(err?.message || '加载隐私防护配置失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadConfig();
  }, [loadConfig]);

  const loadSyntheticValues = useCallback(async () => {
    try {
      setSyntheticLoading(true);
      const records = await fetchPrivacySyntheticValues();
      setSyntheticValues(records || []);
    } catch (err) {
      toast.error(err?.message || '加载模拟值记录失败');
    } finally {
      setSyntheticLoading(false);
    }
  }, [toast]);

  useEffect(() => {
    if (activeSection === 'synthetic') {
      loadSyntheticValues();
    }
  }, [activeSection, loadSyntheticValues]);

  const activeMeta = RULE_TYPE_MAP[activeType] || RULE_TYPES[0];
  const visibleRules = useMemo(
    () => rules.filter((rule) => rule.type === activeType),
    [activeType, rules]
  );
  const validatorRules = useMemo(
    () => rules.filter((rule) => rule.type === 'validator'),
    [rules]
  );
  const enabledValidatorValues = useMemo(() => {
    const values = new Set();
    validatorRules
      .filter((rule) => rule.enabled)
      .forEach((rule) => {
        splitRuleList(rule.pattern_or_value || rule.pattern_masked || rule.patternMasked)
          .forEach((value) => values.add(value));
      });
    return values;
  }, [validatorRules]);

  const stats = useMemo(() => {
    const extended = rules.filter((rule) => !['exact', 'regex'].includes(rule.type)).length;
    return {
      total: rules.length,
      enabled: rules.filter((rule) => rule.enabled).length,
      regex: rules.filter((rule) => rule.type === 'regex').length,
      extended
    };
  }, [rules]);

  const overviewCards = useMemo(() => ([
    {
      title: '规则总数',
      value: stats.total,
      subText: '全部脱敏规则',
      icon: ListFilter,
      statusColor: 'bg-slate-100 text-slate-600'
    },
    {
      title: '已启用',
      value: stats.enabled,
      subText: '当前参与发送前处理',
      icon: ShieldCheck,
      statusColor: 'bg-emerald-50 text-emerald-600'
    },
    {
      title: '正则规则',
      value: stats.regex,
      subText: '含内置模板创建的规则',
      icon: Regex,
      statusColor: 'bg-amber-50 text-amber-600'
    },
    {
      title: '扩展策略',
      value: stats.extended,
      subText: '字段/关键词/校验/结构等',
      icon: Braces,
      statusColor: 'bg-sky-50 text-sky-600'
    }
  ]), [stats]);

  const openCreate = () => {
    const nextType = RULE_TYPE_MAP[activeType] && activeType !== 'validator' ? activeType : 'exact';
    setActiveType(nextType);
    setForm(createEmptyForm(nextType));
    setFormError('');
    setModal({ open: true, mode: 'create', rule: null });
  };

  const openEdit = (rule) => {
    setForm({
      name: rule.name || '',
      type: rule.type || 'exact',
      pattern_or_value: rule.type === 'exact' ? '' : (rule.pattern_or_value || rule.pattern_masked || rule.patternMasked || ''),
      enabled: Boolean(rule.enabled)
    });
    setFormError('');
    setModal({ open: true, mode: 'edit', rule });
  };

  const closeModal = () => {
    if (!saving) {
      setModal({ open: false, mode: 'create', rule: null });
      setFormError('');
    }
  };

  const buildInput = (sourceForm) => ({
    name: sourceForm.name.trim(),
    type: sourceForm.type,
    category: getCategoryForForm(sourceForm),
    pattern_or_value: sourceForm.pattern_or_value,
    replacement_mode: PSEUDONYM_REPLACEMENT_MODE,
    enabled: Boolean(sourceForm.enabled),
    priority: 100
  });

  const handleSubmit = async (event) => {
    event.preventDefault();
    setSaving(true);
    setFormError('');
    try {
      const input = buildInput(form);
      if (modal.mode === 'edit' && modal.rule?.id) {
        await updatePrivacyRule(modal.rule.id, input);
        toast.success('隐私规则已更新');
      } else {
        await createPrivacyRule(input);
        toast.success('隐私规则已创建');
        setActiveType(input.type);
      }
      setModal({ open: false, mode: 'create', rule: null });
      setFormError('');
      await loadConfig();
    } catch (err) {
      setFormError(err?.message || '保存规则失败');
    } finally {
      setSaving(false);
    }
  };

  const handleToggleRule = async (rule, checked) => {
    try {
      await updatePrivacyRule(rule.id, {
        name: rule.name,
        type: rule.type,
        category: rule.category || DEFAULT_RULE_CATEGORY,
        pattern_or_value: rule.type === 'exact' ? '' : (rule.pattern_or_value || rule.pattern_masked || rule.patternMasked || ''),
        replacement_mode: PSEUDONYM_REPLACEMENT_MODE,
        enabled: checked,
        priority: 100
      });
      setRules((prev) => prev.map((item) => item.id === rule.id ? { ...item, enabled: checked } : item));
    } catch (err) {
      toast.error(err?.message || '更新规则状态失败');
    }
  };

  const handleToggleValidatorAlgorithm = async (value, checked) => {
    const nextValues = new Set(enabledValidatorValues);
    if (checked) {
      nextValues.add(value);
    } else {
      nextValues.delete(value);
    }
    const orderedValues = VALIDATOR_OPTIONS
      .map((option) => option.value)
      .filter((optionValue) => nextValues.has(optionValue));
    const primaryRule = validatorRules.find((rule) => rule.name === '校验算法') || validatorRules[0];

    try {
      setValidatorUpdating(value);
      if (primaryRule) {
        await updatePrivacyRule(primaryRule.id, {
          name: '校验算法',
          type: 'validator',
          category: primaryRule.category || DEFAULT_RULE_CATEGORY,
          pattern_or_value: orderedValues.join(','),
          replacement_mode: PSEUDONYM_REPLACEMENT_MODE,
          enabled: orderedValues.length > 0,
          priority: primaryRule.priority || 100
        });
      } else if (orderedValues.length > 0) {
        await createPrivacyRule({
          name: '校验算法',
          type: 'validator',
          category: DEFAULT_RULE_CATEGORY,
          pattern_or_value: orderedValues.join(','),
          replacement_mode: PSEUDONYM_REPLACEMENT_MODE,
          enabled: true,
          priority: 100
        });
      }

      await Promise.all(
        validatorRules
          .filter((rule) => primaryRule && rule.id !== primaryRule.id && rule.enabled)
          .map((rule) => updatePrivacyRule(rule.id, {
            name: rule.name || '校验算法',
            type: 'validator',
            category: rule.category || DEFAULT_RULE_CATEGORY,
            pattern_or_value: rule.pattern_or_value || rule.pattern_masked || rule.patternMasked || '',
            replacement_mode: PSEUDONYM_REPLACEMENT_MODE,
            enabled: false,
            priority: rule.priority || 100
          }))
      );

      await loadConfig();
    } catch (err) {
      toast.error(err?.message || '更新校验算法状态失败');
    } finally {
      setValidatorUpdating('');
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await deletePrivacyRule(deleteTarget.id);
      toast.success('隐私规则已删除');
      setDeleteTarget(null);
      await loadConfig();
    } catch (err) {
      toast.error(err?.message || '删除规则失败');
    }
  };

  const handleClearSyntheticValues = async () => {
    try {
      await clearPrivacySyntheticValues();
      toast.success('模拟值记录已清空');
      setClearSyntheticConfirm(false);
      await loadSyntheticValues();
    } catch (err) {
      toast.error(err?.message || '清空模拟值记录失败');
    }
  };

  const handleDeleteSyntheticValue = async () => {
    if (!syntheticDeleteTarget) return;
    const rawHash = syntheticDeleteTarget.raw_hash || syntheticDeleteTarget.rawHash || '';
    const kind = syntheticDeleteTarget.kind || DEFAULT_RULE_CATEGORY;
    const recordKey = getSyntheticRecordKey(syntheticDeleteTarget);
    try {
      setSyntheticDeletingKey(recordKey);
      await deletePrivacySyntheticValue(kind, rawHash);
      toast.success('模拟值记录已删除');
      setSyntheticDeleteTarget(null);
      await loadSyntheticValues();
    } catch (err) {
      toast.error(err?.message || '删除模拟值记录失败');
    } finally {
      setSyntheticDeletingKey('');
    }
  };

  if (loading) {
    return <LoadingSpinner text="加载隐私防护配置..." />;
  }

  if (error) {
    return <ErrorMessage message={error} onRetry={loadConfig} />;
  }

  return (
    <div className="animate-fade-in">
      <div className="mb-8 flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div className="flex items-center gap-3">
          <div className="rounded-lg bg-slate-900 p-2 text-white shadow-lg">
            <ShieldCheck className="h-6 w-6" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-900">隐私防护</h1>
            <p className="mt-1 text-sm text-slate-500">发送上游前按已启用规则处理请求正文</p>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Button
            variant="secondary"
            icon={RefreshCw}
            onClick={activeSection === 'synthetic' ? loadSyntheticValues : loadConfig}
          >
            刷新
          </Button>
          {activeSection === 'rules' && <Button icon={Plus} onClick={openCreate}>添加规则</Button>}
        </div>
      </div>

      <div className="mb-6 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {overviewCards.map((card) => (
          <KPICard
            key={card.title}
            title={card.title}
            value={card.value}
            subText={card.subText}
            icon={card.icon}
            statusColor={card.statusColor}
            layout="side"
            size="lg"
          />
        ))}
      </div>

      <SectionTabs activeSection={activeSection} onChange={setActiveSection} syntheticCount={syntheticValues.length} />

      {activeSection === 'synthetic' ? (
        <SyntheticValuesPanel
          records={syntheticValues}
          loading={syntheticLoading}
          deletingKey={syntheticDeletingKey}
          onRefresh={loadSyntheticValues}
          onClear={() => setClearSyntheticConfirm(true)}
          onDelete={setSyntheticDeleteTarget}
        />
      ) : (
        <>
          <div className="mb-4">
            <RuleTypeTabs activeType={activeType} rules={rules} onChange={setActiveType} />
          </div>

          {activeType === 'validator' ? (
            <ValidatorRulePanel
              typeMeta={activeMeta}
              enabledValues={enabledValidatorValues}
              updatingValue={validatorUpdating}
              onToggle={handleToggleValidatorAlgorithm}
            />
          ) : (
            <RuleGrid
              rules={visibleRules}
              typeMeta={activeMeta}
              onEdit={openEdit}
              onDelete={setDeleteTarget}
              onToggle={handleToggleRule}
            />
          )}
        </>
      )}

      <RuleModal
        open={modal.open}
        mode={modal.mode}
        typeMeta={RULE_TYPE_MAP[form.type] || activeMeta}
        form={form}
        saving={saving}
        error={formError}
        onClose={closeModal}
        onChange={setForm}
        onSubmit={handleSubmit}
      />

      <ConfirmDialog
        isOpen={Boolean(deleteTarget)}
        title="删除隐私规则"
        message={`确定删除规则「${deleteTarget?.name || ''}」？`}
        confirmText="删除"
        variant="danger"
        onConfirm={handleDelete}
        onCancel={() => setDeleteTarget(null)}
      />

      <ConfirmDialog
        isOpen={clearSyntheticConfirm}
        title="清空模拟值记录"
        message="确定清空所有已登记的模拟值记录？清空后后续请求会重新生成新的同值关联映射。"
        confirmText="清空"
        variant="danger"
        onConfirm={handleClearSyntheticValues}
        onCancel={() => setClearSyntheticConfirm(false)}
      />

      <ConfirmDialog
        isOpen={Boolean(syntheticDeleteTarget)}
        title="删除模拟值记录"
        message={`确定删除「${syntheticDeleteTarget?.raw_value || syntheticDeleteTarget?.rawValue || ''} → ${syntheticDeleteTarget?.replacement || syntheticDeleteTarget?.value || ''}」？删除后该原值会在下次命中时重新生成映射。`}
        confirmText="删除"
        variant="danger"
        onConfirm={handleDeleteSyntheticValue}
        onCancel={() => setSyntheticDeleteTarget(null)}
      />
    </div>
  );
};

export default PrivacyPage;
