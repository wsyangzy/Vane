import { useEffect, useId, useState } from 'react';
import { GitBranch, Pencil, RefreshCw, X } from 'lucide-react';
import { saveRoutingProfile } from '@/services/wails/dispatch.js';
import { getSettingsByCategory } from '@/services/wails/settings.js';
import { readDispatchPolicy } from '@/shared/lib/dispatchPolicy.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import ProfileEditor from '../dispatch/ProfileEditor.jsx';

function RoutingReferencesDialog({ impact, onClose }) {
  const titleId = useId();
  const [editing, setEditing] = useState(null);
  const [policy, setPolicy] = useState(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState('');

  useEffect(() => {
    let active = true;
    getSettingsByCategory('retry').then(readDispatchPolicy).then((value) => {
      if (active) setPolicy(value);
    }).catch(() => {
      // The editor can still save explicit overrides without global settings.
    });
    return () => { active = false; };
  }, []);

  const close = () => { if (!saving) onClose(); };
  const exitEditor = () => { setEditing(null); setSaveError(''); };
  const save = async (draft) => {
    if (saving) return;
    setSaving(true);
    setSaveError('');
    try {
      await saveRoutingProfile(draft);
      exitEditor();
      impact.refresh();
    } catch (error) {
      setSaveError(getErrorMessage(error, '保存调度方案失败'));
    } finally {
      setSaving(false);
    }
  };
  const workspace = impact.workspaces.find((item) => item.provider === editing?.provider);
  const profile = workspace?.profiles.find((item) => item.id === editing.id);

  return renderModalPortal(
    <div className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 p-4" onKeyDown={(event) => {
      if (event.key === 'Escape' && !document.querySelector('[role="listbox"]')) {
        event.preventDefault();
        event.stopPropagation();
        if (!saving) (editing ? exitEditor : close)();
      }
    }}>
      <section role="dialog" aria-modal="true" aria-labelledby={titleId} className={`flex max-h-[calc(100dvh-2rem)] w-full flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-xl ${profile ? 'h-[min(46rem,calc(100dvh-2rem))] max-w-4xl' : 'max-w-lg'}`}>
        <header className="flex shrink-0 items-center justify-between gap-3 border-b border-slate-100 px-5 py-3">
          <h2 id={titleId} className="flex items-center gap-2 text-sm font-semibold text-slate-900"><GitBranch size={16} className="text-slate-500" />关联调度方案</h2>
          <button type="button" className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-slate-500 hover:bg-slate-100 focus-visible:outline-2 focus-visible:outline-indigo-500 disabled:opacity-50" aria-label="关闭" onClick={close} disabled={saving}><X size={16} /></button>
        </header>
        {profile ? <div className="flex min-h-0 flex-1 flex-col gap-2 p-4">
          {saveError && <div role="alert" className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700">
            <span className="min-w-0 flex-1 wrap-anywhere">{saveError}</span>
            <Button type="button" size="sm" variant="ghost" icon={RefreshCw} onClick={() => { exitEditor(); impact.refresh(); }}>返回并刷新</Button>
          </div>}
          <ProfileEditor profile={profile} profiles={workspace.profiles} channels={workspace.channels} accounts={workspace.accounts} relatedWorkspaces={impact.workspaces} policy={policy} busy={saving} showStoredIntent onSave={save} onCancel={exitEditor} />
        </div> : <>
          <div className="min-h-0 overflow-y-auto px-5 py-4">
            {impact.loading && <p role="status" className="py-3 text-sm text-slate-500">正在检查调度方案引用…</p>}
            {impact.error && <div className="flex items-start justify-between gap-3"><p role="alert" className="text-sm text-rose-700 wrap-anywhere">{impact.error}</p><Button type="button" variant="secondary" size="sm" icon={RefreshCw} onClick={impact.refresh}>重试</Button></div>}
            {!impact.loading && !impact.error && !impact.profiles.length && <p role="status" className="py-3 text-sm text-slate-600">此分组已没有调度方案引用。</p>}
            <ul className="divide-y divide-slate-100">
              {impact.profiles.map((item) => {
                const references = [
                  item.memberReference && '参与分组',
                  item.ruleReference && '匹配规则',
                  item.intentReference && '意图目标',
                  item.classifierReference && '意图识别器',
                ].filter(Boolean);
                return <li key={`${item.provider}/${item.id}`} className="flex items-start justify-between gap-3 py-3 first:pt-0 last:pb-0">
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-slate-800 wrap-anywhere">{item.name}</p>
                    <p className="mt-1 text-xs leading-5 text-slate-500">{item.provider === 'claude' ? 'Claude' : 'Codex'} · {references.join('、')}</p>
                    {item.losesLastMember && <p className="mt-1 text-xs text-amber-700">这是方案中唯一参与的分组。</p>}
                  </div>
                  <Button type="button" variant="secondary" size="sm" icon={Pencil} onClick={() => setEditing(item)}>编辑方案</Button>
                </li>;
              })}
            </ul>
          </div>
          <footer className="flex shrink-0 justify-end border-t border-slate-100 px-5 py-3">
            <Button type="button" variant="secondary" onClick={close}>返回删除确认</Button>
          </footer>
        </>}
      </section>
    </div>
  );
}

export default function CredentialGroupRoutingImpact({ impact, endpoint = false, disabled = false }) {
  const [open, setOpen] = useState(false);
  return <>
    {impact.loading && <p className="mt-3 text-xs text-slate-500" role="status">正在检查调度方案引用…</p>}
    {impact.error && <div className="mt-3 flex items-start justify-between gap-2">
      <p className="text-xs text-rose-700 wrap-anywhere" role="alert">{impact.error}</p>
      <Button type="button" size="sm" variant="secondary" icon={RefreshCw} onClick={impact.refresh}>重试</Button>
    </div>}
    {impact.profiles.length > 0 && <div className="mt-3 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2">
      <p className="text-xs font-medium leading-5 text-amber-900" role="status">
        {endpoint ? '端点所属分组' : '该分组'}被 {impact.profiles.length} 个调度方案引用{!endpoint && '，暂不可删除'}
      </p>
      <Button type="button" size="sm" variant="secondary" icon={GitBranch} disabled={disabled} onClick={() => setOpen(true)}>管理调度引用</Button>
    </div>}
    {open && <RoutingReferencesDialog impact={impact} onClose={() => setOpen(false)} />}
  </>;
}
