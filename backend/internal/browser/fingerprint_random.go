package browser

import (
	"fmt"
	"math/rand"
	"strings"
)

// fpFamily 一个内部连贯的指纹身份家族（同一 platform 下选项相互兼容）
type fpFamily struct {
	platform       string
	platformVers   []string
	brandVers      []string
	langs          []string
	timezones      []string
	resolutions    []string
	cores          []string
	memories       []string
	colorDepths    []string
	webglVendors   []string
	webglRenderers map[string][]string
	fontsList      []string
	touchPoints    []string
}

var fpFamilies = []fpFamily{
	{
		platform:     "windows",
		platformVers: []string{"10.0.0", "15.0.0"},
		brandVers:    []string{"146.0.7680.177", "146.0.7680.168", "146.0.7680.156", "146.0.7679.125", "146.0.7678.101"},
		langs:        []string{"en-US", "en-GB", "zh-CN", "ja-JP", "ko-KR", "fr-FR", "de-DE"},
		timezones:    []string{"America/New_York", "America/Los_Angeles", "America/Chicago", "Europe/London", "Europe/Paris", "Europe/Berlin", "Asia/Shanghai", "Asia/Tokyo", "Asia/Seoul"},
		resolutions:  []string{"1920,1080", "1366,768", "1440,900", "1600,900", "2560,1440", "1280,800"},
		cores:        []string{"4", "6", "8", "12", "16"},
		memories:     []string{"4", "8", "16", "32"},
		colorDepths:  []string{"24", "30"},
		webglVendors: []string{"Intel", "NVIDIA", "AMD"},
		webglRenderers: map[string][]string{
			"Intel":  {"Intel(R) UHD Graphics 630", "Intel(R) UHD Graphics 620", "Intel(R) HD Graphics 520", "Intel(R) Iris(R) Xe Graphics"},
			"NVIDIA": {"NVIDIA GeForce RTX 3080", "NVIDIA GeForce RTX 3060", "NVIDIA GeForce GTX 1660", "NVIDIA GeForce GTX 1080 Ti"},
			"AMD":    {"AMD Radeon RX 6600", "AMD Radeon RX 580", "AMD Radeon Vega 8"},
		},
		fontsList: []string{
			"Arial,Helvetica,Times New Roman,Courier New,Verdana,Georgia,Tahoma",
			"Arial,Microsoft YaHei,SimSun,SimHei,Helvetica,Times New Roman",
			"Arial,Helvetica,Calibri,Cambria,Consolas,Times New Roman",
		},
		touchPoints: []string{"0"},
	},
	{
		platform:     "mac",
		platformVers: []string{"14.0.0", "15.0.0"},
		brandVers:    []string{"146.0.7680.177", "146.0.7680.168", "146.0.7679.125"},
		langs:        []string{"en-US", "zh-CN", "ja-JP", "fr-FR"},
		timezones:    []string{"America/New_York", "America/Los_Angeles", "Europe/London", "Asia/Shanghai", "Asia/Tokyo"},
		resolutions:  []string{"1440,900", "2560,1440", "1920,1080", "2880,1800"},
		cores:        []string{"8", "10", "12", "16"},
		memories:     []string{"8", "16", "32"},
		colorDepths:  []string{"24", "30"},
		webglVendors: []string{"Apple", "Intel"},
		webglRenderers: map[string][]string{
			"Apple": {"Apple M1", "Apple M2", "Apple M3"},
			"Intel": {"Intel(R) Iris(R) Plus Graphics", "Intel(R) UHD Graphics 630"},
		},
		fontsList: []string{
			"Arial,Helvetica,PingFang SC,Hiragino Sans GB,STHeiti,Times New Roman",
			"Arial,Helvetica Neue,Lucida Grande,Times New Roman,Courier",
			"SF Pro Display,Helvetica,Arial,Times New Roman",
		},
		touchPoints: []string{"0"},
	},
	{
		platform:     "linux",
		platformVers: []string{"6.1.0", "6.5.0", "6.8.0"},
		brandVers:    []string{"146.0.7680.177", "146.0.7680.168", "146.0.7679.125"},
		langs:        []string{"en-US", "zh-CN", "de-DE", "fr-FR"},
		timezones:    []string{"Europe/Berlin", "Europe/Paris", "America/New_York", "Asia/Shanghai"},
		resolutions:  []string{"1920,1080", "1366,768", "1600,900", "2560,1440"},
		cores:        []string{"4", "8", "12", "16"},
		memories:     []string{"4", "8", "16", "32"},
		colorDepths:  []string{"24"},
		webglVendors: []string{"Intel", "NVIDIA", "AMD"},
		webglRenderers: map[string][]string{
			"Intel":  {"Mesa Intel(R) UHD Graphics 630", "Mesa Intel(R) HD Graphics 520"},
			"NVIDIA": {"GeForce RTX 3060/PCIe/SSE2", "GeForce GTX 1660/PCIe/SSE2"},
			"AMD":    {"AMD Radeon RX 580 (POLARIS10, DRM 3.42.0, 5.15.0, LLVM 13.0.1)"},
		},
		fontsList: []string{
			"DejaVu Sans,Liberation Sans,Ubuntu,Arial,Times New Roman",
			"Noto Sans,DejaVu Sans,Arial,Helvetica,Times New Roman",
		},
		touchPoints: []string{"0"},
	},
}

func pick(rng *rand.Rand, opts []string) string {
	if len(opts) == 0 {
		return ""
	}
	return opts[rng.Intn(len(opts))]
}

// RandomFingerprintIdentity 生成一组连贯的随机身份参数（不含 --fingerprint=<seed>）
// 同一组里的 platform / GPU / 语言 / 时区 / 分辨率等互相匹配，避免出现 mac+NVIDIA 这类不真实组合。
func RandomFingerprintIdentity() []string {
	return RandomFingerprintIdentityForPlatform("")
}

func RandomFingerprintIdentityForPlatform(platform string) []string {
	rng := rand.New(rand.NewSource(rand.Int63()))
	fam := fpFamilies[rng.Intn(len(fpFamilies))]
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform != "" {
		for _, candidate := range fpFamilies {
			if candidate.platform == platform {
				fam = candidate
				break
			}
		}
	}

	vendor := pick(rng, fam.webglVendors)
	renderer := ""
	if rs, ok := fam.webglRenderers[vendor]; ok {
		renderer = pick(rng, rs)
	}

	args := []string{
		"--fingerprint-brand=Chrome",
		"--fingerprint-brand-version=" + pick(rng, fam.brandVers),
		"--fingerprint-platform=" + fam.platform,
		"--fingerprint-platform-version=" + pick(rng, fam.platformVers),
		"--lang=" + pick(rng, fam.langs),
		"--timezone=" + pick(rng, fam.timezones),
		"--window-size=" + pick(rng, fam.resolutions),
		"--fingerprint-color-depth=" + pick(rng, fam.colorDepths),
		"--fingerprint-hardware-concurrency=" + pick(rng, fam.cores),
		"--fingerprint-device-memory=" + pick(rng, fam.memories),
		"--fingerprint-canvas-noise=true",
		"--fingerprint-audio-noise=true",
		"--fingerprint-touch-points=" + pick(rng, fam.touchPoints),
		"--fingerprint-fonts=" + pick(rng, fam.fontsList),
		"--webrtc-ip-handling-policy=disable_non_proxied_udp",
	}
	if vendor != "" {
		args = append(args, "--fingerprint-webgl-vendor="+vendor)
	}
	if renderer != "" {
		args = append(args, "--fingerprint-webgl-renderer="+renderer)
	}
	return args
}

// identityKeys 已识别的「基础身份」相关 CLI 前缀
var identityKeys = []string{
	"--fingerprint-brand",
	"--fingerprint-platform",
	"--fingerprint-brand-version",
	"--fingerprint-platform-version",
	"--fingerprint-locale",
	"--lang",
	"--timezone",
	"--window-size",
	"--fingerprint-color-depth",
	"--fingerprint-hardware-concurrency",
	"--fingerprint-device-memory",
	"--fingerprint-canvas-noise",
	"--fingerprint-audio-noise",
	"--fingerprint-touch-points",
	"--fingerprint-fonts",
	"--webrtc-ip-handling-policy",
	"--fingerprint-webgl-vendor",
	"--fingerprint-webgl-renderer",
}

func StripIdentityArgs(args []string) []string {
	rest := make([]string, 0, len(args))
	for _, a := range args {
		la := strings.ToLower(strings.TrimSpace(a))
		drop := false
		for _, k := range identityKeys {
			if strings.HasPrefix(la, k+"=") {
				drop = true
				break
			}
		}
		if strings.HasPrefix(la, "--fingerprint=") {
			drop = true
		}
		if !drop {
			rest = append(rest, a)
		}
	}
	return rest
}

func HasArgPrefix(args []string, prefix string) bool {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	for _, a := range args {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(a)), prefix) {
			return true
		}
	}
	return false
}

func PlatformFromArgs(args []string) string {
	for _, a := range args {
		trimmed := strings.TrimSpace(a)
		if strings.HasPrefix(strings.ToLower(trimmed), "--fingerprint-platform=") {
			return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "--fingerprint-platform=")))
		}
	}
	return ""
}

func RandomBrandVersionArg() string {
	rng := rand.New(rand.NewSource(rand.Int63()))
	fam := fpFamilies[rng.Intn(len(fpFamilies))]
	return "--fingerprint-brand-version=" + pick(rng, fam.brandVers)
}

func RandomPlatformVersionArg(platform string) string {
	rng := rand.New(rand.NewSource(rand.Int63()))
	platform = strings.ToLower(strings.TrimSpace(platform))
	for _, fam := range fpFamilies {
		if fam.platform == platform {
			return "--fingerprint-platform-version=" + pick(rng, fam.platformVers)
		}
	}
	return "--fingerprint-platform-version=10.0.0"
}

// HasAnyIdentity 判断 args 里是否已包含基础身份字段
func HasAnyIdentity(args []string) bool {
	for _, a := range args {
		la := strings.ToLower(strings.TrimSpace(a))
		for _, k := range identityKeys {
			if strings.HasPrefix(la, k+"=") {
				return true
			}
		}
	}
	return false
}

// AddRandomSeed 给 args 追加一个新的随机 --fingerprint=<seed>（先剥离旧的）
func AddRandomSeed(args []string) []string {
	rest := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(a)), "--fingerprint=") {
			continue
		}
		rest = append(rest, a)
	}
	return append(rest, fmt.Sprintf("--fingerprint=%d", rand.Int31n(2147483647)+1))
}
