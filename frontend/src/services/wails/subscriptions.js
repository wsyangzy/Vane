import { WailsApp, initWails } from './connection.js';

// Subscription secrets are explicit local inputs only, never mapped back to UI.
const manageSubscriptionConnection = async (provider, action, input = {}) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const result = await WailsApp.ManageSubscriptionConnection({
    provider, action, pool_id: input.poolId || 0, group_id: input.groupId || 0, local_key: input.localKey || '',
    revision: input.revision || '', binding_version: input.bindingVersion || 0, content_revision: input.contentRevision || ''
  });
  return {
    configPath: result.config_path, baseURL: result.base_url, running: Boolean(result.running),
    backupAvailable: Boolean(result.backup_available), revision: result.revision,
    bindingVersion: result.binding_version, endpointReady: Boolean(result.endpoint_ready),
    officialURL: result.official_url, managed: Boolean(result.managed),
    officialEnabled: Boolean(result.official_enabled), snippet: result.snippet, command: result.command,
    contentRevision: result.content_revision
  };
};

const manageSubscriptionAccounts = async (provider, action, input = {}) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  const result = await WailsApp.ManageSubscriptionAccounts({ provider, action, ...input });
  return {
    accounts: (result?.accounts || []).map(mapSubscriptionAccount),
    account: result?.account ? mapSubscriptionAccount(result.account) : null,
    login: result?.login ? { id: result.login.id, provider: result.login.provider, state: result.login.state,
      url: result.login.url || '', code: result.login.code || '', error: result.login.error || '',
      method: result.login.method || 'browser', pasteCallback: Boolean(result.login.paste_callback),
      pasteCode: Boolean(result.login.paste_code), callbackWarning: result.login.callback_warning || '',
      proxy: result.login.proxy || '',
      account: result.login.account ? mapSubscriptionAccount(result.login.account) : null } : null
  };
};

const mapSubscriptionAccount = (account) => ({
  id: account.id, provider: account.provider, name: account.name, email: account.email || '', plan: account.plan || '',
  accountId: account.account_id || '', expiresAt: account.expires_at || '', enabled: Boolean(account.enabled),
  status: account.status || 'unknown', revision: account.revision || 0, updatedAt: account.updated_at || ''
});

export { manageSubscriptionConnection, manageSubscriptionAccounts, mapSubscriptionAccount };
