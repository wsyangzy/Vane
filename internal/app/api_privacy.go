package app

import (
	"context"
	"fmt"
	"time"

	"github.com/wsyangzy/Vane/internal/privacy"
)

func (a *App) errPrivacyServiceDisabled() error {
	return fmt.Errorf("隐私防护服务未启用（管理数据库未就绪）。请稍等重试；若一直失败，请检查是否有另一个 Vane 实例占用数据库或重启应用。")
}

func (a *App) privacySvc() (*privacy.Service, error) {
	a.mu.RLock()
	svc := a.privacyService
	a.mu.RUnlock()
	if svc == nil {
		return nil, a.errPrivacyServiceDisabled()
	}
	return svc, nil
}

func privacyCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// GetPrivacyOverview 返回隐私页所需的全部数据：运行设置、规则、旧版规则清单、
// 未扫描路径统计与可用预设。不含任何命中原文。
func (a *App) GetPrivacyOverview() (privacy.Overview, error) {
	svc, err := a.privacySvc()
	if err != nil {
		return privacy.Overview{}, err
	}
	ctx, cancel := privacyCtx()
	defer cancel()

	overview, err := svc.GetOverview(ctx)
	if err != nil {
		return privacy.Overview{}, fmt.Errorf("获取隐私防护配置失败: %w", err)
	}
	return overview, nil
}

// UpdatePrivacySettings 更新运行模式与兜底策略，保存后立即重编译规则快照
func (a *App) UpdatePrivacySettings(settings privacy.Settings) error {
	svc, err := a.privacySvc()
	if err != nil {
		return err
	}
	ctx, cancel := privacyCtx()
	defer cancel()

	if err := svc.UpdateSettings(ctx, settings); err != nil {
		return fmt.Errorf("更新隐私防护设置失败: %w", err)
	}
	return nil
}

func (a *App) CreatePrivacyRule(rule privacy.Rule) (privacy.StoredRule, error) {
	svc, err := a.privacySvc()
	if err != nil {
		return privacy.StoredRule{}, err
	}
	ctx, cancel := privacyCtx()
	defer cancel()

	created, err := svc.CreateRule(ctx, rule)
	if err != nil {
		return privacy.StoredRule{}, fmt.Errorf("创建隐私规则失败: %w", err)
	}
	return created, nil
}

func (a *App) UpdatePrivacyRule(id int64, rule privacy.Rule) (privacy.StoredRule, error) {
	svc, err := a.privacySvc()
	if err != nil {
		return privacy.StoredRule{}, err
	}
	ctx, cancel := privacyCtx()
	defer cancel()

	updated, err := svc.UpdateRule(ctx, id, rule)
	if err != nil {
		return privacy.StoredRule{}, fmt.Errorf("更新隐私规则失败: %w", err)
	}
	return updated, nil
}

func (a *App) DeletePrivacyRule(id int64) error {
	svc, err := a.privacySvc()
	if err != nil {
		return err
	}
	ctx, cancel := privacyCtx()
	defer cancel()

	if err := svc.DeleteRule(ctx, id); err != nil {
		return fmt.Errorf("删除隐私规则失败: %w", err)
	}
	return nil
}

// DeletePrivacyRules 批量删除规则，返回实际删除条数。内置规则会被跳过（只能停用）。
func (a *App) DeletePrivacyRules(ids []int64) (int64, error) {
	svc, err := a.privacySvc()
	if err != nil {
		return 0, err
	}
	ctx, cancel := privacyCtx()
	defer cancel()

	affected, err := svc.DeleteRules(ctx, ids)
	if err != nil {
		return 0, fmt.Errorf("批量删除隐私规则失败: %w", err)
	}
	return affected, nil
}

// SetPrivacyRulesEnabled 批量启用/停用规则，返回实际影响条数
func (a *App) SetPrivacyRulesEnabled(ids []int64, enabled bool) (int64, error) {
	svc, err := a.privacySvc()
	if err != nil {
		return 0, err
	}
	ctx, cancel := privacyCtx()
	defer cancel()

	affected, err := svc.SetRulesEnabled(ctx, ids, enabled)
	if err != nil {
		return 0, fmt.Errorf("批量更新隐私规则失败: %w", err)
	}
	return affected, nil
}

// ApplyPrivacyPreset 导入内置预设规则集，返回新增条数
func (a *App) ApplyPrivacyPreset(presetID string) (int, error) {
	svc, err := a.privacySvc()
	if err != nil {
		return 0, err
	}
	ctx, cancel := privacyCtx()
	defer cancel()

	added, err := svc.ApplyPreset(ctx, presetID)
	if err != nil {
		return 0, fmt.Errorf("导入隐私预设失败: %w", err)
	}
	return added, nil
}

// DiscardLegacyPrivacyRules 用户确认旧版规则已手工重建后，清除只读清单
func (a *App) DiscardLegacyPrivacyRules() error {
	svc, err := a.privacySvc()
	if err != nil {
		return err
	}
	ctx, cancel := privacyCtx()
	defer cancel()

	if err := svc.DiscardLegacyRules(ctx); err != nil {
		return fmt.Errorf("清除旧版隐私规则清单失败: %w", err)
	}
	return nil
}

// PreviewPrivacyRedaction 规则测试：对输入文本试跑当前规则。输入输出均不落库。
func (a *App) PreviewPrivacyRedaction(input string) (privacy.PreviewResult, error) {
	svc, err := a.privacySvc()
	if err != nil {
		return privacy.PreviewResult{}, err
	}
	return svc.PreviewText(input), nil
}

// PreviewPrivacyRequest 按 API 路径扫描一段完整 JSON 请求体，列出被扫描的字段与命中情况。
// 仅在本地推演，不发往任何上游；返回的 RuleHit 不含命中原文。
func (a *App) PreviewPrivacyRequest(path, body string) (privacy.RequestPreviewResult, error) {
	svc, err := a.privacySvc()
	if err != nil {
		return privacy.RequestPreviewResult{}, err
	}
	return svc.PreviewRequest(path, []byte(body)), nil
}

// GetPrivacyUnscannedPaths 未被隐私规则扫描的请求路径统计。
// 白名单之外的路径不扫描，此接口保证该情况对用户可见，不做静默漏扫。
func (a *App) GetPrivacyUnscannedPaths() ([]privacy.UnscannedPathStat, error) {
	svc, err := a.privacySvc()
	if err != nil {
		return nil, err
	}
	return svc.UnscannedPaths(), nil
}

func (a *App) ClearPrivacyUnscannedPaths() error {
	svc, err := a.privacySvc()
	if err != nil {
		return err
	}
	svc.ClearUnscannedPaths()
	return nil
}
