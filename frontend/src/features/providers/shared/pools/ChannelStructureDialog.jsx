import Tooltip from '@/shared/ui/Tooltip.jsx';
import { useCallback, useContext, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { Info, KeyRound, Layers, Maximize2, Minus, MoreVertical, Network, Pencil, Plus, Server, Trash2, X } from 'lucide-react';
import { ModalPortalContext, renderModalPortal } from '@/shared/ui/modalPortal.js';
import { SubscriptionStatus } from '@/features/providers/shared/subscriptions/SubscriptionSettings.jsx';
import { buildChannelGraph, fitChannelGraph, MAX_GRAPH_ZOOM, MIN_GRAPH_ZOOM, zoomChannelGraph } from './channelGraph.js';

const iconButton = 'inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-700 focus-visible:outline-2 focus-visible:outline-indigo-500 disabled:opacity-40';
const menuItem = 'flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-xs text-slate-600 hover:bg-slate-100 focus:bg-slate-100 focus:outline-none disabled:opacity-40';

export default function ChannelStructureDialog({ name, groups, endpoints, busy, onClose, onEdit, onDelete, onAddGroup, onOpenGroup, onEditGroup, onDeleteGroup, onAddEndpoint, onOpenEndpoint, onEditEndpoint, onDeleteEndpoint }) {
  const titleId = useId();
  const dialogRef = useRef(null);
  const closeRef = useRef(null);
  const canvasRef = useRef(null);
  const menuRef = useRef(null);
  const menuAnchorRef = useRef(null);
  const dragRef = useRef(null);
  const modalScope = useContext(ModalPortalContext);
  const hasOperation = (modalScope?.layerCount || 0) > 0;
  const operationRef = useRef(false);
  const [closing, setClosing] = useState(false);
  const [viewport, setViewport] = useState({ width: 780, height: 330 });
  const [manualView, setManualView] = useState(null);
  const [dragging, setDragging] = useState(false);
  const [selectedKey, setSelectedKey] = useState(null);
  const [menu, setMenu] = useState(null);
  const graph = useMemo(() => buildChannelGraph(name, groups, endpoints), [name, groups, endpoints]);
  const view = manualView || fitChannelGraph(graph, viewport);
  const menuNode = graph.nodes.find((node) => node.key === menu?.key);
  const controlsBusy = busy;
  const requestClose = useCallback(() => {
    if (!hasOperation) setClosing(true);
  }, [hasOperation]);

  useEffect(() => {
    if (!closing) return;
    const duration = matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : parseFloat(getComputedStyle(dialogRef.current).getPropertyValue('--modal-close-dur')) || 150;
    const timer = setTimeout(onClose, duration);
    return () => clearTimeout(timer);
  }, [closing, onClose]);

  useLayoutEffect(() => {
    if (operationRef.current && !hasOperation) {
      const anchor = menuAnchorRef.current;
      if (anchor?.isConnected) anchor.focus();
      else closeRef.current?.focus();
    }
    operationRef.current = hasOperation;
  }, [hasOperation]);

  useLayoutEffect(() => {
    const canvas = canvasRef.current;
    const measure = () => setViewport({ width: canvas.clientWidth, height: canvas.clientHeight });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(canvas);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const previousFocus = document.activeElement;
    closeRef.current?.focus();
    return () => {
      if (previousFocus?.isConnected) previousFocus.focus();
    };
  }, []);

  useEffect(() => {
    const onKeyDown = (event) => {
      if (hasOperation || event.defaultPrevented) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        if (menuRef.current) {
          setMenu(null);
          menuAnchorRef.current?.focus();
        } else requestClose();
      }
      if (event.key === 'Tab') {
        const controls = [...dialogRef.current.querySelectorAll('button:not(:disabled), [href], [tabindex="0"]')].filter((element) => element.getClientRects().length);
        if (event.shiftKey && document.activeElement === controls[0]) {
          event.preventDefault();
          controls.at(-1)?.focus();
        } else if (!event.shiftKey && document.activeElement === controls.at(-1)) {
          event.preventDefault();
          controls[0]?.focus();
        }
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [hasOperation, requestClose]);

  useLayoutEffect(() => {
    if (!menu || !menuRef.current) return;
    const element = menuRef.current;
    const shell = dialogRef.current;
    element.style.maxHeight = `${Math.min(420, shell.clientHeight - 16)}px`;
    const left = menu.x + element.offsetWidth > shell.clientWidth - 8 ? menu.x - element.offsetWidth - 12 : menu.x;
    element.style.left = `${Math.max(8, Math.min(left, shell.clientWidth - element.offsetWidth - 8))}px`;
    element.style.top = `${Math.max(8, Math.min(menu.y, shell.clientHeight - element.offsetHeight - 8))}px`;
    element.querySelector('button:not(:disabled)')?.focus();
  }, [menu, viewport]);

  useEffect(() => {
    if (!menu) return;
    const dismiss = (event) => {
      if (!menuRef.current?.contains(event.target) && !event.target.closest('[data-mindmap-node]')) setMenu(null);
    };
    document.addEventListener('pointerdown', dismiss);
    return () => document.removeEventListener('pointerdown', dismiss);
  }, [menu]);

  useEffect(() => {
    const canvas = canvasRef.current;
    const wheel = (event) => {
      if (hasOperation) return;
      event.preventDefault();
      if (menuRef.current?.contains(event.target)) return;
      const rect = canvas.getBoundingClientRect();
      setMenu(null);
      setManualView(zoomChannelGraph(view, view.zoom * Math.exp(-event.deltaY * 0.002), { x: event.clientX - rect.left, y: event.clientY - rect.top }));
    };
    canvas.addEventListener('wheel', wheel, { passive: false });
    return () => canvas.removeEventListener('wheel', wheel);
  }, [view, hasOperation]);

  const openNodeMenu = (event, node, atPointer = false) => {
    event.preventDefault();
    const rect = event.currentTarget.getBoundingClientRect();
    const shell = dialogRef.current.getBoundingClientRect();
    menuAnchorRef.current = event.currentTarget;
    setSelectedKey(node.key);
    setMenu({ key: node.key, x: (atPointer ? event.clientX : rect.right + 6) - shell.left, y: (atPointer ? event.clientY : rect.top) - shell.top });
  };
  // Provider forms and confirmation dialogs continue to own all mutations.
  const openAction = (callback, ...args) => {
    setMenu(null);
    setManualView(view);
    callback?.(...args);
  };
  const zoom = (factor) => {
    setMenu(null);
    setManualView(zoomChannelGraph(view, view.zoom * factor, { x: viewport.width / 2, y: viewport.height / 2 }));
  };
  const startPan = (event) => {
    if (hasOperation || event.button !== 0 || event.target.closest('button')) return;
    event.preventDefault();
    setMenu(null);
    dragRef.current = { pointerId: event.pointerId, x: event.clientX, y: event.clientY, view };
    event.currentTarget.setPointerCapture(event.pointerId);
    setDragging(true);
  };
  const pan = (event) => {
    const drag = dragRef.current;
    if (drag?.pointerId !== event.pointerId) return;
    setManualView({ ...drag.view, x: drag.view.x + event.clientX - drag.x, y: drag.view.y + event.clientY - drag.y });
  };
  const endPan = () => { dragRef.current = null; setDragging(false); };

  return renderModalPortal(
    <div className={`channel-workspace-backdrop fixed inset-0 z-[100] flex items-center justify-center px-6 pb-5 pt-10 ${closing ? 'is-closing' : ''}`} onMouseDown={(event) => { if (event.target === event.currentTarget) requestClose(); }}>
      <section ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} className={`channel-workspace t-modal ${closing ? 'is-closing' : 'is-open'} relative flex max-h-[calc(100dvh-5rem)] max-w-full flex-col overflow-hidden rounded-2xl border border-slate-200 shadow-xl`} style={{ width: hasOperation ? 940 : Math.min(860, Math.max(560, graph.width + 100)), height: hasOperation ? 660 : Math.min(600, Math.max(320, graph.height + 142)) }}>
        <header inert={hasOperation} className="flex shrink-0 items-center justify-between gap-3 border-b border-slate-200/60 px-4 py-3">
          <div className="flex min-w-0 items-center gap-2.5">
            <div className="channel-resource-emblem !h-8 !w-8"><Network size={16} /></div>
            <h2 id={titleId} className="shrink-0 text-sm font-semibold text-slate-900">凭证分组与端点</h2>
            <span className="truncate text-xs text-slate-400">{name}</span>
          </div>
          <button ref={closeRef} type="button" aria-label="关闭渠道结构" className={iconButton} onClick={requestClose}><X size={16} /></button>
        </header>
        <div inert={hasOperation} className="flex h-9 shrink-0 items-center justify-between gap-2 border-b border-slate-200/50 px-4 text-[11px] text-slate-400">
          <span className="tabular-nums">{groups.length} 个分组 · {endpoints.length} 个端点</span>
          <div role="group" aria-label="画布缩放" className="flex items-center gap-0.5">
            <button type="button" className={iconButton} aria-label="缩小画布" disabled={view.zoom <= MIN_GRAPH_ZOOM} onClick={() => zoom(1 / 1.2)}><Minus size={13} /></button>
            <span aria-label="当前缩放比例" className="w-9 text-center tabular-nums text-slate-500">{Math.round(view.zoom * 100)}%</span>
            <button type="button" className={iconButton} aria-label="放大画布" disabled={view.zoom >= MAX_GRAPH_ZOOM} onClick={() => zoom(1.2)}><Plus size={13} /></button>
            <span className="mx-1 h-3 w-px bg-slate-200" />
            <Tooltip asChild content="适应窗口"><button type="button" className={iconButton} aria-label="适应窗口" onClick={() => { setManualView(null); setMenu(null); }}><Maximize2 size={13} /></button></Tooltip>
          </div>
        </div>

        <div inert={hasOperation} ref={canvasRef} aria-label="渠道结构画布" className={`channel-graph-canvas relative min-h-0 flex-1 touch-none overflow-hidden ${dragging ? 'cursor-grabbing' : 'cursor-grab'}`} onPointerDown={startPan} onPointerMove={pan} onPointerUp={endPan} onPointerCancel={endPan}>
          <div role="group" aria-label={`${name}思维导图`} className="absolute left-0 top-0 origin-top-left" style={{ width: graph.width, height: graph.height, transform: `translate(${view.x}px, ${view.y}px) scale(${view.zoom})` }}>
            <svg aria-hidden="true" className="pointer-events-none absolute inset-0" width={graph.width} height={graph.height}>
              {graph.links.map((link) => {
                const highlighted = selectedKey === link.target || selectedKey === link.source;
                return <path key={link.key} className="channel-graph-link" d={link.path} fill="none" stroke={highlighted ? '#d6a05f' : link.trunk ? '#cbb696' : '#bac8d1'} strokeWidth={highlighted ? 2 : 1.4} strokeLinecap="round" />;
              })}
            </svg>
            {graph.nodes.map((node) => {
              const isSubscription = node.group?.authMode === 'client_passthrough';
              const selected = selectedKey === node.key;
              const Icon = node.kind === 'channel' ? Layers : node.kind === 'group' ? KeyRound : Server;
              return <button key={node.key} type="button" data-mindmap-node={node.key} data-node-kind={node.kind} aria-label={`${node.kind === 'channel' ? '渠道' : node.kind === 'group' ? '凭证分组' : '端点'}节点 ${node.label}`} aria-haspopup="menu" aria-expanded={menu?.key === node.key} className={`channel-flow-node absolute cursor-pointer text-left outline-none ${selected ? 'is-selected' : ''}`} style={{ left: node.x, top: node.y, width: node.width, height: node.height }} onClick={(event) => openNodeMenu(event, node)} onContextMenu={(event) => openNodeMenu(event, node, true)} onKeyDown={(event) => { if (event.key === 'ContextMenu' || event.shiftKey && event.key === 'F10') openNodeMenu(event, node); }}>
                {node.kind !== 'channel' && <span aria-hidden="true" className="channel-node-port is-input" />}
                {(node.kind === 'channel' && groups.length > 0 || node.kind === 'group' && node.endpointCount > 0) && <span aria-hidden="true" className="channel-node-port is-output" />}
                <span className="channel-node-heading">
                  <span className="channel-node-symbol"><Icon size={11} /></span>
                  <span className="flex-1">{node.kind === 'channel' ? '渠道' : node.kind === 'group' ? isSubscription ? '订阅账号' : '凭证分组' : '端点'}</span>
                  {node.kind === 'endpoint' && <Tooltip asChild content="端点优先级"><span className="channel-node-priority">P{node.endpoint.priority}</span></Tooltip>}
                  <MoreVertical size={12} className="opacity-50" />
                </span>
                <span className="channel-node-content">
                  <span className="channel-node-name block truncate">{node.label}</span>
                  {node.kind === 'channel' && <span className="channel-node-meta">{groups.length} 个分组<span className="text-slate-300">·</span>{endpoints.length} 个端点</span>}
                  {node.kind === 'group' && <span className="channel-node-meta"><span className="inline-flex items-center gap-1"><Server size={10} />{node.endpointCount} 端点</span><span className="channel-node-status"><span className="channel-node-status-dot" />{node.group.canAddEndpoint === false ? '待配置' : '已配置'}</span></span>}
                  {node.kind === 'endpoint' && <span className="channel-node-url block truncate">{node.endpoint.url}</span>}
                </span>
              </button>;
            })}
            {!groups.length && <p className="absolute left-0 w-full text-center text-[11px] text-slate-400" style={{ top: graph.height - 28 }}>右键渠道节点，添加第一个凭证分组</p>}
          </div>
        </div>
        <footer inert={hasOperation} className="flex h-8 shrink-0 items-center justify-between gap-3 border-t border-slate-200/50 px-4 text-[10px] text-slate-400">
          <span>点击节点操作 · 右键父节点添加</span>
          <span>拖动画布 · 滚轮缩放</span>
        </footer>

        {menu && menuNode && <div ref={menuRef} role="menu" aria-label={`${menuNode.label}节点操作`} className="channel-node-menu t-dropdown is-open absolute z-10 w-60 overflow-y-auto rounded-xl border border-slate-200 p-1.5 shadow-xl" style={{ left: menu.x, top: menu.y }} onContextMenu={(event) => event.preventDefault()} onKeyDown={(event) => {
          if (event.key === 'Escape') { event.stopPropagation(); event.preventDefault(); setMenu(null); menuAnchorRef.current?.focus(); }
          if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
            event.preventDefault();
            const items = [...event.currentTarget.querySelectorAll('button:not(:disabled)')];
            const index = items.indexOf(document.activeElement);
            items[event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : (index + (event.key === 'ArrowUp' ? -1 : 1) + items.length) % items.length]?.focus();
          }
        }}>
          <div className="mb-1 border-b border-slate-100 px-2.5 py-2">
            <p className="text-xs font-semibold text-slate-800 [overflow-wrap:anywhere]">{menuNode.label}</p>
            <p className="mt-1 text-[10px] text-slate-400">{menuNode.kind === 'channel' ? `渠道 · ${groups.length} 个凭证分组` : menuNode.kind === 'group' ? `${menuNode.group.authMode === 'client_passthrough' ? '订阅账号' : 'API 凭证'} · ${menuNode.endpointCount} 个端点` : `优先级 #${menuNode.endpoint.priority}`}</p>
            {menuNode.kind === 'endpoint' && <p className="mt-1 text-[10px] text-slate-400 [overflow-wrap:anywhere]">{menuNode.endpoint.url}</p>}
          </div>
          {menuNode.kind === 'channel' && <>
            <button type="button" role="menuitem" className={menuItem} disabled={controlsBusy} onClick={() => openAction(onAddGroup)}><Plus size={14} />添加凭证分组</button>
            <button type="button" role="menuitem" className={menuItem} disabled={controlsBusy} onClick={() => openAction(onEdit)}><Pencil size={13} />编辑渠道</button>
            <button type="button" role="menuitem" className={`${menuItem} text-rose-600 hover:bg-rose-50`} disabled={controlsBusy} onClick={() => openAction(onDelete)}><Trash2 size={13} />删除渠道</button>
          </>}
          {menuNode.kind === 'group' && <>
            <Tooltip asChild content={menuNode.group.canAddEndpoint === false ? '请先配置可用凭证' : undefined}><button type="button" role="menuitem" className={menuItem} disabled={controlsBusy || menuNode.group.canAddEndpoint === false} onClick={() => openAction(onAddEndpoint, menuNode.group)}><Plus size={14} />添加端点{menuNode.group.canAddEndpoint === false && <span className="ml-auto text-[10px]">需配置凭证</span>}</button></Tooltip>
            <button type="button" role="menuitem" className={menuItem} onClick={() => openAction(onOpenGroup, menuNode.group)}><Info size={13} />查看分组详情</button>
            <button type="button" role="menuitem" className={menuItem} disabled={controlsBusy} onClick={() => openAction(onEditGroup, menuNode.group)}><Pencil size={13} />编辑凭证分组</button>
            <button type="button" role="menuitem" className={`${menuItem} text-rose-600 hover:bg-rose-50`} disabled={controlsBusy} onClick={() => openAction(onDeleteGroup, menuNode.group)}><Trash2 size={13} />删除凭证分组</button>
            {menuNode.group.authMode === 'client_passthrough' && <div className="mt-1 max-h-32 overflow-y-auto border-t border-slate-100 p-1 [&>div]:p-2 [&>div]:text-[10px]"><SubscriptionStatus group={menuNode.group} profileMode /></div>}
          </>}
          {menuNode.kind === 'endpoint' && <>
            <button type="button" role="menuitem" className={menuItem} onClick={() => openAction(onOpenEndpoint, menuNode.endpoint.original)}><Info size={13} />查看端点详情</button>
            <button type="button" role="menuitem" className={menuItem} disabled={controlsBusy} onClick={() => openAction(onEditEndpoint, menuNode.endpoint.original)}><Pencil size={13} />编辑端点</button>
            <button type="button" role="menuitem" className={`${menuItem} text-rose-600 hover:bg-rose-50`} disabled={controlsBusy} onClick={() => openAction(onDeleteEndpoint, menuNode.endpoint.original)}><Trash2 size={13} />删除端点</button>
          </>}
        </div>}
        <div ref={modalScope?.setTarget} className="channel-canvas-overlays" />
      </section>
    </div>,
    { root: true }
  );
}
