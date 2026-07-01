package launchcode_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"boost-browser/backend/internal/launchcode"
)

func buildAuthProtectedTestHandler() http.Handler {
	srv := launchcode.NewLaunchServer(newInMemoryService(), newMockStarter(), nil, 0)
	srv.SetAPIAuthConfig(launchcode.APIAuthConfig{
		Enabled: true,
		APIKey:  "secret-key",
		Header:  "X-Test-Api-Key",
	})
	return launchcode.NewTestHandler(srv)
}

func TestAPIAuthRejectsMissingKey(t *testing.T) {
	handler := buildAuthProtectedTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("缺少 API Key 时应返回 401: got=%d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp["error"] != "unauthorized: invalid api key" {
		t.Fatalf("错误信息不正确: %+v", resp)
	}
	if resp["authHeader"] != "X-Test-Api-Key" {
		t.Fatalf("应返回当前使用的认证 Header: %+v", resp)
	}
}

func TestAPIAuthRejectsWrongKey(t *testing.T) {
	handler := buildAuthProtectedTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("X-Test-Api-Key", "wrong-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("错误 API Key 时应返回 401: got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIAuthAllowsCorrectKey(t *testing.T) {
	handler := buildAuthProtectedTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("X-Test-Api-Key", "secret-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("正确 API Key 时应返回 200: got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIAuthDoesNotProtectCDPProxyRoutes(t *testing.T) {
	handler := buildAuthProtectedTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/json/version", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("CDP 路径不应被 API 认证拦截: got=%d body=%s", w.Code, w.Body.String())
	}
}
