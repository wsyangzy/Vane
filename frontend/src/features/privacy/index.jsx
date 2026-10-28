import Tooltip from '@/shared/ui/Tooltip.jsx';
import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import {
  AlertTriangle,
  Check,
  ChevronDown,
  Eye,
  FlaskConical,
  History,
  ListFilter,
  Minus,
  Package,
  PencilLine,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  ShieldOff,
  SlidersHorizontal,
  Trash,
  X
} from 'lucide-react';
import { Button, ConfirmDialog, CustomSelect, ErrorMessage, LoadingSpinner, useToast } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import {
  applyPrivacyPreset,
  clearPrivacyUnscannedPaths,
  createPrivacyRule,
  deletePrivacyRule,
  deletePrivacyRules,
  discardLegacyPrivacyRules,
  fetchPrivacyOverview,
  previewPrivacyRedaction,
  previewPrivacyRequest,
  setPrivacyRulesEnabled,
  updatePrivacyRule,
  updatePrivacySettings
} from '@/services/http/api.js';
import { REGEX_TEMPLATES } from './regexTemplates.js';
import { ProtectionOptions } from './ProtectionOptions.jsx';
import { PrivacyActivity } from './PrivacyActivity.jsx';
import './privacy.css';

const MODES = [
  {
    value: 'disabled',
    label: '关闭',
    icon: ShieldOff,
    description: '请求原样发出，不扫描、不改写。'
  },
  {
    value: 'detect',
    label: '仅检测',
    icon: Eye,
    description: '默认不改写；规则拦截与渠道设置仍生效。'
  },
  {
    value: 'redact',
    label: '脱敏',
    icon: ShieldCheck,
    description: '按规则与渠道设置替换命中内容。'
  }
];

const MATCH_TYPES = [
  { value: 'literal', label: '指定文本', helper: '填写项目名称、内部代号或其他指定文本。替换和检测区分大小写；放行忽略大小写。' },
  { value: 'prefix', label: '密钥前缀', helper: '填写固定前缀（3–64 字节），例如 sk-team-；匹配其后至少 8 位字母、数字、下划线或连字符。' },
  { value: 'regex', label: '正则', helper: 'Go RE2 语法。可用 (?P<redact>...) 命名捕获组只替换其中一段，保留字段名。' },
  { value: 'builtin', label: '内置', helper: '带校验位与卡组织画像的确定型 PII 匹配器，误报率远低于纯正则。' }
];

const REPLACEMENT_MODES = [
  { value: 'placeholder', label: '占位符', helper: '统一替换成固定文本，如 [手机号]。' },
  {
    value: 'pseudonym',
    label: '假名',
    helper: '替换成格式合法但虚构的值。同一请求内同值同假名，AI 仍能区分多个客户；不留任何映射记录。'
  }
];

const PSEUDONYM_KIND_LABELS = {
  name: '姓名',
  phone: '手机号',
  id_card: '身份证',
  bank_card: '银行卡',
  secret: '密钥',
  token: '令牌',
  api_key: 'API Key',
  custom: '通用'
};

// 内置匹配器的人类可读说明。
// pattern 字段存的是 builtin:xxx 内部标识，直接展示给用户没有意义。
const BUILTIN_MATCHERS = {
  'builtin:cn_mobile': {
    label: '中国大陆手机号',
    detail: '匹配运营商号段，支持 +86 前缀。命中区间前后紧邻数字时判为更长数字串的一部分，不计命中。'
  },
  'builtin:cn_id_card': {
    label: '中国身份证号',
    detail: '18 位格式 + 省级地址码白名单 + 合法出生日期 + 校验位验算，四项全过才算命中。'
  },
  'builtin:bank_card_luhn': {
    label: '银行卡号',
    detail: '13–19 位 + Luhn 校验 + 主流卡组织（Visa / 万事达 / 运通 / 大来 / Discover / JCB / 银联）的前缀与长度画像。'
  },
  'builtin:email': {
    label: '邮箱地址',
    detail: '排除 git@host 形式的仓库地址与 logo@2x.png 一类资源名。'
  },
  'builtin:cn_landline': {
    label: '中国座机号',
    detail: '0 开头区号 + 7~8 位号码，区号与号码之间须有分隔符或括号，可带分机号。'
  },
  'builtin:cn_passport': {
    label: '中国护照号',
    detail: 'E/G 开头的 9 位编号，且前方 32 字节内须出现「护照」或 passport 字样，避免误伤普通编号。'
  },
  'builtin:cn_credit_code': {
    label: '统一社会信用代码',
    detail: '18 位，按 GB 32100 校验位验算。'
  },
  'builtin:public_ipv4': {
    label: '公网 IPv4 地址',
    detail: '排除内网、回环、链路本地、CGNAT 与保留地址段，以及 1.2.3.4.5 一类版本号形态。'
  }
};

// 内置匹配器带校验位与卡组织画像，误报率远低于同类正则，因此不提供正则版本。
const describePattern = (rule) => {
  if (rule.matchType !== 'builtin') return rule.pattern;
  return BUILTIN_MATCHERS[rule.pattern]?.label || rule.pattern;
};

const SOURCE_LABELS = {
  builtin: '内置',
  preset: '预设',
  custom: '自定义'
};

const SOURCE_TONES = {
  builtin: 'border-indigo-200 bg-indigo-50 text-indigo-700',
  preset: 'border-sky-200 bg-sky-50 text-sky-700',
  custom: 'border-slate-200 bg-slate-50 text-slate-600'
};

// 与 @/shared/ui 的 Input 保持一致的输入控件样式（含 focus ring）。
// 原生控件默认没有聚焦态，混在自定义组件之间会很突兀。
const FIELD_CLASS =
  'privacy-input w-full min-w-0 rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-xs transition-colors ' +
  'focus:border-indigo-400 focus:outline-none focus:ring-2 focus:ring-indigo-100';

const FIELD_MONO_CLASS = FIELD_CLASS + ' font-mono text-xs';

// 弹窗动效对齐 RequestDetailModal：遮罩淡入，面板 zoom-in-95 + 淡入。
const OVERLAY_CLASS = 'absolute inset-0 bg-slate-900/20 backdrop-blur-sm animate-in fade-in duration-200';
const PANEL_CLASS =
  'privacy-dialog relative flex w-full min-w-0 flex-col overflow-hidden rounded-2xl bg-white shadow-2xl ring-1 ring-black/5 ' +
  'animate-in zoom-in-95 fade-in duration-200';

const PrivacyDialog = ({ title, icon: Icon, children, footer, busy = false, onClose }) => {
  const titleId = useId();
  const dialogRef = useRef(null);
  const closeRef = useRef(onClose);
  const busyRef = useRef(busy);
  useEffect(() => { closeRef.current = onClose; busyRef.current = busy; }, [onClose, busy]);
  useEffect(() => {
    const previousFocus = document.activeElement;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    const controls = () => [...dialogRef.current.querySelectorAll('button:not(:disabled), input:not(:disabled), textarea:not(:disabled), [tabindex="0"]')].filter(element => element.getClientRects().length > 0);
    (dialogRef.current.querySelector('input, textarea') || controls()[0])?.focus();
    const onKeyDown = event => {
      if (event.defaultPrevented || (!dialogRef.current?.contains(event.target) && event.target !== document.body)) return;
      if (document.querySelector('[role="listbox"]:not(.is-closing)')) return;
      if (event.key === 'Escape' && !busyRef.current) {
        event.preventDefault();
        closeRef.current();
      }
      if (event.key === 'Tab') {
        const items = controls();
        const first = items[0], last = items.at(-1);
        if ((event.shiftKey && document.activeElement === first) || (!event.shiftKey && document.activeElement === last)) {
          event.preventDefault();
          (event.shiftKey ? last : first)?.focus();
        }
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener('keydown', onKeyDown);
      previousFocus?.focus();
    };
  }, []);
  return renderModalPortal(<div className="fixed inset-0 z-[10000] flex items-center justify-center p-4">
    <div className={OVERLAY_CLASS} onClick={() => { if (!busy) onClose(); }} />
    <section ref={dialogRef} role="dialog" aria-modal="true" aria-busy={busy} aria-labelledby={titleId} className={`${PANEL_CLASS} max-w-2xl`}>
      <header className="privacy-dialog-header">
        <h2 id={titleId}>{Icon && <Icon size={16} />}{title}</h2>
        <button type="button" className="privacy-icon-button" aria-label={`关闭${title}`} disabled={busy} onClick={onClose}><X size={16} /></button>
      </header>
      <div className="privacy-dialog-body">{children}</div>
      <footer className="privacy-dialog-footer">{footer}</footer>
    </section>
  </div>);
};

const SOURCE_FILTERS = [
  { value: 'all', label: '全部' },
  { value: 'builtin', label: '内置' },
  { value: 'preset', label: '预设' },
  { value: 'custom', label: '自定义' }
];

const emptyRule = () => ({
  id: 0,
  name: '',
  description: '',
  matchType: 'literal',
  pattern: '',
  action: 'redact',
  replacementMode: 'placeholder',
  placeholder: '[已脱敏]',
  pseudonymKind: 'custom',
  source: 'custom',
  enabled: true,
  priority: 100
});

const Toggle = ({ checked, onChange, disabled = false, label }) => (
  <button
    type="button"
    role="switch"
    aria-checked={checked}
    aria-label={label}
    disabled={disabled}
    onClick={() => !disabled && onChange(!checked)}
    className={`relative inline-flex h-5 w-9 flex-shrink-0 items-center rounded-full transition-colors ${
      checked ? 'bg-indigo-500' : 'bg-slate-300'
    } focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-500 ${disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'}`}
  >
    <span
      className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow transition-transform ${
        checked ? 'translate-x-[18px]' : 'translate-x-[3px]'
      }`}
    />
  </button>
);

// 自绘勾选框：原生 input 用 peer sr-only 隐藏，继续承担键盘操作与读屏语义，
// 视觉交给 span，避免各平台原生控件样式不一致。与 ViewConfigPanel 的做法一致。
const Checkbox = ({ checked, indeterminate = false, onChange, label, disabled = false }) => (
  <label className={`group relative inline-flex items-center ${disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'}`}>
    <input
      type="checkbox"
      checked={checked}
      disabled={disabled}
      onChange={(event) => onChange(event.target.checked)}
      aria-label={label}
      ref={(node) => { if (node) node.indeterminate = indeterminate; }}
      className="peer sr-only"
    />
    <span
      aria-hidden="true"
      className="checkbox-indicator"
    >
      {indeterminate
        ? <Minus className="h-3 w-3" strokeWidth={3} />
        : checked && <Check className="h-3 w-3" strokeWidth={3} />}
    </span>
  </label>
);

const Badge = ({ children, className = '' }) => (
  <span className={`inline-flex items-center rounded-md border px-1.5 py-0.5 text-[11px] font-medium ${className}`}>
    {children}
  </span>
);

const ModeSelector = ({ mode, onChange, saving }) => (
  <div className="privacy-modes" role="group" aria-label="运行模式">
    {MODES.map((item) => {
      const Icon = item.icon;
      const active = mode === item.value;
      return (
        <button
          key={item.value}
          type="button"
          aria-pressed={active}
          disabled={saving}
          onClick={() => { if (!active) onChange(item.value); }}
          className="privacy-mode"
          data-active={active}
        >
          <span className="privacy-mode-label"><Icon size={14} /><span>{item.label}</span>{active && <Check size={12} className="ml-auto" />}</span>
        </button>
      );
    })}
  </div>
);

// 白名单之外的路径不扫描。这里把它显式呈现出来：漏扫是静默失败，
// 用户必须能看见「有请求没被扫过」，否则会误以为已全面受保护。
const UnscannedPathsBanner = ({ paths, onClear }) => {
  if (!paths.length) return null;
  const total = paths.reduce((sum, item) => sum + item.count, 0);
  return (
    <div className="privacy-unscanned rounded-xl border border-amber-200 bg-amber-50/80 p-3">
      <div className="flex items-start gap-3">
        <AlertTriangle className="mt-0.5 h-5 w-5 flex-shrink-0 text-amber-600" />
        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-semibold text-amber-900">
            有 {total} 个请求未经隐私扫描
          </h3>
          <p className="mt-1 text-xs leading-relaxed text-amber-800">
            以下路径不在字段白名单内，隐私规则未对其生效。若这些路径会携带客户信息，请改用「拒发请求」策略，或反馈该路径以便补充白名单。
          </p>
          <details className="privacy-paths"><summary>查看 {paths.length} 个未覆盖路径<ChevronDown size={12} /></summary><div className="mt-2 space-y-1">
            {paths.map((item) => (
              <div key={item.path} className="flex items-center justify-between gap-3 text-xs">
                <code className="min-w-0 wrap-anywhere rounded bg-white/70 px-1.5 py-0.5 font-mono text-amber-900">
                  {item.path}
                </code>
                <span className="flex-shrink-0 font-medium text-amber-700">{item.count} 次</span>
              </div>
            ))}
          </div></details>
        </div>
        <button
          type="button"
          onClick={onClear}
          className="flex-shrink-0 rounded-md p-1 text-amber-600 transition-colors hover:bg-amber-100"
          aria-label="清除未扫描路径统计"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
};

// 旧版规则不自动转换：转换出错在隐私场景是静默失败——规则看似还在、实际已不匹配。
//
// 一次性迁移代码，有明确终点：仅对从 v8.5 及更早版本升级上来的用户显示一次，
// 用户点「我已重建完毕」后 privacy_legacy_rules 表被 DROP，此后永不再现。
// 全新安装根本不会产生该表。
// 移除条件：确认在网用户不再有 v8.5 及更早版本时，可连同后端
// Store.ListLegacyRules / DiscardLegacyRules / migrateLegacyRulesTable 一并删除。
const LegacyRulesPanel = ({ rules, onDiscard }) => {
  const [expanded, setExpanded] = useState(false);
  if (!rules.length) return null;

  return (
    <div className="rounded-xl border border-slate-200 bg-white">
      <button
        type="button"
        onClick={() => setExpanded((value) => !value)}
        aria-expanded={expanded}
        className="flex w-full min-w-0 items-center gap-2 px-3 py-2.5 text-left"
      >
        <History className="h-4 w-4 text-slate-400" />
        <span className="min-w-0 flex-1 text-xs font-medium text-slate-700">旧版规则 · 需手工重建（{rules.length}）</span>
        <ChevronDown size={13} className={`shrink-0 text-slate-400 ${expanded ? 'rotate-180' : ''}`} />
      </button>
      {expanded && (
        <div className="border-t border-slate-100 px-4 py-3">
          <p className="mb-3 text-xs leading-relaxed text-slate-500">
            规则引擎已更换，旧规则刻意未自动转换 —— 转换一旦出错，规则看起来还在、实际已不再匹配，
            你会以为受保护而实际在裸奔。请对照下表重建，确认无误后清除本清单。
            精准匹配类规则的值已掩码，不会明文展示。
          </p>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead className="text-slate-500">
                <tr className="border-b border-slate-100">
                  <th className="py-1.5 pr-3 font-medium">名称</th>
                  <th className="py-1.5 pr-3 font-medium">类型</th>
                  <th className="py-1.5 pr-3 font-medium">内容</th>
                  <th className="py-1.5 font-medium">状态</th>
                </tr>
              </thead>
              <tbody>
                {rules.map((rule) => (
                  <tr key={rule.id} className="border-b border-slate-50 last:border-0">
                    <td className="py-1.5 pr-3 text-slate-800">{rule.name}</td>
                    <td className="py-1.5 pr-3 text-slate-500">{rule.type}</td>
                    <td className="max-w-md py-1.5 pr-3">
                      <code className="block truncate font-mono text-slate-600">{rule.patternMasked || '-'}</code>
                    </td>
                    <td className="py-1.5 text-slate-500">{rule.enabled ? '曾启用' : '曾停用'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="mt-3 flex justify-end">
            <Button variant="secondary" size="sm" onClick={onDiscard}>
              我已重建完毕，清除清单
            </Button>
          </div>
        </div>
      )}
    </div>
  );
};

const RuleRow = ({ rule, settings, onEdit, onDelete, onToggle, busy, selected, onSelect }) => {
  const isBuiltin = rule.source === 'builtin';
  const action = rule.action;
  return (
    <tr data-selected={selected} data-enabled={rule.enabled} data-action={action}>
      <td className="py-2.5 pr-2">
        <Checkbox
          checked={selected}
          onChange={(value) => onSelect(rule.id, value)}
          label={`选择 ${rule.name}`}
        />
      </td>
      <td className="py-2.5 pr-3">
        <Toggle
          checked={rule.enabled}
          disabled={busy}
          onChange={(value) => onToggle(rule, value)}
          label={`启用 ${rule.name}`}
        />
      </td>
      <td>
        <div className="privacy-rule-name"><strong>{rule.name}</strong><Badge className={SOURCE_TONES[rule.source] || SOURCE_TONES.custom}>{SOURCE_LABELS[rule.source] || rule.source}</Badge></div>
        {rule.description && <p className="privacy-rule-description">{rule.description}</p>}
      </td>
      <td>
        <span className="privacy-rule-type">{action === 'allow' && rule.matchType === 'literal' ? '完整值 · 忽略大小写' : MATCH_TYPES.find(item => item.value === rule.matchType)?.label || rule.matchType}</span>
        {rule.matchType === 'builtin' ? (
          <Tooltip asChild content={BUILTIN_MATCHERS[rule.pattern]?.detail || rule.pattern}><span className="privacy-rule-pattern">
            {describePattern(rule)}
          </span></Tooltip>
        ) : (
          <code className="privacy-rule-pattern font-mono">
            {rule.pattern}
          </code>
        )}
      </td>
      <td>
        <span className="privacy-rule-type">{action === 'allow' ? '例外放行' : action === 'block' ? '拦截' : action === 'detect' ? '仅检测' : settings.restoreResponses ? '可逆替身' : rule.replacementMode === 'pseudonym' ? '生成假名' : '占位符替换'}</span>
        <span className="privacy-rule-pattern">{action === 'allow' ? '豁免完整命中值' : action === 'block' ? '本机拒发，不重试' : action === 'detect' ? '记录命中，原文放行' : settings.restoreResponses ? '会话内稳定替换' : rule.replacementMode === 'pseudonym' ? PSEUDONYM_KIND_LABELS[rule.pseudonymKind] || rule.pseudonymKind : rule.placeholder || '[已脱敏]'}</span>
      </td>
      <td className="py-2.5 pr-3 text-xs text-slate-500">{rule.priority}</td>
      <td className="py-2.5">
        <div className="flex items-center justify-end gap-1">
          <Tooltip asChild content="编辑规则"><button
            type="button"
            onClick={() => onEdit(rule)}
            className="privacy-icon-button"
            disabled={busy}
            aria-label={`编辑 ${rule.name}`}
          >
            <PencilLine className="h-4 w-4" />
          </button></Tooltip>
          <Tooltip asChild content={isBuiltin ? '内置规则不可删除，可停用' : '删除'}><button
            type="button"
            disabled={isBuiltin || busy}
            onClick={() => onDelete(rule)}
            className="privacy-icon-button privacy-delete"
            aria-label={`删除 ${rule.name}`}
          >
            <Trash className="h-4 w-4" />
          </button></Tooltip>
        </div>
      </td>
    </tr>
  );
};

const RuleModal = ({ open, rule, saving, error, onChange, onClose, onSubmit, builtinMatchers = [] }) => {
  if (!open) return null;
  // 种子化的内置规则匹配逻辑锁定；用户新建的内置匹配器规则（source=custom）可以改选匹配器
  const isBuiltin = rule.source === 'builtin';
  const changeMatchType = (value) => {
    if (value === 'builtin' && rule.matchType !== 'builtin') {
      const first = builtinMatchers[0];
      onChange({
        ...rule,
        matchType: value,
        pattern: first?.pattern || '',
        name: rule.name || first?.name || '',
        placeholder: first?.placeholder || rule.placeholder,
        pseudonymKind: first?.pseudonymKind || rule.pseudonymKind
      });
      return;
    }
    // 从内置切走时清掉 builtin:xxx 标识，避免它被当作字面量或正则保存
    onChange({ ...rule, matchType: value, pattern: rule.matchType === 'builtin' ? '' : rule.pattern });
  };
  const changeBuiltinMatcher = (pattern) => {
    const matcher = builtinMatchers.find((item) => item.pattern === pattern);
    onChange({
      ...rule,
      pattern,
      placeholder: matcher?.placeholder || rule.placeholder,
      pseudonymKind: matcher?.pseudonymKind || rule.pseudonymKind
    });
  };
  const matchMeta = MATCH_TYPES.find((item) => item.value === rule.matchType);
  const replacementMeta = REPLACEMENT_MODES.find((item) => item.value === rule.replacementMode);

  return (
    <PrivacyDialog title={rule.id ? '编辑规则' : '新建规则'} icon={PencilLine} busy={saving} onClose={onClose} footer={<><Button variant="secondary" size="sm" icon={X} onClick={onClose} disabled={saving}>取消</Button><Button size="sm" icon={Check} onClick={onSubmit} loading={saving}>保存规则</Button></>}>
        <fieldset disabled={saving} className="min-w-0 space-y-3">
          {error && <p role="alert" className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700 wrap-anywhere">{error}</p>}

          <div className="grid gap-3 sm:grid-cols-2"><label className="block text-sm">
            <span className="font-medium text-slate-700">规则名称</span>
            <input
              value={rule.name}
              onChange={(event) => onChange({ ...rule, name: event.target.value })}
              className={`mt-1.5 ${FIELD_CLASS}`}
              placeholder="例如：客户手机号"
            />
          </label>

          <label className="block text-sm">
            <span className="font-medium text-slate-700">说明（可选）</span>
            <input
              value={rule.description}
              onChange={(event) => onChange({ ...rule, description: event.target.value })}
              className={`mt-1.5 ${FIELD_CLASS}`}
            />
          </label></div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="block text-sm">
              <span className="font-medium text-slate-700">匹配方式</span>
              <div className="mt-1.5">
                <CustomSelect
                  fullWidth
                  disabled={isBuiltin}
                  ariaLabel="匹配方式"
                  value={rule.matchType}
                  onChange={changeMatchType}
                  options={MATCH_TYPES.map((item) => ({ value: item.value, label: item.label }))}
                />
              </div>
              <p className="mt-1 text-xs text-slate-500">{matchMeta?.helper}</p>
            </div>

            <label className="block text-sm">
              <span className="font-medium text-slate-700">优先级（数字越小越先生效）</span>
              <input
                type="number"
                value={rule.priority}
                onChange={(event) => onChange({ ...rule, priority: Number(event.target.value) })}
                className={`mt-1.5 ${FIELD_CLASS}`}
              />
              <p className="mt-1 text-xs text-slate-500">数值越小越先匹配。完整值放行先判断，拦截和完整私钥优先于其他规则。</p>
            </label>
          </div>

          {rule.matchType === 'builtin' ? (
            <div className="block text-sm">
              <span className="font-medium text-slate-700">内置匹配器</span>
              {!isBuiltin && builtinMatchers.length > 0 && (
                <div className="mt-1.5">
                  <CustomSelect
                    fullWidth
                    ariaLabel="内置匹配器"
                    value={rule.pattern}
                    onChange={changeBuiltinMatcher}
                    options={builtinMatchers.map((item) => ({ value: item.pattern, label: item.name }))}
                  />
                </div>
              )}
              <div className="mt-1.5 rounded-lg border border-indigo-100 bg-indigo-50/60 px-3 py-2.5">
                <div className="text-sm font-medium text-indigo-900">{describePattern(rule)}</div>
                <p className="mt-1 text-xs leading-relaxed text-indigo-700">
                  {BUILTIN_MATCHERS[rule.pattern]?.detail || rule.pattern}
                </p>
              </div>
              <p className="mt-1 text-xs text-slate-500">
                判定逻辑在程序内实现，带校验位或上下文约束，不可编辑。需要不同口径请另建一条正则规则。
              </p>
            </div>
          ) : (
          <label className="block text-sm">
            <span className="font-medium text-slate-700">
              {rule.matchType === 'prefix' ? '固定密钥前缀' : rule.matchType === 'literal' ? '匹配文本' : '正则表达式'}
            </span>
            <textarea
              aria-label={rule.matchType === 'prefix' ? '固定密钥前缀' : rule.matchType === 'literal' ? '匹配文本' : '正则表达式'}
              value={rule.pattern}
              onChange={(event) => onChange({ ...rule, pattern: event.target.value })}
              rows={3}
              className={`mt-1.5 ${FIELD_MONO_CLASS}`}
            />
            {rule.matchType === 'regex' && (
              <div className="mt-2 flex flex-wrap gap-1.5">
                {REGEX_TEMPLATES.map((template) => (
                  <button
                    key={template.name}
                    type="button"
                    onClick={() => onChange({ ...rule, pattern: template.pattern, name: rule.name || template.name })}
                    className="rounded-md border border-slate-200 bg-slate-50 px-2 py-0.5 text-[11px] text-slate-600 hover:border-slate-300"
                  >
                    {template.name}
                  </button>
                ))}
              </div>
            )}
          </label>
          )}

          <div className={`grid gap-4 ${rule.action === 'redact' ? 'sm:grid-cols-2' : ''}`}>
            <div className="block text-sm">
              <span className="font-medium text-slate-700">动作</span>
              <div className="mt-1.5">
                <CustomSelect
                  fullWidth
                  ariaLabel="规则动作"
                  value={rule.action}
                  onChange={(value) => onChange({ ...rule, action: value })}
                  options={[
                    { value: 'redact', label: '替换' },
                    { value: 'block', label: '拦截 · 拒绝发送' },
                    { value: 'detect', label: '仅记录命中' },
                    { value: 'allow', label: '例外放行 · 豁免完整值' }
                  ]}
                />
              </div>
              {rule.action === 'allow' && <p className="mt-1 text-xs text-slate-500">仅当其他规则的完整命中值符合此规则时，才免于检测、替换和拦截。指定文本忽略大小写，子串相同不会放行整个值。</p>}
            </div>

            {rule.action === 'redact' && (
              <div className="block text-sm">
                <span className="font-medium text-slate-700">替换方式</span>
                <div className="mt-1.5">
                  <CustomSelect
                    fullWidth
                    ariaLabel="替换方式"
                    value={rule.replacementMode}
                    onChange={(value) => onChange({ ...rule, replacementMode: value })}
                    options={REPLACEMENT_MODES.map((item) => ({ value: item.value, label: item.label }))}
                  />
                </div>
                <p className="mt-1 text-xs text-slate-500">{replacementMeta?.helper}</p>
              </div>
            )}
          </div>

          {rule.action === 'redact' && rule.replacementMode === 'placeholder' && (
            <label className="block text-sm">
              <span className="font-medium text-slate-700">占位符</span>
              <input
                value={rule.placeholder}
                onChange={(event) => onChange({ ...rule, placeholder: event.target.value })}
                className={`mt-1.5 ${FIELD_CLASS}`}
              />
            </label>
          )}

          {rule.action === 'redact' && rule.replacementMode === 'pseudonym' && (
            <div className="block text-sm">
              <span className="font-medium text-slate-700">假名类别</span>
              <div className="mt-1.5">
                <CustomSelect
                  fullWidth
                  ariaLabel="假名类别"
                  value={rule.pseudonymKind}
                  onChange={(value) => onChange({ ...rule, pseudonymKind: value })}
                  options={Object.entries(PSEUDONYM_KIND_LABELS).map(([value, label]) => ({ value, label }))}
                />
              </div>
              <p className="mt-1 text-xs text-slate-500">
                生成格式合法的虚构值（身份证、银行卡带正确校验位），避免上游因格式校验拒绝请求。
              </p>
            </div>
          )}
        </fieldset>

    </PrivacyDialog>
  );
};

// 测试面板的输入与结果都停留在本机，不落库、不上报。
// 规则测试弹窗。按需打开，不常驻占位。
// 输入与结果都停留在本机：不落库、不进日志、不发往任何服务商。
// 完整请求模式可选的 API 路径；与后端扫描白名单一致
const PREVIEW_PATHS = [
  { value: '/v1/messages', label: 'Claude /v1/messages' },
  { value: '/v1/responses', label: 'Codex /v1/responses' },
  { value: '/v1/chat/completions', label: 'OpenAI /v1/chat/completions' }
];

const TestModal = ({ open, onClose }) => {
  const [mode, setMode] = useState('text');
  const [path, setPath] = useState(PREVIEW_PATHS[0].value);
  const [input, setInput] = useState('');
  const [result, setResult] = useState(null);
  const [running, setRunning] = useState(false);
  const toast = useToast();

  if (!open) return null;

  const run = async () => {
    if (!input.trim()) return;
    setRunning(true);
    try {
      setResult(mode === 'request'
        ? await previewPrivacyRequest(path, input)
        : await previewPrivacyRedaction(input));
    } catch (error) {
      toast.error(`测试失败: ${error.message}`);
    } finally {
      setRunning(false);
    }
  };

  return (
    <PrivacyDialog title="规则测试" icon={FlaskConical} busy={running} onClose={onClose} footer={<><Button variant="secondary" size="sm" icon={X} onClick={onClose} disabled={running}>关闭</Button><Button size="sm" icon={FlaskConical} onClick={run} loading={running} disabled={!input.trim()}>运行测试</Button></>}>
        <div className="space-y-3">
          <p className="text-xs text-slate-500">
            试跑当前全部启用规则，始终按「脱敏」推演。输入与结果只停留在本机，不会写入数据库或发往任何服务商。
          </p>
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex gap-1 rounded-lg bg-slate-100 p-1">
              {[
                { value: 'text', label: '文本' },
                { value: 'request', label: '完整请求 JSON' }
              ].map((item) => (
                <button
                  key={item.value}
                  type="button"
                  disabled={running}
                  onClick={() => { setMode(item.value); setResult(null); }}
                  aria-pressed={mode === item.value}
                  className={`rounded-md px-2.5 py-1 text-xs font-medium ${
                    mode === item.value ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500 hover:text-slate-700'
                  }`}
                >
                  {item.label}
                </button>
              ))}
            </div>
            {mode === 'request' && (
              <div className="min-w-0 flex-1 basis-56">
                <CustomSelect
                  fullWidth
                  disabled={running}
                  ariaLabel="请求路径"
                  value={path}
                  onChange={(value) => { setPath(value); setResult(null); }}
                  options={PREVIEW_PATHS}
                />
              </div>
            )}
          </div>
          <textarea
            autoFocus
            disabled={running}
            value={input}
            onChange={(event) => { setInput(event.target.value); setResult(null); }}
            rows={mode === 'request' ? 9 : 5}
            placeholder={mode === 'request'
              ? '粘贴一段完整的请求体 JSON，查看哪些字段会被扫描、哪些会被改写'
              : '粘贴一段文本，检查规则会命中什么'}
            className={mode === 'request' ? FIELD_MONO_CLASS : FIELD_CLASS}
          />
          {result?.error && (
            <p className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700">
              {result.blocked ? '预演结果：该请求会被本机策略拒发。' : '扫描失败：'}{result.error}
            </p>
          )}
          {result && mode === 'request' && !result.supported && (
            <p className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
              该路径不在扫描范围内，实际请求会按「路径未被覆盖时」策略处理。
            </p>
          )}
          {result && (!result.error || result.blocked) && (
            <div className="space-y-2">
              <div className="text-xs text-slate-500">
                命中 {result.hitCount} 处{result.blocked ? '，将拒绝发送' : result.changed ? '，按脱敏模式已替换' : '，无替换'}
              </div>
              {mode === 'request' && result.fields?.length > 0 && (
                <div className="max-h-40 overflow-auto rounded-lg border border-slate-200">
                  <table className="w-full text-left text-xs">
                    <thead className="sticky top-0 bg-slate-50 text-slate-500">
                      <tr>
                        <th className="px-2 py-1 font-medium">被扫描的字段（{result.fields.length}）</th>
                        <th className="px-2 py-1 font-medium">字节</th>
                        <th className="px-2 py-1 font-medium">结果</th>
                      </tr>
                    </thead>
                    <tbody>
                      {result.fields.map((field, index) => (
                        <tr key={`${field.path}-${index}`} className="border-t border-slate-100">
                          <td className="px-2 py-1 font-mono text-slate-700 [overflow-wrap:anywhere]">{field.path}</td>
                          <td className="px-2 py-1 text-slate-500">{field.bytes}</td>
                          <td className={`px-2 py-1 ${field.redacted ? 'font-medium text-emerald-700' : 'text-slate-400'}`}>
                            {field.redacted ? '已改写' : '未命中'}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              {result.ruleHits.length > 0 && (
                <div className="flex flex-wrap gap-1.5">
                  {result.ruleHits.map((hit) => (
                    <Badge key={hit.ruleId} className="border-emerald-200 bg-emerald-50 text-emerald-700">
                      {hit.ruleName} × {hit.count}
                    </Badge>
                  ))}
                </div>
              )}
              <pre className="max-h-56 overflow-auto whitespace-pre-wrap break-words rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 font-mono text-xs text-slate-800">
                {result.output}
              </pre>
            </div>
          )}
        </div>

    </PrivacyDialog>
  );
};

const PrivacyPage = () => {
  const [optionsOpen, setOptionsOpen] = useState(false);
  const [activityOpen, setActivityOpen] = useState(false);
  const [overview, setOverview] = useState(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [savingSettings, setSavingSettings] = useState(false);
  const [busyRuleId, setBusyRuleId] = useState(0);

  const [modalRule, setModalRule] = useState(null);
  const [modalError, setModalError] = useState('');
  const [modalSaving, setModalSaving] = useState(false);
  const [pendingDelete, setPendingDelete] = useState(null);
  const [testOpen, setTestOpen] = useState(false);
  const [sourceFilter, setSourceFilter] = useState('all');
  const [statusFilter, setStatusFilter] = useState('all');
  const [search, setSearch] = useState('');
  const [presetsOpen, setPresetsOpen] = useState(false);
  const [importingPreset, setImportingPreset] = useState('');
  const [selectedIds, setSelectedIds] = useState([]);
  const [bulkBusy, setBulkBusy] = useState(false);
  const [bulkDeleteOpen, setBulkDeleteOpen] = useState(false);

  const toast = useToast();

  const load = useCallback(async () => {
    try {
      setLoadError('');
      setOverview(await fetchPrivacyOverview());
    } catch (error) {
      setLoadError(error.message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const settings = overview?.settings;
  const rules = useMemo(() => overview?.rules || [], [overview]);
  const enabledCount = useMemo(() => rules.filter((rule) => rule.enabled).length, [rules]);
  const visibleRules = useMemo(
    () => {
      const query = search.trim().toLocaleLowerCase();
      return rules.filter(rule => (sourceFilter === 'all' || rule.source === sourceFilter)
        && (statusFilter === 'all' || rule.enabled === (statusFilter === 'enabled'))
        && (!query || [rule.name, rule.description, rule.pattern, describePattern(rule), rule.placeholder].some(value => value?.toLocaleLowerCase().includes(query))));
    },
    [rules, sourceFilter, statusFilter, search]
  );
  const visibleIds = useMemo(() => visibleRules.map((rule) => rule.id), [visibleRules]);
  const someVisibleSelected = visibleIds.some((id) => selectedIds.includes(id));
  const allVisibleSelected = visibleIds.length > 0 && visibleIds.every((id) => selectedIds.includes(id));
  // 内置规则不可删除（只能停用），批量删除时先剔除，避免弹窗数字与实际结果对不上
  const deletableSelectedCount = useMemo(
    () => rules.filter((rule) => selectedIds.includes(rule.id) && rule.source !== 'builtin').length,
    [rules, selectedIds]
  );

  const toggleSelectOne = (id, checked) => {
    setSelectedIds((prev) => (checked ? [...new Set([...prev, id])] : prev.filter((item) => item !== id)));
  };

  const toggleSelectAll = (checked) => {
    setSelectedIds((prev) => (checked
      ? [...new Set([...prev, ...visibleIds])]
      : prev.filter((id) => !visibleIds.includes(id))));
  };

  const bulkToggle = async (enabled) => {
    setBulkBusy(true);
    try {
      const affected = await setPrivacyRulesEnabled(selectedIds, enabled);
      setSelectedIds([]);
      await load();
      toast.success(`已${enabled ? '启用' : '停用'} ${affected} 条规则`);
    } catch (error) {
      toast.error(`批量更新失败: ${error.message}`);
    } finally {
      setBulkBusy(false);
    }
  };

  const bulkDelete = async () => {
    setBulkBusy(true);
    try {
      const affected = await deletePrivacyRules(selectedIds);
      setBulkDeleteOpen(false);
      setSelectedIds([]);
      await load();
      toast.success(`已删除 ${affected} 条规则`);
    } catch (error) {
      toast.error(`批量删除失败: ${error.message}`);
    } finally {
      setBulkBusy(false);
    }
  };

  const saveSettings = async (next) => {
    setSavingSettings(true);
    try {
      await updatePrivacySettings(next);
      await load();
      toast.success('隐私防护设置已生效');
      return true;
    } catch (error) {
      toast.error(`保存失败: ${error.message}`);
      return false;
    } finally {
      setSavingSettings(false);
    }
  };

  const toggleRule = async (rule, enabled) => {
    setBusyRuleId(rule.id);
    try {
      await updatePrivacyRule(rule.id, { ...rule, enabled });
      await load();
    } catch (error) {
      toast.error(`更新失败: ${error.message}`);
    } finally {
      setBusyRuleId(0);
    }
  };

  const submitRule = async () => {
    setModalSaving(true);
    setModalError('');
    try {
      if (modalRule.id) {
        await updatePrivacyRule(modalRule.id, modalRule);
      } else {
        await createPrivacyRule(modalRule);
      }
      setModalRule(null);
      await load();
      toast.success('规则已保存');
    } catch (error) {
      setModalError(error.message);
    } finally {
      setModalSaving(false);
    }
  };

  const confirmDelete = async () => {
    try {
      await deletePrivacyRule(pendingDelete.id);
      setPendingDelete(null);
      await load();
      toast.success('规则已删除');
    } catch (error) {
      toast.error(`删除失败: ${error.message}`);
    }
  };

  const importPreset = async (presetId) => {
    if (importingPreset) return;
    setImportingPreset(presetId);
    try {
      const added = await applyPrivacyPreset(presetId);
      await load();
      if (added > 0) {
        toast.success(`已导入 ${added} 条规则`);
      } else {
        toast.info('预设规则均已存在');
      }
    } catch (error) {
      toast.error(`导入失败: ${error.message}`);
    } finally {
      setImportingPreset('');
    }
  };

  if (loading) return <LoadingSpinner />;
  if (loadError) return <ErrorMessage message={loadError} onRetry={load} />;
  if (!settings) return <ErrorMessage message="隐私防护服务未就绪" onRetry={load} />;

  return (
    <div className="privacy-workspace animate-fade-in">
      {/* 页面标题 */}
      <div className="flex min-h-9 shrink-0 flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="shrink-0 rounded-lg bg-slate-900 p-1.5 text-white shadow-sm">
            <ShieldCheck className="h-5 w-5" />
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <h1 className="text-xl font-bold text-slate-900">隐私防护</h1>
            <span className="privacy-status" data-mode={settings.mode}><span />{MODES.find(item => item.value === settings.mode)?.label || settings.mode}<span className="privacy-status-count">{enabledCount} / {rules.length} 条启用</span></span>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button variant="secondary" size="sm" icon={SlidersHorizontal} onClick={() => setOptionsOpen(true)}>运行设置</Button>
          <Button variant="secondary" size="sm" icon={History} onClick={() => { setActivityOpen(true); load(); }}>防护记录</Button>
          <Button variant="secondary" size="sm" icon={FlaskConical} onClick={() => setTestOpen(true)}>规则测试</Button>
          <Button variant="ghost" size="sm" icon={RefreshCw} onClick={load}>刷新</Button>
        </div>
      </div>

      <div className="privacy-content">
        <div className="privacy-configuration">
        <UnscannedPathsBanner
          paths={overview.unscannedPaths}
          onClear={async () => {
            await clearPrivacyUnscannedPaths();
            await load();
          }}
        />

        <section className="privacy-runtime" aria-label="运行模式">
          <div className="privacy-runtime-control">
            <span className="privacy-runtime-label">运行模式</span>
            <ModeSelector
              mode={settings.mode}
              saving={savingSettings}
              onChange={(mode) => saveSettings({ ...settings, mode })}
            />
          </div>
          <p>{MODES.find(item => item.value === settings.mode)?.description}<span>选择后立即生效</span></p>
        </section>

        <LegacyRulesPanel
          rules={overview.legacyRules}
          onDiscard={async () => {
            await discardLegacyPrivacyRules();
            await load();
            toast.success('旧版规则清单已清除');
          }}
        />
        </div>

        <section className="privacy-rules" aria-label="隐私规则">
          <div className="privacy-rules-heading">
            <div className="privacy-section-heading"><h2><ListFilter size={14} />防护规则</h2><span>{rules.length} 条 · 匹配、处理与放行统一管理</span></div>
            <div className="ml-auto flex flex-wrap items-center gap-2">
              {!!overview.presets.length && <Button variant="secondary" size="sm" icon={Package} onClick={() => setPresetsOpen(true)}>导入预设</Button>}
              <Button size="sm" icon={Plus} onClick={() => { setModalRule(emptyRule()); setModalError(''); }}>新建规则</Button>
            </div>
          </div>

          {/* 来源筛选：规则会随预设导入快速增长，按来源收窄是最省事的定位方式 */}
          <div className="privacy-rule-toolbar"><div className="privacy-source-filters" role="group" aria-label="规则来源">
            {SOURCE_FILTERS.map((item) => {
              const count = item.value === 'all'
                ? rules.length
                : rules.filter((rule) => rule.source === item.value).length;
              const active = sourceFilter === item.value;
              return (
                <button
                  key={item.value}
                  type="button"
                  aria-pressed={active}
                  onClick={() => setSourceFilter(item.value)}
                  className="privacy-source-button"
                >
                  {item.label}
                  <span>{count}</span>
                </button>
              );
            })}
          </div>
          <div className="privacy-rule-filters">
            <label className="privacy-search">
              <Search size={13} />
              <input aria-label="搜索隐私规则" value={search} onChange={event => setSearch(event.target.value)} placeholder="搜索名称、匹配内容…" />
              {search && <button type="button" aria-label="清除规则搜索" onClick={() => setSearch('')}><X size={12} /></button>}
            </label>
            <CustomSelect size="sm" fullWidth ariaLabel="规则状态" className="privacy-state-filter" value={statusFilter} onChange={setStatusFilter} options={[{ value: 'all', label: '全部状态' }, { value: 'enabled', label: '已启用' }, { value: 'disabled', label: '已停用' }]} />
          </div></div>

          {/* 批量操作条：导入预设是一次性加十几条，逐条清理太痛苦 */}
          {selectedIds.length > 0 && (
            <div className="privacy-bulk-actions">
              <span className="text-xs font-medium text-indigo-900">已选 {selectedIds.length} 条</span>
              <div className="ml-auto flex flex-wrap items-center gap-1.5">
                <Button variant="ghost" size="sm" icon={ShieldCheck} onClick={() => bulkToggle(true)} disabled={bulkBusy}>
                  批量启用
                </Button>
                <Button variant="ghost" size="sm" icon={ShieldOff} onClick={() => bulkToggle(false)} disabled={bulkBusy}>
                  批量停用
                </Button>
                <Button variant="dangerSoft" size="sm" icon={Trash} onClick={() => setBulkDeleteOpen(true)} disabled={bulkBusy || !deletableSelectedCount}>
                  批量删除
                </Button>
                <Button variant="ghost" size="sm" icon={X} onClick={() => setSelectedIds([])} disabled={bulkBusy}>
                  取消选择
                </Button>
              </div>
            </div>
          )}

          {rules.length === 0 ? (
            <p className="privacy-rules-empty">
              还没有任何规则。先导入一套预设，或新建自定义规则。
            </p>
          ) : visibleRules.length === 0 ? (
            <div className="privacy-rules-empty"><Search size={22} /><p>没有符合条件的规则</p><button type="button" onClick={() => { setSearch(''); setSourceFilter('all'); setStatusFilter('all'); }}><RefreshCw size={12} />清除筛选</button></div>
          ) : (
            // 限高滚动而非无限增高：规则可以到几十条，页面不该跟着一直变长
            <div className="privacy-rule-scroll">
              <table className="privacy-rule-table" aria-label="隐私规则列表">
                <colgroup><col className="privacy-col-check" /><col className="privacy-col-state" /><col className="privacy-col-name" /><col className="privacy-col-pattern" /><col className="privacy-col-action" /><col className="privacy-col-priority" /><col className="privacy-col-operations" /></colgroup>
                <thead className="sticky top-0 z-10 bg-white text-xs text-slate-500">
                  <tr className="border-b border-slate-100">
                    <th className="w-8 py-2 pr-2 font-medium">
                      <Checkbox
                        checked={allVisibleSelected}
                        indeterminate={someVisibleSelected && !allVisibleSelected}
                        onChange={toggleSelectAll}
                        label="全选当前筛选下的规则"
                      />
                    </th>
                    <th className="w-12 py-2 pr-3 font-medium">启用</th>
                    <th className="py-2 pr-3 font-medium">名称</th>
                    <th className="py-2 pr-3 font-medium">匹配内容</th>
                    <th className="py-2 pr-3 font-medium">处理方式</th>
                    <th className="py-2 pr-3 font-medium">优先级</th>
                    <th className="py-2"><span className="sr-only">操作</span></th>
                  </tr>
                </thead>
                <tbody>
                  {visibleRules.map((rule) => (
                    <RuleRow
                      key={rule.id}
                      rule={rule}
                      settings={settings}
                      busy={bulkBusy || busyRuleId === rule.id}
                      selected={selectedIds.includes(rule.id)}
                      onSelect={toggleSelectOne}
                      onToggle={toggleRule}
                      onEdit={(item) => { setModalRule({ ...item }); setModalError(''); }}
                      onDelete={setPendingDelete}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <footer className="privacy-rules-footer"><span>显示 {visibleRules.length} / {rules.length} 条规则{selectedIds.length > 0 && ` · 已选 ${selectedIds.length} 条`}</span><span>优先级数值越小，越先匹配</span></footer>
        </section>
      </div>

      {testOpen && <TestModal open onClose={() => setTestOpen(false)} />}
      {optionsOpen && <ProtectionOptions settings={settings} Dialog={PrivacyDialog} saving={savingSettings} onSave={saveSettings} onClose={() => setOptionsOpen(false)} />}
      {activityOpen && <PrivacyActivity activity={overview.activity || []} Dialog={PrivacyDialog} onClose={() => setActivityOpen(false)} />}

      {presetsOpen && <PrivacyDialog
        title="导入规则预设" icon={Package} busy={Boolean(importingPreset)} onClose={() => setPresetsOpen(false)}
        footer={<Button size="sm" variant="secondary" icon={X} disabled={Boolean(importingPreset)} onClick={() => setPresetsOpen(false)}>关闭</Button>}
      >
        <p className="mb-3 text-xs leading-5 text-slate-500">选择需要的预设导入。已有规则会自动跳过，导入后可在列表中调整或停用。</p>
        <div className="privacy-presets">{overview.presets.map(preset => <article key={preset.id}>
          <span className="privacy-preset-icon"><Package size={18} /></span>
          <div><h3>{preset.name}<span>{preset.ruleCount} 条规则</span></h3><p>{preset.description || '导入后可单独管理每条规则。'}</p></div>
          <Button variant="secondary" size="sm" icon={Plus} loading={importingPreset === preset.id} disabled={Boolean(importingPreset) && importingPreset !== preset.id} onClick={() => importPreset(preset.id)}>导入</Button>
        </article>)}</div>
      </PrivacyDialog>}

      <RuleModal
        open={Boolean(modalRule)}
        rule={modalRule || emptyRule()}
        builtinMatchers={overview.builtinMatchers || []}
        saving={modalSaving}
        error={modalError}
        onChange={setModalRule}
        onClose={() => setModalRule(null)}
        onSubmit={submitRule}
      />

      <ConfirmDialog
        isOpen={bulkDeleteOpen}
        variant="danger"
        title="批量删除隐私规则"
        message={
          deletableSelectedCount === selectedIds.length
            ? `确定删除选中的 ${selectedIds.length} 条规则？删除后这些内容将不再被脱敏。`
            : `选中 ${selectedIds.length} 条，其中 ${selectedIds.length - deletableSelectedCount} 条是内置规则不可删除（可改为停用）。将删除其余 ${deletableSelectedCount} 条。`
        }
        confirmText="删除"
        onConfirm={bulkDelete}
        onCancel={() => setBulkDeleteOpen(false)}
      />

      <ConfirmDialog
        isOpen={Boolean(pendingDelete)}
        title="删除隐私规则"
        message={`确定删除规则「${pendingDelete?.name || ''}」？删除后该类内容将不再被脱敏。`}
        confirmText="删除"
        onConfirm={confirmDelete}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
};

export default PrivacyPage;
