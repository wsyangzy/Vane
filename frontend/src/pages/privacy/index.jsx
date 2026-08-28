import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  AlertTriangle,
  Check,
  Eye,
  FlaskConical,
  History,
  Minus,
  Package,
  PencilLine,
  Plus,
  RefreshCw,
  ShieldCheck,
  ShieldOff,
  Trash,
  X
} from 'lucide-react';
import { Button, ConfirmDialog, CustomSelect, ErrorMessage, LoadingSpinner, useToast } from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal';
import {
  applyPrivacyPreset,
  clearPrivacyUnscannedPaths,
  createPrivacyRule,
  deletePrivacyRule,
  deletePrivacyRules,
  discardLegacyPrivacyRules,
  fetchPrivacyOverview,
  previewPrivacyRedaction,
  setPrivacyRulesEnabled,
  updatePrivacyRule,
  updatePrivacySettings
} from '@utils/api.js';
import { REGEX_TEMPLATES } from './regexTemplates.js';

const MODES = [
  {
    value: 'disabled',
    label: '关闭',
    icon: ShieldOff,
    description: '不扫描、不改写。请求原样发往服务商。'
  },
  {
    value: 'detect',
    label: '仅检测',
    icon: Eye,
    description: '统计命中但不改写请求。用它先量清规则的误伤面，再决定是否开启脱敏。'
  },
  {
    value: 'redact',
    label: '脱敏',
    icon: ShieldCheck,
    description: '命中内容在发往服务商前被替换。未命中的字节与原始请求完全一致。'
  }
];

const MATCH_TYPES = [
  { value: 'literal', label: '字面量', helper: '按原样查找这段文本，不做任何模式解释。' },
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

// 与 @components/ui 的 Input 保持一致的输入控件样式（含 focus ring）。
// 原生控件默认没有聚焦态，混在自定义组件之间会很突兀。
const FIELD_CLASS =
  'w-full rounded-lg border border-slate-200 px-3 py-2 text-sm transition-colors ' +
  'focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500';

const FIELD_MONO_CLASS = FIELD_CLASS + ' font-mono text-xs';

// 弹窗动效对齐 RequestDetailModal：遮罩淡入，面板 zoom-in-95 + 淡入。
const OVERLAY_CLASS = 'absolute inset-0 bg-slate-900/20 backdrop-blur-sm animate-in fade-in duration-200';
const PANEL_CLASS =
  'relative flex w-full flex-col overflow-hidden rounded-2xl bg-white shadow-2xl ring-1 ring-black/5 ' +
  'animate-in zoom-in-95 fade-in duration-200';

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
  matchType: 'regex',
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
      checked ? 'bg-emerald-500' : 'bg-slate-300'
    } ${disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'}`}
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
  <label className={`group inline-flex items-center ${disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'}`}>
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
      className={`inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-md border transition-all
        peer-focus-visible:ring-2 peer-focus-visible:ring-indigo-200 peer-focus-visible:ring-offset-1 ${
        checked || indeterminate
          ? 'border-indigo-500 bg-indigo-600 text-white shadow-sm shadow-indigo-500/20'
          : 'border-slate-300 bg-white text-transparent group-hover:border-indigo-300'
      }`}
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
  <div className="grid gap-2 sm:grid-cols-3">
    {MODES.map((item) => {
      const Icon = item.icon;
      const active = mode === item.value;
      return (
        <button
          key={item.value}
          type="button"
          disabled={saving}
          onClick={() => onChange(item.value)}
          className={`rounded-xl border p-3 text-left transition-colors ${
            active
              ? 'border-emerald-300 bg-emerald-50 ring-1 ring-emerald-200'
              : 'border-slate-200 bg-white hover:border-slate-300'
          } ${saving ? 'cursor-not-allowed opacity-60' : ''}`}
        >
          <div className="flex items-center gap-2">
            <Icon className={`h-4 w-4 ${active ? 'text-emerald-600' : 'text-slate-400'}`} />
            <span className={`text-sm font-semibold ${active ? 'text-emerald-800' : 'text-slate-700'}`}>
              {item.label}
            </span>
            {active && <Check className="ml-auto h-4 w-4 text-emerald-600" />}
          </div>
          <p className="mt-1.5 text-xs leading-relaxed text-slate-500">{item.description}</p>
        </button>
      );
    })}
  </div>
);

const PolicyPanel = ({ settings, onChange, saving }) => (
  <div className="grid gap-4 sm:grid-cols-3">
    <div className="text-sm">
      <span className="font-medium text-slate-700">扫描出错时</span>
      <div className="mt-1.5">
        <CustomSelect
          fullWidth
          disabled={saving}
          ariaLabel="扫描出错时的处理方式"
          value={settings.onError}
          onChange={(value) => onChange({ ...settings, onError: value })}
          options={[
            { value: 'fail_open', label: '放行请求' },
            { value: 'fail_closed', label: '拒发请求' }
          ]}
        />
      </div>
      <p className="mt-1 text-xs text-slate-500">选「拒发」表示宁可请求失败，也不冒未经扫描就外发的风险。</p>
    </div>

    <div className="text-sm">
      <span className="font-medium text-slate-700">路径未被覆盖时</span>
      <div className="mt-1.5">
        <CustomSelect
          fullWidth
          disabled={saving}
          ariaLabel="未覆盖路径的处理方式"
          value={settings.unsupportedPathAction}
          onChange={(value) => onChange({ ...settings, unsupportedPathAction: value })}
          options={[
            { value: 'warn', label: '放行并告警' },
            { value: 'fail_closed', label: '拒发请求' }
          ]}
        />
      </div>
      <p className="mt-1 text-xs text-slate-500">扫描按 API 路径的字段白名单进行，白名单外的路径不扫描。</p>
    </div>

    <div className="text-sm">
      <span className="font-medium text-slate-700">超出扫描上限时</span>
      <div className="mt-1.5">
        <CustomSelect
          fullWidth
          disabled={saving}
          ariaLabel="超出扫描上限的处理方式"
          value={settings.overLimitAction}
          onChange={(value) => onChange({ ...settings, overLimitAction: value })}
          options={[
            { value: 'scan_prefix', label: `只扫描前 ${Math.round(settings.scanMaxBytes / 1024 / 1024)} MB` },
            { value: 'fail_closed', label: '拒发请求' }
          ]}
        />
      </div>
      <p className="mt-1 text-xs text-slate-500">超长上下文下，未扫描的尾部会原样发出。</p>
    </div>
  </div>
);

// 白名单之外的路径不扫描。这里把它显式呈现出来：漏扫是静默失败，
// 用户必须能看见「有请求没被扫过」，否则会误以为已全面受保护。
const UnscannedPathsBanner = ({ paths, onClear }) => {
  if (!paths.length) return null;
  const total = paths.reduce((sum, item) => sum + item.count, 0);
  return (
    <div className="rounded-xl border border-amber-200 bg-amber-50 p-4">
      <div className="flex items-start gap-3">
        <AlertTriangle className="mt-0.5 h-5 w-5 flex-shrink-0 text-amber-600" />
        <div className="min-w-0 flex-1">
          <h3 className="text-sm font-semibold text-amber-900">
            有 {total} 个请求未经隐私扫描
          </h3>
          <p className="mt-1 text-xs leading-relaxed text-amber-800">
            以下路径不在字段白名单内，隐私规则未对其生效。若这些路径会携带客户信息，请改用「拒发请求」策略，或反馈该路径以便补充白名单。
          </p>
          <div className="mt-2.5 space-y-1">
            {paths.map((item) => (
              <div key={item.path} className="flex items-center justify-between gap-3 text-xs">
                <code className="min-w-0 truncate rounded bg-white/70 px-1.5 py-0.5 font-mono text-amber-900">
                  {item.path}
                </code>
                <span className="flex-shrink-0 font-medium text-amber-700">{item.count} 次</span>
              </div>
            ))}
          </div>
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
        className="flex w-full items-center gap-2 px-4 py-3 text-left"
      >
        <History className="h-4 w-4 text-slate-400" />
        <span className="text-sm font-semibold text-slate-800">旧版规则 · 需手工重建（{rules.length}）</span>
        <span className="ml-auto text-xs text-slate-400">{expanded ? '收起' : '展开'}</span>
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

const RuleRow = ({ rule, onEdit, onDelete, onToggle, busy, selected, onSelect }) => {
  const isBuiltin = rule.source === 'builtin';
  return (
    <tr className={`border-b border-slate-50 last:border-0 ${selected ? 'bg-indigo-50/40' : ''}`}>
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
      <td className="py-2.5 pr-3">
        <div className="text-sm font-medium text-slate-800">{rule.name}</div>
        {rule.description && <div className="mt-0.5 text-xs text-slate-500">{rule.description}</div>}
      </td>
      <td className="max-w-xs py-2.5 pr-3">
        {rule.matchType === 'builtin' ? (
          <span className="block truncate text-xs text-slate-600" title={BUILTIN_MATCHERS[rule.pattern]?.detail || rule.pattern}>
            {describePattern(rule)}
          </span>
        ) : (
          <code className="block truncate font-mono text-xs text-slate-600" title={rule.pattern}>
            {rule.pattern}
          </code>
        )}
      </td>
      <td className="py-2.5 pr-3">
        <Badge className={SOURCE_TONES[rule.source] || SOURCE_TONES.custom}>
          {SOURCE_LABELS[rule.source] || rule.source}
        </Badge>
      </td>
      <td className="py-2.5 pr-3 text-xs text-slate-600">
        {rule.action === 'detect'
          ? '仅记录'
          : rule.replacementMode === 'pseudonym'
            ? `假名 · ${PSEUDONYM_KIND_LABELS[rule.pseudonymKind] || rule.pseudonymKind}`
            : rule.placeholder}
      </td>
      <td className="py-2.5 pr-3 text-xs text-slate-500">{rule.priority}</td>
      <td className="py-2.5">
        <div className="flex items-center justify-end gap-1">
          <button
            type="button"
            onClick={() => onEdit(rule)}
            className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
            aria-label={`编辑 ${rule.name}`}
          >
            <PencilLine className="h-4 w-4" />
          </button>
          <button
            type="button"
            disabled={isBuiltin}
            onClick={() => onDelete(rule)}
            title={isBuiltin ? '内置规则不可删除，可停用' : '删除'}
            className={`rounded-md p-1.5 transition-colors ${
              isBuiltin
                ? 'cursor-not-allowed text-slate-200'
                : 'text-slate-400 hover:bg-rose-50 hover:text-rose-600'
            }`}
            aria-label={`删除 ${rule.name}`}
          >
            <Trash className="h-4 w-4" />
          </button>
        </div>
      </td>
    </tr>
  );
};

const RuleModal = ({ open, rule, saving, error, onChange, onClose, onSubmit }) => {
  if (!open) return null;
  const isBuiltin = rule.source === 'builtin';
  const matchMeta = MATCH_TYPES.find((item) => item.value === rule.matchType);
  const replacementMeta = REPLACEMENT_MODES.find((item) => item.value === rule.replacementMode);

  return renderModalPortal(
    <div className="fixed inset-0 z-[10000] flex items-center justify-center p-4">
      <div className={OVERLAY_CLASS} />
      <div className={`${PANEL_CLASS} max-h-[90vh] max-w-2xl`}>
        <div className="flex items-center justify-between border-b border-slate-100 px-5 py-3.5">
          <h3 className="text-sm font-semibold text-slate-900">{rule.id ? '编辑规则' : '新建规则'}</h3>
          <button type="button" onClick={onClose} className="rounded-md p-1 text-slate-400 hover:bg-slate-100">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex-1 space-y-4 overflow-y-auto px-5 py-4">
          {error && <ErrorMessage message={error} />}

          <label className="block text-sm">
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
          </label>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="block text-sm">
              <span className="font-medium text-slate-700">匹配方式</span>
              <div className="mt-1.5">
                <CustomSelect
                  fullWidth
                  disabled={isBuiltin}
                  ariaLabel="匹配方式"
                  value={rule.matchType}
                  onChange={(value) => onChange({ ...rule, matchType: value })}
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
              <p className="mt-1 text-xs text-slate-500">与高优先级规则命中区间重叠时，本规则的命中会被丢弃。</p>
            </label>
          </div>

          {rule.matchType === 'builtin' ? (
            <div className="block text-sm">
              <span className="font-medium text-slate-700">内置匹配器</span>
              <div className="mt-1.5 rounded-lg border border-indigo-100 bg-indigo-50/60 px-3 py-2.5">
                <div className="text-sm font-medium text-indigo-900">{describePattern(rule)}</div>
                <p className="mt-1 text-xs leading-relaxed text-indigo-700">
                  {BUILTIN_MATCHERS[rule.pattern]?.detail || rule.pattern}
                </p>
              </div>
              <p className="mt-1 text-xs text-slate-500">
                判定逻辑在程序内实现，带校验位与结构画像，不可编辑。需要不同口径请另建一条正则规则。
              </p>
            </div>
          ) : (
          <label className="block text-sm">
            <span className="font-medium text-slate-700">
              {rule.matchType === 'literal' ? '匹配文本' : '正则表达式'}
            </span>
            <textarea
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

          <div className="grid gap-4 sm:grid-cols-2">
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
                    { value: 'detect', label: '仅记录命中' }
                  ]}
                />
              </div>
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
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/80 px-5 py-3">
          <Button variant="secondary" size="sm" onClick={onClose} disabled={saving}>取消</Button>
          <Button size="sm" onClick={onSubmit} disabled={saving}>{saving ? '保存中…' : '保存'}</Button>
        </div>
      </div>
    </div>
  );
};

// 测试面板的输入与结果都停留在本机，不落库、不上报。
// 规则测试弹窗。按需打开，不常驻占位。
// 输入与结果都停留在本机：不落库、不进日志、不发往任何服务商。
const TestModal = ({ open, onClose }) => {
  const [input, setInput] = useState('');
  const [result, setResult] = useState(null);
  const [running, setRunning] = useState(false);
  const toast = useToast();

  useEffect(() => {
    if (!open) return undefined;
    const onKeyDown = (event) => {
      if (event.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [open, onClose]);

  if (!open) return null;

  const run = async () => {
    if (!input.trim()) return;
    setRunning(true);
    try {
      setResult(await previewPrivacyRedaction(input));
    } catch (error) {
      toast.error(`测试失败: ${error.message}`);
    } finally {
      setRunning(false);
    }
  };

  return renderModalPortal(
    <div className="fixed inset-0 z-[10000] flex items-center justify-center p-4">
      <div className={OVERLAY_CLASS} onClick={onClose} />
      <div className={`${PANEL_CLASS} max-h-[85vh] max-w-2xl`}>
        <div className="flex items-center justify-between border-b border-slate-100 px-5 py-3.5">
          <h3 className="flex items-center gap-2 text-sm font-semibold text-slate-900">
            <FlaskConical className="h-4 w-4 text-slate-400" />
            规则测试
          </h3>
          <button type="button" onClick={onClose} className="rounded-md p-1 text-slate-400 hover:bg-slate-100">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex-1 space-y-3 overflow-y-auto px-5 py-4">
          <p className="text-xs text-slate-500">
            试跑当前全部启用规则。输入与结果只停留在本机，不会写入数据库或发往任何服务商。
          </p>
          <textarea
            autoFocus
            value={input}
            onChange={(event) => setInput(event.target.value)}
            rows={5}
            placeholder="粘贴一段文本，检查规则会命中什么"
            className={FIELD_CLASS}
          />
          {result && (
            <div className="space-y-2">
              <div className="text-xs text-slate-500">
                命中 {result.hitCount} 处{result.changed ? '，已替换' : '，无替换'}
              </div>
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

        <div className="flex items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/80 px-5 py-3">
          <Button variant="secondary" size="sm" onClick={onClose}>关闭</Button>
          <Button size="sm" onClick={run} disabled={running || !input.trim()}>
            {running ? '测试中…' : '运行'}
          </Button>
        </div>
      </div>
    </div>
  );
};

const PrivacyPage = () => {
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
    () => (sourceFilter === 'all' ? rules : rules.filter((rule) => rule.source === sourceFilter)),
    [rules, sourceFilter]
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
    } catch (error) {
      toast.error(`保存失败: ${error.message}`);
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
    }
  };

  if (loading) return <LoadingSpinner />;
  if (loadError) return <ErrorMessage message={loadError} onRetry={load} />;
  if (!settings) return <ErrorMessage message="隐私防护服务未就绪" onRetry={load} />;

  return (
    <div className="space-y-5 p-6">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="flex items-center gap-2 text-lg font-semibold text-slate-900">
            <ShieldCheck className="h-5 w-5 text-emerald-600" />
            隐私防护
          </h1>
          <p className="mt-1 text-sm text-slate-500">
            请求发往服务商前先在本机扫描。未命中的字节与原始请求完全一致，命中的原文不写入数据库、日志或本页面。
          </p>
        </div>
        <Button variant="secondary" size="sm" onClick={load}>
          <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
          刷新
        </Button>
      </div>

      <UnscannedPathsBanner
        paths={overview.unscannedPaths}
        onClear={async () => {
          await clearPrivacyUnscannedPaths();
          await load();
        }}
      />

      <div className="rounded-xl border border-slate-200 bg-white p-4">
        <h2 className="mb-3 text-sm font-semibold text-slate-900">运行模式</h2>
        <ModeSelector
          mode={settings.mode}
          saving={savingSettings}
          onChange={(mode) => saveSettings({ ...settings, mode })}
        />
        <div className="mt-4 border-t border-slate-100 pt-4">
          <PolicyPanel settings={settings} saving={savingSettings} onChange={saveSettings} />
        </div>
      </div>

      <LegacyRulesPanel
        rules={overview.legacyRules}
        onDiscard={async () => {
          await discardLegacyPrivacyRules();
          await load();
          toast.success('旧版规则清单已清除');
        }}
      />

      <div className="rounded-xl border border-slate-200 bg-white">
        <div className="flex flex-wrap items-center gap-2 border-b border-slate-100 px-4 py-3">
          <h2 className="text-sm font-semibold text-slate-900">
            规则（{enabledCount}/{rules.length} 启用）
          </h2>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            {overview.presets.map((preset) => (
              <Button key={preset.id} variant="secondary" size="sm" onClick={() => importPreset(preset.id)}>
                <Package className="mr-1.5 h-3.5 w-3.5" />
                导入{preset.name}（{preset.ruleCount}）
              </Button>
            ))}
            <Button variant="secondary" size="sm" onClick={() => setTestOpen(true)}>
              <FlaskConical className="mr-1.5 h-3.5 w-3.5" />
              规则测试
            </Button>
            <Button size="sm" onClick={() => { setModalRule(emptyRule()); setModalError(''); }}>
              <Plus className="mr-1.5 h-3.5 w-3.5" />
              新建规则
            </Button>
          </div>
        </div>

        {/* 来源筛选：规则会随预设导入快速增长，按来源收窄是最省事的定位方式 */}
        <div className="flex flex-wrap items-center gap-1.5 border-b border-slate-100 px-4 py-2">
          {SOURCE_FILTERS.map((item) => {
            const count = item.value === 'all'
              ? rules.length
              : rules.filter((rule) => rule.source === item.value).length;
            const active = sourceFilter === item.value;
            return (
              <button
                key={item.value}
                type="button"
                onClick={() => setSourceFilter(item.value)}
                className={`rounded-md px-2 py-1 text-xs font-medium transition-colors ${
                  active ? 'bg-slate-900 text-white' : 'text-slate-500 hover:bg-slate-100'
                }`}
              >
                {item.label}
                <span className={`ml-1 ${active ? 'text-white/60' : 'text-slate-400'}`}>{count}</span>
              </button>
            );
          })}
        </div>

        {/* 批量操作条：导入预设是一次性加十几条，逐条清理太痛苦 */}
        {selectedIds.length > 0 && (
          <div className="flex flex-wrap items-center gap-2 border-b border-indigo-100 bg-indigo-50/60 px-4 py-2">
            <span className="text-xs font-medium text-indigo-900">已选 {selectedIds.length} 条</span>
            <div className="ml-auto flex flex-wrap items-center gap-1.5">
              <Button variant="secondary" size="sm" onClick={() => bulkToggle(true)} disabled={bulkBusy}>
                批量启用
              </Button>
              <Button variant="secondary" size="sm" onClick={() => bulkToggle(false)} disabled={bulkBusy}>
                批量停用
              </Button>
              <Button variant="dangerSoft" size="sm" onClick={() => setBulkDeleteOpen(true)} disabled={bulkBusy}>
                <Trash className="mr-1.5 h-3.5 w-3.5" />
                批量删除
              </Button>
              <Button variant="ghost" size="sm" onClick={() => setSelectedIds([])} disabled={bulkBusy}>
                取消选择
              </Button>
            </div>
          </div>
        )}

        {rules.length === 0 ? (
          <p className="px-4 py-8 text-center text-sm text-slate-500">
            还没有任何规则。先导入一套预设，或新建自定义规则。
          </p>
        ) : visibleRules.length === 0 ? (
          <p className="px-4 py-8 text-center text-sm text-slate-500">当前筛选下没有规则。</p>
        ) : (
          // 限高滚动而非无限增高：规则可以到几十条，页面不该跟着一直变长
          <div className="max-h-[38rem] overflow-auto px-4">
            <table className="w-full text-left">
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
                  <th className="py-2 pr-3 font-medium">来源</th>
                  <th className="py-2 pr-3 font-medium">替换为</th>
                  <th className="py-2 pr-3 font-medium">优先级</th>
                  <th className="py-2" />
                </tr>
              </thead>
              <tbody>
                {visibleRules.map((rule) => (
                  <RuleRow
                    key={rule.id}
                    rule={rule}
                    busy={busyRuleId === rule.id}
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
      </div>

      <TestModal open={testOpen} onClose={() => setTestOpen(false)} />

      <RuleModal
        open={Boolean(modalRule)}
        rule={modalRule || emptyRule()}
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
