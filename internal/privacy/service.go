package privacy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// PreviewResult 规则测试面板的结果。
// RuleHits 中的 Matches 由 json:"-" 阻断，不会随响应回传。
type PreviewResult struct {
	Output   string    `json:"output"`
	Changed  bool      `json:"changed"`
	HitCount int       `json:"hit_count"`
	RuleHits []RuleHit `json:"rule_hits"`
}

// UnscannedPathStat 未被隐私规则扫描的请求路径统计。
//
// 白名单之外的路径不做扫描，但绝不静默：漏扫是静默失败，
// 用户必须能在界面上看见「有请求没被扫过」，否则会误以为已全面受保护。
type UnscannedPathStat struct {
	Path       string    `json:"path"`
	Count      int64     `json:"count"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// Service 隐私规则的业务编排：持久化 + 快照编译缓存 + 热路径过滤入口。
type Service struct {
	store  *Store
	logger *slog.Logger

	mu       sync.RWMutex
	snapshot *Snapshot

	unscannedMu sync.Mutex
	unscanned   map[string]*UnscannedPathStat

	now func() time.Time
}

func NewService(store *Store, logger ...*slog.Logger) *Service {
	svc := &Service{
		store:     store,
		unscanned: make(map[string]*UnscannedPathStat),
		now:       time.Now,
	}
	if len(logger) > 0 {
		svc.logger = logger[0]
	}
	return svc
}

func (s *Service) ready() error {
	if s == nil || s.store == nil {
		return fmt.Errorf("privacy store not initialized")
	}
	return nil
}

// Reload 从库中重新加载并编译规则快照。
// 编译失败时保留当前快照不变——半可用的规则集会让用户误以为受保护。
func (s *Service) Reload(ctx context.Context) error {
	if err := s.ready(); err != nil {
		return err
	}
	settings, err := s.store.LoadSettings(ctx)
	if err != nil {
		return err
	}
	stored, err := s.store.ListEnabledRules(ctx)
	if err != nil {
		return err
	}
	rules := make([]Rule, 0, len(stored))
	for _, item := range stored {
		rules = append(rules, item.Rule)
	}
	compiled, err := CompileRules(rules)
	if err != nil {
		return fmt.Errorf("编译隐私规则失败，已保留上一份规则快照: %w", err)
	}

	s.mu.Lock()
	version := int64(1)
	if s.snapshot != nil {
		version = s.snapshot.Version + 1
	}
	s.snapshot = &Snapshot{
		Version:  version,
		Settings: settings,
		Rules:    compiled,
		LoadedAt: s.now(),
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) currentSnapshot() *Snapshot {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

// SnapshotVersion 当前生效快照版本，规则每次变更递增
func (s *Service) SnapshotVersion() int64 {
	snapshot := s.currentSnapshot()
	if snapshot == nil {
		return 0
	}
	return snapshot.Version
}

// Apply 热路径入口：对出站请求体执行隐私过滤。
//
// 返回的 error 只会是 *PolicyError，调用方必须 errors.As 识别并短路返回，
// 不得进入重试 / 故障转移 / 端点冷却。
func (s *Service) Apply(ctx context.Context, req Request, body []byte) (ApplyResult, error) {
	if s == nil {
		return ApplyResult{Body: body, Action: ModeDisabled}, nil
	}
	snapshot := s.currentSnapshot()
	if snapshot == nil {
		// 尚未加载成功时放行：隐私模块自身不可用不应打断全部转发，
		// 该情况由 Reload 的错误日志暴露。
		return ApplyResult{Body: body, Action: ModeDisabled}, nil
	}
	result, err := snapshot.Apply(req, body)
	if result.SkippedReason == SkippedUnsupportedPath {
		s.recordUnscannedPath(req.Path)
	}
	return result, err
}

func (s *Service) recordUnscannedPath(path string) {
	if path == "" {
		return
	}
	s.unscannedMu.Lock()
	defer s.unscannedMu.Unlock()
	stat := s.unscanned[path]
	if stat == nil {
		stat = &UnscannedPathStat{Path: path}
		s.unscanned[path] = stat
	}
	stat.Count++
	stat.LastSeenAt = s.now()
}

// UnscannedPaths 返回未被扫描的路径统计（进程内计数，重启清零）
func (s *Service) UnscannedPaths() []UnscannedPathStat {
	if s == nil {
		return nil
	}
	s.unscannedMu.Lock()
	defer s.unscannedMu.Unlock()
	out := make([]UnscannedPathStat, 0, len(s.unscanned))
	for _, stat := range s.unscanned {
		out = append(out, *stat)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

func (s *Service) ClearUnscannedPaths() {
	if s == nil {
		return
	}
	s.unscannedMu.Lock()
	s.unscanned = make(map[string]*UnscannedPathStat)
	s.unscannedMu.Unlock()
}

// Overview 隐私页所需的完整视图。
// 全部字段都不含命中原文或规则敏感原值（legacy 规则已掩码）。
type Overview struct {
	Settings       Settings            `json:"settings"`
	Rules          []StoredRule        `json:"rules"`
	LegacyRules    []LegacyRule        `json:"legacy_rules"`
	UnscannedPaths []UnscannedPathStat `json:"unscanned_paths"`
	Presets        []Preset            `json:"presets"`
	PseudonymKinds []string            `json:"pseudonym_kinds"`
}

func (s *Service) GetOverview(ctx context.Context) (Overview, error) {
	if err := s.ready(); err != nil {
		return Overview{}, err
	}
	settings, err := s.store.LoadSettings(ctx)
	if err != nil {
		return Overview{}, err
	}
	rules, err := s.store.ListRules(ctx)
	if err != nil {
		return Overview{}, err
	}
	legacy, err := s.store.ListLegacyRules(ctx)
	if err != nil {
		return Overview{}, err
	}
	return Overview{
		Settings:       settings,
		Rules:          rules,
		LegacyRules:    legacy,
		UnscannedPaths: s.UnscannedPaths(),
		Presets:        Presets(),
		PseudonymKinds: PseudonymKinds(),
	}, nil
}

func (s *Service) GetSettings(ctx context.Context) (Settings, error) {
	if err := s.ready(); err != nil {
		return Settings{}, err
	}
	return s.store.LoadSettings(ctx)
}

func (s *Service) UpdateSettings(ctx context.Context, settings Settings) error {
	if err := s.ready(); err != nil {
		return err
	}
	if err := s.store.SaveSettings(ctx, settings); err != nil {
		return err
	}
	return s.Reload(ctx)
}

func (s *Service) ListRules(ctx context.Context) ([]StoredRule, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	return s.store.ListRules(ctx)
}

func (s *Service) CreateRule(ctx context.Context, rule Rule) (StoredRule, error) {
	if err := s.ready(); err != nil {
		return StoredRule{}, err
	}
	normalized, err := normalizeRule(rule)
	if err != nil {
		return StoredRule{}, err
	}
	created, err := s.store.CreateRule(ctx, normalized)
	if err != nil {
		return StoredRule{}, err
	}
	return created, s.Reload(ctx)
}

func (s *Service) UpdateRule(ctx context.Context, id int64, rule Rule) (StoredRule, error) {
	if err := s.ready(); err != nil {
		return StoredRule{}, err
	}
	existing, err := s.store.GetRule(ctx, id)
	if err != nil {
		return StoredRule{}, err
	}
	// 内置规则的匹配逻辑不可改写，只允许调整启用状态与替换方式
	if existing.Source == SourceBuiltin {
		rule.MatchType = existing.MatchType
		rule.Pattern = existing.Pattern
	}
	normalized, err := normalizeRule(rule)
	if err != nil {
		return StoredRule{}, err
	}
	updated, err := s.store.UpdateRule(ctx, id, normalized)
	if err != nil {
		return StoredRule{}, err
	}
	return updated, s.Reload(ctx)
}

func (s *Service) DeleteRule(ctx context.Context, id int64) error {
	if err := s.ready(); err != nil {
		return err
	}
	if err := s.store.DeleteRule(ctx, id); err != nil {
		return err
	}
	return s.Reload(ctx)
}

// DeleteRules 批量删除规则，只重编译一次快照。
// 逐条删除会让每删一条都触发一次全量规则重编译，规则多时明显拖慢界面。
func (s *Service) DeleteRules(ctx context.Context, ids []int64) (int64, error) {
	if err := s.ready(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	affected, err := s.store.DeleteRules(ctx, ids)
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, nil
	}
	return affected, s.Reload(ctx)
}

// SetRulesEnabled 批量启用/停用规则，只重编译一次快照。
func (s *Service) SetRulesEnabled(ctx context.Context, ids []int64, enabled bool) (int64, error) {
	if err := s.ready(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	affected, err := s.store.SetRulesEnabled(ctx, ids, enabled)
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, nil
	}
	return affected, s.Reload(ctx)
}

// ApplyPreset 导入预设规则集，返回新增条数。已存在同名规则的跳过。
func (s *Service) ApplyPreset(ctx context.Context, presetID string) (int, error) {
	if err := s.ready(); err != nil {
		return 0, err
	}
	preset, ok := FindPreset(presetID)
	if !ok {
		return 0, fmt.Errorf("预设不存在: %s", presetID)
	}
	existing, err := s.store.ListRules(ctx)
	if err != nil {
		return 0, err
	}
	known := make(map[string]struct{}, len(existing))
	for _, rule := range existing {
		known[rule.Name] = struct{}{}
	}

	added := 0
	for _, rule := range preset.Rules {
		if _, exists := known[rule.Name]; exists {
			continue
		}
		if _, err := s.store.CreateRule(ctx, rule); err != nil {
			return added, err
		}
		added++
	}
	if added == 0 {
		return 0, nil
	}
	return added, s.Reload(ctx)
}

func (s *Service) ListLegacyRules(ctx context.Context) ([]LegacyRule, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	return s.store.ListLegacyRules(ctx)
}

func (s *Service) DiscardLegacyRules(ctx context.Context) error {
	if err := s.ready(); err != nil {
		return err
	}
	return s.store.DiscardLegacyRules(ctx)
}

// PreviewText 对一段文本试跑当前规则（规则测试面板）。
// 输入与输出都停留在本机，不落库。
func (s *Service) PreviewText(text string) PreviewResult {
	snapshot := s.currentSnapshot()
	if snapshot == nil {
		return PreviewResult{Output: text}
	}
	result := snapshot.ApplyToText(text)
	return PreviewResult{
		Output:   string(result.Body),
		Changed:  result.Changed,
		HitCount: result.HitCount,
		RuleHits: result.RuleHits,
	}
}

// normalizeRule 补齐默认值并校验
func normalizeRule(rule Rule) (Rule, error) {
	if rule.Action == "" {
		rule.Action = ActionRedact
	}
	if rule.ReplacementMode == "" {
		rule.ReplacementMode = ReplacementPlaceholder
	}
	if rule.ReplacementMode == ReplacementPlaceholder && rule.Placeholder == "" {
		rule.Placeholder = DefaultPlaceholder
	}
	if rule.Source == "" {
		rule.Source = SourceCustom
	}
	if rule.Priority == 0 {
		rule.Priority = 100
	}
	if err := ValidateRule(rule); err != nil {
		return Rule{}, err
	}
	// 编译校验前置，避免把无法编译的正则写进库
	if _, err := CompileRule(rule); err != nil {
		return Rule{}, err
	}
	return rule, nil
}

// SummaryToJSON 序列化命中摘要用于落库与前端展示。
//
// 只输出规则名、来源、动作与命中数。RuleHit.Matches 带 json:"-"，
// 命中原文不可能经由此处泄漏——这是本次重构的核心红线。
func SummaryToJSON(result ApplyResult) string {
	if len(result.RuleHits) == 0 {
		return ""
	}
	data, err := json.Marshal(struct {
		Changed  bool      `json:"changed"`
		Action   string    `json:"action"`
		HitCount int       `json:"hit_count"`
		Hits     []RuleHit `json:"hits"`
	}{
		Changed:  result.Changed,
		Action:   result.Action,
		HitCount: result.HitCount,
		Hits:     result.RuleHits,
	})
	if err != nil {
		return ""
	}
	return string(data)
}
