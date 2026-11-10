package remote

import "testing"

func TestQuote(t *testing.T) {
	cases := map[string]string{
		"deploy":            "deploy",
		"--repo=user/app":   "'--repo=user/app'",
		"it's":              `'it'\''s'`,
		"":                  "''",
		"curl -f x || exit": "'curl -f x || exit'",
	}
	for in, want := range cases {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
}
