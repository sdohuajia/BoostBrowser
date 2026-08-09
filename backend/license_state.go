package backend

import (
	appconfig "boost-browser/backend/internal/config"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const localLicenseStateFilename = ".boost-license.json"

type localLicenseState struct {
	MaxProfileLimit int      `json:"maxProfileLimit"`
	UsedCDKeys      []string `json:"usedCdKeys,omitempty"`
}

func localLicenseStatePath(configPath string) string {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return localLicenseStateFilename
	}
	dir := filepath.Dir(configPath)
	if dir == "." || dir == "" {
		if cwd, err := os.Getwd(); err == nil {
			dir = cwd
		}
	}
	return filepath.Join(dir, localLicenseStateFilename)
}

func loadLocalLicenseState(configPath string) (*localLicenseState, bool, error) {
	statePath := localLicenseStatePath(configPath)
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &localLicenseState{}, false, nil
		}
		return nil, false, fmt.Errorf("读取本机额度状态失败: %w", err)
	}

	var state localLicenseState
	if err := json.Unmarshal(data, &state); err != nil {
		// 状态文件损坏时回退到当前配置并在后续自动重建，避免阻断启动。
		return &localLicenseState{}, false, nil
	}
	normalizeLocalLicenseState(&state)
	return &state, true, nil
}

func saveLocalLicenseState(configPath string, state *localLicenseState) error {
	if state == nil {
		state = &localLicenseState{}
	}
	cloned := *state
	normalizeLocalLicenseState(&cloned)

	data, err := json.MarshalIndent(cloned, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化本机额度状态失败: %w", err)
	}
	if err := os.WriteFile(localLicenseStatePath(configPath), data, 0644); err != nil {
		return fmt.Errorf("写入本机额度状态失败: %w", err)
	}
	return nil
}

func reconcileConfigWithLocalLicense(configPath string, cfg *Config) (bool, bool, error) {
	if cfg == nil {
		return false, false, nil
	}

	state, stateExists, err := loadLocalLicenseState(configPath)
	if err != nil {
		return false, false, err
	}

	originalKeys := normalizeUsedCDKeys(cfg.App.UsedCDKeys)
	originalMax := cfg.App.MaxProfileLimit

	mergedKeys := unionUsedCDKeys(originalKeys, state.UsedCDKeys)
	effectiveMax := maxInt(originalMax, state.MaxProfileLimit)
	minLimit := appconfig.MinimumProfileLimitForUsedKeys(mergedKeys)
	if effectiveMax < minLimit {
		effectiveMax = minLimit
	}

	cfg.App.UsedCDKeys = mergedKeys
	cfg.App.MaxProfileLimit = effectiveMax

	configChanged := originalMax != effectiveMax || !sameStringSlice(originalKeys, mergedKeys)

	desiredState := &localLicenseState{
		MaxProfileLimit: effectiveMax,
		UsedCDKeys:      mergedKeys,
	}
	normalizeLocalLicenseState(desiredState)

	stateChanged := state.MaxProfileLimit != desiredState.MaxProfileLimit || !sameStringSlice(state.UsedCDKeys, desiredState.UsedCDKeys)
	shouldPersist := stateExists || desiredState.MaxProfileLimit > DefaultConfig().App.MaxProfileLimit || len(desiredState.UsedCDKeys) > 0
	if shouldPersist && stateChanged {
		if err := saveLocalLicenseState(configPath, desiredState); err != nil {
			return configChanged, false, err
		}
		return configChanged, true, nil
	}

	return configChanged, false, nil
}

func normalizeLocalLicenseState(state *localLicenseState) {
	if state == nil {
		return
	}
	state.UsedCDKeys = normalizeUsedCDKeys(state.UsedCDKeys)
	minLimit := appconfig.MinimumProfileLimitForUsedKeys(state.UsedCDKeys)
	if state.MaxProfileLimit < minLimit {
		state.MaxProfileLimit = minLimit
	}
}

func normalizeUsedCDKeys(keys []string) []string {
	result := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		normalized := strings.ToUpper(strings.TrimSpace(key))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func unionUsedCDKeys(primary, secondary []string) []string {
	result := make([]string, 0, len(primary)+len(secondary))
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	appendKeys := func(list []string) {
		for _, key := range normalizeUsedCDKeys(list) {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, key)
		}
	}
	appendKeys(primary)
	appendKeys(secondary)
	return result
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
