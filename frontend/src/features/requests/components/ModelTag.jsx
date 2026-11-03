// ============================================
// ModelTag - 模型标签组件（带颜色区分）
// 2025-12-01 09:30:08
// ============================================

import { GitBranch, Layers, Replace, Route, Sparkles, Split } from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';
import { describeRouteSnapshot } from '@/features/providers/shared/routing/routeTargets.js';

const BRAND_ICON_CLASS = 'h-3 w-3 shrink-0';
const CLAUDE_ICON_CLASS = 'h-3.5 w-3.5 shrink-0';

const OpenAIIcon = ({ className = '' }) => (
  <svg
    viewBox="0 0 24 24"
    fill="currentColor"
    aria-hidden="true"
    className={className}
  >
    <path
      d="M22.28 9.82a5.98 5.98 0 0 0-.52-4.91 6.05 6.05 0 0 0-6.5-2.9A5.98 5.98 0 0 0 10.74 0a6.05 6.05 0 0 0-5.73 4.18 5.98 5.98 0 0 0-4 2.9 6.05 6.05 0 0 0 .74 7.1 5.98 5.98 0 0 0 .51 4.91 6.05 6.05 0 0 0 6.52 2.9A5.98 5.98 0 0 0 13.26 24a6.06 6.06 0 0 0 5.77-4.21 5.99 5.99 0 0 0 4-2.9 6.06 6.06 0 0 0-.75-7.07Zm-9.02 12.61a4.48 4.48 0 0 1-2.88-1.04l.14-.08 4.78-2.76a.8.8 0 0 0 .4-.68v-6.74l2.02 1.17a.07.07 0 0 1 .04.05v5.58a4.5 4.5 0 0 1-4.5 4.5Zm-9.66-4.13a4.47 4.47 0 0 1-.54-3.01l.14.08 4.78 2.76a.77.77 0 0 0 .79 0l5.84-3.37v2.33a.08.08 0 0 1-.03.07l-4.84 2.79a4.5 4.5 0 0 1-6.14-1.65ZM2.34 7.9A4.48 4.48 0 0 1 4.7 5.92v5.68a.77.77 0 0 0 .39.68l5.82 3.35-2.02 1.17a.08.08 0 0 1-.07 0l-4.83-2.79A4.5 4.5 0 0 1 2.34 7.9Zm16.6 3.85-5.84-3.39 2.02-1.16a.08.08 0 0 1 .07 0l4.83 2.79a4.49 4.49 0 0 1-.68 8.1v-5.67a.79.79 0 0 0-.4-.67Zm2.01-3.02-.14-.09-4.78-2.78a.78.78 0 0 0-.78 0L9.41 9.23V6.9a.07.07 0 0 1 .03-.06l4.83-2.79a4.5 4.5 0 0 1 6.68 4.68ZM8.31 12.86 6.29 11.7a.08.08 0 0 1-.04-.07V6.07a4.5 4.5 0 0 1 7.37-3.45l-.14.08-4.77 2.76a.8.8 0 0 0-.4.68v6.72Zm1.1-2.36 2.6-1.5 2.6 1.5v3l-2.6 1.5-2.6-1.5v-3Z"
    />
  </svg>
);

export const ClaudeIcon = ({ className = '' }) => (
  <svg
    viewBox="0 0 24 24"
    fill="currentColor"
    aria-hidden="true"
    className={className}
  >
    <path
      d="m4.7144 15.9555 4.7174-2.6471.079-.2307-.079-.1275h-.2307l-.7893-.0486-2.6956-.0729-2.3375-.0971-2.2646-.1214-.5707-.1215-.5343-.7042.0546-.3522.4797-.3218.686.0608 1.5179.1032 2.2767.1578 1.6514.0972 2.4468.255h.3886l.0546-.1579-.1336-.0971-.1032-.0972L6.973 9.8356l-2.55-1.6879-1.3356-.9714-.7225-.4918-.3643-.4614-.1578-1.0078.6557-.7225.8803.0607.2246.0607.8925.686 1.9064 1.4754 2.4893 1.8336.3643.3035.1457-.1032.0182-.0728-.164-.2733-1.3539-2.4467-1.445-2.4893-.6435-1.032-.17-.6194c-.0607-.255-.1032-.4674-.1032-.7285L6.287.1335 6.6997 0l.9957.1336.419.3642.6192 1.4147 1.0018 2.2282 1.5543 3.0296.4553.8985.2429.8318.091.255h.1579v-.1457l.1275-1.706.2368-2.0947.2307-2.6957.0789-.7589.3764-.9107.7468-.4918.5828.2793.4797.686-.0668.4433-.2853 1.8517-.5586 2.9021-.3643 1.9429h.2125l.2429-.2429.9835-1.3053 1.6514-2.0643.7286-.8196.85-.9046.5464-.4311h1.0321l.759 1.1293-.34 1.1657-1.0625 1.3478-.8804 1.1414-1.2628 1.7-.7893 1.36.0729.1093.1882-.0183 2.8535-.607 1.5421-.2794 1.8396-.3157.8318.3886.091.3946-.3278.8075-1.967.4857-2.3072.4614-3.4364.8136-.0425.0304.0486.0607 1.5482.1457.6618.0364h1.621l3.0175.2247.7892.522.4736.6376-.079.4857-1.2142.6193-1.6393-.3886-3.825-.9107-1.3113-.3279h-.1822v.1093l1.0929 1.0686 2.0035 1.8092 2.5075 2.3314.1275.5768-.3218.4554-.34-.0486-2.2039-1.6575-.85-.7468-1.9246-1.621h-.1275v.17l.4432.6496 2.3436 3.5214.1214 1.0807-.17.3521-.6071.2125-.6679-.1214-1.3721-1.9246L14.38 17.959l-1.1414-1.9428-.1397.079-.674 7.2552-.3156.3703-.7286.2793-.6071-.4614-.3218-.7468.3218-1.4753.3886-1.9246.3157-1.53.2853-1.9004.17-.6314-.0121-.0425-.1397.0182-1.4328 1.9672-2.1796 2.9446-1.7243 1.8456-.4128.164-.7164-.3704.0667-.6618.4008-.5889 2.386-3.0357 1.4389-1.882.929-1.0868-.0062-.1579h-.0546l-6.3385 4.1164-1.1293.1457-.4857-.4554.0608-.7467.2307-.2429 1.9064-1.3114Z"
    />
  </svg>
);

const GenericModelIcon = ({ className = '' }) => (
  <span
    aria-hidden="true"
    className={`${className} flex items-center justify-center`}
  >
    <span className="h-1.5 w-1.5 rounded-full bg-current" />
  </span>
);

const MODEL_ICONS = {
  openai: OpenAIIcon,
  claude: ClaudeIcon,
  generic: GenericModelIcon
};

const createModelStyle = (classes, icon = 'generic') => ({
  classes,
  icon
});

const CLAUDE_TAG_CLASSES = {
  default: 'bg-[#FFF8DD] text-[#806A00] border-[#F3DEAA]',
  haiku: 'bg-[#DFF6F8] text-[#0F5F6A] border-[#BCEBF0]',
  fable: 'bg-[#FEE2E2] text-[#B91C1C] border-[#FECACA]',
  sonnet: 'bg-[#FFF3E6] text-[#9A4D00] border-[#FED7AA]',
  opus: 'bg-[#F3E8FF] text-[#6D28D9] border-[#DDD6FE]'
};

const GPT5_MINOR_COLOR_MAP = {
  3: createModelStyle('bg-[#E8FAFF] text-cyan-900 border-[#A5F3FC]', 'openai'),
  4: createModelStyle('bg-[#E0F7FA] text-[#006B78] border-[#7DD3FC]', 'openai'),
  5: createModelStyle('bg-[#EFF6FF] text-[#0369A1] border-[#93C5FD]', 'openai'),
  6: createModelStyle('bg-[#EEF2FF] text-[#1E3A8A] border-[#818CF8]', 'openai')
};

const GPT5_CODEX_STYLE = createModelStyle('bg-[#F5F3FF] text-[#4338CA] border-[#A5B4FC]', 'openai');

const GPT5_FALLBACK_COLORS = [
  createModelStyle('bg-[#27408B]/15 text-[#172554] border-[#27408B]/45', 'openai'),
  createModelStyle('bg-[#191970]/15 text-[#111827] border-[#191970]/45', 'openai'),
  createModelStyle('bg-[#000080]/15 text-[#0F172A] border-[#000080]/45', 'openai')
];

const CLAUDE_OPUS4_COLOR_MAP = {
  1: createModelStyle(CLAUDE_TAG_CLASSES.opus, 'claude'),
  5: createModelStyle(CLAUDE_TAG_CLASSES.opus, 'claude'),
  6: createModelStyle(CLAUDE_TAG_CLASSES.opus, 'claude'),
  7: createModelStyle(CLAUDE_TAG_CLASSES.opus, 'claude'),
  8: createModelStyle(CLAUDE_TAG_CLASSES.opus, 'claude')
};

// 同一次版本下的代号变体（gpt-5.6-sol / -terra / -luna）此前共用 minor 配色而无法区分。
// 这里在 minor 配色之上按代号分色：保持靛蓝同色系以体现同属 5.6，仅拉开色阶与冷暖。
const GPT5_CODENAME_COLOR_MAP = {
  sol: createModelStyle('bg-[#FFF1E7] text-[#9A3412] border-[#FDBA74]', 'openai'),
  terra: createModelStyle('bg-[#ECFDF5] text-[#065F46] border-[#6EE7B7]', 'openai'),
  luna: createModelStyle('bg-[#EEF2FF] text-[#3730A3] border-[#A5B4FC]', 'openai')
};

const GPT6_CODENAME_COLOR_MAP = {
  astra: createModelStyle('bg-[#FFF1F2] text-[#9F1239] border-[#FDA4AF]', 'openai')
};

const getGpt6ColorClasses = (lowerName) => {
  const match = lowerName.match(/\bgpt[-.]?6(?:[.-]([a-z0-9]+))?\b/);
  if (!match) return null;

  const codename = Object.keys(GPT6_CODENAME_COLOR_MAP)
    .find((name) => lowerName.includes(name));
  return codename
    ? GPT6_CODENAME_COLOR_MAP[codename]
    : createModelStyle('bg-[#F0F9FF] text-[#075985] border-[#7DD3FC]', 'openai');
};

const getGpt5ColorClasses = (lowerName) => {
  const match = lowerName.match(/\bgpt[-.]?5(?:[.-](\d+))?\b/);
  if (!match) return null;

  if (lowerName.includes('codex')) {
    return GPT5_CODEX_STYLE;
  }

  const codename = Object.keys(GPT5_CODENAME_COLOR_MAP)
    .find((name) => lowerName.includes(name));
  if (codename) {
    return GPT5_CODENAME_COLOR_MAP[codename];
  }

  const minorVersion = match[1] ? Number(match[1]) : null;
  if (minorVersion === null) {
    return createModelStyle('bg-slate-900 text-white border-slate-900', 'openai');
  }

  if (GPT5_MINOR_COLOR_MAP[minorVersion]) {
    return GPT5_MINOR_COLOR_MAP[minorVersion];
  }

  const fallbackIndex = Math.min(
    Math.max(minorVersion - 7, 0),
    GPT5_FALLBACK_COLORS.length - 1
  );
  return GPT5_FALLBACK_COLORS[fallbackIndex];
};

const getClaudeColorClasses = (lowerName) => {
  const isClaude = lowerName.includes('claude')
    || lowerName.includes('sonnet')
    || lowerName.includes('haiku')
    || lowerName.includes('opus');

  if (!isClaude) return null;

  if (lowerName.includes('fable')) {
    return createModelStyle(CLAUDE_TAG_CLASSES.fable, 'claude');
  }

  if (lowerName.includes('haiku')) {
    return createModelStyle(CLAUDE_TAG_CLASSES.haiku, 'claude');
  }

  const opus4Match = lowerName.match(/opus[-.]4[-.](\d+)/);
  if (opus4Match) {
    const opusMinor = Number(opus4Match[1]);
    if (CLAUDE_OPUS4_COLOR_MAP[opusMinor]) {
      return CLAUDE_OPUS4_COLOR_MAP[opusMinor];
    }
  }

  // Claude Sonnet 4 系列 - 温暖橙色
  if (lowerName.includes('sonnet-4') || lowerName.includes('claude-sonnet-4')) {
    return createModelStyle(CLAUDE_TAG_CLASSES.sonnet, 'claude');
  }

  // Claude 3.5 Sonnet 系列 - 优雅蓝色
  if (lowerName.includes('3-5-sonnet') || (lowerName.includes('sonnet') && lowerName.includes('3.5'))) {
    return createModelStyle(CLAUDE_TAG_CLASSES.sonnet, 'claude');
  }

  // Claude Opus 系列 - 高贵紫色
  if (lowerName.includes('opus')) {
    return createModelStyle(CLAUDE_TAG_CLASSES.opus, 'claude');
  }

  return createModelStyle(CLAUDE_TAG_CLASSES.default, 'claude');
};

/**
 * 根据模型名称获取对应的颜色类名
 */
const getModelStyle = (modelName) => {
  if (!modelName || modelName === 'unknown' || modelName === '-') {
    return createModelStyle('bg-slate-50 text-slate-600 border-slate-200');
  }

  const lowerName = modelName.toLowerCase();

  const claudeColorClasses = getClaudeColorClasses(lowerName);
  if (claudeColorClasses) {
    return claudeColorClasses;
  }

  if (lowerName.includes('mini')) {
    return createModelStyle('bg-[#B4EEB4]/45 text-green-900 border-[#B4EEB4]/80', 'openai');
  }

  const gpt6ColorClasses = getGpt6ColorClasses(lowerName);
  if (gpt6ColorClasses) {
    return gpt6ColorClasses;
  }

  const gpt5ColorClasses = getGpt5ColorClasses(lowerName);
  if (gpt5ColorClasses) {
    return gpt5ColorClasses;
  }

  if (lowerName.includes('codex')) {
    return createModelStyle('bg-violet-50 text-violet-800 border-violet-200', 'openai');
  }

  // GPT-4 系列
  if (lowerName.includes('gpt-4o')) {
    return createModelStyle('bg-teal-50 text-teal-700 border-teal-200', 'openai');
  }
  if (lowerName.includes('gpt-4.1')) {
    return createModelStyle('bg-emerald-50 text-emerald-700 border-emerald-200', 'openai');
  }
  if (lowerName.includes('gpt-4')) {
    return createModelStyle('bg-sky-50 text-sky-700 border-sky-200', 'openai');
  }
  // o 系列推理模型
  if (lowerName.includes('o3') || lowerName.includes('o4')) {
    return createModelStyle('bg-violet-50 text-violet-700 border-violet-200', 'openai');
  }

  // 其他未知模型 - 中性灰色
  return createModelStyle('bg-slate-50 text-slate-600 border-slate-200');
};

const getRewriteIndicator = (source) => {
  switch (source) {
    case 'mapping':
      return {
        label: '模型映射',
        classes: 'text-orange-500'
      };
    case 'channel_model':
      return {
        label: '渠道模型覆盖',
        classes: 'text-violet-500'
      };
    case 'route':
      return {
        label: '路由改写',
        classes: 'text-sky-500'
      };
    default:
      return null;
  }
};

/**
 * ModelTag - 显示模型名称的标签组件
 * @param {Object} props
 * @param {string} props.model - 模型名称
 */
const ModelTag = ({
  model,
  className = '',
  title,
  rewriteSource = '',
  originalModel = '',
  rewriteTarget = '',
  routeSnapshot,
  routePath,
  compact = false
}) => {
  const modelStyle = getModelStyle(model);
  const BrandIcon = MODEL_ICONS[modelStyle.icon] || GenericModelIcon;
  // Segoe UI's g/p descenders put GPT names below the line-box center.
  // Lift the text's visible center while keeping the icon centered in the tag.
  const nameAlignment = modelStyle.icon === 'openai'
    ? (compact ? '-translate-y-[1.5px]' : '-translate-y-px')
    : '';
  const displayName = (!model || model === 'unknown') ? '-' : model;
  const rewriteIndicator = routePath === '' ? null : getRewriteIndicator(rewriteSource);
  const rewriteTitle = rewriteIndicator
    ? `${originalModel || '原模型未记录'} → ${rewriteTarget || displayName}`
    : '';
  const route = routePath === '' ? null : describeRouteSnapshot(routeSnapshot);
  const routeIndicator = route?.kind === 'model_rule'
    ? { label: '模型路由', Icon: Route, classes: 'text-sky-600' }
    : route?.kind === 'non_stream'
      ? { label: '非流改道', Icon: Split, classes: 'text-teal-600' }
      : route?.kind === 'mapping'
        ? { label: '模型映射', Icon: Replace, classes: 'text-orange-500' }
        : route?.kind === 'composite'
          ? { label: '条件路由', Icon: GitBranch, classes: 'text-sky-600' }
          : route?.kind === 'intent'
            ? { label: '意图路由', Icon: Sparkles, classes: 'text-violet-500' }
            : route?.kind === 'pool' ? { label: '账号池', Icon: Layers, classes: 'text-slate-500' } : null;
  const RouteIcon = routeIndicator?.Icon;
  const tag = (
    <span
      className={`model-tag inline-flex h-[26px] items-center justify-center whitespace-nowrap rounded-md border py-0 align-middle font-normal leading-4 shadow-[inset_0_1px_0_rgba(255,255,255,0.8)] transition-colors ${compact ? 'min-w-0 max-w-full gap-1 px-1 text-[11px]' : 'w-fit max-w-none gap-1 px-2 text-xs'} ${modelStyle.classes} ${className}`}
    >
      <BrandIcon className={modelStyle.icon === 'claude' ? CLAUDE_ICON_CLASS : BRAND_ICON_CLASS} />
      <span className={`model-tag-name block leading-4 ${nameAlignment} ${compact ? 'min-w-0 overflow-hidden text-ellipsis' : ''}`}>{displayName}</span>
      {rewriteIndicator && !(rewriteSource === 'route' && routeIndicator) && !(rewriteSource === 'mapping' && route?.kind === 'mapping') && (
        <Replace
          className={`h-3 w-3 shrink-0 -scale-x-100 stroke-[2.4] ${rewriteIndicator.classes}`}
          aria-label={rewriteIndicator.label}
        />
      )}
      {routeIndicator && <RouteIcon className={`h-3 w-3 shrink-0 stroke-[2.4] ${routeIndicator.classes}`} aria-label={routeIndicator.label} />}
    </span>
  );

  return (
    <Tooltip className={compact ? 'min-w-0 max-w-full' : ''} contentClassName={routePath ? 'whitespace-pre-line text-left' : ''} content={title || routePath || rewriteTitle || displayName}>
      {tag}
    </Tooltip>
  );
};

export default ModelTag;
