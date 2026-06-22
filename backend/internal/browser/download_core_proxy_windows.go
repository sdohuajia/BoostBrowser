//go:build windows

package browser

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// readSystemProxy 从 Windows 注册表读取当前系统代理（WinINet，Clash 会写这里）。
// 返回格式如 "http://127.0.0.1:7890" 或 "socks5://127.0.0.1:7891"。
func readSystemProxy() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()

	enabled, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return "", fmt.Errorf("系统代理未启用")
	}

	proxyServer, _, err := k.GetStringValue("ProxyServer")
	if err != nil || proxyServer == "" {
		return "", fmt.Errorf("代理地址为空")
	}

	// proxyServer 可能是 "127.0.0.1:7890" 或 "http=..;https=.." 多协议格式
	// 不含协议前缀时默认补 http://
	if !strings.Contains(proxyServer, ":") {
		return "", fmt.Errorf("无效的代理格式: %s", proxyServer)
	}
	if !strings.HasPrefix(proxyServer, "http") && !strings.HasPrefix(proxyServer, "socks") {
		return "http://" + proxyServer, nil
	}
	return proxyServer, nil
}
