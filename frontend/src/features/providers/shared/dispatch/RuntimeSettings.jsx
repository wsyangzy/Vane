import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { AlertCircle, CheckCircle2, Hash, Save, X } from 'lucide-react';
import { Button, ConfirmDialog } from '@/shared/ui/index.js';
import { batchUpdateSettings, getSettingsByScope, resetSettingsByScope } from '@/services/wails/settings.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';
import { runtimeSettingSections, settingsForRuntimeSection } from '@/shared/lib/settingsSections.js';
import { SettingItem, SettingsSection } from '@/features/settings/components/index.js';
import { buildSettingsUpdates } from '@/features/settings/settingsUpdates.js';

const settingKey = setting => `global.${setting.category}.${setting.key}`;

export default function RuntimeSettings({ provider, refreshToken, onBusyChange, onApplied }) {
  const sections = useMemo(() => runtimeSettingSections(provider), [provider]);
  const [settings, setSettings] = useState([]);
  const [changes, setChanges] = useState({});
  const loadKey = `${provider}:${refreshToken ?? ''}`;
  const [loadedKey, setLoadedKey] = useState(null);
  const loading = loadedKey !== loadKey;
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [resetSection, setResetSection] = useState(null);
  const generation = useRef(0);
  const busy = loading || saving;
  const changeCount = Object.keys(changes).length;

  const load = useCallback(() => {
    const request = ++generation.current;
    return Promise.all(sections.map(section => getSettingsByScope('global', section.category))).then((batches) => {
      if (request !== generation.current) return;
      setSettings(batches.flat()); setChanges({}); setError(''); setNotice('');
    }).catch((error) => {
      if (request === generation.current) setError(getErrorMessage(error, '读取 Token 预估设置失败，请刷新重试'));
    }).finally(() => {
      if (request === generation.current) setLoadedKey(loadKey);
    });
  }, [sections, loadKey]);
  useEffect(() => {
    const pending = generation;
    load();
    return () => { pending.current++; };
  }, [load, refreshToken]);
  useEffect(() => { onBusyChange?.(busy); return () => onBusyChange?.(false); }, [busy, onBusyChange]);

  const currentValue = setting => changes[settingKey(setting)] ?? setting.value;
  const change = (category, key, value) => {
    setNotice(''); setError('');
    setChanges(previous => {
      const next = { ...previous }, id = `global.${category}.${key}`;
      const original = settings.find(setting => settingKey(setting) === id);
      if (String(value) === String(original?.value)) delete next[id];
      else next[id] = value;
      return next;
    });
  };
  const revert = (category, key) => setChanges(previous => {
    const next = { ...previous }; delete next[`global.${category}.${key}`]; return next;
  });
  const save = async () => {
    if (busy || !changeCount) return;
    const request = generation.current;
    const updates = buildSettingsUpdates(changes);
    setSaving(true); setError(''); setNotice('');
    try {
      await batchUpdateSettings({ settings: updates });
      if (request !== generation.current) return;
      setSettings(previous => previous.map(setting => ({ ...setting, value: changes[settingKey(setting)] ?? setting.value })));
      setChanges({});
      await onApplied?.();
      if (request === generation.current) setNotice('Token 预估设置已保存并生效');
    } catch (error) {
      if (request === generation.current) setError(getErrorMessage(error, '保存 Token 预估设置失败'));
    } finally {
      if (request === generation.current) setSaving(false);
    }
  };
  const reset = async () => {
    const section = resetSection, request = generation.current;
    if (!section || busy) return;
    setResetSection(null); setSaving(true); setError(''); setNotice('');
    let applied = false;
    try {
      await resetSettingsByScope('global', section.category);
      applied = true;
      const next = await getSettingsByScope('global', section.category);
      if (request !== generation.current) return;
      setSettings(previous => [...previous.filter(setting => setting.category !== section.category), ...next]);
      setChanges(previous => Object.fromEntries(Object.entries(previous).filter(([key]) => !key.startsWith(`global.${section.category}.`))));
      await onApplied?.();
      if (request === generation.current) setNotice(`${section.title}已恢复默认值`);
    } catch (error) {
      if (request === generation.current) setError(`${applied ? '已重置，但刷新设置失败：' : ''}${getErrorMessage(error, '重置 Token 预估设置失败')}`);
    } finally {
      if (request === generation.current) setSaving(false);
    }
  };

  const renderSection = section => {
    const items = settingsForRuntimeSection(settings, section);
    const renderItem = setting => <SettingItem key={settingKey(setting)} setting={setting} value={currentValue(setting)} onChange={change} onRevert={revert} hasChange={Object.hasOwn(changes, settingKey(setting))} disabled={busy} />;
    return <section key={section.category} className="min-w-0" aria-label={section.title}>
      <SettingsSection compact title={section.title} icon={Hash} description={`仅 Claude · ${section.description}`} onReset={items.length ? () => setResetSection(section) : undefined} resetDisabled={busy}>
        {items.map(renderItem)}
        {!items.length && <p className="py-3 text-xs text-slate-400">{loading ? '正在读取…' : '未读取到可用配置，请刷新重试。'}</p>}
      </SettingsSection>
    </section>;
  };

  return <section className="min-w-0 space-y-3" aria-label="Claude Token 预估设置">
    <header className="sticky top-0 z-10 flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2">
      <div className="min-w-0"><h2 className="flex items-center gap-1.5 text-sm font-semibold text-slate-800"><Hash size={14} className="text-indigo-500" />Token 预估</h2><p className="mt-0.5 text-[11px] leading-4 text-slate-500">仅用于 Claude 的 Token 预估请求。请求控制、健康检查与调度保护统一在顶部「设置」中配置。</p></div>
      <div className="flex shrink-0 items-center gap-2">{changeCount > 0 && <Button type="button" size="sm" variant="ghost" icon={X} disabled={busy} onClick={() => { setChanges({}); setNotice(''); setError(''); }}>撤销修改</Button>}<Button type="button" size="sm" icon={Save} loading={saving} disabled={busy || !changeCount} onClick={save}>{changeCount ? `保存 (${changeCount})` : '保存'}</Button></div>
    </header>
    {error && <p role="alert" className="flex min-w-0 items-start gap-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700"><AlertCircle size={14} className="mt-0.5 shrink-0" /><span className="min-w-0 wrap-anywhere">{error}</span></p>}
    {notice && <p role="status" className="flex items-center gap-2 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-700"><CheckCircle2 size={14} className="shrink-0" />{notice}</p>}
    <div className="min-w-0 space-y-3">{sections.map(renderSection)}</div>
    <ConfirmDialog isOpen={Boolean(resetSection)} title="重置 Token 预估设置" message={resetSection ? `将「${resetSection.title}」恢复为默认值？仅影响 Claude。` : ''} confirmText="重置" onConfirm={reset} onCancel={() => setResetSection(null)} />
  </section>;
}
