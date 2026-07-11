import { useState } from 'react';
import { XCircle } from 'lucide-react';
import { Button } from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';
import { parseMultiplierExpression } from '@utils/multiplier.js';

const getFieldKey = (field) => field.key || field.name;

const LongContextMultiplierSettings = ({ fields, values, disabled = false, onCancel, onSave }) => {
  const [draft, setDraft] = useState(() => ({ ...values }));
  const [error, setError] = useState('');

  const handleSave = () => {
    for (const field of fields) {
      const value = parseMultiplierExpression(draft[getFieldKey(field)], Number.NaN);
      if (!Number.isFinite(value) || value < 0) {
        setError(`${field.label}必须是非负数字或分数，例如 1.25 或 40/30`);
        return;
      }
    }
    onSave?.(draft);
  };

  return renderModalPortal(
    <div className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-sm animate-fade-in">
      <div role="dialog" aria-modal="true" aria-labelledby="long-context-settings-title" className="w-full max-w-xl overflow-hidden rounded-2xl bg-white shadow-2xl">
        <div className="flex items-center justify-between border-b border-slate-100 px-5 py-4">
          <div>
            <h3 id="long-context-settings-title" className="text-base font-semibold text-slate-900">Long context 成本倍率</h3>
            <p className="mt-0.5 text-xs text-slate-500">命中模型 Long context 阈值后应用以下倍率。</p>
          </div>
          <button type="button" onClick={onCancel} className="rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700" aria-label="关闭" disabled={disabled}>
            <XCircle size={18} />
          </button>
        </div>

        <div className="grid grid-cols-1 gap-3 px-5 py-4 sm:grid-cols-2">
          {fields.map((field) => {
            const key = getFieldKey(field);
            return (
              <div key={key}>
                <label className="mb-1 block text-xs text-slate-500">{field.label}</label>
                <input
                  type="text"
                  inputMode="decimal"
                  value={draft[key] ?? ''}
                  onChange={(event) => {
                    setDraft((current) => ({ ...current, [key]: event.target.value }));
                    if (error) setError('');
                  }}
                  className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                  placeholder="1.0"
                  disabled={disabled}
                />
              </div>
            );
          })}
        </div>

        {error && <p className="px-5 pb-3 text-sm text-red-600">{error}</p>}

        <div className="flex justify-end gap-3 border-t border-slate-100 px-5 py-4">
          <Button type="button" variant="ghost" onClick={onCancel} disabled={disabled}>取消</Button>
          <Button type="button" variant="primary" onClick={handleSave} disabled={disabled}>保存设置</Button>
        </div>
      </div>
    </div>
  );
};

export default LongContextMultiplierSettings;
