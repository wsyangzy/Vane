import Tooltip from '@/shared/ui/Tooltip.jsx';
import { useCallback, useEffect, useRef, useState } from 'react';
import { AlertCircle, Check, ChevronDown, Loader2, RefreshCw, Star } from 'lucide-react';
import { listRoutingProfiles, setDefaultRoutingProfile } from '@/services/wails/dispatch.js';
import { strategyOptions } from '@/features/providers/shared/dispatch/dispatchState.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';
import { ClaudeIcon } from './ModelTag.jsx';
import { CodexLogo } from '@/features/providers/codex/components/CodexLogo.jsx';

const PROVIDERS = [{ id: 'claude', label: 'Claude', icon: ClaudeIcon }, { id: 'codex', label: 'Codex', icon: CodexLogo }];
const emptyState = () => ({ profiles: [], loading: true, error: '' });
const strategyLabel = (value) => strategyOptions.find((option) => option.value === value)?.label.split(' · ')[0] || value;

export default function RoutingProfileSwitcher() {
  const [isOpen, setIsOpen] = useState(false);
  const [states, setStates] = useState(() => ({ claude: emptyState(), codex: emptyState() }));
  const [switchErrors, setSwitchErrors] = useState({});
  const [switching, setSwitching] = useState(null);
  const containerRef = useRef(null);
  const triggerRef = useRef(null);
  const panelRef = useRef(null);
  const switchingRef = useRef(false);
  const generations = useRef({ claude: 0, codex: 0 });

  const reload = useCallback(async (provider) => {
    const generation = ++generations.current[provider];
    setStates((previous) => ({ ...previous, [provider]: { ...previous[provider], loading: true, error: '' } }));
    try {
      const state = await listRoutingProfiles(provider);
      if (generation !== generations.current[provider]) return;
      setStates((previous) => ({ ...previous, [provider]: { profiles: state.profiles, loading: false, error: '' } }));
    } catch (error) {
      if (generation !== generations.current[provider]) return;
      setStates((previous) => ({ ...previous, [provider]: { ...previous[provider], loading: false, error: getErrorMessage(error, '方案加载失败，请重试') } }));
    }
  }, []);

  useEffect(() => {
    const reloadAll = () => {
      if (!switchingRef.current) PROVIDERS.forEach(({ id }) => { void reload(id); });
    };
    reloadAll();
    window.addEventListener('focus', reloadAll);
    const pending = generations.current;
    return () => {
      pending.claude += 1;
      pending.codex += 1;
      window.removeEventListener('focus', reloadAll);
    };
  }, [reload]);

  useEffect(() => {
    if (!isOpen) return;
    const outside = (event) => {
      if (!containerRef.current?.contains(event.target)) setIsOpen(false);
    };
    const keyDown = (event) => {
      if (event.key !== 'Escape') return;
      setIsOpen(false);
      triggerRef.current?.focus();
    };
    const position = () => {
      const panel = panelRef.current;
      if (!panel) return;
      panel.style.translate = '0 0';
      const rect = panel.getBoundingClientRect();
      const shift = rect.left < 16 ? 16 - rect.left : Math.min(innerWidth - 16 - rect.right, 0);
      panel.style.translate = `${shift}px 0`;
      panel.style.maxHeight = `${Math.max(0, innerHeight - rect.top - 16)}px`;
    };
    position();
    document.addEventListener('mousedown', outside);
    document.addEventListener('keydown', keyDown);
    window.addEventListener('resize', position);
    return () => {
      document.removeEventListener('mousedown', outside);
      document.removeEventListener('keydown', keyDown);
      window.removeEventListener('resize', position);
    };
  }, [isOpen]);

  const selectProfile = async (provider, profile) => {
    if (switchingRef.current || states[provider].loading || states[provider].error || !profile.enabled || profile.isDefault || !profile.members.some((member) => member.enabled)) return;
    switchingRef.current = true;
    setSwitching({ provider, id: profile.id });
    setSwitchErrors((previous) => ({ ...previous, [provider]: '' }));
    try {
      await setDefaultRoutingProfile(provider, profile.id);
      // Only the successful desktop operation confirms a default change.
      setStates((previous) => ({ ...previous, [provider]: { ...previous[provider], profiles: previous[provider].profiles.map((item) => ({ ...item, isDefault: item.id === profile.id })) } }));
    } catch (error) {
      setSwitchErrors((previous) => ({ ...previous, [provider]: getErrorMessage(error, '默认方案切换失败') }));
    } finally {
      await reload(provider);
      switchingRef.current = false;
      setSwitching(null);
    }
  };

  const defaultLabel = (state) => state.profiles.find((profile) => profile.isDefault)?.name || (state.loading ? '加载中' : state.error ? '读取失败' : '未设置方案');

  return <div className="requests-profile-switcher" ref={containerRef}>
    <button type="button" ref={triggerRef} className="requests-profile-trigger motion-control" data-active={isOpen}
      aria-label="切换默认调度方案" aria-expanded={isOpen} aria-controls="requests-profile-menu"
      onClick={() => {
        if (!isOpen && !switchingRef.current) PROVIDERS.forEach(({ id }) => { void reload(id); });
        setIsOpen((previous) => !previous);
      }}>
      <span className="requests-profile-labels">{PROVIDERS.map(({ id, label, icon: Icon }) => <span key={id} className="requests-profile-entry" data-provider={id}>
        <span className="requests-provider-mark"><Icon className="h-3 w-3" /></span><span className="requests-profile-prefix">{label}</span>
        <span className="min-w-0 truncate" data-warning={Boolean(states[id].error || (!states[id].loading && !states[id].profiles.some((profile) => profile.isDefault && profile.enabled)))}>{defaultLabel(states[id])}</span>
      </span>)}</span>
      {switching ? <Loader2 className="animate-spin" /> : <ChevronDown className={`requests-profile-chevron ${isOpen ? 'rotate-180' : ''}`} />}
    </button>
    {isOpen && <div id="requests-profile-menu" ref={panelRef} role="region" aria-label="默认调度方案" className="requests-profile-menu">
      <div className="requests-menu-heading"><span>默认调度方案</span><small>按提供商独立切换</small></div>
      <div className="requests-profile-columns">{PROVIDERS.map(({ id, label, icon: Icon }) => {
        const state = states[id];
        return <section key={id} className="requests-profile-column" aria-label={`${label} 调度方案`} aria-busy={state.loading || switching?.provider === id}>
          <h3><span className="requests-provider-mark" data-provider={id}><Icon className="h-3 w-3" /></span>{label}<span className="requests-profile-count">{state.profiles.length}</span>
            <Tooltip asChild content="重新读取方案"><button type="button" aria-label={`刷新 ${label} 调度方案`} disabled={state.loading || Boolean(switching)} onClick={() => { setSwitchErrors((previous) => ({ ...previous, [id]: '' })); void reload(id); }}><RefreshCw size={12} className={state.loading ? 'animate-spin' : ''} /></button></Tooltip>
          </h3>
          <div className="requests-profile-options">
            {(state.error || switchErrors[id]) && <p role="alert" className="requests-profile-error"><AlertCircle size={13} /><span>{state.error || switchErrors[id]}</span></p>}
            {!state.profiles.length && !state.error && <p className="requests-profile-empty">{state.loading ? '正在加载方案…' : `尚无方案，请在 ${label} 管理中创建。`}</p>}
            {state.profiles.map((profile) => {
              const members = profile.members.filter((member) => member.enabled);
              const unavailable = !profile.enabled || !members.length;
              const isSwitching = switching?.provider === id && switching.id === profile.id;
              return <button type="button" key={profile.id} className="requests-profile-option" data-active={profile.isDefault}
                disabled={Boolean(switching) || state.loading || Boolean(state.error) || profile.isDefault || unavailable}
                aria-current={profile.isDefault ? 'true' : undefined} aria-label={`${label}：${profile.name}${profile.isDefault ? '，当前默认' : ''}`}
                onClick={() => { void selectProfile(id, profile); }}>
                <span className="requests-profile-option-copy"><span className="requests-profile-name">{profile.name}</span><span className="requests-profile-meta">{!profile.enabled ? '方案已停用' : !members.length ? '无参与分组' : `${strategyLabel(profile.strategy)} · ${new Set(members.map((member) => member.channelId)).size} 渠道`}</span></span>
                {isSwitching ? <Loader2 size={13} className="animate-spin" /> : profile.isDefault ? <Check size={13} /> : <Star size={12} className="requests-profile-set-default" />}
              </button>;
            })}
          </div>
        </section>;
      })}</div>
      <p className="requests-profile-footnote">选择后设为该提供商的默认方案，用于后续请求调度。</p>
    </div>}
  </div>;
}
