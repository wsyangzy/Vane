package proxy

import "testing"

func TestExtractReasoningConfigSeparatesThinkingModeFromEffort(t *testing.T) {
	h := &Handler{}

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			// thinking.type 是扩展思考的开关模式，不是推理强度：
			// 只有它时结果里不能出现 effort，否则前端会把 adaptive 当强度展示
			name: "只有 thinking 模式",
			body: `{"model":"claude-opus-5","thinking":{"type":"adaptive"}}`,
			want: `{"type":"adaptive"}`,
		},
		{
			name: "只有推理强度",
			body: `{"model":"claude-opus-5","output_config":{"effort":"high"}}`,
			want: `{"effort":"high"}`,
		},
		{
			// 两者并存时合并到同一对象，前端据此优先展示真正的强度
			name: "模式与强度并存",
			body: `{"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}}`,
			want: `{"effort":"xhigh","type":"adaptive"}`,
		},
		{
			name: "关闭思考仍保留模式",
			body: `{"thinking":{"type":"disabled"}}`,
			want: `{"type":"disabled"}`,
		},
		{
			name: "两者都没有",
			body: `{"model":"claude-opus-5","messages":[]}`,
			want: "",
		},
		{
			name: "空请求体",
			body: "",
			want: "",
		},
		{
			name: "非法 JSON",
			body: `{"thinking":`,
			want: "",
		},
		{
			// output_config 存在但 effort 为空，不应写出空的 effort 键
			name: "空 effort 不落库",
			body: `{"thinking":{"type":"adaptive"},"output_config":{"effort":"  "}}`,
			want: `{"type":"adaptive"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := h.extractReasoningConfig([]byte(tc.body))
			if got != tc.want {
				t.Fatalf("extractReasoningConfig() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractReasoningConfigKeepsBudgetTokens(t *testing.T) {
	// 旧模型仍使用 budget_tokens，合并时不能丢
	h := &Handler{}
	got := h.extractReasoningConfig([]byte(`{"thinking":{"type":"enabled","budget_tokens":16000}}`))
	want := `{"budget_tokens":16000,"type":"enabled"}`
	if got != want {
		t.Fatalf("extractReasoningConfig() = %q, want %q", got, want)
	}
}
