import { WailsApp, initWails } from './connection.js';

export function mapClientConfigBackups(result = {}) {
  return {
    directory: result.directory || '', defaultDirectory: result.default_directory || '',
    revision: result.revision || '', configPath: result.config_path || '',
    records: (result.records || []).map(record => ({
      id: record.id, name: record.name || '', directory: record.directory || '', createdAt: record.created_at, provider: record.provider,
      operation: record.operation, configPath: record.config_path,
      exists: Boolean(record.exists), size: Number(record.size || 0),
    })),
  };
}

export async function manageClientConfigBackups(provider, action = 'list', input = {}) {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');
  return mapClientConfigBackups(await WailsApp.ManageClientConfigBackups({
    provider, action, id: input.id || '', revision: input.revision || '', name: input.name || '',
  }));
}
