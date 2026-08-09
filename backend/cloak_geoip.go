package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"boost-browser/backend/internal/logger"

	"golang.org/x/net/proxy"
)

// cloakGeoLookupTimeout 是 geoip 查询的硬超时。
// 代理质量参差不齐，超时太长会让浏览器启动卡 5+ 秒。
const cloakGeoLookupTimeout = 4 * time.Second

// cloakGeoEndpoint 用 ipapi.co，返回 timezone + country_code + languages，
// 全部 HTTPS，无需鉴权，IPv4/IPv6 都会自动用 caller 的源 IP。
const cloakGeoEndpoint = "https://ipapi.co/json/"

// cloakGeoIPResult 是 ipapi.co 的部分字段，只解我们需要的。
type cloakGeoIPResult struct {
	Country   string `json:"country_code"`   // "DE"
	Timezone  string `json:"timezone"`       // "Europe/Berlin"
	Languages string `json:"languages"`      // "de-DE,en-DE,en"
	IP        string `json:"ip"`
	Error     bool   `json:"error,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// resolveCloakGeoArgs 通过 effectiveProxy 反推 timezone/locale。
//   - effectiveProxy 是已 bridge 解过的本地 SOCKS5/HTTP 代理 URL
//     （形如 socks5://127.0.0.1:NN 或 http://user:pass@host:port，或空）
//   - 空代理 / direct:// 直接返回空切片，让浏览器走系统时区+语言
//   - 查询失败也返回空切片，绝不阻塞启动；调用方继续走默认路径
func resolveCloakGeoArgs(effectiveProxy string) []string {
	log := logger.New("CloakGeoIP")
	if !shouldQueryGeoIP(effectiveProxy) {
		log.Debug("无代理或 direct 直连，跳过 geoip 反推",
			logger.F("proxy", effectiveProxy),
		)
		return nil
	}

	httpClient, err := newProxiedHTTPClient(effectiveProxy)
	if err != nil {
		log.Warn("代理 HTTP client 构造失败，跳过 geoip 反推（非致命）",
			logger.F("proxy", effectiveProxy),
			logger.F("error", err.Error()),
		)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), cloakGeoLookupTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cloakGeoEndpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "BoostBrowser/CloakGeoIP")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Warn("geoip 查询失败（非致命）",
			logger.F("proxy", effectiveProxy),
			logger.F("error", err.Error()),
		)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Warn("geoip 查询返回非 200（非致命）",
			logger.F("proxy", effectiveProxy),
			logger.F("status", resp.StatusCode),
		)
		return nil
	}

	var data cloakGeoIPResult
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Warn("geoip 响应解析失败（非致命）",
			logger.F("error", err.Error()),
		)
		return nil
	}
	if data.Error || strings.TrimSpace(data.Timezone) == "" {
		log.Warn("geoip 响应缺少 timezone（非致命）",
			logger.F("country", data.Country),
			logger.F("reason", data.Reason),
		)
		return nil
	}

	// 时区 + 语言都跟随代理 IP 反推：避免 fingerprint.com / Cloudflare 等
	// 风控按"VPN: timezone mismatch / accept-language mismatch"打分。
	//   - --fingerprint-timezone=<IANA>   覆写 Intl/Date 时区
	//   - --lang=<primary>                覆写 navigator.language + Accept-Language 头
	//   - --accept-lang=<list>            兜底覆写 Accept-Language（部分 Chromium 用这个）
	args := []string{"--fingerprint-timezone=" + data.Timezone}

	primary := primaryLanguage(data.Languages)
	if primary == "" {
		// ipapi 偶尔返回空 languages 字段，回退到国家码推断
		primary = languageFromCountry(data.Country)
	}
	if primary != "" {
		args = append(args,
			"--lang="+primary,
			"--accept-lang="+buildAcceptLanguageHeader(primary, data.Languages),
		)
	}

	log.Info("geoip 反推成功（timezone + language）",
		logger.F("proxy_ip", data.IP),
		logger.F("country", data.Country),
		logger.F("timezone", data.Timezone),
		logger.F("language", primary),
	)
	return args
}

// languageFromCountry 在 ipapi 没返回 languages 时按国家码做兜底映射。
// 仅覆盖最常见的几个国家，其它 fallback 到 en-US。
func languageFromCountry(country string) string {
	switch strings.ToUpper(strings.TrimSpace(country)) {
	case "CN":
		return "zh-CN"
	case "TW":
		return "zh-TW"
	case "HK":
		return "zh-HK"
	case "JP":
		return "ja-JP"
	case "KR":
		return "ko-KR"
	case "DE":
		return "de-DE"
	case "FR":
		return "fr-FR"
	case "ES":
		return "es-ES"
	case "IT":
		return "it-IT"
	case "RU":
		return "ru-RU"
	case "BR", "PT":
		return "pt-BR"
	case "VN":
		return "vi-VN"
	case "TH":
		return "th-TH"
	case "ID":
		return "id-ID"
	case "TR":
		return "tr-TR"
	case "AR", "MX":
		return "es-419"
	case "GB", "UK":
		return "en-GB"
	case "US", "CA", "AU", "NZ", "SG", "IN", "PH", "MY":
		return "en-US"
	}
	return "en-US"
}

// buildAcceptLanguageHeader 构造 Accept-Language 头。
// 形如 "de-DE,de;q=0.9,en-US;q=0.8,en;q=0.7"，匹配真实浏览器在该地区的默认值。
func buildAcceptLanguageHeader(primary, raw string) string {
	primary = strings.TrimSpace(primary)
	if primary == "" {
		return "en-US,en;q=0.9"
	}
	base := primary
	if idx := strings.Index(primary, "-"); idx > 0 {
		base = primary[:idx]
	}
	// ipapi 给的 raw 形如 "de-DE,en-DE,en"，直接拿前几条做 q 衰减
	parts := []string{primary}
	if base != primary {
		parts = append(parts, base+";q=0.9")
	}
	seen := map[string]bool{strings.ToLower(primary): true, strings.ToLower(base): true}
	q := 0.8
	for _, seg := range strings.Split(raw, ",") {
		s := strings.TrimSpace(seg)
		if idx := strings.Index(s, "("); idx > 0 {
			s = strings.TrimSpace(s[:idx])
		}
		if s == "" || seen[strings.ToLower(s)] {
			continue
		}
		seen[strings.ToLower(s)] = true
		parts = append(parts, fmt.Sprintf("%s;q=%.1f", s, q))
		q -= 0.1
		if q < 0.5 {
			break
		}
	}
	// 始终保底带上英文，避免某些站点对没有 en 的 Accept-Language 行为异常
	if !seen["en-us"] && !seen["en"] {
		parts = append(parts, fmt.Sprintf("en-US;q=%.1f", q), fmt.Sprintf("en;q=%.1f", q-0.1))
	}
	return strings.Join(parts, ",")
}

// shouldQueryGeoIP 判断当前代理串是否需要走 geoip 反推。
// 直连 / 空 / direct:// 都不查询，让浏览器使用系统时区。
func shouldQueryGeoIP(effectiveProxy string) bool {
	p := strings.TrimSpace(effectiveProxy)
	if p == "" {
		return false
	}
	if strings.EqualFold(p, "direct://") {
		return false
	}
	return true
}

// newProxiedHTTPClient 把 effectiveProxy 解析成可用于 http.Client 的 transport。
// 同时支持 http/https/socks5/socks5h 四种 scheme。
func newProxiedHTTPClient(rawProxy string) (*http.Client, error) {
	parsed, err := url.Parse(rawProxy)
	if err != nil {
		return nil, fmt.Errorf("解析代理 URL 失败: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)

	transport := &http.Transport{
		ResponseHeaderTimeout: cloakGeoLookupTimeout,
		TLSHandshakeTimeout:   cloakGeoLookupTimeout,
	}

	switch scheme {
	case "http", "https":
		transport.Proxy = http.ProxyURL(parsed)
	case "socks5", "socks5h":
		var auth *proxy.Auth
		if parsed.User != nil {
			password, _ := parsed.User.Password()
			auth = &proxy.Auth{User: parsed.User.Username(), Password: password}
		}
		dialer, err := proxy.SOCKS5("tcp", parsed.Host, auth, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("SOCKS5 dialer 构造失败: %w", err)
		}
		transport.Dial = dialer.Dial
	default:
		return nil, fmt.Errorf("不支持的代理协议: %s", scheme)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   cloakGeoLookupTimeout,
	}, nil
}

// primaryLanguage 从 ipapi.co 的 languages 字段（"de-DE,en-DE,en"）取第一个。
// 如果是简短的两字母代码（"de"），保持不变；浏览器会自动识别。
func primaryLanguage(raw string) string {
	for _, segment := range strings.Split(raw, ",") {
		s := strings.TrimSpace(segment)
		if s == "" {
			continue
		}
		// 部分 ipapi 数据里语种带括号 "ar-AE (offc)"，去掉括号部分
		if idx := strings.Index(s, "("); idx > 0 {
			s = strings.TrimSpace(s[:idx])
		}
		return s
	}
	return ""
}
