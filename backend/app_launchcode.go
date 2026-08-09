package backend

import (
	"boost-browser/backend/internal/browser"
	"boost-browser/backend/internal/launchcode"
	"fmt"
)

// StartInstance 实现 launchcode.BrowserStarter 接口
func (a *App) StartInstance(profileId string) (*browser.Profile, error) {
	return a.BrowserInstanceStart(profileId)
}

// StartInstanceWithParams 实现 launchcode.BrowserStarterWithParams 接口
func (a *App) StartInstanceWithParams(profileId string, params launchcode.LaunchRequestParams) (*browser.Profile, error) {
	return a.BrowserInstanceStartWithParams(profileId, params.LaunchArgs, params.StartURLs, params.SkipDefaultStartURLs)
}

// BrowserProfileGetCode 获取实例的 LaunchCode（Wails 绑定）
func (a *App) BrowserProfileGetCode(profileId string) (string, error) {
	if a.launchCodeSvc == nil {
		return "", nil
	}
	return a.launchCodeSvc.EnsureCode(profileId)
}

// BrowserProfileRegenerateCode 重新生成实例的 LaunchCode（Wails 绑定）
func (a *App) BrowserProfileRegenerateCode(profileId string) (string, error) {
	if a.launchCodeSvc == nil {
		return "", nil
	}
	return a.launchCodeSvc.RegenerateCode(profileId)
}

// BrowserProfileSetCode 自定义设置实例 LaunchCode（Wails 绑定）
func (a *App) BrowserProfileSetCode(profileId string, code string) (string, error) {
	if a.launchCodeSvc == nil {
		return "", nil
	}
	return a.launchCodeSvc.SetCode(profileId, code)
}

// BrowserInstanceStartByCode 通过 LaunchCode 启动实例（Wails 绑定）
func (a *App) BrowserInstanceStartByCode(code string) (*browser.Profile, error) {
	if a.launchCodeSvc == nil {
		return nil, fmt.Errorf("launch code service not initialized")
	}
	profileId, err := a.launchCodeSvc.Resolve(code)
	if err != nil {
		return nil, err
	}
	return a.BrowserInstanceStart(profileId)
}

// GetLaunchServerInfo 返回 LaunchServer 的当前监听信息（Wails 绑定）
func (a *App) GetLaunchServerInfo() map[string]interface{} {
	preferredPort := 0
	authRequested := false
	authConfigured := false
	authEnabled := false
	authHeader := launchcode.DefaultAPIKeyHeader
	if a.config != nil {
		preferredPort = a.config.LaunchServer.Port
		authRequested = a.config.LaunchServer.Auth.Enabled
		authConfigured = a.config.LaunchServer.Auth.APIKey != ""
		if header := a.config.LaunchServer.Auth.Header; header != "" {
			authHeader = header
		}
	}

	actualPort := 0
	if a.launchServer != nil {
		actualPort = a.launchServer.Port()
		authRequested = a.launchServer.APIAuthRequested()
		authConfigured = a.launchServer.APIAuthConfigured()
		authEnabled = a.launchServer.APIAuthEnabled()
		authHeader = a.launchServer.APIAuthHeader()
	}

	info := map[string]interface{}{
		"host":          "127.0.0.1",
		"preferredPort": preferredPort,
		"port":          actualPort,
		"ready":         actualPort > 0,
		"apiAuth": map[string]interface{}{
			"requested":  authRequested,
			"configured": authConfigured,
			"enabled":    authEnabled,
			"header":     authHeader,
		},
	}
	if actualPort > 0 {
		info["baseUrl"] = fmt.Sprintf("http://127.0.0.1:%d", actualPort)
		info["cdpUrl"] = fmt.Sprintf("http://127.0.0.1:%d", actualPort)
		if a.launchServer != nil {
			info["activeDebugPort"] = a.launchServer.ActiveDebugPort()
		}
	} else {
		info["baseUrl"] = ""
		info["cdpUrl"] = ""
		info["activeDebugPort"] = 0
	}
	return info
}

// 确保编译器检查 App 实现了 BrowserStarter 接口
var _ launchcode.BrowserStarter = (*App)(nil)
var _ launchcode.BrowserStarterWithParams = (*App)(nil)
