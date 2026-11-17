import { useEffect, useRef } from 'react';
import { ArrowLeft, ArrowRight, Check, Plus, X } from 'lucide-react';
import { Button } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import './channelSetup.css';

const CHANNEL_SETUP_STEPS = [
  { label: '渠道信息', title: '添加你的渠道', description: '先为渠道命名，方便在资源和调度方案中识别。' },
  { label: '凭证分组', title: '配置凭证分组', description: '为分组命名，并选择认证方式、填写访问凭证。' },
  { label: '成本倍率', title: '设置成本倍率', description: '配置这个分组的计费倍率与每日成本限额，可保留默认设置。' },
  { label: '端点配置', title: '连接到上游端点', description: '填写首个端点的名称和地址，按需调整请求设置。' },
  { label: '检测与确认', title: '确认配置，准备连接', description: '核对下方信息，确认后统一创建渠道、凭证分组和首个端点。' },
];

export default function ChannelSetupDialog({ provider, step, furthestStep, panels, formId, onStepChange, onNext, onDraftChange, onClose, onComplete, loading, error }) {
  const dialog = useRef(null);
  const heading = useRef(null);
  const steps = CHANNEL_SETUP_STEPS.map((item, index) => provider === 'claude' && index === 4 ? { ...item, label: '确认创建' } : item);
  const current = steps[step];
  useEffect(() => {
    const previous = document.activeElement;
    return () => { if (previous?.isConnected) previous.focus(); };
  }, []);
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      const fields = dialog.current?.querySelectorAll(`[data-setup-panel="${step}"] input:not([type="hidden"]), [data-setup-panel="${step}"] textarea`);
      const first = [...(fields || [])].find(element => !element.disabled && element.getClientRects().length);
      (first || heading.current)?.focus({ preventScroll: true });
      dialog.current?.querySelector('.channel-setup-content')?.scrollTo(0, 0);
    });
    return () => cancelAnimationFrame(frame);
  }, [step]);
  const handleKeyDown = (event) => {
    if (event.target.closest('[role="dialog"]') !== dialog.current) return;
    if (event.key === 'Escape' && !loading) { event.stopPropagation(); onClose(); }
    if (event.key !== 'Tab') return;
    const controls = [...dialog.current.querySelectorAll('button:not([disabled]), input:not([disabled]), textarea:not([disabled]), [tabindex="0"]')]
      .filter(element => element.getClientRects().length && !element.closest('[hidden]'));
    const first = controls[0], last = controls.at(-1);
    if (event.shiftKey && (document.activeElement === first || document.activeElement === heading.current)) { event.preventDefault(); last?.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
  };
  return renderModalPortal(<div className="channel-setup-overlay">
    <div ref={dialog} role="dialog" aria-modal="true" aria-labelledby="channel-setup-title" aria-describedby="channel-setup-description" data-confirm={step === steps.length - 1 ? 'true' : undefined} className="channel-setup-dialog" onKeyDown={handleKeyDown} onChangeCapture={onDraftChange}>
      <div className="channel-setup-caption"><span>添加 {provider === 'codex' ? 'Codex' : 'Claude'} 渠道</span><button type="button" aria-label="关闭添加渠道" onClick={onClose} disabled={loading}><X size={17} /></button></div>
      <nav aria-label="添加渠道步骤" className="channel-setup-progress">
        <p className="channel-setup-compact" aria-hidden="true">第 {step + 1} / {steps.length} 步 · {current.label}</p>
        <ol>{steps.map((item, index) => {
          const completed = index < furthestStep;
          const available = index <= furthestStep;
          const current = index === step;
          return <li key={item.label} data-state={current ? 'current' : completed ? 'complete' : available ? 'available' : 'upcoming'} aria-current={current ? 'step' : undefined}>
          <span className="channel-setup-line" aria-hidden="true" />
          <button type="button" disabled={loading || current || !available} onClick={() => onStepChange(index)} aria-label={`${current ? '' : index < step ? '返回' : '前往'}${item.label}${current ? '，当前步骤' : completed ? '，已完成' : available ? '，已访问' : '，尚未完成'}`}>
            <span className="channel-setup-step-number" aria-hidden="true">{completed && !current ? <Check size={14} strokeWidth={2.5} /> : index + 1}</span><span>{item.label}</span>
          </button>
        </li>;
        })}</ol>
      </nav>
      <div className="channel-setup-heading"><h2 ref={heading} tabIndex={-1} id="channel-setup-title">{current.title}</h2><p id="channel-setup-description">{current.description}</p></div>
      <div className="channel-setup-content">
        {error && <p role="alert" className="channel-setup-error">{error}</p>}
        {panels.map(({ steps: panelSteps, content }) => {
          const active = panelSteps.includes(step);
          // Credentials and billing share one mounted form so raw drafts survive both steps.
          return <section key={panelSteps[0]} hidden={!active} data-setup-panel={active ? step : panelSteps[0]} aria-label={steps[active ? step : panelSteps[0]].label}>{content}</section>;
        })}
      </div>
      <div className="channel-setup-footer">
        {step > 0 ? <Button type="button" variant="secondary" icon={ArrowLeft} disabled={loading} onClick={() => onStepChange(step - 1)}>上一步</Button> : <span />}
        {step === steps.length - 1 && <p className="channel-setup-footer-note">创建后可在渠道卡片中继续添加分组与端点，并在调度方案中设置参与方式。</p>}
        {step < steps.length - 1
          ? <Button type="submit" form={formId} onClick={event => { event.preventDefault(); onNext(); }} icon={ArrowRight} disabled={loading} className="channel-setup-next">下一步</Button>
          : <Button type="button" icon={Plus} loading={loading} onClick={onComplete} className="channel-setup-next">创建渠道</Button>}
      </div>
      <p className="sr-only" role="status" aria-live="polite">第 {step + 1} 步，共 {steps.length} 步：{current.label}</p>
    </div>
  </div>);
}
