// ============================================
// CredentialGroupStack - 凭证分组堆叠卡片选择器
// ============================================
// 替代原先的横向滚动胶囊条。折叠态最多露出 3 层，选中分组始终排在最前——
// 后层卡片的内容不可读，若不置顶会出现「露出的不是用户选中的那个」。
//
// 视觉层级由行内的 --stack-i 驱动，DOM 顺序始终与传入顺序一致，
// 避免读屏朗读顺序和 Tab 顺序随选中项来回跳动。
//
// Claude 与 Codex 的回调签名不同（前者传 channelName，后者传 channel 对象），
// 由各自调用方用闭包适配，本组件内统一只回传 group。

import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { CheckCircle2, ChevronDown, KeyRound, Loader2, Pencil, Plus, Trash2 } from 'lucide-react';
import { Tooltip } from '@components/ui';

// 露出层数上限。按 0.05 的缩放步长，第 4 层已缩到 0.85 且被前层阴影压住，不再可辨
const MAX_STACK_LAYERS = 3;
// 展开后超过此数量改为内部滚动，否则会把渠道卡片推得过长
const SCROLL_THRESHOLD = 6;
const STACK_OFFSET_PX = 5;
const STACK_SCALE_STEP = 0.05;

const isDefaultGroup = (group) => Boolean(group?.isDefault ?? group?.is_default);

const CredentialGroupStack = ({
  groups = [],
  selectedId = 0,
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
  const [expanded, setExpanded] = useState(false);
  const listId = useId();
  const containerRef = useRef(null);

  // 选中卡置顶，其余保持原有相对顺序
  const orderedGroups = useMemo(() => {
    const selected = groups.filter((group) => Number(group.id) === Number(selectedId));
    const rest = groups.filter((group) => Number(group.id) !== Number(selectedId));
    return [...selected, ...rest];
  }, [groups, selectedId]);

  const hiddenCount = Math.max(0, groups.length - 1);
  const canExpand = hiddenCount > 0;
  // 派生而非同步：分组减少到 1 个时展开态自动失效，不需要 effect 回写 state
  const isExpanded = expanded && canExpand;

  // 展开后点击外部收起，与端点列表的展开行为保持一致
  useEffect(() => {
    if (!isExpanded) return undefined;
    const handlePointerDown = (event) => {
      if (!containerRef.current?.contains(event.target)) setExpanded(false);
    };
    document.addEventListener('pointerdown', handlePointerDown);
    return () => document.removeEventListener('pointerdown', handlePointerDown);
  }, [isExpanded]);

  const handleSelect = useCallback((group) => {
    onSelect?.(Number(group.id) || 0);
    setExpanded(false);
  }, [onSelect]);

  if (groups.length === 0) {
    return (
      <div className="flex items-center justify-between gap-2">
        <span className="truncate text-[11px] text-slate-400">{emptyHint}</span>
        <AddGroupButton onAdd={onAdd} disabled={disabled} />
      </div>
    );
  }

  const needsScroll = groups.length > SCROLL_THRESHOLD;
  // 折叠态容器高度 = 前卡高度 + 背景层累计偏移
  const collapsedLayers = Math.min(groups.length, MAX_STACK_LAYERS);
  const collapsedHeight = 36 + (collapsedLayers - 1) * STACK_OFFSET_PX;

  return (
    <div ref={containerRef} className="flex min-w-0 flex-col gap-1">
      <div
        id={listId}
        className={
          isExpanded
            ? `flex flex-col gap-1.5 ${needsScroll ? 'max-h-[15rem] overflow-y-auto pr-0.5' : ''}`
            : 'relative'
        }
        style={isExpanded ? undefined : { height: `${collapsedHeight}px` }}
      >
        {orderedGroups.map((group, visualIndex) => {
          const isFront = visualIndex === 0;
          // 折叠时只有最前一张可交互，后层是纯视觉暗示：
          // 单靠 opacity:0 仍可被 Tab 聚焦，必须用 inert 同时移出焦点顺序与无障碍树
          const isDecorative = !isExpanded && !isFront;
          const isBuried = !isExpanded && visualIndex >= MAX_STACK_LAYERS;

          return (
            <div
              key={group.id}
              inert={isDecorative}
              aria-hidden={isDecorative || undefined}
              className={
                isExpanded
                  ? 'flex-shrink-0'
                  : 'absolute inset-x-0 top-0 origin-top transition-[transform,opacity] duration-[var(--duration-fast)] ease-[var(--ease-smooth-out)] motion-reduce:transition-none'
              }
              style={
                isExpanded
                  ? undefined
                  : {
                      zIndex: groups.length - visualIndex,
                      opacity: isBuried ? 0 : 1,
                      transform: `translateY(${visualIndex * STACK_OFFSET_PX}px) scale(${1 - visualIndex * STACK_SCALE_STEP})`
                    }
              }
            >
              <GroupCard
                group={group}
                selected={Number(group.id) === Number(selectedId)}
                contentHidden={isDecorative}
                disabled={disabled}
                loading={loadingGroupId != null && Number(loadingGroupId) === Number(group.id)}
                onSelect={handleSelect}
                onOpen={onOpen}
                onSetDefault={onSetDefault}
                onEdit={onEdit}
                onDelete={onDelete}
              />
            </div>
          );
        })}
      </div>

      <div className="flex items-center justify-between gap-2">
        {canExpand ? (
          <button
            type="button"
            onClick={() => setExpanded((value) => !value)}
            aria-expanded={isExpanded}
            aria-controls={listId}
            className="inline-flex items-center gap-0.5 rounded-md px-1 py-0.5 text-[10px] font-medium text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300"
          >
            {isExpanded ? '收起' : `展开 ${hiddenCount} 个`}
            <ChevronDown
              size={11}
              className={`transition-transform duration-200 ease-out motion-reduce:transition-none ${isExpanded ? 'rotate-180' : ''}`}
            />
          </button>
        ) : (
          <span />
        )}
        <AddGroupButton onAdd={onAdd} disabled={disabled} />
      </div>
    </div>
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

// 操作按钮统一 24×24，满足 WCAG 2.5.8 最小目标尺寸（原胶囊内按钮仅约 14×14）
const ACTION_BUTTON_CLASS =
  'inline-flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-md text-slate-400 transition-colors duration-150 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300 disabled:cursor-not-allowed disabled:opacity-50';

const GroupCard = ({
  group,
  selected,
  contentHidden,
  disabled,
  loading,
  onSelect,
  onOpen,
  onSetDefault,
  onEdit,
  onDelete
}) => {
  const groupIsDefault = isDefaultGroup(group);

  return (
    <div
      onDoubleClick={() => onOpen?.(group)}
      title="单击切换，双击查看详情"
      className={`flex h-9 min-w-0 items-center gap-1.5 rounded-lg border bg-white px-2 shadow-[0_1px_2px_rgba(15,23,42,0.06)] transition-colors ${
        selected ? 'border-indigo-200 bg-indigo-50/60' : 'border-slate-200 hover:border-slate-300'
      }`}
    >
      {/* 后层卡片只保留外壳轮廓，内容淡出，避免堆叠处文字互相重叠 */}
      <div
        className={`flex min-w-0 flex-1 items-center gap-1.5 transition-opacity duration-[var(--duration-fast)] ease-[var(--ease-smooth-out)] motion-reduce:transition-none ${
          contentHidden ? 'opacity-0' : 'opacity-100'
        }`}
      >
        <button
          type="button"
          onClick={() => onSelect?.(group)}
          disabled={disabled}
          aria-pressed={selected}
          className="inline-flex min-w-0 flex-1 items-center gap-1.5 rounded-md text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300 disabled:cursor-not-allowed"
        >
          <KeyRound
            size={12}
            className={
              selected
                ? 'flex-shrink-0 text-indigo-500'
                : groupIsDefault
                  ? 'flex-shrink-0 text-emerald-500'
                  : 'flex-shrink-0 text-slate-400'
            }
          />
          <span className={`min-w-0 truncate text-[11px] font-medium ${selected ? 'text-indigo-700' : 'text-slate-600'}`}>
            {group.name || '未命名分组'}
          </span>
          {groupIsDefault && (
            <span className="flex-shrink-0 rounded bg-emerald-50 px-1 text-[9px] font-medium leading-[1.4] text-emerald-600">
              默认
            </span>
          )}
        </button>

        <div className="flex flex-shrink-0 items-center gap-0.5">
          {!groupIsDefault && (
            <Tooltip content="设为默认分组">
              <button
                type="button"
                onClick={() => onSetDefault?.(group)}
                onDoubleClick={(event) => event.stopPropagation()}
                disabled={disabled || loading}
                className={`${ACTION_BUTTON_CLASS} hover:bg-emerald-50 hover:text-emerald-600`}
                aria-label="设为默认分组"
              >
                {loading ? <Loader2 size={13} className="animate-spin" /> : <CheckCircle2 size={13} />}
              </button>
            </Tooltip>
          )}
          <Tooltip content="编辑分组">
            <button
              type="button"
              onClick={() => onEdit?.(group)}
              onDoubleClick={(event) => event.stopPropagation()}
              disabled={disabled}
              className={`${ACTION_BUTTON_CLASS} hover:bg-indigo-50 hover:text-indigo-600`}
              aria-label="编辑分组"
            >
              <Pencil size={13} />
            </button>
          </Tooltip>
          {!groupIsDefault && (
            <Tooltip content="删除分组">
              <button
                type="button"
                onClick={() => onDelete?.(group)}
                onDoubleClick={(event) => event.stopPropagation()}
                disabled={disabled}
                className={`${ACTION_BUTTON_CLASS} hover:bg-rose-50 hover:text-rose-600`}
                aria-label="删除分组"
              >
                <Trash2 size={13} />
              </button>
            </Tooltip>
          )}
        </div>
      </div>
    </div>
  );
};

export default CredentialGroupStack;
