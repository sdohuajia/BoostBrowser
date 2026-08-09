package backend

import "testing"

func TestLastTabsTrackerEnabledDefaultsOn(t *testing.T) {
	t.Setenv("BOOST_BROWSER_ENABLE_LAST_TABS_TRACKER", "")
	if !lastTabsTrackerEnabled() {
		t.Fatal("last tabs tracker should be enabled by default")
	}
}

func TestLastTabsTrackerCanBeDisabledByEnv(t *testing.T) {
	for _, value := range []string{"0", "false", "no", "off"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("BOOST_BROWSER_ENABLE_LAST_TABS_TRACKER", value)
			if lastTabsTrackerEnabled() {
				t.Fatalf("last tabs tracker should be disabled for %q", value)
			}
		})
	}
}

func TestWindowBoundsTrackerEnabledDefaultsOnAndCanBeDisabled(t *testing.T) {
	t.Setenv("BOOST_BROWSER_ENABLE_WINDOW_BOUNDS_TRACKER", "")
	if !windowBoundsTrackerEnabled() {
		t.Fatal("window bounds tracker should be enabled by default")
	}

	t.Setenv("BOOST_BROWSER_ENABLE_WINDOW_BOUNDS_TRACKER", "0")
	if windowBoundsTrackerEnabled() {
		t.Fatal("window bounds tracker should be disabled when env is 0")
	}
}

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
