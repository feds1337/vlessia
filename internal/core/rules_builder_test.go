package core

import "testing"

func TestRuleBuilderValidation(t *testing.T) {
	for _, tc := range []struct {
		kind, value string
		valid       bool
	}{
		{"site", " youtube.com ", true}, {"site", "10.0.0.0/8", true}, {"site", "сайт.рф", true},
		{"site", "regexp:[", false}, {"site", "#comment", false}, {"site", "a.com\nb.com", false},
		{"app", "C:/Program Files/Browser/", true}, {"app", "chrome.exe", true}, {"app", "*.exe", false},
		{"app", "", false}, {"other", "x", false},
	} {
		_, err := ValidateRule(tc.kind, tc.value)
		if (err == nil) != tc.valid {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}
