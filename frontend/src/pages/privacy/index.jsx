import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  AlertTriangle,
  Check,
  Eye,
  FlaskConical,
  History,
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
import {
  applyPrivacyPreset,
  clearPrivacyUnscannedPaths,
  createPrivacyRule,
  deletePrivacyRule,
  discardLegacyPrivacyRules,
  fetchPrivacyOverview,
  previewPrivacyRedaction,
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

const RuleRow = ({ rule, onEdit, onDelete, onToggle, busy }) => {
  const isBuiltin = rule.source === 'builtin';
  return (
    <tr className="border-b border-slate-50 last:border-0">
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
        <code className="block truncate font-mono text-xs text-slate-600" title={rule.pattern}>
          {rule.pattern}
        </code>
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

  return (
    <div className="fixed inset-0 z-[10000] flex items-center justify-center bg-slate-900/40 p-4">
      <div className="flex max-h-[90vh] w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl">
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
              className="mt-1.5 w-full rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm"
              placeholder="例如：客户手机号"
            />
          </label>

          <label className="block text-sm">
            <span className="font-medium text-slate-700">说明（可选）</span>
            <input
              value={rule.description}
              onChange={(event) => onChange({ ...rule, description: event.target.value })}
              className="mt-1.5 w-full rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm"
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
                className="mt-1.5 w-full rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm"
              />
              <p className="mt-1 text-xs text-slate-500">与高优先级规则命中区间重叠时，本规则的命中会被丢弃。</p>
            </label>
          </div>

          <label className="block text-sm">
            <span className="font-medium text-slate-700">
              {rule.matchType === 'literal' ? '匹配文本' : rule.matchType === 'builtin' ? '内置匹配器' : '正则表达式'}
            </span>
            <textarea
              disabled={isBuiltin}
              value={rule.pattern}
              onChange={(event) => onChange({ ...rule, pattern: event.target.value })}
              rows={3}
              className="mt-1.5 w-full rounded-lg border border-slate-200 px-2.5 py-1.5 font-mono text-xs disabled:bg-slate-50 disabled:text-slate-500"
            />
            {rule.matchType === 'regex' && !isBuiltin && (
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
                className="mt-1.5 w-full rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm"
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
const TestPanel = () => {
  const [input, setInput] = useState('');
  const [result, setResult] = useState(null);
  const [running, setRunning] = useState(false);
  const toast = useToast();

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

  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4">
      <h3 className="flex items-center gap-2 text-sm font-semibold text-slate-900">
        <FlaskConical className="h-4 w-4 text-slate-400" />
        规则测试
      </h3>
      <p className="mt-1 text-xs text-slate-500">
        试跑当前全部启用规则。输入与结果只停留在本机，不会写入数据库或发往任何服务商。
      </p>
      <textarea
        value={input}
        onChange={(event) => setInput(event.target.value)}
        rows={3}
        placeholder="粘贴一段文本，检查规则会命中什么"
        className="mt-3 w-full rounded-lg border border-slate-200 px-2.5 py-2 text-sm"
      />
      <div className="mt-2 flex items-center gap-2">
        <Button size="sm" onClick={run} disabled={running || !input.trim()}>
          {running ? '测试中…' : '运行'}
        </Button>
        {result && (
          <span className="text-xs text-slate-500">
            命中 {result.hitCount} 处{result.changed ? '，已替换' : '，无替换'}
          </span>
        )}
      </div>
      {result && (
        <div className="mt-3 space-y-2">
          {result.ruleHits.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {result.ruleHits.map((hit) => (
                <Badge key={hit.ruleId} className="border-emerald-200 bg-emerald-50 text-emerald-700">
                  {hit.ruleName} × {hit.count}
                </Badge>
              ))}
            </div>
          )}
          <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 font-mono text-xs text-slate-800">
            {result.output}
          </pre>
        </div>
      )}
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
            <Button size="sm" onClick={() => { setModalRule(emptyRule()); setModalError(''); }}>
              <Plus className="mr-1.5 h-3.5 w-3.5" />
              新建规则
            </Button>
          </div>
        </div>

        {rules.length === 0 ? (
          <p className="px-4 py-8 text-center text-sm text-slate-500">
            还没有任何规则。先导入一套预设，或新建自定义规则。
          </p>
        ) : (
          <div className="overflow-x-auto px-4">
            <table className="w-full text-left">
              <thead className="text-xs text-slate-500">
                <tr className="border-b border-slate-100">
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
                {rules.map((rule) => (
                  <RuleRow
                    key={rule.id}
                    rule={rule}
                    busy={busyRuleId === rule.id}
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

      <TestPanel />

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
