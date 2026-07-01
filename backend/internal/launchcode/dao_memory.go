package launchcode

import (
	"fmt"
	"sync"
)

// MemoryLaunchCodeDAO 基于内存的 LaunchCodeDAO 实现，仅用于测试
type MemoryLaunchCodeDAO struct {
	mu            sync.RWMutex
	profileToCode map[string]string
	codeToProfile map[string]string
}

// NewMemoryLaunchCodeDAO 创建内存 DAO
func NewMemoryLaunchCodeDAO() *MemoryLaunchCodeDAO {
	return &MemoryLaunchCodeDAO{
		profileToCode: make(map[string]string),
		codeToProfile: make(map[string]string),
	}
}

func (d *MemoryLaunchCodeDAO) FindProfileId(code string) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	profileId, ok := d.codeToProfile[code]
	if !ok {
		return "", fmt.Errorf("launch code not found: %s", code)
	}
	return profileId, nil
}

func (d *MemoryLaunchCodeDAO) FindCode(profileId string) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	code, ok := d.profileToCode[profileId]
	if !ok {
		return "", fmt.Errorf("profile not found: %s", profileId)
	}
	return code, nil
}

func (d *MemoryLaunchCodeDAO) Upsert(profileId, code string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	// 清理旧 code 的反向映射
	if oldCode, ok := d.profileToCode[profileId]; ok {
		delete(d.codeToProfile, oldCode)
	}
	d.profileToCode[profileId] = code
	d.codeToProfile[code] = profileId
	return nil
}

func (d *MemoryLaunchCodeDAO) Delete(profileId string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if code, ok := d.profileToCode[profileId]; ok {
		delete(d.codeToProfile, code)
		delete(d.profileToCode, profileId)
	}
	return nil
}

func (d *MemoryLaunchCodeDAO) LoadAll() (map[string]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make(map[string]string, len(d.profileToCode))
	for k, v := range d.profileToCode {
		result[k] = v
	}
	return result, nil
}
