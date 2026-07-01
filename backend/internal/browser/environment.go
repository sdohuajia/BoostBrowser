package browser

import (
	"boost-browser/backend/internal/logger"
	"path/filepath"
	"strings"
)

// GetProxyConfigById 根据代理 ID 获取代理配置
func (m *Manager) GetProxyConfigById(proxyId string) (string, bool) {
	if proxy, ok := m.GetProxyByID(proxyId); ok {
		return strings.TrimSpace(proxy.ProxyConfig), true
	}
	return "", false
}

// ResolveUserDataDir 解析用户数据目录
func (m *Manager) ResolveUserDataDir(profile *Profile) string {
	userDataDir := strings.TrimSpace(profile.UserDataDir)
	if userDataDir == "" {
		userDataDir = profile.ProfileId
	}
	if filepath.IsAbs(userDataDir) {
		return userDataDir
	}
	root := strings.TrimSpace(m.Config.Browser.UserDataRoot)
	if root == "" {
		root = "data"
	}
	root = m.ResolveRelativePath(root)
	return filepath.Join(root, userDataDir)
}

// MigrateConfig 迁移旧配置到新格式
func (m *Manager) MigrateConfig() bool {
	log := logger.New("Browser")

	// 如果存在 environments 但没有 cores，执行迁移
	if len(m.Config.Browser.Environments) > 0 && len(m.Config.Browser.Cores) == 0 {
		log.Info("检测到旧配置格式，开始迁移")

		for _, env := range m.Config.Browser.Environments {
			m.Config.Browser.Cores = append(m.Config.Browser.Cores, Core{
				CoreId:    env.CoreId,
				CoreName:  env.CoreName,
				CorePath:  env.CorePath,
				IsDefault: env.IsDefault,
			})
		}

		// 清空旧字段
		m.Config.Browser.Environments = nil
		m.Config.Browser.ChromeBinaryPath = ""
		m.Config.Browser.CoreRoot = ""
		m.Config.Browser.DefaultCoreId = ""
		m.Config.Browser.DefaultConnectorType = ""

		if err := m.Config.Save(m.ResolveRelativePath("config.yaml")); err != nil {
			log.Error("配置迁移保存失败", logger.F("error", err.Error()))
			return false
		}

		log.Info("配置迁移完成", logger.F("cores_count", len(m.Config.Browser.Cores)))
		return true
	}

	return false
}
