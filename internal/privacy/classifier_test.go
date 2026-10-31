package privacy

import "testing"

func TestClassifierPathScansEveryPermittedContextField(t *testing.T) {
	body := []byte(`{"current_user_message":"current-private","previous_user_message":"previous-private","previous_assistant_text":"assistant-private","model":"do-not-rewrite","tools":[{"text":"not-allowed"}]}`)
	match := walkerForPath("/internal/routing/classify")
	if match == nil || !IsScannablePath("/internal/routing/classify") {
		t.Fatal("classifier path is unsupported")
	}
	segments, err := collectJSONTextSegments(body, match)
	if err != nil || len(segments) != 3 {
		t.Fatalf("segments=%+v err=%v", segments, err)
	}
	for i, want := range []string{"current-private", "previous-private", "assistant-private"} {
		if segments[i].Value != want {
			t.Fatalf("segment %d=%+v", i, segments[i])
		}
	}
}
