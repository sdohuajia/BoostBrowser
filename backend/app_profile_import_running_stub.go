//go:build !windows

package backend

import "fmt"

func discoverMoreLoginRunningProfiles() ([]moreLoginRunningProfile, error) {
	return nil, fmt.Errorf("导入运行中的 MoreLogin 环境仅支持 Windows 宿主")
}
