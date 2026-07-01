package proxy

import (
	"boost-browser/backend/internal/config"
	"strings"
	"testing"
)

func TestValidateProxyConfigInvalidRawString(t *testing.T) {
	ok, msg := ValidateProxyConfig("not-a-proxy-config", nil, "")
	if ok {
		t.Fatalf("expected invalid raw string to fail validation")
	}
	if !strings.Contains(msg, "解析失败") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestValidateProxyConfigMissingProxyId(t *testing.T) {
	ok, msg := ValidateProxyConfig("", []config.BrowserProxy{
		{ProxyId: "p1", ProxyConfig: "http://127.0.0.1:7890"},
	}, "missing-proxy")
	if ok {
		t.Fatalf("expected missing proxyId to fail validation")
	}
	if !strings.Contains(msg, "不存在") {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestValidateProxyConfigMissingProxyIdFallbackToRawConfig(t *testing.T) {
	ok, msg := ValidateProxyConfig("socks5://127.0.0.1:1080", []config.BrowserProxy{
		{ProxyId: "p1", ProxyConfig: "http://127.0.0.1:7890"},
	}, "missing-proxy")
	if !ok {
		t.Fatalf("expected fallback proxyConfig to pass, msg=%s", msg)
	}
}

func TestValidateProxyConfigStandardProxy(t *testing.T) {
	ok, msg := ValidateProxyConfig("socks5://127.0.0.1:1080", nil, "")
	if !ok {
		t.Fatalf("expected standard proxy to pass: %s", msg)
	}
}
