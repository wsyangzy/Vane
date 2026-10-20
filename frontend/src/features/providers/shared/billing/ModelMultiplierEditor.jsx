import { Plus, Trash2 } from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';
import { createModelMultiplierRow, MODEL_MULTIPLIER_FIELDS } from './modelMultiplierRows.js';

const ModelMultiplierEditor = ({ rows = [], onChange, disabled = false, className = '' }) => {
  const updateRow = (id, key, value) => {
    onChange?.(rows.map((row) => (row.id === id ? { ...row, [key]: value } : row)));
  };

  const addRow = () => {
    onChange?.([...rows, createModelMultiplierRow()]);
  };

  const removeRow = (id) => {
    onChange?.(rows.filter((row) => row.id !== id));
  };

  return (
    <div className={`rounded-xl border border-slate-200 bg-white p-3 ${className}`}>
      <div className={`${rows.length > 0 ? 'mb-3' : ''} flex items-center justify-between gap-3`}>
        <div className="min-w-0">
          <div className="text-sm font-semibold text-slate-800">模型附加倍率</div>
          <p className="mt-0.5 text-xs text-slate-500">请求使用映射后的模型命中规则；默认只设置总倍率。</p>
        </div>
        <button
          type="button"
          onClick={addRow}
          disabled={disabled}
          className="inline-flex h-9 flex-shrink-0 items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 text-sm font-normal text-slate-600 shadow-none transition-colors duration-150 hover:border-slate-300 hover:bg-slate-50 hover:text-slate-800 disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Plus size={16} strokeWidth={2} />
          添加模型
        </button>
      </div>

      {rows.length > 0 && (
        <div className="space-y-3">
          {rows.map((row) => (
            <div key={row.id} className="rounded-lg border border-slate-200 bg-white p-3 shadow-sm">
              <div className="mb-3 flex items-start gap-2">
                <div className="min-w-0 flex-1">
                  <label className="mb-1 block text-xs font-medium text-slate-500">模型名称</label>
                  <input
                    type="text"
                    value={row.model}
                    onChange={(event) => updateRow(row.id, 'model', event.target.value)}
                    placeholder="例如 gpt-5.1 / claude-sonnet-4-5"
                    className="w-full rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                    disabled={disabled}
                    autoComplete="off"
                    data-form-type="other"
                  />
                </div>
                <Tooltip content="删除模型倍率">
                  <button
                    type="button"
                    onClick={() => removeRow(row.id)}
                    className="mt-6 rounded-lg p-2 text-slate-400 transition-colors hover:bg-rose-50 hover:text-rose-600 disabled:cursor-not-allowed disabled:opacity-50"
                    disabled={disabled}
                    aria-label="删除模型倍率"
                  >
                    <Trash2 size={16} />
                  </button>
                </Tooltip>
              </div>

              <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
                {MODEL_MULTIPLIER_FIELDS.map((field) => (
                  <div key={field.key}>
                    <label className="mb-1 block text-xs text-slate-500">{field.label}</label>
                    <input
                      type="text"
                      inputMode="decimal"
                      value={row[field.key]}
                      onChange={(event) => updateRow(row.id, field.key, event.target.value)}
                      placeholder="1.0"
                      className="w-full rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                      disabled={disabled}
                      autoComplete="off"
                      data-form-type="other"
                    />
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

export default ModelMultiplierEditor;
