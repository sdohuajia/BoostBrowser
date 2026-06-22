package backend

import "testing"

func TestRestorableTabURLsFromTargetsFiltersInternalAndDedupes(t *testing.T) {
	targets := []cdpTarget{
		{Type: "page", URL: "about:blank"},
		{Type: "page", URL: "chrome://settings"},
		{Type: "page", URL: "chrome-extension://abcdef/home.html"},
		{Type: "other", URL: "https://ignored.example/"},
		{Type: "page", URL: "https://example.com/a"},
		{Type: "page", URL: "https://example.com/a"},
		{Type: "page", URL: "http://example.org/b"},
	}

	got := restorableTabURLsFromTargets(targets)
	want := []string{"https://example.com/a", "http://example.org/b"}
	if !sameLastTabs(got, want) {
		t.Fatalf("restorable tabs mismatch: got=%v want=%v", got, want)
	}
}

func TestBuildTargetURLsPrefersSavedLastTabsOverDefaultVerificationURLs(t *testing.T) {
	profile := &BrowserProfile{LastTabs: []string{"https://last.example/one", "https://last.example/two"}}
	got := buildTargetURLs(profile, nil, false)
	if !sameLastTabs(got, profile.LastTabs) {
		t.Fatalf("expected saved tabs, got=%v", got)
	}
}

func TestBuildTargetURLsExplicitStartURLsOverrideSavedTabs(t *testing.T) {
	profile := &BrowserProfile{LastTabs: []string{"https://last.example/"}}
	explicit := []string{"https://explicit.example/"}
	got := buildTargetURLs(profile, explicit, false)
	if !sameLastTabs(got, explicit) {
		t.Fatalf("expected explicit URLs, got=%v", got)
	}
}

func TestBuildTargetURLsSkipDefaultDisablesSavedTabs(t *testing.T) {
	profile := &BrowserProfile{LastTabs: []string{"https://last.example/"}}
	got := buildTargetURLs(profile, nil, true)
	if len(got) != 0 {
		t.Fatalf("expected no URLs when skipDefaultStartURLs=true, got=%v", got)
	}
}
