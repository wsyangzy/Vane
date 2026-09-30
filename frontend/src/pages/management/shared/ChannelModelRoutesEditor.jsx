// ============================================
// 渠道编辑弹窗的「模型路由」标签页（Claude / Codex 共用）
//
// 规则挂在当前渠道上：只在本渠道为激活渠道时生效，语义是
// "本渠道不适合处理模型 X，把 X 送到这些目标"。其它渠道的请求不受影响。
// 规则逐条即时保存（与隐私规则一致），不跟随渠道表单的「保存修改」。
// ============================================

import { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowRight, Check, FlaskConical, PencilLine, Plus, Trash, X } from 'lucide-react';
import { Button, ConfirmDialog, SegmentedTabs, useToast } from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';
import {
  deleteModelRoute,
  getRouteTargetOptions,
  listModelRoutes,
  previewRoute,
  saveModelRoute,
  setModelRouteEnabled
} from '@utils/wailsApi.js';
import RouteTargetsEditor from './RouteTargetsEditor.jsx';
import { getErrorMessage } from './formatters.js';
import {
  emptyRouteTarget,
  ROUTE_MODE_OPTIONS,
  routeModeLabel,
  validateModelRouteForm
} from './routeTargets.js';

const FIELD_CLASS =
  'w-full rounded-lg border border-slate-200 px-3 py-2 text-sm transition-colors ' +
  'focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500';

// 叠在渠道编辑弹窗之上，层级与 ConfirmDialog 一致；CustomSelect 下拉浮层为 z-[10010]
const OVERLAY_CLASS = 'absolute inset-0 bg-slate-900/20 backdrop-blur-sm animate-in fade-in duration-200';
const PANEL_CLASS =
  'relative flex w-full flex-col overflow-hidden rounded-2xl bg-white shadow-2xl ring-1 ring-black/5 ' +
  'animate-in zoom-in-95 fade-in duration-200';

const MODEL_PLACEHOLDER = {
  claude: 'claude-haiku-4-5 或 claude-haiku-*',
  codex: 'gpt-5-mini 或 gpt-5-*'
};

const emptyRule = (provider, sourceChannelId) => ({
  id: 0,
  provider,
  sourceChannelId,
  name: '',
  enabled: true,
  priority: '100',
  matchModel: '',
  mode: 'prefer',
  targets: [emptyRouteTarget()]
});

const describeTarget = (target) => {
  if (!target.channelName) return '已删除的渠道';
  if (target.missing) return `${target.channelName} / 已删除的分组`;
  return target.groupName ? `${target.channelName} / ${target.groupName}` : target.channelName;
};

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

const RuleItem = ({ rule, busy, onToggle, onEdit, onDelete }) => (
  <div className="rounded-lg border border-slate-200 bg-white px-3 py-2.5">
    <div className="flex items-center gap-3">
      <Toggle checked={rule.enabled} disabled={busy} onChange={(value) => onToggle(rule, value)} label={`启用 ${rule.name}`} />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-medium text-slate-800">{rule.name}</span>
          <code className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-xs text-slate-700">{rule.matchModel}</code>
          <span className="text-xs text-slate-500">{routeModeLabel('model_rule', rule.mode)} · 优先级 {rule.priority}</span>
        </div>
        <div className="mt-1.5 flex flex-wrap items-center gap-1">
          {rule.targets.map((target, index) => (
            <span
              key={`${target.channelId}-${target.groupId}-${index}`}
              className={`inline-flex max-w-full items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] ${
                target.missing ? 'border-rose-200 bg-rose-50 text-rose-700' : 'border-slate-200 bg-slate-50 text-slate-600'
              }`}
            >
              <span className="font-semibold text-slate-400">{index + 1}</span>
              <span className="truncate">{describeTarget(target)}</span>
              {target.upstreamModel && (
                <>
                  <ArrowRight className="h-3 w-3 flex-shrink-0 text-slate-400" />
                  <code className="truncate font-mono">{target.upstreamModel}</code>
                </>
              )}
            </span>
          ))}
        </div>
      </div>
      <div className="flex flex-shrink-0 items-center gap-1">
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
          onClick={() => onDelete(rule)}
          className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-rose-50 hover:text-rose-600"
          aria-label={`删除 ${rule.name}`}
        >
          <Trash className="h-4 w-4" />
        </button>
      </div>
    </div>
  </div>
);

const RuleModal = ({ rule, provider, channelId, options, saving, error, onChange, onClose, onSubmit }) => renderModalPortal(
  <div className="fixed inset-0 z-[10000] flex items-center justify-center p-4">
    <div className={OVERLAY_CLASS} />
    {/* 独立 div 而非 form：本弹窗通过 portal 渲染，但 React 合成事件仍会冒泡到渠道表单的 onSubmit */}
    <div className={`${PANEL_CLASS} max-h-[90vh] max-w-3xl`} role="dialog" aria-modal="true" aria-label={rule.id ? '编辑模型路由规则' : '新建模型路由规则'}>
      <div className="flex items-center justify-between border-b border-slate-100 px-5 py-3.5">
        <h3 className="text-sm font-semibold text-slate-900">{rule.id ? '编辑模型路由规则' : '新建模型路由规则'}</h3>
        <button type="button" onClick={onClose} className="rounded-md p-1 text-slate-400 hover:bg-slate-100" aria-label="关闭">
          <X className="h-4 w-4" />
        </button>
      </div>

      <div className="flex-1 space-y-4 overflow-y-auto px-5 py-4">
        {error && (
          <div className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">{error}</div>
        )}

        <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_10rem]">
          <label className="block text-sm">
            <span className="font-medium text-slate-700">规则名称</span>
            <input
              value={rule.name}
              onChange={(event) => onChange({ ...rule, name: event.target.value })}
              className={`mt-1.5 ${FIELD_CLASS}`}
              placeholder="例如：haiku 改道"
            />
          </label>
          <label className="block text-sm">
            <span className="font-medium text-slate-700">优先级</span>
            <input
              type="number"
              step={1}
              value={rule.priority}
              onChange={(event) => onChange({ ...rule, priority: event.target.value })}
              className={`mt-1.5 ${FIELD_CLASS}`}
            />
          </label>
        </div>

        <label className="block text-sm">
          <span className="font-medium text-slate-700">匹配模型</span>
          <input
            value={rule.matchModel}
            onChange={(event) => onChange({ ...rule, matchModel: event.target.value })}
            className={`mt-1.5 ${FIELD_CLASS} font-mono`}
            placeholder={MODEL_PLACEHOLDER[provider] || MODEL_PLACEHOLDER.claude}
          />
          <p className="mt-1 text-xs leading-relaxed text-slate-500">
            只看请求里的模型名，大小写不敏感，可用 * 通配；流式与非流式请求都适用。
            本渠道多条规则都能匹配时：精确值 &gt; 通配（* 之前的固定部分越长越优先）&gt; 单独的 *，再比优先级（数字小者优先）。
          </p>
        </label>

        <div className="text-sm">
          <span className="font-medium text-slate-700">执行方式</span>
          <div className="mt-1.5 grid gap-2 sm:grid-cols-2">
            {ROUTE_MODE_OPTIONS.model_rule.map((mode) => {
              const active = rule.mode === mode.value;
              return (
                <button
                  key={mode.value}
                  type="button"
                  onClick={() => onChange({ ...rule, mode: mode.value })}
                  aria-pressed={active}
                  className={`rounded-xl border p-3 text-left transition-colors ${
                    active ? 'border-indigo-300 bg-indigo-50 ring-1 ring-indigo-200' : 'border-slate-200 bg-white hover:border-slate-300'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    <span className={`text-sm font-semibold ${active ? 'text-indigo-800' : 'text-slate-700'}`}>{mode.label}</span>
                    {active && <Check className="ml-auto h-4 w-4 text-indigo-600" />}
                  </div>
                  <p className="mt-1 text-xs leading-relaxed text-slate-500">{mode.description}</p>
                </button>
              );
            })}
          </div>
        </div>

        <div className="text-sm">
          <span className="font-medium text-slate-700">目标（按顺序尝试）</span>
          <div className="mt-1.5">
            <RouteTargetsEditor
              targets={rule.targets}
              onChange={(targets) => onChange({ ...rule, targets })}
              channels={options.channels}
              models={options.models}
              excludeChannelId={channelId}
              disabled={saving}
              idPrefix={`${provider}-rule-${channelId}-${rule.id || 'new'}`}
            />
          </div>
          <p className="mt-2 text-xs leading-relaxed text-slate-400">
            上游模型留空时沿用目标分组的模型策略；填写后强制改写为该值。只想在本渠道内改写模型名，请用「模型映射」。
            跨厂商目标建议显式填写上游模型，并在「基础定价」中补齐它的定价，否则成本会落到兜底价。
          </p>
        </div>

        <div className="flex items-center justify-between gap-4 rounded-lg border border-slate-200 px-3 py-2.5">
          <span className="text-sm font-medium text-slate-700">保存后立即启用</span>
          <Toggle checked={rule.enabled} onChange={(enabled) => onChange({ ...rule, enabled })} label="启用规则" />
        </div>
      </div>

      <div className="flex items-center justify-end gap-2 border-t border-slate-100 px-5 py-3">
        <Button variant="ghost" size="sm" type="button" onClick={onClose} disabled={saving}>
          取消
        </Button>
        <Button size="sm" type="button" onClick={onSubmit} loading={saving}>
          保存规则
        </Button>
      </div>
    </div>
  </div>
);

const PreviewResult = ({ result, channelName }) => {
  if (!result.kind) {
    return (
      <p className="text-xs text-slate-600">
        未命中本渠道的模型规则{result.streaming ? '' : '，也未开启非流改道'}：由「{channelName}」按默认流程处理。
      </p>
    );
  }
  const title = result.kind === 'model_rule' ? `命中规则「${result.ruleName}」` : '命中非流改道';
  return (
    <div className="space-y-1.5">
      <div className="text-xs font-medium text-slate-800">
        {title}
        <span className="ml-2 rounded-md border border-indigo-200 bg-indigo-50 px-1.5 py-0.5 text-[11px] font-medium text-indigo-700">
          {routeModeLabel(result.kind, result.mode)}
        </span>
      </div>
      <ol className="space-y-1">
        {result.stages.map((stage, index) => {
          const skipped = stage.missing || stage.paused;
          const label = stage.target === 0
            ? `本渠道「${channelName}」（只在渠道内换端点）`
            : `目标 ${stage.target}：${stage.channelName || '已删除的渠道'} / ${stage.groupName || (stage.groupId > 0 ? '已删除的分组' : '全部分组')}`;
          return (
            <li key={`${stage.target}-${index}`} className="flex flex-wrap items-center gap-2 text-xs">
              <span className="inline-flex h-5 min-w-5 items-center justify-center rounded bg-slate-100 px-1 font-semibold text-slate-500">{index + 1}</span>
              <span className={skipped ? 'text-slate-400 line-through' : 'text-slate-700'}>{label}</span>
              {stage.upstreamModel && (
                <span className="inline-flex items-center gap-1 text-slate-500">
                  <ArrowRight className="h-3 w-3" />
                  <code className="font-mono">{stage.upstreamModel}</code>
                </span>
              )}
              {skipped && (
                <span className="rounded border border-amber-200 bg-amber-50 px-1.5 py-0.5 text-[11px] text-amber-700">
                  {stage.paused ? '已暂停，将被跳过' : '将被跳过'}
                </span>
              )}
            </li>
          );
        })}
      </ol>
    </div>
  );
};

// dry-run：假设本渠道为激活渠道，按已保存的配置推演，不发任何上游请求。
// 渠道表单里尚未保存的非流改道改动不参与推演。
const PreviewPanel = ({ provider, channelId, channelName, models }) => {
  const [model, setModel] = useState('');
  const [streaming, setStreaming] = useState(false);
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');
  const datalistId = `${provider}-route-preview-${channelId}`;

  const run = async () => {
    const trimmed = model.trim();
    if (!trimmed) {
      setError('请输入请求模型');
      return;
    }
    setRunning(true);
    setError('');
    try {
      const preview = await previewRoute(provider, channelId, trimmed, streaming);
      setResult({ ...preview, streaming });
    } catch (err) {
      setResult(null);
      setError(getErrorMessage(err, '预演失败'));
    } finally {
      setRunning(false);
    }
  };

  return (
    <div className="rounded-lg border border-slate-200 bg-slate-50/60 p-3">
      <div className="mb-2 flex items-center gap-2">
        <FlaskConical className="h-3.5 w-3.5 text-slate-400" />
        <span className="text-xs font-semibold text-slate-700">路由预演</span>
        <span className="text-[11px] text-slate-400">假设本渠道为激活渠道，按已保存的配置推演，不发出请求</span>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <input
          list={datalistId}
          value={model}
          onChange={(event) => setModel(event.target.value)}
          onKeyDown={(event) => {
            // 阻止回车冒泡提交外层渠道表单
            if (event.key === 'Enter') {
              event.preventDefault();
              run();
            }
          }}
          placeholder="请求模型"
          aria-label="预演请求模型"
          className={`${FIELD_CLASS} max-w-xs flex-1 bg-white py-1.5 font-mono text-xs`}
        />
        <datalist id={datalistId}>
          {models.map((item) => (
            <option key={item} value={item} />
          ))}
        </datalist>
        <SegmentedTabs
          tabs={[
            { key: 'non-stream', label: '非流式' },
            { key: 'stream', label: '流式' }
          ]}
          activeTab={streaming ? 'stream' : 'non-stream'}
          onChange={(key) => setStreaming(key === 'stream')}
          className="w-36 grid-cols-2"
        />
        <Button size="sm" type="button" onClick={run} loading={running}>
          预演
        </Button>
      </div>
      {error && <p className="mt-2 text-xs text-rose-600">{error}</p>}
      {result && (
        <div className="mt-2 rounded-md border border-slate-100 bg-white p-2.5">
          <PreviewResult result={result} channelName={channelName} />
        </div>
      )}
    </div>
  );
};

const ChannelModelRoutesEditor = ({ provider, channelId, channelName, disabled = false }) => {
  const [rules, setRules] = useState([]);
  const [options, setOptions] = useState({ channels: [], models: [] });
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [busyRuleId, setBusyRuleId] = useState(0);
  const [modalRule, setModalRule] = useState(null);
  const [modalError, setModalError] = useState('');
  const [modalSaving, setModalSaving] = useState(false);
  const [pendingDelete, setPendingDelete] = useState(null);
  const loadSeqRef = useRef(0);
  const toast = useToast();

  const load = useCallback(async () => {
    const seq = ++loadSeqRef.current;
    try {
      const [nextRules, nextOptions] = await Promise.all([
        listModelRoutes(provider, channelId),
        getRouteTargetOptions(provider)
      ]);
      if (seq !== loadSeqRef.current) return;
      setLoadError('');
      setRules(nextRules);
      setOptions(nextOptions);
    } catch (error) {
      if (seq !== loadSeqRef.current) return;
      setLoadError(getErrorMessage(error, '加载模型路由失败'));
    } finally {
      if (seq === loadSeqRef.current) setLoading(false);
    }
  }, [provider, channelId]);

  useEffect(() => { load(); }, [load]);

  const toggleRule = async (rule, enabled) => {
    setBusyRuleId(rule.id);
    try {
      await setModelRouteEnabled(rule.id, enabled);
      await load();
    } catch (error) {
      toast.error(getErrorMessage(error, '更新失败'), '更新失败');
    } finally {
      setBusyRuleId(0);
    }
  };

  const submitRule = async () => {
    const validationError = validateModelRouteForm(modalRule);
    if (validationError) {
      setModalError(validationError);
      return;
    }
    setModalSaving(true);
    setModalError('');
    try {
      const result = await saveModelRoute({ ...modalRule, provider, sourceChannelId: channelId });
      setModalRule(null);
      await load();
      if (result.warnings.length > 0) {
        toast.warning(result.warnings, '规则已保存，请留意');
      } else {
        toast.success('规则已保存并立即生效');
      }
    } catch (error) {
      setModalError(getErrorMessage(error, '保存失败'));
    } finally {
      setModalSaving(false);
    }
  };

  const confirmDelete = async () => {
    try {
      await deleteModelRoute(pendingDelete.id);
      setPendingDelete(null);
      await load();
      toast.success('规则已删除');
    } catch (error) {
      toast.error(getErrorMessage(error, '删除失败'), '删除失败');
    }
  };

  const openEdit = (rule) => {
    setModalError('');
    setModalRule({
      ...rule,
      priority: String(rule.priority),
      targets: rule.targets.length > 0 ? rule.targets.map((target) => ({ ...target })) : [emptyRouteTarget()]
    });
  };

  return (
    <div className="space-y-4">
      <div className="rounded-xl border border-sky-100 bg-sky-50/70 px-3.5 py-3 text-xs leading-relaxed text-sky-900">
        本渠道作为当前激活渠道时，把命中规则的模型改送到指定目标（渠道 / 凭证分组 / 上游模型）。
        规则只属于本渠道：切换到其它渠道后不生效，也不会影响其它渠道的请求。改道只作用于本次请求，不切换全局激活渠道。
        模型规则优先于非流改道。
      </div>

      <div className="flex items-center justify-between gap-3">
        <span className="text-sm font-medium text-slate-700">
          规则（{rules.filter((rule) => rule.enabled).length}/{rules.length} 启用）
        </span>
        <Button
          size="sm"
          type="button"
          disabled={disabled || loading || Boolean(loadError)}
          onClick={() => {
            setModalError('');
            setModalRule(emptyRule(provider, channelId));
          }}
        >
          <Plus className="mr-1.5 h-3.5 w-3.5" />
          新建规则
        </Button>
      </div>

      {loading ? (
        <p className="py-6 text-center text-xs text-slate-400">加载中...</p>
      ) : loadError ? (
        <div className="flex items-center justify-between gap-3 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700">
          <span>{loadError}</span>
          <button type="button" onClick={load} className="font-medium underline">重试</button>
        </div>
      ) : rules.length === 0 ? (
        <p className="rounded-lg border border-dashed border-slate-200 py-6 text-center text-xs text-slate-500">
          本渠道还没有模型路由规则，所有模型都按原流程在本渠道处理。
        </p>
      ) : (
        <div className="space-y-2">
          {rules.map((rule) => (
            <RuleItem
              key={rule.id}
              rule={rule}
              busy={disabled || busyRuleId === rule.id}
              onToggle={toggleRule}
              onEdit={openEdit}
              onDelete={setPendingDelete}
            />
          ))}
        </div>
      )}

      {!loading && !loadError && (
        <PreviewPanel provider={provider} channelId={channelId} channelName={channelName} models={options.models} />
      )}

      {modalRule && (
        <RuleModal
          rule={modalRule}
          provider={provider}
          channelId={channelId}
          options={options}
          saving={modalSaving}
          error={modalError}
          onChange={setModalRule}
          onClose={() => setModalRule(null)}
          onSubmit={submitRule}
        />
      )}

      <ConfirmDialog
        isOpen={Boolean(pendingDelete)}
        variant="danger"
        title="删除模型路由规则"
        message={`确定删除规则「${pendingDelete?.name || ''}」？删除后本渠道匹配的请求将按原流程处理。`}
        confirmText="删除"
        onConfirm={confirmDelete}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  );
};

export default ChannelModelRoutesEditor;
