import Tooltip from '@/shared/ui/Tooltip.jsx';
import { useEffect, useRef, useState } from 'react';
import { Maximize2, Minimize2, Minus, X } from 'lucide-react';
import { useToast } from '@/shared/ui/index.js';

// Uses the Wails runtime directly; Close reaches the existing tray-hide hook.
export default function WindowControls() {
  const [maximised, setMaximised] = useState(false);
  const runtimeRef = useRef(null);
  const toast = useToast();

  useEffect(() => {
    let active = true;
    let subscriptions = [];
    let stateRevision = 0;
    import('@wailsio/runtime').then(async (runtime) => {
      if (!active) return;
      runtimeRef.current = runtime;
      const sync = async () => {
        const revision = ++stateRevision;
        try {
          const value = await runtime.Window.IsMaximised();
          if (active && revision === stateRevision) setMaximised(value);
        } catch { /* The window may already be closing. */ }
      };
      subscriptions = ['WindowMaximise', 'WindowUnMaximise', 'WindowRestore', 'WindowDidResize']
        .map((name) => runtime.Events.On(runtime.Events.Types.Common[name], sync));
      await sync();
    }).catch(() => { /* Each button can retry loading the runtime. */ });
    return () => { active = false; subscriptions.forEach((unsubscribe) => unsubscribe()); };
  }, []);

  const run = async (action) => {
    try {
      const runtime = runtimeRef.current || await import('@wailsio/runtime');
      runtimeRef.current = runtime;
      await runtime.Window[action]();
      if (action === 'ToggleMaximise') setMaximised(await runtime.Window.IsMaximised());
    } catch { toast.error('窗口操作失败，请重试'); }
  };
  const button = 'flex h-8 w-11 shrink-0 items-center justify-center text-slate-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-indigo-500';

  return <div className="desktop-window-controls flex shrink-0 items-center" aria-label="窗口操作">
    <Tooltip asChild content="最小化"><button type="button" aria-label="最小化窗口" className={`${button} hover:bg-slate-100 hover:text-slate-900`} onClick={() => run('Minimise')}><Minus size={15} /></button></Tooltip>
    <Tooltip asChild content={maximised ? '还原' : '最大化'}><button type="button" aria-label={maximised ? '还原窗口' : '最大化窗口'} className={`${button} hover:bg-slate-100 hover:text-slate-900`} onClick={() => run('ToggleMaximise')}>{maximised ? <Minimize2 size={14} /> : <Maximize2 size={14} />}</button></Tooltip>
    <Tooltip asChild content="关闭窗口（隐藏到托盘）"><button type="button" aria-label="关闭窗口并隐藏到托盘" className={`${button} hover:bg-rose-500 hover:text-white`} onClick={() => run('Close')}><X size={17} /></button></Tooltip>
  </div>;
}
