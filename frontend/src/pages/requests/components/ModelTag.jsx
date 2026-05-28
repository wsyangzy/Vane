// ============================================
// ModelTag - 模型标签组件（带颜色区分）
// 2025-12-01 09:30:08
// ============================================

/**
 * 根据模型名称获取对应的颜色类名
 */
const getModelColorClasses = (modelName) => {
  if (!modelName || modelName === 'unknown' || modelName === '-') {
    return 'bg-slate-50 text-slate-600 border-slate-200';
  }

  const lowerName = modelName.toLowerCase();

  // Claude Sonnet 4 系列 - 温暖橙色
  if (lowerName.includes('sonnet-4') || lowerName.includes('claude-sonnet-4')) {
    return 'bg-orange-50 text-orange-700 border-orange-200';
  }

  // Claude 3.5 Haiku 系列 - 清新绿色
  if (lowerName.includes('3-5-haiku') || lowerName.includes('haiku')) {
    return 'bg-green-50 text-green-800 border-green-200';
  }

  // Claude 3.5 Sonnet 系列 - 优雅蓝色
  if (lowerName.includes('3-5-sonnet') || (lowerName.includes('sonnet') && lowerName.includes('3.5'))) {
    return 'bg-blue-50 text-blue-700 border-blue-100';
  }

  // Claude Opus 系列 - 高贵紫色
  if (lowerName.includes('opus')) {
    return 'bg-purple-50 text-purple-700 border-purple-200';
  }

  // GPT-5 系列
  if (lowerName.includes('gpt-5.5')) {
    return 'bg-rose-50 text-rose-700 border-rose-200';
  }
  if (lowerName.includes('gpt-5.4')) {
    return 'bg-pink-50 text-pink-700 border-pink-200';
  }
  if (lowerName.includes('gpt-5.3') || lowerName.includes('codex')) {
    return 'bg-cyan-50 text-cyan-700 border-cyan-200';
  }
  if (lowerName.includes('gpt-5')) {
    return 'bg-fuchsia-50 text-fuchsia-700 border-fuchsia-200';
  }
  // GPT-4 系列
  if (lowerName.includes('gpt-4o')) {
    return 'bg-teal-50 text-teal-700 border-teal-200';
  }
  if (lowerName.includes('gpt-4.1')) {
    return 'bg-emerald-50 text-emerald-700 border-emerald-200';
  }
  if (lowerName.includes('gpt-4')) {
    return 'bg-sky-50 text-sky-700 border-sky-200';
  }
  // o 系列推理模型
  if (lowerName.includes('o3') || lowerName.includes('o4')) {
    return 'bg-violet-50 text-violet-700 border-violet-200';
  }

  // 其他未知模型 - 中性灰色
  return 'bg-slate-50 text-slate-600 border-slate-200';
};

/**
 * ModelTag - 显示模型名称的标签组件
 * @param {Object} props
 * @param {string} props.model - 模型名称
 */
const ModelTag = ({ model, className = '', title }) => {
  const colorClasses = getModelColorClasses(model);
  const displayName = (!model || model === 'unknown') ? '-' : model;

  return (
    <span
      className={`px-2 py-1 rounded text-xs font-mono border transition-all ${colorClasses} ${className}`}
      title={title || displayName}
    >
      {displayName}
    </span>
  );
};

export default ModelTag;
