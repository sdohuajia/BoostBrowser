package proxy_test

import (
	"fmt"
	"strings"
	"testing"

	"boost-browser/backend/internal/config"
	"boost-browser/backend/internal/proxy"
)

// 模拟数据库中实际存储的 Clash YAML 格式代理配置
var testTrojanConfig = `- name: HK01|香港|x1.0
  type: trojan
  server: trojan.example.com
  port: 443
  password: example-password
  udp: true
  skip-cert-verify: true
  network: tcp`

var testVmessConfig = `- name: DE-Vmess(NL1) 1x
  type: vmess
  server: 203.0.113.55
  port: 443
  uuid: 11111111-1111-4111-8111-111111111111
  alterId: 0
  cipher: auto
  udp: true
  tls: true
  skip-cert-verify: false
  servername: vmess.example.com
  network: ws
  ws-opts:
    path: /
    headers:
      Host: vmess.example.com`

var testHysteria2Config = `- name: Hysteria Japan Pluse | 0.1x
  server: hy2.example.com
  port: 443
  sni: hy2.example.com
  up: 102400
  down: 102400
  skip-cert-verify: true
  ports: 10800-10888
  type: hysteria2
  password: example-password`

func TestProtocolDetection(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{"trojan-clash", testTrojanConfig},
		{"vmess-clash", testVmessConfig},
		{"hysteria2-clash", testHysteria2Config},
		{"socks5-direct", "socks5://127.0.0.1:1080"},
		{"http-direct", "http://127.0.0.1:7890"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.TrimSpace(tt.config)
			l := strings.ToLower(src)

			isSingBox := proxy.IsSingBoxProtocol(src)
			requiresBridge := proxy.RequiresBridge(src, nil, "")
			isDirectHTTP := strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
			isDirectSocks := strings.HasPrefix(l, "socks5://")

			fmt.Printf("\n=== %s ===\n", tt.name)
			fmt.Printf("  config前30字符: %q\n", src[:minInt(30, len(src))])
			fmt.Printf("  IsSingBoxProtocol: %v\n", isSingBox)
			fmt.Printf("  RequiresBridge:    %v\n", requiresBridge)
			fmt.Printf("  isDirectHTTP:      %v\n", isDirectHTTP)
			fmt.Printf("  isDirectSocks:     %v\n", isDirectSocks)

			// 测试 ParseProxyNode
			standardProxy, outbound, err := proxy.ParseProxyNode(src)
			fmt.Printf("  ParseProxyNode:\n")
			fmt.Printf("    standardProxy: %q\n", standardProxy)
			fmt.Printf("    outbound nil?: %v\n", outbound == nil)
			fmt.Printf("    error:         %v\n", err)

			if !isSingBox && !requiresBridge && !isDirectHTTP && !isDirectSocks {
				t.Errorf("代理配置未被任何分支识别！会走到兜底逻辑")
			}
		})
	}
}

func TestSpeedTestWithMockProxies(t *testing.T) {
	// 模拟 a.config.Browser.Proxies 的内容
	proxies := []config.BrowserProxy{
		{ProxyId: "test-trojan", ProxyName: "测试trojan", ProxyConfig: testTrojanConfig},
		{ProxyId: "test-vmess", ProxyName: "测试vmess", ProxyConfig: testVmessConfig},
		{ProxyId: "test-hysteria2", ProxyName: "测试hysteria2", ProxyConfig: testHysteria2Config},
		{ProxyId: "test-http", ProxyName: "测试http", ProxyConfig: "http://127.0.0.1:7890"},
	}

	for _, p := range proxies {
		t.Run(p.ProxyName, func(t *testing.T) {
			// 不传 xrayMgr/singboxMgr，看会走到哪个分支
			result := proxy.SpeedTest(p.ProxyId, proxies, nil, nil, nil)
			fmt.Printf("\n=== SpeedTest %s ===\n", p.ProxyName)
			fmt.Printf("  Ok:        %v\n", result.Ok)
			fmt.Printf("  LatencyMs: %d\n", result.LatencyMs)
			fmt.Printf("  Error:     %q\n", result.Error)
		})
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
