import { useEffect, useState } from 'react';
import { ArrowLeft, CheckCircle2, XCircle } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';

const buildRows = (endpoints = []) => (
  endpoints.map((endpoint, index) => ({
    rowId: `${endpoint?.id || 'copy'}-${index}`,
    sourceName: endpoint?.name || '',
    name: endpoint?.name || '',
    url: endpoint?.url || endpoint?.baseURL || endpoint?.base_url || '',
    priority: endpoint?.priority,
    timeoutSeconds: endpoint?.timeoutSeconds ?? endpoint?.timeout_seconds,
    failoverEnabled: endpoint?.failoverEnabled ?? endpoint?.failover_enabled,
    skipHealthCheck: endpoint?.skipHealthCheck ?? endpoint?.skip_health_check,
    endpoint
  }))
);

const getValidationError = (rows) => {
  const names = rows.map((row) => row.name.trim());
  if (names.some((name) => !name)) return '端点名称不能为空';
  const seen = new Set();
  for (const name of names) {
    if (seen.has(name)) return `端点名称重复：${name}`;
    seen.add(name);
  }
  return '';
};

const formatMeta = (row) => {
  const meta = [];
  if (row.priority !== undefined && row.priority !== null && row.priority !== '') meta.push(`P${row.priority}`);
  if (row.timeoutSeconds) meta.push(`${row.timeoutSeconds}s`);
  meta.push(row.failoverEnabled === false ? '不参与转移' : '参与转移');
  if (row.skipHealthCheck) meta.push('跳过检测');
  return meta.join(' · ');
};

export const EndpointCopySourcePicker = ({
  options = [],
  value = '',
  disabled = false,
  onChange
}) => {
  const [open, setOpen] = useState(false);
  const selectedOption = options.find((option) => option.value === value) || options[0] || null;
  const hasSource = !!value;

  const selectOption = (nextValue) => {
    onChange?.(nextValue);
    setOpen(false);
  };

  return (
    <>
      <button
        type="button"
        onClick={() => !disabled && setOpen(true)}
        disabled={disabled}
        className={`flex w-full items-center justify-between gap-3 rounded-lg border border-slate-200 bg-white px-3 py-2 text-left text-sm transition-colors ${
          disabled ? 'cursor-not-allowed opacity-60' : 'hover:border-indigo-200 hover:bg-slate-50'
        }`}
      >
        <span className="min-w-0 truncate font-medium text-slate-700">
          {hasSource ? (selectedOption?.name || selectedOption?.label) : '选择来源分组'}
        </span>
        <span className="shrink-0 text-xs text-slate-400">
          {hasSource && selectedOption?.endpointCount > 0 ? `${selectedOption.endpointCount} 个端点` : '未选择'}
        </span>
      </button>

      {open && renderModalPortal(
        <div className="fixed inset-0 z-[140] flex items-center justify-center bg-slate-900/25 px-4 backdrop-blur-[1px]">
          <div className="w-full max-w-md overflow-hidden rounded-2xl bg-white shadow-2xl">
            <div className="flex items-start justify-between gap-3 border-b border-slate-100 px-5 py-4">
              <div>
                <h3 className="text-base font-semibold text-slate-900">选择来源分组</h3>
                <p className="mt-0.5 text-xs text-slate-500">选择已有分组后，下一步编辑复制端点名称</p>
              </div>
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="rounded-lg p-1.5 text-slate-400 hover:bg-slate-100 hover:text-slate-700"
                aria-label="关闭"
              >
                <XCircle size={18} />
              </button>
            </div>

            <div className="max-h-[50vh] space-y-1.5 overflow-y-auto p-2">
              {options.map((option) => {
                const selected = option.value === value;
                return (
                  <button
                    key={option.value || 'none'}
                    type="button"
                    onClick={() => selectOption(option.value)}
                    className={`flex w-full items-center justify-between gap-3 rounded-xl px-3 py-2.5 text-left text-sm transition-colors ${
                      selected
                        ? 'bg-indigo-50 text-indigo-700 ring-1 ring-indigo-200'
                        : 'text-slate-600 hover:bg-slate-50'
                    }`}
                  >
                    <span className="min-w-0 truncate font-medium">{option.name || option.label}</span>
                    <span className="shrink-0 text-xs text-slate-400">
                      {option.endpointCount > 0 ? `${option.endpointCount} 个端点` : '不复制'}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>
        </div>
      )}
    </>
  );
};

const EndpointCopyNameEditorStep = ({
  endpoints = [],
  description = '',
  loading = false,
  error = '',
  onCancel,
  onBack,
  onConfirm
}) => {
  const [rows, setRows] = useState(() => buildRows(endpoints));

  useEffect(() => {
    setRows(buildRows(endpoints));
  }, [endpoints]);

  const validationError = getValidationError(rows);
  const displayError = validationError || error;

  const updateName = (rowId, value) => {
    setRows((prev) => prev.map((row) => (
      row.rowId === rowId ? { ...row, name: value } : row
    )));
  };

  const confirm = () => {
    if (validationError) return;
    onConfirm?.(rows.map((row) => ({
      ...row.endpoint,
      name: row.name.trim()
    })));
  };

  return (
    <>
      <div className="border-b border-slate-100 bg-slate-50/70 px-6 py-3">
        <button
          type="button"
          onClick={onBack}
          className="inline-flex items-center gap-1.5 rounded-lg px-2 py-1 text-xs font-medium text-slate-500 hover:bg-white hover:text-slate-700"
          disabled={loading}
        >
          <ArrowLeft size={14} />
          返回分组信息
        </button>
      </div>

      <div className="px-6 py-5">
        <div className="mb-4">
          <h3 className="text-sm font-semibold text-slate-900">编辑端点名称</h3>
          {description && <p className="mt-0.5 text-xs text-slate-500">{description}</p>}
        </div>

        {displayError && (
          <div className="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
            {displayError}
          </div>
        )}

        <div className="max-h-[42vh] overflow-y-auto rounded-xl border border-slate-200 bg-slate-50/70 p-2">
          <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
            {rows.map((row, index) => (
              <div key={row.rowId} className="min-w-0 rounded-lg border border-slate-200 bg-white p-2">
                <div className="mb-1 flex min-w-0 items-center justify-between gap-2">
                  <span className="shrink-0 text-xs font-medium text-slate-500">端点 {index + 1}</span>
                  {row.sourceName && row.sourceName !== row.name && (
                    <span className="min-w-0 truncate text-[11px] text-slate-400">源名称：{row.sourceName}</span>
                  )}
                </div>
                <input
                  value={row.name}
                  onChange={(event) => updateName(row.rowId, event.target.value)}
                  className="w-full rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                  disabled={loading}
                  autoFocus={index === 0}
                />
                <div className="mt-1 flex min-w-0 items-center gap-2 text-xs text-slate-400">
                  {row.url && <span className="min-w-0 truncate font-mono">{row.url}</span>}
                  {formatMeta(row) && <span className="shrink-0">{formatMeta(row)}</span>}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="flex items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/70 px-6 py-4">
        <Button type="button" variant="ghost" onClick={onCancel} disabled={loading}>
          取消
        </Button>
        <Button
          type="button"
          variant="primary"
          icon={CheckCircle2}
          onClick={confirm}
          loading={loading}
          disabled={loading || !!validationError || rows.length === 0}
        >
          创建分组并配置端点
        </Button>
      </div>
    </>
  );
};

export default EndpointCopyNameEditorStep;
