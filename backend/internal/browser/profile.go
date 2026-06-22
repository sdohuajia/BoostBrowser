package browser

import (
	"boost-browser/backend/internal/logger"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// InitData 初始化浏览器数据
func (m *Manager) InitData() {
	m.Mutex.Lock()
	defer m.Mutex.Unlock()
	if m.Profiles == nil {
		m.Profiles = make(map[string]*Profile)
	}
	if m.BrowserProcesses == nil {
		m.BrowserProcesses = make(map[string]*exec.Cmd)
	}
	if m.XrayBridges == nil {
		m.XrayBridges = make(map[string]*XrayBridge)
	}
	// 执行配置迁移
	m.MigrateConfig()
	if len(m.Profiles) > 0 {
		return
	}
	m.loadProfiles()
}

func (m *Manager) loadProfiles() {
	log := logger.New("Browser")

	// 优先从 DAO（SQLite）加载
	if m.ProfileDAO != nil {
		profiles, err := m.ProfileDAO.List()
		if err != nil {
			log.Error("从数据库加载实例配置失败", logger.F("error", err))
		} else {
			// SQLite 模式：无论是否为空都直接使用，不自动创建默认实例
			for _, p := range profiles {
				p.CoreId = normalizeProfileCoreID(p.CoreId)
				m.Profiles[p.ProfileId] = p
			}
			if len(profiles) > 0 {
				log.Info("实例配置从数据库加载完成", logger.F("count", len(profiles)))
			} else {
				log.Info("实例表为空，用户可手动创建新实例")
			}
			return
		}
	}

	// 降级：从 config.yaml 加载（仅在无 SQLite 时使用）
	if len(m.Config.Browser.Profiles) == 0 {
		// 不自动创建默认实例，保持空列表
		log.Info("实例配置为空，用户可手动创建新实例")
		return
	}
	now := time.Now().Format(time.RFC3339)
	for _, item := range m.Config.Browser.Profiles {
		profileId := strings.TrimSpace(item.ProfileId)
		if profileId == "" {
			continue
		}
		createdAt := strings.TrimSpace(item.CreatedAt)
		if createdAt == "" {
			createdAt = now
		}
		updatedAt := strings.TrimSpace(item.UpdatedAt)
		if updatedAt == "" {
			updatedAt = createdAt
		}
		m.Profiles[profileId] = &Profile{
			ProfileId:          profileId,
			ProfileName:        item.ProfileName,
			UserDataDir:        item.UserDataDir,
			CoreId:             normalizeProfileCoreID(item.CoreId),
			FingerprintArgs:    append([]string{}, item.FingerprintArgs...),
			ProxyId:            item.ProxyId,
			ProxyConfig:        item.ProxyConfig,
			ProxyBindSourceID:  item.ProxyBindSourceID,
			ProxyBindSourceURL: item.ProxyBindSourceURL,
			ProxyBindName:      item.ProxyBindName,
			ProxyBindUpdatedAt: item.ProxyBindUpdatedAt,
			LaunchArgs:         append([]string{}, item.LaunchArgs...),
			LastTabs:           append([]string{}, item.LastTabs...),
			Tags:               append([]string{}, item.Tags...),
			Keywords:           append([]string{}, item.Keywords...),
			Running:            false,
			DebugPort:          0,
			Pid:                0,
			LastError:          "",
			CreatedAt:          createdAt,
			UpdatedAt:          updatedAt,
		}
	}
	log.Info("浏览器配置从文件加载完成", logger.F("count", len(m.Profiles)))
}

// SaveProfiles 保存所有实例配置（DAO 模式：逐条 upsert）
func (m *Manager) SaveProfiles() error {
	log := logger.New("Browser")
	if m.ProfileDAO != nil {
		for _, profile := range m.Profiles {
			profile.CoreId = normalizeProfileCoreID(profile.CoreId)
			if err := m.ProfileDAO.Upsert(profile); err != nil {
				log.Error("实例配置持久化失败", logger.F("profile_id", profile.ProfileId), logger.F("error", err))
				return err
			}
		}
		log.Info("实例配置持久化成功", logger.F("count", len(m.Profiles)))
		return nil
	}

	// 降级：写回 config.yaml
	profiles := make([]ProfileConfig, 0, len(m.Profiles))
	for _, profile := range m.Profiles {
		profiles = append(profiles, ProfileConfig{
			ProfileId:          profile.ProfileId,
			ProfileName:        profile.ProfileName,
			UserDataDir:        profile.UserDataDir,
			CoreId:             normalizeProfileCoreID(profile.CoreId),
			FingerprintArgs:    append([]string{}, profile.FingerprintArgs...),
			ProxyId:            profile.ProxyId,
			ProxyConfig:        profile.ProxyConfig,
			ProxyBindSourceID:  profile.ProxyBindSourceID,
			ProxyBindSourceURL: profile.ProxyBindSourceURL,
			ProxyBindName:      profile.ProxyBindName,
			ProxyBindUpdatedAt: profile.ProxyBindUpdatedAt,
			LaunchArgs:         append([]string{}, profile.LaunchArgs...),
			LastTabs:           append([]string{}, profile.LastTabs...),
			Tags:               append([]string{}, profile.Tags...),
			Keywords:           append([]string{}, profile.Keywords...),
			CreatedAt:          profile.CreatedAt,
			UpdatedAt:          profile.UpdatedAt,
		})
	}
	m.Config.Browser.Profiles = profiles
	if err := m.Config.Save(m.ResolveRelativePath("config.yaml")); err != nil {
		log.Error("浏览器配置持久化失败", logger.F("error", err))
		return err
	}
	log.Info("浏览器配置持久化成功（文件）", logger.F("count", len(profiles)))
	return nil
}

// List 获取配置列表
func (m *Manager) List() []Profile {
	log := logger.New("Browser")
	m.InitData()
	m.Mutex.Lock()
	defer m.Mutex.Unlock()
	list := make([]Profile, 0, len(m.Profiles))
	for _, profile := range m.Profiles {
		p := *profile
		if m.CodeProvider != nil {
			if code, err := m.CodeProvider.EnsureCode(p.ProfileId); err == nil {
				p.LaunchCode = code
			}
		}
		list = append(list, p)
	}
	// 按 ProfileId 排序，保持稳定顺序
	sort.Slice(list, func(i, j int) bool {
		return list[i].ProfileId < list[j].ProfileId
	})
	log.Info("浏览器配置列表查询", logger.F("count", len(list)))
	return list
}

// ListByTag 按标签筛选配置列表
func (m *Manager) ListByTag(tag string) []Profile {
	tag = strings.TrimSpace(tag)
	all := m.List()
	if tag == "" {
		return all
	}
	result := make([]Profile, 0)
	for _, p := range all {
		for _, t := range p.Tags {
			if strings.EqualFold(t, tag) {
				result = append(result, p)
				break
			}
		}
	}
	return result
}

// GetAllTags 获取所有已使用的标签（去重排序）
func (m *Manager) GetAllTags() []string {
	m.InitData()
	m.Mutex.Lock()
	defer m.Mutex.Unlock()
	seen := make(map[string]struct{})
	for _, p := range m.Profiles {
		for _, t := range p.Tags {
			t = strings.TrimSpace(t)
			if t != "" {
				seen[t] = struct{}{}
			}
		}
	}
	tags := make([]string, 0, len(seen))
	for t := range seen {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// Create 创建配置
func (m *Manager) Create(input ProfileInput) (*Profile, error) {
	log := logger.New("Browser")
	m.InitData()
	m.Mutex.Lock()
	defer m.Mutex.Unlock()

	// 实例数量上限已取消（不再受 MaxProfileLimit 约束）

	now := time.Now().Format(time.RFC3339)
	profileId := uuid.NewString()
	userDataDir := strings.TrimSpace(input.UserDataDir)
	if userDataDir == "" {
		userDataDir = profileId
	}
	proxyConfig := strings.TrimSpace(input.ProxyConfig)
	proxyId := strings.TrimSpace(input.ProxyId)
	selectedProxy := Proxy{}
	hasSelectedProxy := false
	if proxyId != "" {
		if proxyItem, ok := m.GetProxyByID(proxyId); ok {
			proxyConfig = strings.TrimSpace(proxyItem.ProxyConfig)
			selectedProxy = proxyItem
			hasSelectedProxy = true
		} else {
			log.Error("代理绑定失败", logger.F("profile_id", profileId), logger.F("proxy_id", proxyId))
		}
	}
	coreId := normalizeProfileCoreID(input.CoreId)
	if coreId == "" {
		if defaultCore, ok := m.GetDefaultCore(); ok {
			coreId = defaultCore.CoreId
		}
	}
	if proxyConfig == "" && m.Config.Browser.DefaultProxy != "" {
		proxyConfig = m.Config.Browser.DefaultProxy
	}
	// 指纹参数：新建实例始终生成一组独立基础身份。
	// 前端通常会把全局 default_fingerprint_args 原样传进来；如果把这些静态 brand/platform/version
	// 直接落库，就会出现每个实例 navigator.userAgent / UA-CH 都一样的问题。
	// 因此这里只保留未识别的自定义扩展开关，基础身份字段统一由 RandomFingerprintIdentity 重建。
	fpArgs := StripIdentityArgs(input.FingerprintArgs)
	fpArgs = append(fpArgs, RandomFingerprintIdentityForPlatform(PlatformFromArgs(input.FingerprintArgs))...)
	hasSeed := false
	for _, a := range fpArgs {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(a)), "--fingerprint=") {
			hasSeed = true
			break
		}
	}
	if !hasSeed {
		fpArgs = append(fpArgs, fmt.Sprintf("--fingerprint=%d", rand.Int31n(2147483647)+1))
	}

	profile := &Profile{
		ProfileId:       profileId,
		ProfileName:     input.ProfileName,
		UserDataDir:     userDataDir,
		CoreId:          coreId,
		FingerprintArgs: fpArgs,
		ProxyId:         proxyId,
		ProxyConfig:     proxyConfig,
		LaunchArgs:      input.LaunchArgs,
		Tags:            input.Tags,
		Keywords:        append([]string{}, input.Keywords...),
		GroupId:         strings.TrimSpace(input.GroupId),
		Running:         false,
		DebugPort:       0,
		Pid:             0,
		LastError:       "",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if hasSelectedProxy {
		_ = BindProfileToProxy(profile, selectedProxy, true)
	}
	m.Profiles[profileId] = profile
	log.Info("浏览器配置创建", logger.F("profile_id", profileId), logger.F("profile_name", input.ProfileName))
	if err := m.SaveProfiles(); err != nil {
		return nil, err
	}
	if m.CodeProvider != nil {
		if code, err := m.CodeProvider.EnsureCode(profile.ProfileId); err == nil {
			profile.LaunchCode = code
		}
	}
	return profile, nil
}

// RandomizeFingerprint 重新生成指纹：随机种子 + 随机基础身份（platform/lang/timezone/分辨率/CPU/内存/GPU/字体）
// 仅保留「未识别参数」（unknownArgs）以避免丢失用户自定义的额外开关。
// 启动参数 LaunchArgs 等其它配置不受影响。
func (m *Manager) RandomizeFingerprint(profileId string) (*Profile, error) {
	log := logger.New("Browser")
	m.InitData()
	m.Mutex.Lock()
	defer m.Mutex.Unlock()
	profile, exists := m.Profiles[profileId]
	if !exists {
		return nil, fmt.Errorf("profile not found")
	}
	// 剥离已有的「基础身份 + 种子」相关字段，保留未识别字段
	stripPrefixes := []string{
		"--fingerprint=",
		"--fingerprint-brand=",
		"--fingerprint-platform=",
		"--lang=",
		"--timezone=",
		"--window-size=",
		"--fingerprint-color-depth=",
		"--fingerprint-hardware-concurrency=",
		"--fingerprint-device-memory=",
		"--fingerprint-canvas-noise=",
		"--fingerprint-audio-noise=",
		"--fingerprint-touch-points=",
		"--fingerprint-fonts=",
		"--webrtc-ip-handling-policy=",
		"--fingerprint-webgl-vendor=",
		"--fingerprint-webgl-renderer=",
	}
	rest := make([]string, 0, len(profile.FingerprintArgs))
	for _, a := range profile.FingerprintArgs {
		la := strings.ToLower(strings.TrimSpace(a))
		drop := false
		for _, p := range stripPrefixes {
			if strings.HasPrefix(la, p) {
				drop = true
				break
			}
		}
		if drop {
			continue
		}
		rest = append(rest, a)
	}
	// 注入新的随机身份 + 新种子
	rest = append(rest, RandomFingerprintIdentity()...)
	newSeed := fmt.Sprintf("--fingerprint=%d", rand.Int31n(2147483647)+1)
	profile.FingerprintArgs = append(rest, newSeed)
	profile.UpdatedAt = time.Now().Format(time.RFC3339)
	if err := m.SaveProfiles(); err != nil {
		return nil, err
	}
	log.Info("指纹已重新随机", logger.F("profile_id", profileId), logger.F("seed", newSeed))
	return profile, nil
}

// Update 更新配置
func (m *Manager) Update(profileId string, input ProfileInput) (*Profile, error) {
	log := logger.New("Browser")
	m.InitData()
	m.Mutex.Lock()
	defer m.Mutex.Unlock()
	profile, exists := m.Profiles[profileId]
	if !exists {
		log.Error("浏览器配置不存在", logger.F("profile_id", profileId))
		return nil, fmt.Errorf("profile not found")
	}
	profile.ProfileName = input.ProfileName
	profile.UserDataDir = input.UserDataDir
	profile.CoreId = normalizeProfileCoreID(input.CoreId)
	profile.FingerprintArgs = input.FingerprintArgs
	profile.ProxyId = strings.TrimSpace(input.ProxyId)
	if profile.ProxyId != "" {
		if proxyItem, ok := m.GetProxyByID(profile.ProxyId); ok {
			_ = BindProfileToProxy(profile, proxyItem, true)
		} else {
			log.Error("代理绑定失败", logger.F("profile_id", profileId), logger.F("proxy_id", profile.ProxyId))
		}
	} else {
		profile.ProxyConfig = input.ProxyConfig
		_ = ClearProfileProxyBinding(profile)
	}
	profile.LaunchArgs = input.LaunchArgs
	profile.Tags = input.Tags
	profile.Keywords = append([]string{}, input.Keywords...)
	profile.GroupId = strings.TrimSpace(input.GroupId)
	profile.UpdatedAt = time.Now().Format(time.RFC3339)
	log.Info("浏览器配置更新", logger.F("profile_id", profileId), logger.F("profile_name", input.ProfileName))
	if err := m.SaveProfiles(); err != nil {
		return nil, err
	}
	return profile, nil
}

// Delete 删除配置（默认只删除配置记录，不删除浏览器缓存/用户数据目录）
func (m *Manager) Delete(profileId string) error {
	return m.DeleteWithCache(profileId, false)
}

// DeleteWithCache 删除配置；deleteCache=true 时同时删除该实例的用户数据目录。
func (m *Manager) DeleteWithCache(profileId string, deleteCache bool) error {
	log := logger.New("Browser")
	m.InitData()
	m.Mutex.Lock()
	defer m.Mutex.Unlock()
	profile, exists := m.Profiles[profileId]
	if !exists {
		log.Error("浏览器配置不存在", logger.F("profile_id", profileId))
		return fmt.Errorf("profile not found")
	}

	if deleteCache {
		if profile.Running {
			return fmt.Errorf("请先停止实例再删除缓存文件")
		}
		if cmd, ok := m.BrowserProcesses[profileId]; ok && cmd != nil && cmd.Process != nil {
			return fmt.Errorf("请先停止实例再删除缓存文件")
		}
		userDataDir := m.ResolveUserDataDir(profile)
		if err := validateUserDataDirForDelete(userDataDir, m.ResolveRelativePath(strings.TrimSpace(m.Config.Browser.UserDataRoot))); err != nil {
			return err
		}
		if err := os.RemoveAll(userDataDir); err != nil {
			return fmt.Errorf("删除缓存文件失败: %w", err)
		}
		log.Info("浏览器缓存目录删除", logger.F("profile_id", profileId), logger.F("user_data_dir", userDataDir))
	}

	delete(m.Profiles, profileId)
	log.Info("浏览器配置删除", logger.F("profile_id", profileId), logger.F("delete_cache", deleteCache))

	// DAO 删除
	if m.ProfileDAO != nil {
		if err := m.ProfileDAO.Delete(profileId); err != nil {
			log.Error("数据库删除实例失败", logger.F("profile_id", profileId), logger.F("error", err))
			return err
		}
	} else {
		if err := m.SaveProfiles(); err != nil {
			return err
		}
	}

	if m.CodeProvider != nil {
		_ = m.CodeProvider.Remove(profileId)
	}
	return nil
}

func validateUserDataDirForDelete(userDataDir string, dataRoot string) error {
	userDataDir = strings.TrimSpace(userDataDir)
	if userDataDir == "" {
		return fmt.Errorf("用户数据目录为空，已取消删除缓存")
	}
	absDir, err := filepath.Abs(userDataDir)
	if err != nil {
		return fmt.Errorf("用户数据目录无效: %w", err)
	}
	cleanDir := filepath.Clean(absDir)
	volume := filepath.VolumeName(cleanDir)
	if cleanDir == string(filepath.Separator) || cleanDir == volume+string(filepath.Separator) {
		return fmt.Errorf("用户数据目录指向磁盘根目录，已取消删除缓存")
	}
	if strings.TrimSpace(dataRoot) != "" {
		if absRoot, err := filepath.Abs(dataRoot); err == nil {
			cleanRoot := filepath.Clean(absRoot)
			if samePath(cleanDir, cleanRoot) {
				return fmt.Errorf("用户数据目录指向数据根目录，已取消删除缓存")
			}
		}
	}
	return nil
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// ApplyDefaults 应用默认配置；返回 true 表示发生了需要持久化的变更
func (m *Manager) ApplyDefaults(profile *Profile) bool {
	log := logger.New("Browser")
	changed := false
	if profile.FingerprintArgs == nil || len(profile.FingerprintArgs) == 0 {
		profile.FingerprintArgs = append([]string{}, m.Config.Browser.DefaultFingerprintArgs...)
		changed = true
	}
	// 自动补全指纹随机种子（兼容历史实例：之前只写了 brand/platform 开关，
	// 缺 --fingerprint=<seed>，导致所有实例 UI/DB 上看起来一样）
	hasSeed := false
	for _, a := range profile.FingerprintArgs {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(a)), "--fingerprint=") {
			hasSeed = true
			break
		}
	}
	if !hasSeed {
		seed := fmt.Sprintf("--fingerprint=%d", rand.Int31n(2147483647)+1)
		profile.FingerprintArgs = append(profile.FingerprintArgs, seed)
		changed = true
		log.Debug("自动生成指纹种子", logger.F("profile_id", profile.ProfileId), logger.F("seed", seed))
	}
	if !HasArgPrefix(profile.FingerprintArgs, "--fingerprint-brand-version=") {
		profile.FingerprintArgs = append(profile.FingerprintArgs, RandomBrandVersionArg())
		changed = true
	}
	if !HasArgPrefix(profile.FingerprintArgs, "--fingerprint-platform-version=") {
		platform := PlatformFromArgs(profile.FingerprintArgs)
		if platform == "" {
			platform = "windows"
		}
		profile.FingerprintArgs = append(profile.FingerprintArgs, RandomPlatformVersionArg(platform))
		changed = true
	}
	if profile.LaunchArgs == nil || len(profile.LaunchArgs) == 0 {
		profile.LaunchArgs = append([]string{}, m.Config.Browser.DefaultLaunchArgs...)
	}
	if strings.TrimSpace(profile.UserDataDir) == "" {
		profile.UserDataDir = profile.ProfileId
	}
	profile.CoreId = normalizeProfileCoreID(profile.CoreId)
	if profile.CoreId == "" {
		if defaultCore, ok := m.GetDefaultCore(); ok {
			profile.CoreId = defaultCore.CoreId
		}
	}
	proxyChanged := false
	bindChanged, boundInPool, bindMode := m.ResolveProfileProxyBinding(profile)
	if bindChanged {
		proxyChanged = true
	}
	if bindMode != "" && bindMode != "proxy_id" {
		log.Info("实例代理自动重关联",
			logger.F("profile_id", profile.ProfileId),
			logger.F("proxy_id", profile.ProxyId),
			logger.F("mode", bindMode),
		)
	}
	if profile.ProxyId != "" && !boundInPool {
		if strings.TrimSpace(profile.ProxyConfig) == "" {
			log.Error("实例代理未找到", logger.F("profile_id", profile.ProfileId), logger.F("proxy_id", profile.ProxyId))
		} else {
			log.Warn("实例代理未找到，回退使用历史代理配置", logger.F("profile_id", profile.ProfileId), logger.F("proxy_id", profile.ProxyId))
		}
	}
	if profile.ProxyConfig == "" && m.Config.Browser.DefaultProxy != "" {
		profile.ProxyConfig = m.Config.Browser.DefaultProxy
		proxyChanged = true
	}
	return proxyChanged || changed
}

// Copy 复制实例配置（除指纹参数外全部复制，指纹使用默认值生成新种子）
func (m *Manager) Copy(profileId string, newName string) (*Profile, error) {
	log := logger.New("Browser")
	m.InitData()
	m.Mutex.Lock()
	defer m.Mutex.Unlock()

	// 实例数量上限已取消（不再受 MaxProfileLimit 约束）

	src, exists := m.Profiles[profileId]
	if !exists {
		log.Error("源实例不存在", logger.F("profile_id", profileId))
		return nil, fmt.Errorf("profile not found")
	}

	now := time.Now().Format(time.RFC3339)
	newId := uuid.NewString()

	// 处理名称
	profileName := strings.TrimSpace(newName)
	if profileName == "" {
		profileName = src.ProfileName + " (副本)"
	}

	// 复制配置：保留代理/标签/启动参数，但指纹基础身份重新生成，避免副本 UA/UA-CH 沿用源实例。
	newSeed := fmt.Sprintf("--fingerprint=%d", rand.Int31n(2147483647)+1)
	fpArgs := append(RandomFingerprintIdentityForPlatform(PlatformFromArgs(src.FingerprintArgs)), newSeed)
	profile := &Profile{
		ProfileId:          newId,
		ProfileName:        profileName,
		UserDataDir:        newId, // 新的用户数据目录
		CoreId:             normalizeProfileCoreID(src.CoreId),
		FingerprintArgs:    fpArgs,
		ProxyId:            src.ProxyId,
		ProxyConfig:        src.ProxyConfig,
		ProxyBindSourceID:  src.ProxyBindSourceID,
		ProxyBindSourceURL: src.ProxyBindSourceURL,
		ProxyBindName:      src.ProxyBindName,
		ProxyBindUpdatedAt: src.ProxyBindUpdatedAt,
		LaunchArgs:         append([]string{}, src.LaunchArgs...),
		LastTabs:           append([]string{}, src.LastTabs...),
		Tags:               append([]string{}, src.Tags...),
		Keywords:           append([]string{}, src.Keywords...),
		GroupId:            src.GroupId, // 复制分组
		Running:            false,
		DebugPort:          0,
		Pid:                0,
		LastError:          "",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	m.Profiles[newId] = profile
	log.Info("实例复制成功", logger.F("src_id", profileId), logger.F("new_id", newId), logger.F("new_name", profileName))

	if err := m.SaveProfiles(); err != nil {
		return nil, err
	}

	if m.CodeProvider != nil {
		if code, err := m.CodeProvider.EnsureCode(profile.ProfileId); err == nil {
			profile.LaunchCode = code
		}
	}

	return profile, nil
}

// SetKeywords 设置实例关键字（独立接口，不影响其他字段）
func (m *Manager) SetKeywords(profileId string, keywords []string) (*Profile, error) {
	log := logger.New("Browser")
	m.InitData()
	m.Mutex.Lock()
	defer m.Mutex.Unlock()
	profile, exists := m.Profiles[profileId]
	if !exists {
		return nil, fmt.Errorf("profile not found")
	}
	profile.Keywords = append([]string{}, keywords...)
	profile.UpdatedAt = time.Now().Format(time.RFC3339)
	log.Info("关键字更新", logger.F("profile_id", profileId))
	if err := m.SaveProfiles(); err != nil {
		return nil, err
	}
	return profile, nil
}

// copyKeywords 深拷贝 keywords map
func copyKeywords(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
