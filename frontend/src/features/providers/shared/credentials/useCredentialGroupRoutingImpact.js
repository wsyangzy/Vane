import { useEffect, useState } from 'react';
import { listRoutingProfiles } from '@/services/wails/dispatch.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';
import { credentialGroupRoutingImpact } from '../dispatch/dispatchState.js';

export default function useCredentialGroupRoutingImpact(provider, groupId) {
  const key = groupId ? `${provider}/${groupId}` : '';
  const [state, setState] = useState({ key: '', loading: false, error: '', profiles: [] });
  useEffect(() => {
    if (!groupId) return;
    let active = true;
    Promise.all(['claude', 'codex'].map(async (owner) => ({ provider: owner, ...await listRoutingProfiles(owner) })))
      .then((workspaces) => {
        if (active) setState({ key, loading: false, error: '', profiles: credentialGroupRoutingImpact(workspaces, provider, groupId) });
      })
      .catch((error) => {
        if (active) setState({ key, loading: false, error: getErrorMessage(error, '读取调度方案引用失败'), profiles: [] });
      });
    return () => { active = false; };
  }, [provider, groupId, key]);
  if (!key) return { loading: false, error: '', profiles: [] };
  return state.key === key ? state : { loading: true, error: '', profiles: [] };
}
