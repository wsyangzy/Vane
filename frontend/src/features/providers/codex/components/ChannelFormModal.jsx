import { useState } from 'react';
import { ArrowRight, Pencil, Plus, Trash2, XCircle } from 'lucide-react';
import { Button, Input, SegmentedTabs, Tooltip } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import NonStreamRouteEditor from '@/features/providers/shared/routing/NonStreamRouteEditor.jsx';
import ChannelModelRoutesEditor from '@/features/providers/shared/routing/ChannelModelRoutesEditor.jsx';
import ChannelPrivacyLevelSelector from '@/features/providers/shared/privacy/ChannelPrivacyLevelSelector.jsx';
import { buildNonStreamRouteForm, nonStreamRouteFromForm, validateRouteTargets } from '@/features/providers/shared/routing/routeTargets.js';
import { buildModelMappingPayload } from '@/features/providers/shared/routing/modelMapping.js';
import { isWailsEnvironment } from '@/services/wails/index.js';
import { buildInitialChannelForm, CODEX_CHANNEL_FORM_TABS, CODEX_CHANNEL_EDIT_FORM_TABS } from '../model.js';

// ============================================================================
// 渠道编辑弹窗（凭证分组在渠道卡片中独立维护）
// ============================================================================
const ChannelFormModal = ({ mode, channel, channelFailoverEnabled, loading, serverError, onCancel, onSave }) => {
  const [formData, setFormData] = useState(() => buildInitialChannelForm(channel));
  const [nonStreamForm, setNonStreamForm] = useState(() => buildNonStreamRouteForm(channel?.nonStreamRoute));
  const [privacyLevel, setPrivacyLevel] = useState(() => channel?.privacyLevel || '');
  const [activeTab, setActiveTab] = useState('basic');
  const [fieldError, setFieldError] = useState('');
  const isWails = isWailsEnvironment();
  const isEdit = mode === 'edit';
  const channelId = Number(channel?.id) || 0;
  const canEditNonStream = isEdit && channelId > 0;
  const modalTopPadding = isWails ? 48 : 16;
  const modalBottomPadding = 16;
  const modalMaxHeight = `calc(100vh - ${modalTopPadding + modalBottomPadding}px)`;

  const updateField = (key, value) => {
    setFormData((prev) => ({ ...prev, [key]: value }));
    if (fieldError) {
      setFieldError('');
    }
  };

  const updateMappingRow = (index, key, value) => {
    setFormData((prev) => ({
      ...prev,
      modelMappingRows: (prev.modelMappingRows || []).map((row, rowIndex) =>
        rowIndex === index ? { ...row, [key]: value } : row
      )
    }));
    if (fieldError) {
      setFieldError('');
    }
  };

  const addMappingRow = () => {
    setFormData((prev) => ({
      ...prev,
      modelMappingRows: [
        ...(prev.modelMappingRows || []),
        { sourceModel: '', targetModel: '' }
      ]
    }));
  };

  const removeMappingRow = (index) => {
    setFormData((prev) => {
      const nextRows = (prev.modelMappingRows || []).filter((_, rowIndex) => rowIndex !== index);
      return {
        ...prev,
        modelMappingRows: nextRows.length > 0 ? nextRows : [{ sourceModel: '', targetModel: '' }]
      };
    });
  };

  const handleSubmit = async (event) => {
    event.preventDefault();

    const name = formData.name.trim();
    if (!name) {
      setActiveTab('basic');
      setFieldError('请填写渠道名');
      return;
    }

    const priorityValue = String(formData.priority ?? '').trim();
    const hasPriority = priorityValue !== '';
    const parsedPriority = Number(priorityValue);
    if ((isEdit && !hasPriority) || (hasPriority && (!Number.isInteger(parsedPriority) || parsedPriority < 0))) {
      setActiveTab('basic');
      setFieldError('优先级必须是 0 或更大的整数');
      return;
    }

    if (canEditNonStream && nonStreamForm.enabled) {
      const routeError = validateRouteTargets(nonStreamForm.targets);
      if (routeError) {
        setActiveTab('nonStream');
        setFieldError(routeError);
        return;
      }
    }

    const payload = {
      name,
      apiKey: '',
      website: formData.website.trim(),
      model: formData.model.trim(),
      modelMapping: buildModelMappingPayload(formData.modelMappingRows),
      priority: !isEdit && !hasPriority ? -1 : parsedPriority,
      failoverEnabled: formData.failoverEnabled,
      ...(canEditNonStream ? { nonStreamRoute: nonStreamRouteFromForm(nonStreamForm), privacyLevel } : {})
    };

    await onSave?.(payload);
  };

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center overflow-hidden bg-slate-900/30 p-4 backdrop-blur-sm animate-fade-in"
    >
      <div
        className="flex max-h-[calc(100vh-2rem)] w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl"
        style={{ maxHeight: modalMaxHeight }}
      >
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-6 py-4">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-slate-900 p-2 text-white shadow-sm">
              {isEdit ? <Pencil size={18} /> : <Plus size={18} />}
            </div>
            <div>
              <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑渠道' : '添加渠道'}</h2>
              <p className="mt-0.5 text-xs text-slate-500">渠道保存基础信息；API Key 在凭证分组中维护，模型/推理/1M 在「Codex 配置」中全局调整</p>
            </div>
          </div>
          <Tooltip content="关闭">
            <button
              type="button"
              onClick={onCancel}
              className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
              disabled={loading}
            >
              <XCircle size={18} />
            </button>
          </Tooltip>
        </div>

        <form className="flex min-h-0 flex-1 flex-col overflow-hidden" onSubmit={handleSubmit} autoComplete="off">
          <div className="flex-shrink-0 border-b border-slate-100 bg-slate-50/70 px-6 py-3">
            <SegmentedTabs
              tabs={canEditNonStream ? CODEX_CHANNEL_EDIT_FORM_TABS : CODEX_CHANNEL_FORM_TABS}
              activeTab={activeTab}
              onChange={setActiveTab}
              disabled={loading}
              className={canEditNonStream ? 'grid-cols-4' : 'grid-cols-2'}
              buttonPaddingClass="px-2"
            />
          </div>
          <div className="space-y-3.5 overflow-y-auto px-6 py-5">
            {(fieldError || serverError) && (
              <div className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
                {fieldError || serverError}
              </div>
            )}

            {activeTab === 'basic' ? (
              <>
            <div className="grid grid-cols-2 gap-4">
              <Input
                label="渠道名"
                value={formData.name}
                onChange={(event) => updateField('name', event.target.value)}
                placeholder="例如：主渠道"
                autoFocus
              />
              <Input
                label="官网地址（可选）"
                value={formData.website}
                onChange={(event) => updateField('website', event.target.value)}
                placeholder="https://platform.openai.com"
              />
            </div>

            {channelFailoverEnabled && isEdit && (
              <div className="block">
                <span className="mb-1.5 block text-sm font-medium text-slate-700">参与渠道间故障转移</span>
                <div className="flex h-[42px] min-w-0 items-center justify-between gap-4 rounded-lg border border-slate-200 px-3">
                  <span className="truncate text-xs text-slate-400">
                    {channel?.isActive ? '活跃渠道请在卡片中暂停' : '关闭后自动转移会跳过此渠道'}
                  </span>
                  <button
                    type="button"
                    role="switch"
                    aria-checked={formData.failoverEnabled}
                    onClick={() => updateField('failoverEnabled', !formData.failoverEnabled)}
                    disabled={Boolean(channel?.isActive)}
                    className={`relative inline-flex h-6 w-11 flex-shrink-0 rounded-full transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${formData.failoverEnabled ? 'bg-indigo-600' : 'bg-slate-200'}`}
                  >
                    <span className={`absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow-sm transition-transform ${formData.failoverEnabled ? 'translate-x-5' : 'translate-x-0'}`} />
                  </button>
                </div>
              </div>
            )}

            <div className={channelFailoverEnabled ? 'grid grid-cols-1 gap-4 sm:grid-cols-2' : 'grid grid-cols-1 gap-4'}>
              <div>
                <label className="mb-1.5 block text-sm font-medium text-slate-700">模型（可选）</label>
                <input
                  type="text"
                  value={formData.model}
                  onChange={(event) => updateField('model', event.target.value)}
                  placeholder="留空 = 使用 Codex 全局配置中的默认模型"
                  className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                />
                <p className="mt-1 text-xs text-slate-400">
                  填入后该渠道将覆盖全局 model；不同渠道支持的模型可能不同
                </p>
              </div>

              {channelFailoverEnabled && (
                <label className="block">
                  <span className="mb-1.5 block text-sm font-medium text-slate-700">优先级</span>
                  <input
                    type="number"
                    min="0"
                    step="1"
                    value={formData.priority}
                    onChange={(event) => updateField('priority', event.target.value)}
                    placeholder={isEdit ? undefined : '留空自动分配'}
                    className="h-[42px] w-full rounded-lg border border-slate-200 px-3 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  />
                  <p className="mt-1 text-xs text-slate-400">
                    {isEdit ? '数字越小越优先，相同则交换' : '留空按现有顺序自动分配；填写已有优先级时交换'}
                  </p>
                </label>
              )}
            </div>

            {canEditNonStream && (
              <ChannelPrivacyLevelSelector value={privacyLevel} onChange={setPrivacyLevel} disabled={loading} />
            )}
              </>
            ) : activeTab === 'mapping' ? (
              <div className="space-y-3">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <h3 className="text-sm font-semibold text-slate-900">模型映射</h3>
                    <p className="mt-0.5 text-xs text-slate-500">请求模型精确命中后替换为该渠道支持的模型。</p>
                  </div>
                  <button
                    type="button"
                    onClick={addMappingRow}
                    className="inline-flex items-center gap-1 rounded-lg border border-slate-200 px-3 py-1.5 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-50"
                  >
                    <Plus size={14} />
                    添加
                  </button>
                </div>

                <div className="space-y-2">
                  {(formData.modelMappingRows || []).map((row, index) => (
                    <div
                      key={`codex-model-mapping-${index}`}
                      className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_auto_1fr_auto] sm:items-center"
                    >
                      <input
                        type="text"
                        value={row.sourceModel}
                        onChange={(event) => updateMappingRow(index, 'sourceModel', event.target.value)}
                        placeholder="gpt-5.4-mini"
                        className="min-w-0 rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                      />
                      <ArrowRight size={16} className="text-slate-400" />
                      <input
                        type="text"
                        value={row.targetModel}
                        onChange={(event) => updateMappingRow(index, 'targetModel', event.target.value)}
                        placeholder="gpt-5-mini"
                        className="min-w-0 rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                      />
                      <Tooltip content="删除映射">
                        <button
                          type="button"
                          onClick={() => removeMappingRow(index)}
                          className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-rose-50 hover:text-rose-600"
                        >
                          <Trash2 size={16} />
                        </button>
                      </Tooltip>
                    </div>
                  ))}
                </div>
                <p className="text-xs text-slate-400">未命中映射时，仍按基础信息中的渠道模型覆盖规则处理。</p>
              </div>
            ) : activeTab === 'modelRoutes' && canEditNonStream ? (
              <ChannelModelRoutesEditor
                provider="codex"
                channelId={channelId}
                channelName={channel?.name || ''}
                disabled={loading}
              />
            ) : canEditNonStream ? (
              <NonStreamRouteEditor
                provider="codex"
                channelId={channelId}
                value={nonStreamForm}
                onChange={(next) => {
                  setNonStreamForm(next);
                  if (fieldError) setFieldError('');
                }}
                disabled={loading}
              />
            ) : null}
          </div>

          <div className="flex flex-shrink-0 items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/40 px-6 py-3">
            <Button variant="secondary" onClick={onCancel} disabled={loading}>
              取消
            </Button>
            <Button variant="primary" icon={isEdit ? Pencil : undefined} loading={loading} type="submit">
              {isEdit ? '保存修改' : '下一步'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};

export { ChannelFormModal };
