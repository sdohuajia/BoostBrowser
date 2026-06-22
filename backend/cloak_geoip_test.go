package backend

import (
	"strings"
	"testing"
)

func TestLanguageFromCountry(t *testing.T) {
	cases := map[string]string{
		"JP":      "ja-JP",
		"jp":      "ja-JP",
		" CN ":    "zh-CN",
		"GB":      "en-GB",
		"US":      "en-US",
		"UNKNOWN": "en-US", // fallback
		"":        "en-US",
	}
	for in, want := range cases {
		got := languageFromCountry(in)
		if got != want {
			t.Errorf("languageFromCountry(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestBuildAcceptLanguageHeader(t *testing.T) {
	// 真实 ipapi 输入：德国
	got := buildAcceptLanguageHeader("de-DE", "de-DE,en-DE,en")
	if !strings.HasPrefix(got, "de-DE,de;q=0.9,en-DE;q=") {
		t.Errorf("buildAcceptLanguageHeader DE = %q; missing expected prefix", got)
	}
	if !strings.Contains(got, "en-DE") || !strings.Contains(got, "en") {
		t.Errorf("buildAcceptLanguageHeader DE = %q; should include en-DE / en", got)
	}

	// 日本
	got = buildAcceptLanguageHeader("ja-JP", "ja-JP,ja")
	if !strings.HasPrefix(got, "ja-JP,ja;q=0.9") {
		t.Errorf("buildAcceptLanguageHeader JP = %q", got)
	}
	// 必须保底有英文
	if !strings.Contains(got, "en-US") {
		t.Errorf("buildAcceptLanguageHeader JP = %q; missing en-US fallback", got)
	}

	// raw 为空：应只输出主语言 + 英文兜底
	got = buildAcceptLanguageHeader("zh-CN", "")
	if !strings.HasPrefix(got, "zh-CN,zh;q=0.9") {
		t.Errorf("buildAcceptLanguageHeader CN empty raw = %q", got)
	}
	if !strings.Contains(got, "en-US") {
		t.Errorf("buildAcceptLanguageHeader CN = %q; should fallback to en-US", got)
	}

	// primary 为空：完全 fallback
	got = buildAcceptLanguageHeader("", "")
	if got != "en-US,en;q=0.9" {
		t.Errorf("buildAcceptLanguageHeader empty primary = %q", got)
	}
}
