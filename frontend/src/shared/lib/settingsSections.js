import { dispatchSettingGroups } from './dispatchPolicy.js';

export const sharedRuntimeSettingSections = [
  { category: 'request', title: '请求控制', description: '流式与非流式请求的等待时限', keys: ['global_timeout', 'response_header_timeout', 'default_response_header_timeout'] },
  { category: 'health', title: '健康检查', description: '默认方案内端点的连通性检测', keys: ['active_probes_enabled', 'check_interval', 'timeout', 'health_path'] },
  { category: 'retry', title: '调度保护', description: '请求尝试、故障冷却与规则回退', keys: dispatchSettingGroups.flatMap(group => group.keys) },
];

export const systemSettingCategories = ['server', 'auth', ...sharedRuntimeSettingSections.map(section => section.category), 'retention', 'logging', 'cache', 'backup'];

export function isSystemSetting(setting) {
  if ((setting.provider || 'global') !== 'global' || !systemSettingCategories.includes(setting.category)) return false;
  const section = sharedRuntimeSettingSections.find(section => section.category === setting.category);
  return !section || section.keys.includes(setting.key);
}

const claudeTokenSection = { category: 'token_counting', title: 'Claude Token 预估', description: 'Claude 请求的 Token 数量预估', keys: ['enabled', 'estimation_ratio'], provider: 'claude' };
export const runtimeSettingSections = provider => provider === 'claude' ? [claudeTokenSection] : [];

export function settingsForRuntimeSection(settings, section) {
  return settings.filter(setting => (setting.provider || 'global') === 'global' && setting.category === section.category && section.keys.includes(setting.key))
    .sort((a, b) => section.keys.indexOf(a.key) - section.keys.indexOf(b.key));
}
