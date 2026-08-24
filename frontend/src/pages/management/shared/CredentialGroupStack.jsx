// ============================================
// CredentialGroupStack - 凭证分组堆叠卡片选择器
// ============================================
// 折叠态是一叠卡片，最上层始终是当前选中的分组——后层卡片内容淡出不可读，
// 若不置顶就会出现「露出的不是用户选中的那个」。
//
// 交互：单击卡片弹出居中弹窗列出全部分组，选中某个后关闭，被选中的那张即成为
// 新的最上层。折叠态只有选中项可见，对它「单击切换」没有意义，所以单击留给
// 展开——也因此不需要单独的展开按钮。
//
// 弹窗走居中模态而不是锚定浮层：ChannelCard 根元素带 overflow-hidden（为了让
// 子元素不越出圆角），就地展开会被裁剪；而锚定浮层在渠道卡片密集排布时容易
// 压到相邻卡片，也受视口边缘影响。
//
// 堆叠区高度恒定，不随分组数量变化：否则「有堆叠」与「只有一个分组」的渠道
// 卡片会差出十来像素，并排时底边对不齐。
//
// Claude 与 Codex 的回调签名不同（前者传 channelName，后者传 channel 对象），
// 由各自调用方用闭包适配，本组件内统一只回传 group。

import { useCallback, useEffect, useId, useMemo, useState } from 'react';
import { CheckCircle2, KeyRound, Loader2, Pencil, Plus, Trash2 } from 'lucide-react';
import { Tooltip } from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';

// 露出层数上限。按 0.05 的缩放步长，第 4 层已缩到 0.85 且被前层阴影压住，不再可辨
const MAX_STACK_LAYERS = 3;
const STACK_OFFSET_PX = 5;
const STACK_SCALE_STEP = 0.05;
const CARD_HEIGHT_PX = 36;
const CARD_GAP_PX = 6;
// 弹窗里每行的步进，用于把卡片从展开位置反推回堆叠位置做散开动画
const ROW_STRIDE_PX = CARD_HEIGHT_PX + CARD_GAP_PX;
// 堆叠区固定高度：始终按最大露出层数预留，与实际分组数无关
const STACK_AREA_HEIGHT_PX = CARD_HEIGHT_PX + (MAX_STACK_LAYERS - 1) * STACK_OFFSET_PX;
// 弹窗打开后的静默期，避开「展开那一下」的第二次点击
const OPEN_GUARD_MS = 220;

const isDefaultGroup = (group) => Boolean(group?.isDefault ?? group?.is_default);

const CredentialGroupStack = ({
  groups = [],
  selectedId = 0,
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
  const [open, setOpen] = useState(false);
  const [entered, setEntered] = useState(false);
  const [interactive, setInteractive] = useState(false);
  const dialogId = useId();

  // 选中卡置顶，其余保持原有相对顺序
  const orderedGroups = useMemo(() => {
    const selected = groups.filter((group) => Number(group.id) === Number(selectedId));
    const rest = groups.filter((group) => Number(group.id) !== Number(selectedId));
    return [...selected, ...rest];
  }, [groups, selectedId]);

  const canExpand = groups.length > 1;
  const isOpen = open && canExpand;

  const closeDialog = useCallback(() => setOpen(false), []);

  const openDialog = useCallback(() => {
    setEntered(false);
    setInteractive(false);
    setOpen(true);
  }, []);

  // 挂载后下一帧再切到进入态，卡片才有起始状态可过渡
  useEffect(() => {
    if (!isOpen) return undefined;
    const frame = requestAnimationFrame(() => setEntered(true));
    return () => cancelAnimationFrame(frame);
  }, [isOpen]);

  // 入场期间弹窗不吃点击：双击折叠卡片时，第二次点击会落在刚弹出的弹窗（或其
  // 遮罩）上，误触选中或立刻关闭。此时点击穿透到下方，而下方堆叠已 inert。
  useEffect(() => {
    if (!isOpen) return undefined;
    const timer = setTimeout(() => setInteractive(true), OPEN_GUARD_MS);
    return () => clearTimeout(timer);
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen) return undefined;
    const handleEscape = (event) => {
      if (event.key === 'Escape') closeDialog();
    };
    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [isOpen, closeDialog]);

  const handleSelect = useCallback((group) => {
    onSelect?.(Number(group.id) || 0);
    closeDialog();
  }, [onSelect, closeDialog]);

  const withClose = useCallback((handler) => (group) => {
    closeDialog();
    handler?.(group);
  }, [closeDialog]);

  const header = (
    <div className="flex h-5 items-center justify-between gap-2">
      <div className="flex min-w-0 items-center gap-2">
        {label && (
          <span className="flex-shrink-0 text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-400">
            {label}
          </span>
        )}
        {headerExtra}
      </div>
      <AddGroupButton onAdd={onAdd} disabled={disabled} />
    </div>
  );

  if (groups.length === 0) {
    return (
      <div className="flex min-w-0 flex-col gap-1.5">
        {header}
        {/* 空态也占满堆叠区高度，保证与有分组的渠道卡片等高 */}
        <div className="flex items-center" style={{ height: `${STACK_AREA_HEIGHT_PX}px` }}>
          <span className="truncate text-[11px] text-slate-400">{emptyHint}</span>
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      {header}

      {/* 折叠堆叠：高度固定，与分组数量无关 */}
      <div
        className="relative"
        style={{ height: `${STACK_AREA_HEIGHT_PX}px` }}
        inert={isOpen}
        aria-hidden={isOpen || undefined}
      >
        {orderedGroups.map((group, visualIndex) => {
          const isFront = visualIndex === 0;
          // 折叠时只有最前一张可交互，后层是纯视觉暗示：
          // 单靠 opacity:0 仍可被 Tab 聚焦，必须用 inert 同时移出焦点顺序与无障碍树
          const isDecorative = !isFront;

          return (
            <div
              key={group.id}
              inert={isDecorative}
              aria-hidden={isDecorative || undefined}
              className="absolute inset-x-0 top-0 origin-top transition-[transform,opacity] duration-[var(--duration-fast)] ease-[var(--ease-smooth-out)] motion-reduce:transition-none"
              style={{
                zIndex: groups.length - visualIndex,
                opacity: visualIndex >= MAX_STACK_LAYERS ? 0 : 1,
                transform: `translateY(${visualIndex * STACK_OFFSET_PX}px) scale(${1 - visualIndex * STACK_SCALE_STEP})`
              }}
            >
              <GroupCard
                group={group}
                selected={Number(group.id) === Number(selectedId)}
                contentHidden={isDecorative}
                disabled={disabled}
                loading={loadingGroupId != null && Number(loadingGroupId) === Number(group.id)}
                hint={canExpand ? '单击选择分组' : '双击查看详情'}
                // 展开按钮已移除，弹窗语义挂在最前那张卡上，键盘用户 Tab 到它即可打开
                dialogTrigger={canExpand}
                expanded={isOpen}
                controlsId={dialogId}
                // 折叠态整卡单击即打开弹窗：此时可见的只有选中项，「切换」无从谈起
                onActivate={canExpand ? openDialog : undefined}
                onDoubleClickCard={canExpand ? undefined : onOpen}
                onSetDefault={onSetDefault}
                onEdit={onEdit}
                onDelete={onDelete}
              />
            </div>
          );
        })}
      </div>

      {isOpen && renderModalPortal(
        <div
          className={`fixed inset-0 z-[100] flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-sm animate-fade-in ${
            interactive ? '' : 'pointer-events-none'
          }`}
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) closeDialog();
          }}
        >
          {/* 无标题栏无关闭按钮，就是一叠散开的卡片；遮罩与 Esc 负责关闭 */}
          <div
            id={dialogId}
            role="dialog"
            aria-modal="true"
            aria-label="选择凭证分组"
            className="flex max-h-[70vh] w-full max-w-sm flex-col gap-1.5 overflow-y-auto rounded-2xl bg-white p-2 shadow-[0_20px_50px_-12px_rgba(15,23,42,0.4)]"
            onMouseDown={(event) => event.stopPropagation()}
          >
            {orderedGroups.map((group, index) => (
              <div
                key={group.id}
                className="flex-shrink-0 origin-top transition-[transform,opacity] duration-[var(--duration-fast)] ease-[var(--ease-smooth-out)] motion-reduce:transition-none"
                style={{
                  // 未进入时按行距把每张卡反推回堆叠位置，进入后归位，形成散开动画
                  transform: entered
                    ? 'translateY(0) scale(1)'
                    : `translateY(${index * STACK_OFFSET_PX - index * ROW_STRIDE_PX}px) scale(${1 - Math.min(index, MAX_STACK_LAYERS) * STACK_SCALE_STEP})`,
                  opacity: entered ? 1 : (index < MAX_STACK_LAYERS ? 1 : 0),
                  transitionDelay: entered ? `${Math.min(index, 8) * 20}ms` : '0ms'
                }}
              >
                <GroupCard
                  group={group}
                  selected={Number(group.id) === Number(selectedId)}
                  disabled={disabled}
                  loading={loadingGroupId != null && Number(loadingGroupId) === Number(group.id)}
                  hint="单击选择，双击查看详情"
                  onActivate={handleSelect}
                  onDoubleClickCard={onOpen}
                  onSetDefault={withClose(onSetDefault)}
                  onEdit={withClose(onEdit)}
                  onDelete={withClose(onDelete)}
                />
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};

const AddGroupButton = ({ onAdd, disabled }) => (
  <button
    type="button"
    onClick={() => onAdd?.()}
    disabled={disabled}
    className="inline-flex h-5 flex-shrink-0 items-center gap-1 rounded-md px-1.5 text-[11px] font-medium text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300 disabled:cursor-not-allowed disabled:opacity-50"
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
  contentHidden = false,
  disabled,
  loading,
  hint,
  dialogTrigger = false,
  expanded = false,
  controlsId,
  onActivate,
  onDoubleClickCard,
  onSetDefault,
  onEdit,
  onDelete
}) => {
  const groupIsDefault = isDefaultGroup(group);
  // 操作按钮嵌在整卡点击区里，必须阻止冒泡，否则编辑/删除会顺带打开弹窗或选中
  const stopBubble = (handler) => (event) => {
    event.stopPropagation();
    handler?.(group);
  };

  return (
    <div
      onClick={() => onActivate?.(group)}
      onDoubleClick={onDoubleClickCard ? () => onDoubleClickCard(group) : undefined}
      title={hint}
      className={`flex h-9 min-w-0 items-center gap-1.5 rounded-lg border bg-white px-2 shadow-[0_1px_2px_rgba(15,23,42,0.06)] transition-colors ${
        onActivate ? 'cursor-pointer' : ''
      } ${selected ? 'border-indigo-200 bg-indigo-50/60' : 'border-slate-200 hover:border-slate-300'}`}
    >
      {/* 后层卡片只保留外壳轮廓，内容淡出，避免堆叠处文字互相重叠 */}
      <div
        className={`flex min-w-0 flex-1 items-center gap-1.5 transition-opacity duration-[var(--duration-fast)] ease-[var(--ease-smooth-out)] motion-reduce:transition-none ${
          contentHidden ? 'opacity-0' : 'opacity-100'
        }`}
      >
        <button
          type="button"
          onClick={stopBubble(onActivate)}
          disabled={disabled}
          // 作为弹窗触发器时是 disclosure 语义；在弹窗里才是「选中哪一个」的 toggle
          aria-haspopup={dialogTrigger ? 'dialog' : undefined}
          aria-expanded={dialogTrigger ? expanded : undefined}
          aria-controls={dialogTrigger && expanded ? controlsId : undefined}
          aria-pressed={dialogTrigger ? undefined : selected}
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
                onClick={stopBubble(onSetDefault)}
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
              onClick={stopBubble(onEdit)}
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
                onClick={stopBubble(onDelete)}
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
