//go:build !windows

package backend

// applyChromeEnterprisePolicies 在非 Windows 平台上是空操作。
// 仅 Windows 通过 HKCU 注册表抑制 Chrome 安全警告 infobar。
func applyChromeEnterprisePolicies() {}
