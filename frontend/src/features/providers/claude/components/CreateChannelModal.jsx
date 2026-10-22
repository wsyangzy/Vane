import { useState } from 'react';
import { Plus, Trash2, ArrowRight, CheckCircle2, XCircle } from 'lucide-react';
import { Button, SegmentedTabs, Tooltip } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { buildInitialModelMappingRows, buildModelMappingPayload } from '@/features/providers/shared/routing/modelMapping.js';
import NonStreamRouteEditor from '@/features/providers/shared/routing/NonStreamRouteEditor.jsx';
import ChannelModelRoutesEditor from '@/features/providers/shared/routing/ChannelModelRoutesEditor.jsx';
import ChannelPrivacyLevelSelector from '@/features/providers/shared/privacy/ChannelPrivacyLevelSelector.jsx';
import ChannelFeatureSettings from '@/features/providers/shared/routing/ChannelFeatureSettings.jsx';
import { useChannelFeatureTabs } from '@/features/providers/shared/routing/useChannelFeatureTabs.js';
import { buildNonStreamRouteForm, nonStreamRouteFromForm, validateRouteTargets } from '@/features/providers/shared/routing/routeTargets.js';
import { CHANNEL_FORM_TABS, CHANNEL_EDIT_FORM_TABS } from '../model.js';

// ============================================
// 新建渠道弹窗（SQLite 模式）
// ============================================

const CreateChannelModal = ({
  open,
  onClose,
  onSubmit,
  loading = false,
  serverError = '',
  mode = 'create',
  initialValue = null,
  channelFailoverEnabled = false
}) => {
  const isEdit = mode === 'edit';
  const showPriority = isEdit || channelFailoverEnabled;
  const channelId = Number(initialValue?.id) || 0;
  const canEditNonStream = isEdit && channelId > 0;
  const [name, setName] = useState(() => (isEdit ? (initialValue?.name || '') : ''));
  const [website, setWebsite] = useState(() => (isEdit ? (initialValue?.website || '') : ''));
  const [priority, setPriority] = useState(() => (isEdit ? String(initialValue?.priority ?? 1) : ''));
  const [modelMappingRows, setModelMappingRows] = useState(() => buildInitialModelMappingRows(initialValue?.modelMapping));
  const [nonStreamForm, setNonStreamForm] = useState(() => buildNonStreamRouteForm(initialValue?.nonStreamRoute));
  const [privacyLevel, setPrivacyLevel] = useState(() => initialValue?.privacyLevel || '');
  const [activeTab, setActiveTab] = useState('basic');
  const [error, setError] = useState('');
  const { features, toggleFeature, visibleTabs } = useChannelFeatureTabs({
    provider: 'claude', channelId, modelMapping: initialValue?.modelMapping, nonStreamRoute: initialValue?.nonStreamRoute,
    tabs: canEditNonStream ? CHANNEL_EDIT_FORM_TABS : CHANNEL_FORM_TABS
  });

  if (!open) return null;

  const updateMappingRow = (index, key, value) => {
    setModelMappingRows((prev) =>
      (prev || []).map((row, rowIndex) =>
        rowIndex === index ? { ...row, [key]: value } : row
      )
    );
    if (error) {
      setError('');
    }
  };

  const addMappingRow = () => {
    setModelMappingRows((prev) => [
      ...(prev || []),
      { sourceModel: '', targetModel: '' }
    ]);
  };

  const removeMappingRow = (index) => {
    setModelMappingRows((prev) => {
      const nextRows = (prev || []).filter((_, rowIndex) => rowIndex !== index);
      return nextRows.length > 0 ? nextRows : [{ sourceModel: '', targetModel: '' }];
    });
  };

  return renderModalPortal(
    <div className="fixed inset-0 bg-slate-900/30 backdrop-blur-sm flex items-center justify-center p-4 z-[60]">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-2xl max-h-[calc(100vh-2rem)] flex flex-col overflow-hidden">
        <div className="flex items-start justify-between px-6 py-4 border-b border-slate-100">
          <div>
            <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑渠道' : '新建渠道'}</h2>
            <p className="text-xs text-slate-500 mt-1">
              {isEdit ? '修改渠道官网与优先级' : '创建渠道后继续配置凭证分组与端点'}
            </p>
          </div>
          <Tooltip content="关闭">
            <button
              onClick={onClose}
              className="p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700 rounded-lg transition-colors"
              disabled={loading}
              aria-label="关闭"
            >
              <XCircle size={18} />
            </button>
          </Tooltip>
        </div>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            const trimmedName = name.trim();
            if (!trimmedName) {
              setActiveTab('basic');
              setError('请输入渠道名称');
              return;
            }
            const priorityValue = priority.trim();
            const hasPriority = priorityValue !== '';
            const parsedPriority = Number(priorityValue);
            if ((isEdit && !hasPriority) || (hasPriority && (!Number.isInteger(parsedPriority) || parsedPriority < 1))) {
              setActiveTab('basic');
              setError('优先级必须是 1 或更大的整数');
              return;
            }
            if (canEditNonStream && nonStreamForm.enabled) {
              const routeError = validateRouteTargets(nonStreamForm.targets);
              if (routeError) {
                if (!features.nonStream) toggleFeature('nonStream');
                setActiveTab('nonStream');
                setError(routeError);
                return;
              }
            }
            setError('');
            onSubmit?.({
              name: trimmedName,
              website: website.trim(),
              priority: priorityValue,
              modelMapping: buildModelMappingPayload(modelMappingRows),
              ...(canEditNonStream ? { nonStreamRoute: nonStreamRouteFromForm(nonStreamForm), privacyLevel } : {})
            });
          }}
          className="flex flex-1 flex-col overflow-hidden"
          autoComplete="off"
        >
          <div className="border-b border-slate-100 bg-slate-50/70 px-6 py-3">
            <SegmentedTabs
              tabs={visibleTabs}
              activeTab={activeTab}
              onChange={setActiveTab}
              disabled={loading}
              className="grid-flow-col auto-cols-fr"
            />
          </div>

          <div className="flex-1 overflow-y-auto p-6">
            {(error || serverError) && (
              <div className="mb-4 p-3 bg-rose-50 border border-rose-200 rounded-lg text-rose-700 text-sm">
                {error || serverError}
              </div>
            )}

            {activeTab === 'basic' && (
          <div className="space-y-4">
            <div className={showPriority ? 'grid grid-cols-1 gap-4 sm:grid-cols-3' : ''}>
              <div className={showPriority ? 'sm:col-span-2' : ''}>
                <label className="block text-sm font-medium text-slate-700">
                  渠道名称 <span className="text-rose-500">*</span>
                </label>
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="例如：official / backup / openai"
                  autoComplete="off"
                  data-form-type="other"
                  className={`mt-1 w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 ${
                    isEdit ? 'disabled:bg-slate-100 disabled:text-slate-500 disabled:cursor-not-allowed' : ''
                  }`}
                  disabled={loading || isEdit}
                />
                {isEdit && (
                  <p className="text-xs text-slate-400 mt-1">渠道名称用于关联端点，不支持修改</p>
                )}
              </div>

              {showPriority && (
                <div>
                  <label className="block text-sm font-medium text-slate-700">优先级</label>
                  <input
                    type="number"
                    min={1}
                    step={1}
                    value={priority}
                    onChange={(e) => setPriority(e.target.value)}
                    placeholder={isEdit ? undefined : '留空自动分配'}
                    autoComplete="off"
                    data-form-type="other"
                    className="mt-1 w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500"
                    disabled={loading}
                  />
                </div>
              )}
            </div>

            <div>
              <label className="block text-sm font-medium text-slate-700">渠道官网</label>
              <input
                value={website}
                onChange={(e) => setWebsite(e.target.value)}
                placeholder="https://example.com (可选)"
                autoComplete="off"
                data-form-type="other"
                className="mt-1 w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500"
                disabled={loading}
              />
              <p className="text-xs text-slate-400 mt-1">用于渠道信息展示，非必填</p>
            </div>

            {canEditNonStream && (
              <ChannelPrivacyLevelSelector value={privacyLevel} onChange={setPrivacyLevel} disabled={loading} />
            )}
            <ChannelFeatureSettings features={features} onToggle={toggleFeature} canEditRoutes={canEditNonStream} disabled={loading} />

          </div>
            )}

            {activeTab === 'mapping' && (
            <div className="rounded-xl border border-slate-200 bg-slate-50/60 p-3">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <label className="block text-sm font-medium text-slate-700">模型映射</label>
                  <p className="mt-0.5 text-xs text-slate-400">请求模型精确命中后替换为该渠道支持的模型。</p>
                </div>
                <button
                  type="button"
                  onClick={addMappingRow}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-xs font-medium text-slate-600 transition-colors hover:border-indigo-200 hover:bg-indigo-50 hover:text-indigo-700"
                  disabled={loading}
                >
                  <Plus size={14} />
                  添加
                </button>
              </div>

              <div className="mt-3 space-y-2">
                {(modelMappingRows || []).map((row, index) => (
                  <div
                    key={`channel-model-mapping-${index}`}
                    className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_auto_1fr_auto] sm:items-center"
                  >
                    <input
                      type="text"
                      value={row.sourceModel}
                      onChange={(event) => updateMappingRow(index, 'sourceModel', event.target.value)}
                      placeholder="claude-3-5-haiku-20241022"
                      autoComplete="off"
                      data-form-type="other"
                      className="min-w-0 rounded-lg border border-slate-200 bg-white px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                      disabled={loading}
                    />
                    <ArrowRight size={16} className="text-slate-400" />
                    <input
                      type="text"
                      value={row.targetModel}
                      onChange={(event) => updateMappingRow(index, 'targetModel', event.target.value)}
                      placeholder="claude-sonnet-4-20250514"
                      autoComplete="off"
                      data-form-type="other"
                      className="min-w-0 rounded-lg border border-slate-200 bg-white px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                      disabled={loading}
                    />
                    <Tooltip content="删除映射">
                      <button
                        type="button"
                        onClick={() => removeMappingRow(index)}
                        className="rounded-lg bg-white p-2 text-slate-400 transition-colors hover:bg-rose-50 hover:text-rose-600"
                        disabled={loading}
                      >
                        <Trash2 size={16} />
                      </button>
                    </Tooltip>
                  </div>
                ))}
              </div>
            </div>
            )}

            {activeTab === 'modelRoutes' && canEditNonStream && (
              <ChannelModelRoutesEditor
                provider="claude"
                channelId={channelId}
                channelName={initialValue?.name || ''}
                disabled={loading}
              />
            )}

            {activeTab === 'nonStream' && canEditNonStream && (
              <NonStreamRouteEditor
                provider="claude"
                channelId={channelId}
                value={nonStreamForm}
                onChange={(next) => {
                  setNonStreamForm(next);
                  if (error) setError('');
                }}
                disabled={loading}
              />
            )}
          </div>

          <div className="flex items-center justify-end gap-2 border-t border-slate-100 bg-white px-6 py-4">
            <Button variant="ghost" type="button" onClick={onClose} disabled={loading}>
              取消
            </Button>
            <Button icon={isEdit ? CheckCircle2 : undefined} type="submit" loading={loading}>
              {isEdit ? '保存修改' : '下一步'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};

export { CreateChannelModal };
