// ============================================
// 路由目标编辑器：模型路由规则与渠道非流改道共用
// 目标按顺序尝试，每行依次选择 渠道 → 凭证分组 → 上游模型
// ============================================

import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react';
import { CustomSelect, Tooltip } from '@/shared/ui/index.js';
import { emptyRouteTarget, moveRouteTarget, routeChannelLabel } from './routeTargets.js';

const MODEL_INPUT_CLASS =
  'w-full min-w-0 rounded-lg border border-slate-200 bg-white px-3 py-1.5 font-mono text-xs transition-colors ' +
  'focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20 disabled:bg-slate-50';

const ICON_BUTTON_CLASS =
  'rounded-md p-1.5 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700 ' +
  'disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent';

const RouteTargetsEditor = ({
  targets,
  onChange,
  channels = [],
  models = [],
  excludeChannelId = 0,
  disabled = false,
  idPrefix = 'route-target'
}) => {
  const rows = targets?.length ? targets : [emptyRouteTarget()];
  const channelOptions = channels
    .filter((channel) => channel.id !== excludeChannelId)
    .map((channel) => ({ value: channel.id, label: routeChannelLabel(channel) }));
  const datalistId = `${idPrefix}-models`;

  const updateRow = (index, patch) => {
    onChange(rows.map((row, rowIndex) => (rowIndex === index ? { ...row, ...patch } : row)));
  };

  const removeRow = (index) => {
    const next = rows.filter((_, rowIndex) => rowIndex !== index);
    onChange(next.length > 0 ? next : [emptyRouteTarget()]);
  };

  return (
    <div className="space-y-2">
      {rows.map((row, index) => {
        const channel = channels.find((item) => item.id === row.channelId) || null;
        const missingChannel = row.channelId > 0 && !channel;
        const missingGroup = Boolean(channel) && row.groupId > 0 && !channel.groups.some((group) => group.id === row.groupId);
        const groupOptions = [
          { value: 0, label: '全部分组' },
          ...(channel?.groups || []).map((group) => ({ value: group.id, label: group.name }))
        ];

        return (
          <div key={`${idPrefix}-${index}`} className="rounded-lg border border-slate-200 bg-white p-2.5">
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-[auto_minmax(0,1.3fr)_minmax(0,1fr)_minmax(0,1.3fr)_auto] sm:items-center">
              <span className="inline-flex h-6 min-w-6 items-center justify-center rounded-md bg-slate-100 px-1.5 text-[11px] font-semibold text-slate-500">
                {index + 1}
              </span>
              <CustomSelect
                fullWidth
                disabled={disabled}
                ariaLabel={`第 ${index + 1} 个目标的渠道`}
                value={row.channelId}
                placeholder={missingChannel ? `已删除的渠道（ID=${row.channelId}）` : '选择渠道'}
                options={channelOptions}
                onChange={(value) => updateRow(index, { channelId: Number(value), groupId: 0 })}
              />
              <CustomSelect
                fullWidth
                disabled={disabled || !channel}
                ariaLabel={`第 ${index + 1} 个目标的凭证分组`}
                value={row.groupId}
                placeholder={missingGroup ? `已删除的分组（ID=${row.groupId}）` : '全部分组'}
                options={groupOptions}
                onChange={(value) => updateRow(index, { groupId: Number(value) })}
              />
              <input
                type="text"
                list={datalistId}
                value={row.upstreamModel}
                onChange={(event) => updateRow(index, { upstreamModel: event.target.value })}
                placeholder="上游模型（留空沿用分组策略）"
                aria-label={`第 ${index + 1} 个目标的上游模型`}
                autoComplete="off"
                className={MODEL_INPUT_CLASS}
                disabled={disabled}
              />
              <div className="flex items-center justify-end gap-0.5">
                <Tooltip content="上移">
                  <button
                    type="button"
                    className={ICON_BUTTON_CLASS}
                    onClick={() => onChange(moveRouteTarget(rows, index, -1))}
                    disabled={disabled || index === 0}
                    aria-label={`上移第 ${index + 1} 个目标`}
                  >
                    <ArrowUp size={14} />
                  </button>
                </Tooltip>
                <Tooltip content="下移">
                  <button
                    type="button"
                    className={ICON_BUTTON_CLASS}
                    onClick={() => onChange(moveRouteTarget(rows, index, 1))}
                    disabled={disabled || index === rows.length - 1}
                    aria-label={`下移第 ${index + 1} 个目标`}
                  >
                    <ArrowDown size={14} />
                  </button>
                </Tooltip>
                <Tooltip content="删除目标">
                  <button
                    type="button"
                    className={`${ICON_BUTTON_CLASS} hover:bg-rose-50 hover:text-rose-600`}
                    onClick={() => removeRow(index)}
                    disabled={disabled}
                    aria-label={`删除第 ${index + 1} 个目标`}
                  >
                    <Trash2 size={14} />
                  </button>
                </Tooltip>
              </div>
            </div>
            {(missingChannel || missingGroup) && (
              <p className="mt-1.5 text-xs text-rose-600">
                {missingChannel ? '目标渠道已被删除' : '目标凭证分组已被删除'}，命中时会被跳过。请重新选择或删除这一行。
              </p>
            )}
          </div>
        );
      })}

      <datalist id={datalistId}>
        {models.map((model) => (
          <option key={model} value={model} />
        ))}
      </datalist>

      <button
        type="button"
        onClick={() => onChange([...rows, emptyRouteTarget()])}
        disabled={disabled}
        className="inline-flex items-center gap-1.5 rounded-lg border border-dashed border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-600 transition-colors hover:border-indigo-300 hover:bg-indigo-50 hover:text-indigo-700 disabled:cursor-not-allowed disabled:opacity-50"
      >
        <Plus size={14} />
        添加目标
      </button>
    </div>
  );
};

export default RouteTargetsEditor;
