import { useEffect, useState } from 'react';
import { listRoutingProfiles } from '@/services/wails/dispatch.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';
import { credentialGroupRoutingImpact } from '../dispatch/dispatchState.js';

export default function useCredentialGroupRoutingImpact(provider, groupId) {
  const [revision, setRevision] = useState(0);
  const key = groupId ? `${provider}/${groupId}/${revision}` : '';
  const [state, setState] = useState({ key: '', error: '', profiles: [], workspaces: [] });
  useEffect(() => {
    if (!groupId) return;
    let active = true;
    Promise.all(['claude', 'codex'].map(async (owner) => ({ provider: owner, ...await listRoutingProfiles(owner) })))
      .then((workspaces) => {
        if (active) setState({ key, error: '', profiles: credentialGroupRoutingImpact(workspaces, provider, groupId), workspaces });
      })
      .catch((error) => {
        if (active) setState({ key, error: getErrorMessage(error, '读取调度方案引用失败'), profiles: [], workspaces: [] });
      });
    return () => { active = false; };
  }, [provider, groupId, key]);
  const loading = Boolean(key && state.key !== key);
  const result = key && !loading ? state : { error: '', profiles: [], workspaces: [] };
  return { ...result, loading, refresh: () => setRevision((value) => value + 1) };
}
