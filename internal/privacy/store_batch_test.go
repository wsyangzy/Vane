package privacy

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store := NewStore(db)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return store
}

// 批量删除必须跳过内置规则：它们是开箱即用的 PII 防护底线，
// 只允许停用。若全选后一键删除能把它们抹掉，用户会在毫无察觉的情况下失去保护。
func TestDeleteRulesSkipsBuiltinRules(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	builtins, err := store.ListRules(ctx)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(builtins) == 0 {
		t.Fatal("expected seeded builtin rules")
	}

	custom, err := store.CreateRule(ctx, Rule{
		Enabled: true, Name: "自定义", Priority: 100,
		MatchType: MatchTypeLiteral, Pattern: "secret-token",
		Action: ActionRedact, ReplacementMode: ReplacementPlaceholder,
		Placeholder: "[X]", Source: SourceCustom,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}

	ids := []int64{custom.ID}
	for _, rule := range builtins {
		ids = append(ids, rule.ID)
	}

	affected, err := store.DeleteRules(ctx, ids)
	if err != nil {
		t.Fatalf("delete rules: %v", err)
	}
	if affected != 1 {
		t.Fatalf("only the custom rule may be deleted, got %d", affected)
	}

	remaining, err := store.ListRules(ctx)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(remaining) != len(builtins) {
		t.Fatalf("builtin rules must survive, got %d want %d", len(remaining), len(builtins))
	}
	for _, rule := range remaining {
		if rule.Source != SourceBuiltin {
			t.Fatalf("unexpected surviving rule source: %s", rule.Source)
		}
	}
}

func TestSetRulesEnabledAppliesToBuiltinRules(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	all, err := store.ListRules(ctx)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	ids := make([]int64, 0, len(all))
	for _, rule := range all {
		ids = append(ids, rule.ID)
	}

	// 内置规则不可删除，但必须可以停用——否则用户遇到误报时无从关闭
	affected, err := store.SetRulesEnabled(ctx, ids, false)
	if err != nil {
		t.Fatalf("disable rules: %v", err)
	}
	if affected != int64(len(ids)) {
		t.Fatalf("expected %d rows affected, got %d", len(ids), affected)
	}

	enabled, err := store.ListEnabledRules(ctx)
	if err != nil {
		t.Fatalf("list enabled: %v", err)
	}
	if len(enabled) != 0 {
		t.Fatalf("expected all rules disabled, got %d enabled", len(enabled))
	}
}

func TestDeleteRulesWithEmptyInputIsNoop(t *testing.T) {
	store := newTestStore(t)
	affected, err := store.DeleteRules(context.Background(), nil)
	if err != nil {
		t.Fatalf("empty delete must not error: %v", err)
	}
	if affected != 0 {
		t.Fatalf("expected zero affected, got %d", affected)
	}
}
