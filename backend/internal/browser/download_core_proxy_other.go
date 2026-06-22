//go:build !windows

package browser

import "fmt"

// readSystemProxy 非 Windows 平台不支持从注册表读取系统代理，直接返回未启用。
func readSystemProxy() (string, error) {
	return "", fmt.Errorf("当前平台不支持系统代理注册表读取")
}
