package sqlite

import "testing"

func TestParseSQLiteDateTime(t *testing.T) {
	t.Parallel()

	cases := []string{
		"2026-06-12 11:01:02.123+08:00",
		"2026-06-12T11:01:02.123+08:00",
		"2026-06-12T11:01:02+08:00",
		"2026-06-12 11:01:02",
	}

	for _, value := range cases {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if got := ParseDateTime(value); got.IsZero() {
				t.Fatalf("ParseDateTime(%q) returned zero time", value)
			}
		})
	}
}
