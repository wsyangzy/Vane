import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('./subscriptions.js', import.meta.url), 'utf8');
let sequence = 0;
async function bridge(service) {
  const key = `__subscriptionBridgeTest${++sequence}`;
  globalThis[key] = service;
  const isolated = source.replace("import { WailsApp, initWails } from './connection.js';", `const WailsApp=globalThis[${JSON.stringify(key)}];const initWails=async()=>true;`);
  try { return await import(`data:text/javascript;base64,${Buffer.from(isolated).toString('base64')}`); }
  finally { delete globalThis[key]; }
}

test('catalog selection crosses the Wails boundary together with preview revisions', async () => {
  const calls = [];
  const path = 'C:\\模型目录\\selected models.json';
  const api = await bridge({ ManageSubscriptionConnection: async (input) => {
    calls.push(input);
    return { model_catalog_mode: input.model_catalog_mode, model_catalog_path: input.model_catalog_mode === 'local' ? input.model_catalog_path : '',
      profile_id: 12, revision: 'file-revision', binding_version: 3, content_revision: 'preview-revision', auth_enabled: false };
  } });
  for (const [action, mode] of [['preview', 'local'], ['apply', 'local'], ['preview', 'remote']]) {
    const result = await api.manageSubscriptionConnection('codex', action, {
      modelCatalogMode: mode, modelCatalogPath: path, profileId: 12, revision: 'file-revision', bindingVersion: 3, contentRevision: 'preview-revision'
    });
    assert.deepEqual(calls.at(-1), { provider: 'codex', action, profile_id: 12, local_key: '', revision: 'file-revision', binding_version: 3,
      content_revision: 'preview-revision', model_catalog_mode: mode, model_catalog_path: path });
    assert.equal(result.modelCatalogMode, mode);
    assert.equal(result.modelCatalogPath, mode === 'local' ? path : '');
    assert.equal(result.contentRevision, 'preview-revision');
    assert.equal(result.authEnabled, false);
  }
});

test('file selection preserves spaces and cancellation stays empty', async () => {
  let selected = 'C:\\Users\\Test User\\.codex\\codex-models.json';
  const api = await bridge({ ManageSubscriptionConnection: async (input) => {
    assert.equal(input.action, 'choose_model_catalog');
    return selected ? { model_catalog_mode: 'local', model_catalog_path: selected } : {};
  } });
  assert.equal((await api.manageSubscriptionConnection('codex', 'choose_model_catalog')).modelCatalogPath, selected);
  selected = '';
  assert.equal((await api.manageSubscriptionConnection('codex', 'choose_model_catalog')).modelCatalogPath, '');
});

test('catalog validation errors reach the connection panel', async () => {
  const api = await bridge({ ManageSubscriptionConnection: async () => { throw new Error('model_catalog_json 无效'); } });
  await assert.rejects(api.manageSubscriptionConnection('codex', 'preview', { modelCatalogMode: 'local', modelCatalogPath: 'invalid.json' }), /model_catalog_json 无效/);
});
