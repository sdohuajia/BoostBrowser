package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/metacubex/mihomo/adapter"
	C "github.com/metacubex/mihomo/constant"
	"gopkg.in/yaml.v3"

	"boost-browser/backend/internal/config"
	"boost-browser/backend/internal/logger"
)

// ─── Clash 标准测速 URL ───
// 使用 HTTP 与 Clash 客户端保持一致

const defaultTestURL = "http://www.gstatic.com/generate_204"

var defaultSpeedTestURLs = []string{
	"http://www.gstatic.com/generate_204",
	"http://connectivitycheck.gstatic.com/generate_204",
	"http://cp.cloudflare.com/generate_204",
	"http://www.msftconnecttest.com/connecttest.txt",
}

// SpeedTestConfig 测速参数
type SpeedTestConfig struct {
	Timeout    time.Duration
	TCPTimeout time.Duration
	URLs       []string
}

var DefaultSpeedTestConfig = SpeedTestConfig{
	Timeout:    15 * time.Second,
	TCPTimeout: 6 * time.Second,
}

// ─── 对外入口 ───

// SpeedTest 使用 mihomo 代理适配器进行测速。
// 采用 unified-delay 策略：先建立连接（预热），再单独计时 HTTP 往返，
// 与 Clash 客户端 unified-delay: true 的延迟结果一致。
func SpeedTest(
	proxyId string,
	proxies []config.BrowserProxy,
	xrayMgr *XrayManager,
	singboxMgr *SingBoxManager,
	cfg *SpeedTestConfig,
) TestResult {
	log := logger.New("SpeedTest")

	if cfg == nil {
		c := DefaultSpeedTestConfig
		cfg = &c
	}

	// 查找代理配置
	src := ""
	for _, item := range proxies {
		if strings.EqualFold(item.ProxyId, proxyId) {
			src = strings.TrimSpace(item.ProxyConfig)
			break
		}
	}
	if src == "" {
		return TestResult{ProxyId: proxyId, Ok: false, Error: "代理配置为空"}
	}

	if strings.ToLower(src) == "direct://" {
		return TestResult{ProxyId: proxyId, Ok: true, LatencyMs: 0}
	}

	testURLs := cfg.URLs
	if len(testURLs) == 0 {
		testURLs = defaultSpeedTestURLs
	}

	// 将代理配置转换为 mihomo mapping
	mapping, err := proxyConfigToMapping(src)
	if err != nil {
		log.Warn("代理配置解析失败，降级到 TCP ping",
			logger.F("proxy_id", proxyId),
			logger.F("error", err.Error()),
		)
		return tcpPingFallback(proxyId, src, cfg.TCPTimeout, log)
	}

	// 使用 mihomo adapter.ParseProxy 创建代理实例
	proxyInstance, err := adapter.ParseProxy(mapping)
	if err != nil {
		log.Warn("mihomo 代理创建失败，降级到 TCP ping",
			logger.F("proxy_id", proxyId),
			logger.F("error", err.Error()),
			logger.F("type", mapping["type"]),
		)
		return tcpPingFallback(proxyId, src, cfg.TCPTimeout, log)
	}

	// 稳定优先：多 URL、多方法、最多 2 轮重试。
	// 原来固定 HEAD gstatic + 复用同一 TCP 连接，部分代理会偶发关闭连接，导致同一个代理一会失败一会成功。
	result := robustHTTPProxyTest(proxyId, proxyInstance, testURLs, cfg.Timeout)
	if result.Ok {
		return result
	}

	// 用户批量导入时经常把 HTTP 代理按 SOCKS5 写入。
	// SOCKS5 握手收到 HTTP 响应时会出现 “unexpected protocol version 72”(72='H')。
	// 这里自动按同一 host:port 轮流尝试 http/https/socks5，避免协议填错造成假失败。
	for _, altSrc := range alternateStandardProxyConfigs(src) {
		altMapping, err := proxyConfigToMapping(altSrc)
		if err != nil {
			continue
		}
		altProxy, err := adapter.ParseProxy(altMapping)
		if err != nil {
			continue
		}
		altResult := robustHTTPProxyTest(proxyId, altProxy, testURLs, cfg.Timeout)
		if altResult.Ok {
			return altResult
		}
		if altResult.Error != "" {
			result.Error = altResult.Error
		}
	}

	// 如果目标测试站/线路偶发超时，但代理服务端口本身能连通，按端口连通给出延迟。
	// 这样不会把“测试站慢/被限流”误显示成代理完全失败。
	if fallback := tcpPingFallback(proxyId, src, cfg.TCPTimeout, log); fallback.Ok {
		return fallback
	}
	return result
}

func robustHTTPProxyTest(proxyId string, px C.Proxy, testURLs []string, timeout time.Duration) TestResult {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if len(testURLs) == 0 {
		testURLs = defaultSpeedTestURLs
	}

	lastErr := ""
	methods := []string{http.MethodHead, http.MethodGet}
	for attempt := 0; attempt < 2; attempt++ {
		for _, testURL := range testURLs {
			for _, method := range methods {
				result := singleHTTPProxyTest(proxyId, px, testURL, method, timeout)
				if result.Ok {
					return result
				}
				lastErr = result.Error
			}
		}
	}
	if lastErr == "" {
		lastErr = "代理测试失败"
	}
	return TestResult{ProxyId: proxyId, Ok: false, Error: lastErr}
}

func singleHTTPProxyTest(proxyId string, px C.Proxy, testURL string, method string, timeout time.Duration) TestResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			meta, err := addressToMeta(address)
			if err != nil {
				return nil, err
			}
			return px.DialContext(ctx, &meta)
		},
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: timeout,
		TLSHandshakeTimeout:   timeout,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, method, testURL, nil)
	if err != nil {
		return TestResult{ProxyId: proxyId, Ok: false, Error: err.Error()}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/144 Safari/537.36")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return TestResult{ProxyId: proxyId, Ok: false, LatencyMs: latency, Error: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 1024)

	// 代理连通性测试以“能通过代理拿到 HTTP 响应”为准。
	// 部分测试站会对不同出口返回 204/200/301/403，不能只认 200/204，否则会造成假失败。
	if resp.StatusCode >= 100 && resp.StatusCode < 500 {
		return TestResult{ProxyId: proxyId, Ok: true, LatencyMs: latency}
	}
	return TestResult{ProxyId: proxyId, Ok: false, LatencyMs: latency, Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}
}

func addressToMeta(address string) (C.Metadata, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return C.Metadata{}, err
	}
	port64, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return C.Metadata{}, err
	}
	meta := C.Metadata{Host: host, DstPort: uint16(port64)}
	if addr, err := netip.ParseAddr(host); err == nil {
		meta.DstIP = addr
	}
	return meta, nil
}

// unifiedDelayTest 模拟 Clash unified-delay 模式：
// 1. 通过代理建立到目标的 TCP 连接（预热，不计入延迟）
// 2. 发送第一次 HTTP 请求预热连接（不计入延迟）
// 3. 在已建立的连接上发送第二次 HTTP 请求，只计这次的 RTT
// 这样测出的延迟 = 纯 HTTP 往返时间，和 Clash unified-delay: true 一致。
func unifiedDelayTest(proxyId string, px C.Proxy, testURL string, timeout time.Duration) TestResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 解析目标地址
	addr, err := urlToMeta(testURL)
	if err != nil {
		return TestResult{ProxyId: proxyId, Ok: false, Error: fmt.Sprintf("URL 解析失败: %v", err)}
	}

	// 步骤 1：通过代理 DialContext 建立连接（预热）
	conn, err := px.DialContext(ctx, &addr)
	if err != nil {
		return TestResult{ProxyId: proxyId, Ok: false, Error: fmt.Sprintf("代理连接失败: %v", err)}
	}
	defer conn.Close()

	// 构造复用此连接的 HTTP client
	transport := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return conn, nil
		},
		DisableKeepAlives: false,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()

	// 步骤 2：第一次请求预热（不计时）
	req1, _ := http.NewRequestWithContext(ctx, http.MethodHead, testURL, nil)
	resp1, err := client.Do(req1)
	if err != nil {
		return TestResult{ProxyId: proxyId, Ok: false, Error: err.Error()}
	}
	resp1.Body.Close()

	// 步骤 3：第二次请求计时（纯 HTTP RTT）
	start := time.Now()
	req2, _ := http.NewRequestWithContext(ctx, http.MethodHead, testURL, nil)
	resp2, err := client.Do(req2)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return TestResult{ProxyId: proxyId, Ok: false, LatencyMs: latency, Error: err.Error()}
	}
	resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusNoContent {
		return TestResult{ProxyId: proxyId, Ok: false, LatencyMs: latency,
			Error: fmt.Sprintf("HTTP %d", resp2.StatusCode)}
	}

	return TestResult{ProxyId: proxyId, Ok: true, LatencyMs: latency}
}

// urlToMeta 将 URL 转换为 mihomo Metadata
func urlToMeta(rawURL string) (C.Metadata, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return C.Metadata{}, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return C.Metadata{}, fmt.Errorf("不支持的 URL scheme")
	}
	host := u.Hostname()
	if host == "" {
		return C.Metadata{}, fmt.Errorf("URL host 为空")
	}
	portNum := uint16(80)
	if u.Scheme == "https" {
		portNum = 443
	}
	if port := u.Port(); port != "" {
		port64, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return C.Metadata{}, err
		}
		portNum = uint16(port64)
	}

	meta := C.Metadata{
		Host:    host,
		DstPort: portNum,
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		meta.DstIP = addr
	}
	return meta, nil
}

// ─── 代理配置转换为 mihomo mapping ───

func proxyConfigToMapping(src string) (map[string]any, error) {
	src = strings.TrimSpace(src)
	l := strings.ToLower(src)

	// http/https 直连代理
	if strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") {
		mapping, err := parseStandardProxy(src, "http")
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(l, "https://") {
			mapping["tls"] = true
			mapping["skip-cert-verify"] = true
		}
		return mapping, nil
	}
	// socks5 直连代理
	if strings.HasPrefix(l, "socks5://") {
		return parseStandardProxy(src, "socks5")
	}

	// URI 格式（vmess:// vless:// 等）暂不支持直接转 mapping，降级
	if strings.Contains(l, "://") && !strings.Contains(l, "type:") {
		return nil, fmt.Errorf("URI 格式暂不支持: %s", l[:min(30, len(l))])
	}

	// Clash YAML 格式 → 直接解析
	return parseClashYAMLToMapping(src)
}

func parseStandardProxy(src string, proxyType string) (map[string]any, error) {
	rest := src[strings.Index(src, "://")+3:]

	var username, password, hostport string
	if atIdx := strings.LastIndex(rest, "@"); atIdx >= 0 {
		userInfo := rest[:atIdx]
		hostport = rest[atIdx+1:]
		parts := strings.SplitN(userInfo, ":", 2)
		username = parts[0]
		if len(parts) > 1 {
			password = parts[1]
		}
	} else {
		hostport = rest
	}
	hostport = strings.SplitN(hostport, "/", 2)[0]

	host, port := splitHostPort(hostport)
	if host == "" || port == 0 {
		return nil, fmt.Errorf("无法解析地址: %s", src)
	}

	mapping := map[string]any{
		"name":   "speedtest-proxy",
		"type":   proxyType,
		"server": host,
		"port":   port,
	}
	if username != "" {
		mapping["username"] = username
		mapping["password"] = password
	}
	return mapping, nil
}

func parseClashYAMLToMapping(src string) (map[string]any, error) {
	var payload interface{}
	if err := yaml.Unmarshal([]byte(src), &payload); err != nil {
		return nil, fmt.Errorf("YAML 解析失败: %v", err)
	}

	node := pickClashNode(payload)
	if node == nil {
		return nil, fmt.Errorf("无法提取 Clash 节点")
	}

	if _, ok := node["name"]; !ok {
		node["name"] = "speedtest-proxy"
	}

	return node, nil
}

func splitHostPort(hostport string) (string, int) {
	if strings.HasPrefix(hostport, "[") {
		if idx := strings.LastIndex(hostport, "]:"); idx >= 0 {
			host := hostport[1:idx]
			port := 0
			fmt.Sscanf(hostport[idx+2:], "%d", &port)
			return host, port
		}
		return strings.Trim(hostport, "[]"), 0
	}
	idx := strings.LastIndex(hostport, ":")
	if idx < 0 {
		return hostport, 0
	}
	host := hostport[:idx]
	port := 0
	fmt.Sscanf(hostport[idx+1:], "%d", &port)
	return host, port
}

func DetectWorkingStandardProxyConfig(src string, cfg *SpeedTestConfig) (string, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return "", fmt.Errorf("代理配置为空")
	}
	if cfg == nil {
		c := DefaultSpeedTestConfig
		cfg = &c
	}
	testURLs := cfg.URLs
	if len(testURLs) == 0 {
		testURLs = defaultSpeedTestURLs
	}
	candidates := append([]string{src}, alternateStandardProxyConfigs(src)...)
	lastErr := ""
	for _, candidate := range candidates {
		mapping, err := proxyConfigToMapping(candidate)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		px, err := adapter.ParseProxy(mapping)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		result := robustHTTPProxyTest("detect", px, testURLs, cfg.Timeout)
		if result.Ok {
			return candidate, nil
		}
		if result.Error != "" {
			lastErr = result.Error
		}
	}
	if lastErr == "" {
		lastErr = "代理协议探测失败"
	}
	return "", fmt.Errorf("%s", lastErr)
}

func alternateStandardProxyConfigs(src string) []string {
	trimmed := strings.TrimSpace(src)
	lower := strings.ToLower(trimmed)
	var current string
	switch {
	case strings.HasPrefix(lower, "http://"):
		current = "http"
	case strings.HasPrefix(lower, "https://"):
		current = "https"
	case strings.HasPrefix(lower, "socks5://"):
		current = "socks5"
	case strings.HasPrefix(lower, "socks://"):
		current = "socks5"
		trimmed = "socks5://" + trimmed[len("socks://"):]
	default:
		return nil
	}

	schemes := []string{"http", "https", "socks5"}
	out := make([]string, 0, len(schemes)-1)
	for _, scheme := range schemes {
		if scheme == current {
			continue
		}
		out = append(out, replaceProxyScheme(trimmed, scheme))
	}
	return out
}

func replaceProxyScheme(src string, scheme string) string {
	idx := strings.Index(src, "://")
	if idx < 0 {
		return src
	}
	return scheme + src[idx:]
}

// ─── TCP Ping 降级 ───

func tcpPingFallback(proxyId, src string, timeout time.Duration, log *logger.Logger) TestResult {
	endpoint, err := proxyEndpoint(src)
	if err != nil {
		return TestResult{ProxyId: proxyId, Ok: false, Error: fmt.Sprintf("无法解析代理地址: %v", err)}
	}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", endpoint, timeout)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return TestResult{ProxyId: proxyId, Ok: false, LatencyMs: latency, Error: fmt.Sprintf("TCP 连接失败: %v", err)}
	}
	conn.Close()
	return TestResult{ProxyId: proxyId, Ok: true, LatencyMs: latency}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
