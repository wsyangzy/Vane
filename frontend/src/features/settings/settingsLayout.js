import { codexRecoverySettingKeys } from '../../shared/lib/settingsSections.js';

// UI grouping only: every field retains its stored category and reset scope.
export const settingsCardLayouts = {
  auth: { primary: ['enabled', 'token'], pages: [] },
  request: {
    primary: ['global_timeout'], title: '响应与恢复',
    pages: [
      { key: 'response', label: '响应等待', keys: ['response_header_timeout', 'default_response_header_timeout'] },
      { key: 'strategy', label: '恢复策略', keys: codexRecoverySettingKeys.slice(0, 2) },
      { key: 'storage', label: '恢复存储', keys: codexRecoverySettingKeys.slice(2) },
    ],
  },
  retry: {
    primary: ['max_total_attempts', 'max_total_duration'], title: '冷却与回退',
    pages: [
      { key: 'cooldown', label: '冷却与等待', keys: ['wait_for_recovery', 'cooldown_initial', 'cooldown_multiplier', 'cooldown_max'], description: '方案与全局预算取较小值。新冷却参数用于后续失败，已有冷却保留原到期时间；上游指定的等待时间优先。' },
      { key: 'fallback', label: '规则回退', keys: ['rule_fallback_to_default'], description: '仅作用于选择“跟随全局”的规则，单条规则的允许或禁止优先。' },
    ],
  },
  health: { primary: ['active_probes_enabled', 'check_interval', 'timeout', 'health_path'], pages: [] },
  retention: { primary: ['retention_days', 'archive_directory'], pages: [] },
  backup: { primary: ['directory'], pages: [] },
  logging: { primary: ['level', 'file_enabled'], title: '格式与归档', pages: [{ key: 'files', label: '格式与归档', keys: ['format', 'max_file_size', 'max_files', 'compress_rotated'] }] },
  cache: { primary: ['enabled', 'fallback_ttl', 'max_entries', 'stale_on_error'], pages: [] },
};

export function settingsInOrder(settings, keys) {
  return keys.map(key => settings.find(setting => setting.key === key)).filter(Boolean);
}
