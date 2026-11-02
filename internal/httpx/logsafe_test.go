package httpx

import "testing"

func TestLogSafeStripsLineBreaks(t *testing.T) {
	cases := map[string]string{
		"plain":                      "plain",
		"evil\n{\"level\":\"INFO\"}": "evil{\"level\":\"INFO\"}",
		"a\r\nb":                     "ab",
		"":                           "",
	}
	for in, want := range cases {
		if got := LogSafe(in); got != want {
			t.Errorf("LogSafe(%q) = %q, want %q", in, got, want)
		}
	}
}
