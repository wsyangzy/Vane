// ============================================
// CredentialGroupStrip - 凭证分组横向分段选择器
// ============================================
// 一排分段常驻可见，单击即切换。选分组只是查看下方端点列表的前置动作，
// 让它多花一次「展开」的点击不值得——这是废弃堆叠卡片的原因。
//
// 分段只有一行、只放名字：分组区是过路控件，不该跟下方端点列表抢高度。
// 端点的参与转移情况改在悬浮气泡里逐个列出，比原先挤在段内的点阵信息更全。
// 段内只留一颗 6px 状态点，且只在存在不参与转移的端点时出现——正常情况不
// 占一个像素，异常情况不必悬浮就能看见。
//
// 分段宽度只够放名字，编辑/设为默认/删除因此上移到标题行，统一作用于当前
// 选中的分段。选中现在只要一次点击，「先选后操作」并不比原先的行内按钮多花
// 步骤，而且分段区不必为按钮让出宽度。
//
// 分段区高度恒定、不随分组数变化：否则并排的渠道卡片底边会对不齐。分组多到
// 放不下时横向溢出，最后一段被裁切即是「右边还有」的提示，不换行也不换高度。
//
// Claude 与 Codex 的回调签名不同（前者传 channelName，后者传 channel 对象），
// 由各自调用方用闭包适配，本组件内统一只回传 group。

import { CheckCircle2, KeyRound, Loader2, Pencil, Plus, Trash2 } from 'lucide-react';
import { Tooltip } from '@components/ui';
import { getGroupID, isDefaultGroup } from './credentialGroupStats.js';

// 单行分段：11px 文字的行盒 16px + 上下 6px 内边距 + 1px 边框
const SEGMENT_AREA_HEIGHT_PX = 32;
// 分段用 flex-1 等宽平分，再用 min-w-max 兜住名字：宽度够时每段一样宽，不够时
// 各自退回名字所需宽度、由容器横向滚动。名字因此任何时候都完整，不出省略号。
// 这里的最小宽度只兜底短名字，避免单字分组窄成一条。
const SEGMENT_MIN_WIDTH_PX = 76;
// 气泡里逐个列出的端点上限，再多只报剩余个数
const BUBBLE_ENDPOINT_LIMIT = 8;

const EMPTY_STAT = { total: 0, failover: 0, endpoints: [] };

const buildStatSummary = ({ total, failover }) => {
  if (!total) return '该分组暂无端点';
  if (failover === total) return `${total} 个端点，全部参与转移`;
  if (failover === 0) return `${total} 个端点，全部不参与转移`;
  return `${total} 个端点，${failover} 个参与转移`;
};

const CredentialGroupStrip = ({
  groups = [],
  selectedId = 0,
  stats = {},
  label = '',
  headerExtra = null,
  onSelect,
  onOpen,
  onSetDefault,
  onEdit,
  onDelete,
  onAdd,
  disabled = false,
  loadingGroupId = null,
  emptyHint = '暂无分组'
}) => {
  const selectedGroup = groups.find((group) => getGroupID(group) === Number(selectedId)) || null;
  const selectedName = selectedGroup?.name || '未命名分组';
  const selectedIsDefault = isDefaultGroup(selectedGroup);
  const selectedLoading = loadingGroupId != null && Number(loadingGroupId) === getGroupID(selectedGroup);

  // 编辑/设默认/删除统一作用于选中分段，分段本身只负责「选哪个」
  const header = (
    <div className="flex h-6 items-center justify-between gap-2">
      <div className="flex min-w-0 items-center gap-2">
        {label && (
          <span className="flex-shrink-0 text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-400">
            {label}
          </span>
        )}
        {headerExtra}
      </div>
      <div className="flex flex-shrink-0 items-center gap-0.5">
        {selectedGroup && (
          <>
            {!selectedIsDefault && (
              <Tooltip content={`将「${selectedName}」设为默认分组`}>
                <button
                  type="button"
                  onClick={() => onSetDefault?.(selectedGroup)}
                  disabled={disabled || selectedLoading}
                  className={`${ACTION_BUTTON_CLASS} hover:bg-emerald-50 hover:text-emerald-600`}
                  aria-label={`将 ${selectedName} 设为默认分组`}
                >
                  {selectedLoading ? <Loader2 size={13} className="animate-spin" /> : <CheckCircle2 size={13} />}
                </button>
              </Tooltip>
            )}
            <Tooltip content={`编辑「${selectedName}」`}>
              <button
                type="button"
                onClick={() => onEdit?.(selectedGroup)}
                disabled={disabled}
                className={`${ACTION_BUTTON_CLASS} hover:bg-indigo-50 hover:text-indigo-600`}
                aria-label={`编辑 ${selectedName}`}
              >
                <Pencil size={13} />
              </button>
            </Tooltip>
            {!selectedIsDefault && (
              <Tooltip content={`删除「${selectedName}」`}>
                <button
                  type="button"
                  onClick={() => onDelete?.(selectedGroup)}
                  disabled={disabled}
                  className={`${ACTION_BUTTON_CLASS} hover:bg-rose-50 hover:text-rose-600`}
                  aria-label={`删除 ${selectedName}`}
                >
                  <Trash2 size={13} />
                </button>
              </Tooltip>
            )}
          </>
        )}
        <AddGroupButton onAdd={onAdd} disabled={disabled} />
      </div>
    </div>
  );

  if (groups.length === 0) {
    return (
      <div className="flex min-w-0 flex-col gap-1">
        {header}
        {/* 空态也占满分段区高度，保证与有分组的渠道卡片等高 */}
        <div className="flex items-center" style={{ height: `${SEGMENT_AREA_HEIGHT_PX}px` }}>
          <span className="truncate text-[11px] text-slate-400">{emptyHint}</span>
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-1">
      {header}

      <div
        role="group"
        aria-label="凭证分组"
        className="no-scrollbar flex min-w-0 items-stretch gap-1 overflow-x-auto"
        style={{ height: `${SEGMENT_AREA_HEIGHT_PX}px` }}
      >
        {groups.map((group) => (
          <GroupSegment
            key={group.id}
            group={group}
            selected={getGroupID(group) === Number(selectedId)}
            stat={stats[getGroupID(group)] || EMPTY_STAT}
            disabled={disabled}
            onSelect={onSelect}
            onOpen={onOpen}
          />
        ))}
      </div>
    </div>
  );
};

const GroupSegment = ({ group, selected, stat, disabled, onSelect, onOpen }) => {
  const groupIsDefault = isDefaultGroup(group);
  const name = group.name || '未命名分组';
  const summary = buildStatSummary(stat);
  const excludedCount = stat.total - stat.failover;

  return (
    <Tooltip
      className="flex min-w-max flex-1"
      content={
        <span className="flex flex-col gap-1">
          <span className="font-semibold">{name}</span>
          <span className="text-slate-300">{summary}</span>
          {stat.endpoints.length > 0 && (
            <span className="flex flex-col gap-0.5 border-t border-slate-700/70 pt-1">
              {stat.endpoints.slice(0, BUBBLE_ENDPOINT_LIMIT).map((endpoint) => (
                <span key={endpoint.name} className="flex items-center gap-1.5">
                  <span
                    className={`h-1.5 w-1.5 flex-shrink-0 rounded-full ${
                      endpoint.failover ? 'bg-indigo-400' : 'border border-slate-500'
                    }`}
                  />
                  <span className="min-w-0 flex-1 truncate">{endpoint.name}</span>
                  {!endpoint.failover && (
                    <span className="flex-shrink-0 text-[10px] text-slate-400">不参与</span>
                  )}
                </span>
              ))}
              {stat.endpoints.length > BUBBLE_ENDPOINT_LIMIT && (
                <span className="text-[10px] text-slate-400">
                  …另 {stat.endpoints.length - BUBBLE_ENDPOINT_LIMIT} 个
                </span>
              )}
            </span>
          )}
          <span className="text-[10px] text-slate-400">单击选择 · 双击查看详情</span>
        </span>
      }
    >
      <button
        type="button"
        onClick={() => onSelect?.(getGroupID(group))}
        onDoubleClick={() => onOpen?.(group)}
        disabled={disabled}
        aria-pressed={selected}
        aria-label={`${name}${groupIsDefault ? '（默认分组）' : ''}，${summary}`}
        style={{ minWidth: `${SEGMENT_MIN_WIDTH_PX}px` }}
        className={`flex h-full w-full items-center gap-1 rounded-lg border px-2 text-left transition-colors duration-150 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300 disabled:cursor-not-allowed disabled:opacity-50 ${
          selected
            ? 'border-indigo-300 bg-indigo-50/70'
            : 'border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50'
        }`}
      >
        <KeyRound
          size={11}
          className={
            selected
              ? 'flex-shrink-0 text-indigo-500'
              : groupIsDefault
                ? 'flex-shrink-0 text-emerald-500'
                : 'flex-shrink-0 text-slate-400'
          }
        />
        {/* 行盒固定 16px：11px 的中文在 leading-none 下会被裁掉底部笔画 */}
        {/* 不截断：分组名要完整可读，宽度不够由分段区横向滚动解决 */}
        <span className={`whitespace-nowrap text-[11px] font-medium leading-[16px] ${selected ? 'text-indigo-700' : 'text-slate-600'}`}>
          {name}
        </span>
        {groupIsDefault && (
          <span className="flex-shrink-0 rounded bg-emerald-50 px-1 text-[9px] font-medium leading-[14px] text-emerald-600">
            默认
          </span>
        )}
        {/* 只在存在不参与转移的端点时出现：正常态不占宽度，异常态不必悬浮就能看见 */}
        {excludedCount > 0 && (
          <span
            aria-hidden="true"
            className={`ml-auto h-1.5 w-1.5 flex-shrink-0 rounded-full ${
              stat.failover === 0 ? 'bg-slate-400' : 'bg-amber-500'
            }`}
          />
        )}
      </button>
    </Tooltip>
  );
};

const AddGroupButton = ({ onAdd, disabled }) => (
  <button
    type="button"
    onClick={() => onAdd?.()}
    disabled={disabled}
    className="inline-flex h-6 flex-shrink-0 items-center gap-1 rounded-md px-1.5 text-[11px] font-medium text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300 disabled:cursor-not-allowed disabled:opacity-50"
  >
    <Plus size={12} />
    添加分组
  </button>
);

// 操作按钮统一 24×24，满足 WCAG 2.5.8 最小目标尺寸
const ACTION_BUTTON_CLASS =
  'inline-flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md text-slate-400 transition-colors duration-150 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300 disabled:cursor-not-allowed disabled:opacity-50';

export default CredentialGroupStrip;
