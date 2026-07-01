package backend

import (
	"boost-browser/backend/internal/logger"
	"strings"
)

type managedLaunchArgSpec struct {
	prefix     string
	takesValue bool
}

const (
	chromeTestingInfobarSuppressArg  = "--test-type"
	chromeTestingDisableInfobarsArg  = "--disable-infobars"
	defaultSearchProviderNameArg     = "--search-provider-name=Google"
	defaultSearchProviderKeywordArg  = "--search-provider-keyword=9oo91e.qjz9zk"
	defaultSearchProviderSearchArg   = "--search-provider-search-url=https://www.9oo91e.qjz9zk/search?q={searchTerms}"
	defaultSearchProviderSuggestArg  = "--search-provider-suggest-url=https://www.9oo91e.qjz9zk/complete/search?client=chrome&q={searchTerms}"
	defaultSearchProviderEncodingArg = "--search-provider-encodings=UTF-8"
)

var managedLaunchArgSpecs = []managedLaunchArgSpec{
	{prefix: "--user-data-dir", takesValue: true},
	{prefix: "--remote-debugging-port", takesValue: true},
	{prefix: "--remote-debugging-address", takesValue: true},
	{prefix: "--remote-debugging-pipe", takesValue: false},
	{prefix: "--proxy-server", takesValue: true},
	{prefix: "--user-agent", takesValue: true},
	{prefix: "--search-provider-name", takesValue: true},
	{prefix: "--search-provider-keyword", takesValue: true},
	{prefix: "--search-provider-search-url", takesValue: true},
	{prefix: "--search-provider-suggest-url", takesValue: true},
	{prefix: "--search-provider-encodings", takesValue: true},
}

var managedWindowPlacementArgSpecs = []managedLaunchArgSpec{
	{prefix: "--window-size", takesValue: true},
	{prefix: "--window-position", takesValue: true},
}

func sanitizeManagedLaunchArgs(args []string) ([]string, []string) {
	return sanitizeLaunchArgsBySpecs(args, managedLaunchArgSpecs)
}

func sanitizeManagedWindowPlacementArgs(args []string) ([]string, []string) {
	return sanitizeLaunchArgsBySpecs(args, managedWindowPlacementArgSpecs)
}

func sanitizeLaunchArgsBySpecs(args []string, specs []managedLaunchArgSpec) ([]string, []string) {
	if len(args) == 0 {
		return nil, nil
	}

	sanitized := make([]string, 0, len(args))
	removed := make([]string, 0, 4)

	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}

		spec, matched := matchLaunchArgSpec(arg, specs)
		if !matched {
			sanitized = append(sanitized, arg)
			continue
		}

		removed = appendUniqueString(removed, spec.prefix)
		if spec.takesValue && !strings.Contains(arg, "=") && i+1 < len(args) {
			next := strings.TrimSpace(args[i+1])
			if next != "" && !strings.HasPrefix(next, "-") {
				i++
			}
		}
	}

	return sanitized, removed
}

func matchManagedLaunchArg(arg string) (managedLaunchArgSpec, bool) {
	return matchLaunchArgSpec(arg, managedLaunchArgSpecs)
}

func matchLaunchArgSpec(arg string, specs []managedLaunchArgSpec) (managedLaunchArgSpec, bool) {
	for _, spec := range specs {
		if strings.EqualFold(arg, spec.prefix) || strings.HasPrefix(strings.ToLower(arg), strings.ToLower(spec.prefix)+"=") {
			return spec, true
		}
	}
	return managedLaunchArgSpec{}, false
}

func logManagedLaunchArgOverrides(log *logger.Logger, profileId string, source string, managedArgs []string) {
	if log == nil || len(managedArgs) == 0 {
		return
	}
	log.Warn("忽略由系统接管的浏览器启动参数",
		logger.F("profile_id", profileId),
		logger.F("source", source),
		logger.F("managed_args", managedArgs),
	)
}

func appendUniqueString(items []string, value string) []string {
	for _, item := range items {
		if strings.EqualFold(item, value) {
			return items
		}
	}
	return append(items, value)
}

// appendChromeTestingInfobarSuppressArg 追加 Chrome for Testing 的 infobar 抑制参数。
//
// 同时加 --test-type 和 --disable-infobars 来压两条 infobar：
//   - "您使用的是不受支持的命令行标记: --no-sandbox" 黄色安全警告
//   - Chrome for Testing 自带的 non-closeable infobar
//
// cloak 内核也走同一路径：用户已接受 fingerprint.com 把 Bot type 识别成
// "google"（来源 --test-type）的 3 红灯，infobar 用户体验更重要。
// 真实业务站（CF Turnstile 等）不看 --test-type，对通过率无影响。
//
// 参数 cloakOnly 保留用于以后可能的差异化处理，当前逻辑两条路径相同。
func appendChromeTestingInfobarSuppressArg(args []string, cloakOnly bool) []string {
	args = appendLaunchArgIfMissing(args, chromeTestingInfobarSuppressArg)
	args = appendLaunchArgIfMissing(args, chromeTestingDisableInfobarsArg)
	return args
}

func appendDefaultSearchProviderLaunchArgs(args []string, enabled bool) []string {
	if !enabled {
		return args
	}
	args = append(args,
		defaultSearchProviderNameArg,
		defaultSearchProviderKeywordArg,
		defaultSearchProviderSearchArg,
		defaultSearchProviderSuggestArg,
		defaultSearchProviderEncodingArg,
	)
	return args
}

func appendLaunchArgIfMissing(args []string, want string) []string {
	for _, arg := range args {
		if strings.EqualFold(launchArgKey(arg), want) {
			return args
		}
	}
	return append(args, want)
}
