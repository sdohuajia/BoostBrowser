package launchcode_test

import (
	"strings"
	"testing"

	"boost-browser/backend/internal/launchcode"
)

func TestSetCodeAndResolveCaseInsensitive(t *testing.T) {
	svc := launchcode.NewLaunchCodeService(launchcode.NewMemoryLaunchCodeDAO())
	code, err := svc.SetCode("p1", "demo_code")
	if err != nil {
		t.Fatalf("SetCode 失败: %v", err)
	}
	if code != "DEMO_CODE" {
		t.Fatalf("期望 DEMO_CODE，实际 %s", code)
	}

	profileID, err := svc.Resolve("demo_code")
	if err != nil {
		t.Fatalf("Resolve 失败: %v", err)
	}
	if profileID != "p1" {
		t.Fatalf("期望 p1，实际 %s", profileID)
	}
}

func TestSetCodeConflict(t *testing.T) {
	svc := launchcode.NewLaunchCodeService(launchcode.NewMemoryLaunchCodeDAO())
	if _, err := svc.SetCode("p1", "AAA111"); err != nil {
		t.Fatalf("SetCode p1 失败: %v", err)
	}
	if _, err := svc.SetCode("p2", "AAA111"); err == nil {
		t.Fatal("期望 code 冲突时报错")
	}
}

func TestSetCodeValidation(t *testing.T) {
	svc := launchcode.NewLaunchCodeService(launchcode.NewMemoryLaunchCodeDAO())
	cases := []string{"", "a", "ab", "中文123", "abc!123", strings.Repeat("A", 40)}
	for _, c := range cases {
		if _, err := svc.SetCode("p1", c); err == nil {
			t.Fatalf("期望非法 code 报错: %q", c)
		}
	}
}
