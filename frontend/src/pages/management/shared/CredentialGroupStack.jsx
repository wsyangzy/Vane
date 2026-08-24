// ============================================
// CredentialGroupStack - 凭证分组堆叠卡片选择器
// ============================================
// 折叠态是一叠卡片，最上层始终是当前选中的分组——后层卡片内容淡出不可读，
// 若不置顶就会出现「露出的不是用户选中的那个」。
//
// 交互：单击卡片浮出全部分组（卡片从堆叠位置散开），在浮层里选中某个分组后
// 收起，被选中的那张即成为新的最上层。折叠态只有选中项可见，对它「单击切换」
// 没有意义，所以单击留给展开——也因此不再需要单独的展开按钮。
//
// 浮层走 portal + fixed 并向外溢出锚点若干像素，整体浮在渠道卡片之外：
// ChannelCard 根元素带 overflow-hidden（为了让子元素不越出圆角），就地展开会
// 被裁剪，而撑开又会改变渠道卡片高度。
//
// 堆叠区高度恒定，不随分组数量变化：否则「有堆叠」与「只有一个分组」的渠道
// 卡片会差出十来像素，并排时底边对不齐。
//
// Claude 与 Codex 的回调签名不同（前者传 channelName，后者传 channel 对象），
// 由各自调用方用闭包适配，本组件内统一只回传 group。

import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { CheckCircle2, KeyRound, Loader2, Pencil, Plus, Trash2 } from 'lucide-react';
import { Tooltip } from '@components/ui';

// 露出层数上限。按 0.05 的缩放步长，第 4 层已缩到 0.85 且被前层阴影压住，不再可辨
const MAX_STACK_LAYERS = 3;
// 浮层超过此数量转内部滚动
const SCROLL_THRESHOLD = 6;
const STACK_OFFSET_PX = 5;
const STACK_SCALE_STEP = 0.05;
const CARD_HEIGHT_PX = 36;
const CARD_GAP_PX = 6;
// 浮层里每行的步进，用于把卡片从展开位置反推回堆叠位置做散开动画
const ROW_STRIDE_PX = CARD_HEIGHT_PX + CARD_GAP_PX;
// 堆叠区固定高度：始终按最大露出层数预留，与实际分组数无关
const STACK_AREA_HEIGHT_PX = CARD_HEIGHT_PX + (MAX_STACK_LAYERS - 1) * STACK_OFFSET_PX;
// 浮层内边距（p-1.5），用于抵消后让第一张卡片与堆叠顶层严格对齐
const OVERLAY_PADDING_PX = 6;
// 浮层向左右各溢出的像素，视觉上明确浮在渠道卡片之外
const OVERLAY_BLEED_PX = 6;
// 浮层向下展开所需的最小空间，不足则改为向上浮出
const MIN_DROP_DOWN_SPACE_PX = 140;
// 收起动画时长，与 --duration-quick 对齐；结束后才卸载 portal
const CLOSE_ANIMATION_MS = 150;

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
  const [anchor, setAnchor] = useState(null);
  const [entered, setEntered] = useState(false);
  const [interactive, setInteractive] = useState(false);
  const listId = useId();
  const containerRef = useRef(null);
  const stackRef = useRef(null);
  const popoverRef = useRef(null);
  const closeTimerRef = useRef(null);

  // 选中卡置顶，其余保持原有相对顺序
  const orderedGroups = useMemo(() => {
    const selected = groups.filter((group) => Number(group.id) === Number(selectedId));
    const rest = groups.filter((group) => Number(group.id) !== Number(selectedId));
    return [...selected, ...rest];
  }, [groups, selectedId]);

  const canExpand = groups.length > 1;
  const isOpen = !!anchor && canExpand;

  const closeExpanded = useCallback(() => {
    setEntered(false);
    if (closeTimerRef.current) clearTimeout(closeTimerRef.current);
    // 先播收起动画再卸载 portal，否则浮层会直接消失
    closeTimerRef.current = setTimeout(() => setAnchor(null), CLOSE_ANIMATION_MS);
  }, []);

  const openExpanded = useCallback(() => {
    const element = stackRef.current;
    if (!element) return;
    if (closeTimerRef.current) clearTimeout(closeTimerRef.current);
    const rect = element.getBoundingClientRect();
    const spaceBelow = window.innerHeight - rect.top - 12;
    const spaceAbove = rect.bottom - 12;
    // 渠道卡片可能靠近视口底部，此时向上浮出，避免浮层被截断
    const dropUp = spaceBelow < MIN_DROP_DOWN_SPACE_PX && spaceAbove > spaceBelow;
    setInteractive(false);
    setAnchor({
      left: rect.left - OVERLAY_BLEED_PX,
      width: rect.width + OVERLAY_BLEED_PX * 2,
      // 抵消浮层内边距，使第一张卡片正好落在堆叠顶层原位，散开动画才不跳
      top: dropUp ? undefined : rect.top - OVERLAY_PADDING_PX,
      bottom: dropUp ? window.innerHeight - rect.bottom - OVERLAY_PADDING_PX : undefined,
      maxHeight: Math.max(120, Math.floor(dropUp ? spaceAbove : spaceBelow))
    });
  }, []);

  // 挂载后下一帧再切到展开态，让卡片从堆叠位置过渡出去
  useEffect(() => {
    if (!anchor) return undefined;
    const frame = requestAnimationFrame(() => setEntered(true));
    return () => cancelAnimationFrame(frame);
  }, [anchor]);

  // 展开动画期间浮层不吃点击：否则双击折叠卡片时，第二次点击会落在刚浮出的
  // 浮层上误触选中。此时点击穿透到下方，而下方堆叠已 inert，不会有反应。
  useEffect(() => {
    if (!anchor) return undefined;
    const timer = setTimeout(() => setInteractive(true), 220);
    return () => clearTimeout(timer);
  }, [anchor]);

  useEffect(() => () => {
    if (closeTimerRef.current) clearTimeout(closeTimerRef.current);
  }, []);

  // 点击浮层与堆叠之外收起
  useEffect(() => {
    if (!isOpen) return undefined;
    const handlePointerDown = (event) => {
      if (containerRef.current?.contains(event.target)) return;
      if (popoverRef.current?.contains(event.target)) return;
      closeExpanded();
    };
    document.addEventListener('pointerdown', handlePointerDown);
    return () => document.removeEventListener('pointerdown', handlePointerDown);
  }, [isOpen, closeExpanded]);

  // 浮层用视口坐标定位，页面滚动或缩放后锚点失效，直接收起（与 Tooltip 一致）
  useEffect(() => {
    if (!isOpen) return undefined;
    const handleEscape = (event) => {
      if (event.key === 'Escape') closeExpanded();
    };
    window.addEventListener('scroll', closeExpanded, true);
    window.addEventListener('resize', closeExpanded);
    document.addEventListener('keydown', handleEscape);
    return () => {
      window.removeEventListener('scroll', closeExpanded, true);
      window.removeEventListener('resize', closeExpanded);
      document.removeEventListener('keydown', handleEscape);
    };
  }, [isOpen, closeExpanded]);

  const handleSelect = useCallback((group) => {
    onSelect?.(Number(group.id) || 0);
    closeExpanded();
  }, [onSelect, closeExpanded]);

  const withClose = useCallback((handler) => (group) => {
    closeExpanded();
    handler?.(group);
  }, [closeExpanded]);

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
    <div ref={containerRef} className="flex min-w-0 flex-col gap-1.5">
      {header}

      {/* 折叠堆叠常驻，既是视觉主体也是浮层的定位锚点 */}
      <div
        ref={stackRef}
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
                hint={canExpand ? '单击展开全部分组' : '双击查看详情'}
                // 展开按钮已移除，展开语义挂在最前那张卡上，键盘用户 Tab 到它即可展开
                expandTrigger={canExpand}
                expanded={isOpen}
                controlsId={listId}
                // 折叠态整卡单击即展开：此时可见的只有选中项，「切换」无从谈起
                onActivate={canExpand ? openExpanded : undefined}
                onDoubleClickCard={canExpand ? undefined : onOpen}
                onSetDefault={onSetDefault}
                onEdit={onEdit}
                onDelete={onDelete}
              />
            </div>
          );
        })}
      </div>

      {isOpen && createPortal(
        <div
          ref={popoverRef}
          id={listId}
          role="group"
          aria-label="凭证分组列表"
          className={`fixed z-[80] flex flex-col gap-1.5 rounded-xl border border-slate-200 bg-white p-1.5 shadow-[0_16px_40px_-10px_rgba(15,23,42,0.35)] transition-opacity duration-[var(--duration-fast)] ease-[var(--ease-smooth-out)] motion-reduce:transition-none ${
            entered ? 'opacity-100' : 'opacity-0'
          } ${interactive ? '' : 'pointer-events-none'} ${groups.length > SCROLL_THRESHOLD ? 'overflow-y-auto' : ''}`}
          style={{
            left: `${anchor.left}px`,
            width: `${anchor.width}px`,
            ...(anchor.top != null ? { top: `${anchor.top}px` } : {}),
            ...(anchor.bottom != null ? { bottom: `${anchor.bottom}px` } : {}),
            maxHeight: `min(15rem, ${anchor.maxHeight}px)`
          }}
        >
          {orderedGroups.map((group, index) => (
            <div
              key={group.id}
              className="flex-shrink-0 origin-top transition-[transform,opacity] duration-[var(--duration-fast)] ease-[var(--ease-smooth-out)] motion-reduce:transition-none"
              style={{
                // 未进入时把每张卡反推回堆叠位置，进入后归位，形成散开动画
                transform: entered
                  ? 'translateY(0) scale(1)'
                  : `translateY(${index * STACK_OFFSET_PX - index * ROW_STRIDE_PX}px) scale(${1 - Math.min(index, MAX_STACK_LAYERS) * STACK_SCALE_STEP})`,
                opacity: entered ? 1 : (index < MAX_STACK_LAYERS ? 1 : 0),
                transitionDelay: entered ? `${Math.min(index, 6) * 15}ms` : '0ms'
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
        </div>,
        document.body
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
  expandTrigger = false,
  expanded = false,
  controlsId,
  onActivate,
  onDoubleClickCard,
  onSetDefault,
  onEdit,
  onDelete
}) => {
  const groupIsDefault = isDefaultGroup(group);
  // 操作按钮嵌在整卡点击区里，必须阻止冒泡，否则编辑/删除会顺带触发展开或选中
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
          // 作为展开触发器时是 disclosure 语义；在浮层里才是「选中哪一个」的 toggle
          aria-expanded={expandTrigger ? expanded : undefined}
          aria-controls={expandTrigger ? controlsId : undefined}
          aria-pressed={expandTrigger ? undefined : selected}
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
