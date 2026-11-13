import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Copy, Scissors, ClipboardPaste, Undo2, Redo2, TextSelect } from 'lucide-react';
import { useToast } from './Toast.jsx';

const textInputTypes = new Set(['text', 'search', 'url', 'tel', 'email', 'password', 'number']);
const actions = [
  { id: 'undo', label: '撤销', shortcut: 'Ctrl+Z', icon: Undo2 },
  { id: 'redo', label: '重做', shortcut: 'Ctrl+Y', icon: Redo2 },
  { id: 'cut', label: '剪切', shortcut: 'Ctrl+X', icon: Scissors, separator: true },
  { id: 'copy', label: '复制', shortcut: 'Ctrl+C', icon: Copy },
  { id: 'paste', label: '粘贴', shortcut: 'Ctrl+V', icon: ClipboardPaste },
  { id: 'selectAll', label: '全选', shortcut: 'Ctrl+A', icon: TextSelect, separator: true },
];

function editingContext(target) {
  const input = target.closest('input, textarea');
  const field = input instanceof HTMLTextAreaElement || input instanceof HTMLInputElement && textInputTypes.has(input.type) ? input : null;
  const editable = target.closest('[contenteditable="true"], [contenteditable=""], [contenteditable="plaintext-only"]');
  const editor = field || (target.isContentEditable ? editable : null);
  const selection = window.getSelection();
  const range = selection?.rangeCount ? selection.getRangeAt(0).cloneRange() : null;
  const selectedText = field?.selectionStart != null ? field.value.slice(field.selectionStart, field.selectionEnd) : selection?.toString() || '';
  const writable = editor && !editor.matches(':disabled, [readonly]');
  const canCopy = Boolean(selectedText) && field?.type !== 'password';
  return {
    editor, field, range, selectedText,
    start: field?.selectionStart, end: field?.selectionEnd,
    enabled: {
      undo: Boolean(writable && document.queryCommandEnabled('undo')),
      redo: Boolean(writable && document.queryCommandEnabled('redo')),
      cut: Boolean(writable && canCopy), copy: canCopy, paste: Boolean(writable),
      selectAll: Boolean(editor && (field ? field.value : editor.textContent)),
    },
  };
}

function restoreSelection(context) {
  const { editor, field, range, start, end } = context;
  editor?.focus({ preventScroll: true });
  if (field) {
    if (start != null) field.setSelectionRange(start, end);
  } else if (range) {
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
  }
}

function insertText(context, text) {
  // Browser editing commands preserve the undo stack and emit React's input event.
  if (document.execCommand('insertText', false, text)) return;
  const { field } = context;
  if (!field || field.selectionStart == null) throw new Error('insert_text_unavailable');
  const start = field.selectionStart;
  const end = field.selectionEnd ?? start;
  const value = field.value.slice(0, start) + text + field.value.slice(end);
  // Use the native setter so controlled React inputs observe the value change.
  const prototype = field instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(prototype, 'value').set.call(field, value);
  if (field.selectionStart != null) field.setSelectionRange(start + text.length, start + text.length);
  field.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: text }));
}

export default function EditContextMenu() {
  const [menu, setMenu] = useState(null);
  const menuRef = useRef(null);
  const toast = useToast();

  useEffect(() => {
    const open = (event) => {
      // Feature menus (such as channel nodes) keep their own actions.
      if (event.defaultPrevented || !(event.target instanceof Element)) return;
      event.preventDefault();
      const context = editingContext(event.target);
      if (!context.editor && !context.enabled.copy) { setMenu(null); return; }
      setMenu({ ...context, x: event.clientX, y: event.clientY, keyboard: false });
    };
    const keyboardOpen = (event) => {
      if (event.defaultPrevented || !(event.key === 'ContextMenu' || event.shiftKey && event.key === 'F10')) return;
      if (!(event.target instanceof Element)) return;
      const context = editingContext(event.target);
      if (!context.editor && !context.enabled.copy) return;
      event.preventDefault();
      const rect = event.target.getBoundingClientRect();
      setMenu({ ...context, x: rect.left + 12, y: rect.bottom, keyboard: true });
    };
    document.addEventListener('contextmenu', open);
    document.addEventListener('keydown', keyboardOpen);
    return () => {
      document.removeEventListener('contextmenu', open);
      document.removeEventListener('keydown', keyboardOpen);
    };
  }, []);

  useLayoutEffect(() => {
    if (!menu || !menuRef.current) return;
    const element = menuRef.current;
    const rect = element.getBoundingClientRect();
    element.style.left = `${Math.max(8, Math.min(menu.x, window.innerWidth - rect.width - 8))}px`;
    element.style.top = `${Math.max(8, Math.min(menu.y, window.innerHeight - rect.height - 8))}px`;
    if (menu.keyboard) element.querySelector('button:not(:disabled)')?.focus();
  }, [menu]);

  useEffect(() => {
    if (!menu) return;
    const close = () => setMenu(null);
    const outside = (event) => { if (!menuRef.current?.contains(event.target)) close(); };
    const escape = (event) => {
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); restoreSelection(menu); close(); }
      else if (event.key === 'Tab') close();
    };
    document.addEventListener('pointerdown', outside);
    document.addEventListener('scroll', close, true);
    document.addEventListener('keydown', escape, true);
    window.addEventListener('resize', close);
    window.addEventListener('blur', close);
    return () => {
      document.removeEventListener('pointerdown', outside);
      document.removeEventListener('scroll', close, true);
      document.removeEventListener('keydown', escape, true);
      window.removeEventListener('resize', close);
      window.removeEventListener('blur', close);
    };
  }, [menu]);

  const run = async (id) => {
    const context = menu;
    setMenu(null);
    if (context.editor && !context.editor.isConnected) return;
    restoreSelection(context);
    try {
      if (id === 'copy' || id === 'cut') {
        await navigator.clipboard.writeText(context.selectedText);
        if (id === 'cut' && context.editor.isConnected) { restoreSelection(context); insertText(context, ''); }
      } else if (id === 'paste') {
        const text = await navigator.clipboard.readText();
        if (context.editor.isConnected && text) { restoreSelection(context); insertText(context, text); }
      } else if (id === 'selectAll') {
        if (context.field) context.field.select();
        else window.getSelection().selectAllChildren(context.editor);
      } else document.execCommand(id);
    } catch {
      toast.warning('操作未完成，请使用键盘快捷键重试。');
    }
  };

  if (!menu) return null;
  return createPortal(<div ref={menuRef} role="menu" aria-label="文本编辑" className="fixed z-[20000] w-56 max-h-[calc(100dvh-16px)] overflow-y-auto rounded-xl border border-slate-200 bg-white p-1.5 font-sans text-xs text-slate-700 shadow-xl outline-none" style={{ left: menu.x, top: menu.y }}
    onContextMenu={(event) => event.preventDefault()} onPointerDown={(event) => event.preventDefault()}
    onKeyDown={(event) => {
      const items = [...menuRef.current.querySelectorAll('button:not(:disabled)')];
      const index = items.indexOf(document.activeElement);
      if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
        event.preventDefault();
        const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : (index + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length;
        items[next]?.focus();
      }
    }}>
    {actions.map(({ id, label, shortcut, icon: Icon, separator }) => <div key={id} className={separator ? 'mt-1 border-t border-slate-100 pt-1' : ''}>
      <button type="button" role="menuitem" disabled={!menu.enabled[id]} onClick={() => run(id)} className="flex h-8 w-full items-center gap-2 rounded-lg px-2 text-left outline-none hover:bg-indigo-50 hover:text-indigo-700 focus-visible:bg-indigo-50 focus-visible:text-indigo-700 disabled:pointer-events-none disabled:text-slate-300">
        <Icon size={14} aria-hidden="true" /><span className="flex-1">{label}</span><span className="text-[11px] tabular-nums opacity-60">{shortcut}</span>
      </button>
    </div>)}
  </div>, document.body);
}
