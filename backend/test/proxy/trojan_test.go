package proxy_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	"boost-browser/backend/internal/proxy"
)

func TestTrojanClashYAML(t *testing.T) {
	node := `- name: JP02|日本|x1.0
  type: trojan
  server: trojan.example.com
  port: 443
  password: example-password
  udp: true
  skip-cert-verify: true
  network: tcp`

	standard, outbound, err := proxy.ParseProxyNode(node)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if standard != "" {
		t.Fatalf("期望 outbound，得到 standard: %s", standard)
	}
	data, _ := json.MarshalIndent(outbound, "", "  ")
	t.Logf("trojan clash outbound:\n%s", string(data))

	if outbound["protocol"] != "trojan" {
		t.Errorf("protocol 期望 trojan，得到 %v", outbound["protocol"])
	}
	settings := outbound["settings"].(map[string]interface{})
	if settings["address"] != "trojan.example.com" {
		t.Errorf("address 不匹配: %v", settings["address"])
	}
	if settings["password"] != "example-password" {
		t.Errorf("password 不匹配: %v", settings["password"])
	}
	stream := outbound["streamSettings"].(map[string]interface{})
	if stream["security"] != "tls" {
		t.Errorf("security 期望 tls，得到 %v", stream["security"])
	}
	tls := stream["tlsSettings"].(map[string]interface{})
	if tls["allowInsecure"] != true {
		t.Errorf("allowInsecure 期望 true，得到 %v", tls["allowInsecure"])
	}
}

func TestTrojanURI(t *testing.T) {
	node := "trojan://mypassword@example.com:443?sni=example.com&allowInsecure=1"
	_, outbound, err := proxy.ParseProxyNode(node)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if outbound["protocol"] != "trojan" {
		t.Errorf("protocol 期望 trojan，得到 %v", outbound["protocol"])
	}
	settings := outbound["settings"].(map[string]interface{})
	if settings["address"] != "example.com" {
		t.Errorf("address 不匹配: %v", settings["address"])
	}
	if settings["password"] != "mypassword" {
		t.Errorf("password 不匹配: %v", settings["password"])
	}
}

func TestSSClashYAML(t *testing.T) {
	node := `- name: SS节点
  type: ss
  server: 1.2.3.4
  port: 8388
  cipher: aes-256-gcm
  password: testpassword`

	_, outbound, err := proxy.ParseProxyNode(node)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	data, _ := json.MarshalIndent(outbound, "", "  ")
	t.Logf("SS clash outbound:\n%s", string(data))

	if outbound["protocol"] != "shadowsocks" {
		t.Errorf("protocol 期望 shadowsocks，得到 %v", outbound["protocol"])
	}
	settings := outbound["settings"].(map[string]interface{})
	if settings["address"] != "1.2.3.4" {
		t.Errorf("address 不匹配: %v", settings["address"])
	}
	if settings["method"] != "aes-256-gcm" {
		t.Errorf("method 不匹配: %v", settings["method"])
	}
	if settings["password"] != "testpassword" {
		t.Errorf("password 不匹配: %v", settings["password"])
	}
}

func TestSSURI_SIP002(t *testing.T) {
	// SIP002: ss://BASE64(method:password)@host:port#name
	userInfo := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:mypassword"))
	node := fmt.Sprintf("ss://%s@1.2.3.4:8388#测试节点", userInfo)
	_, outbound, err := proxy.ParseProxyNode(node)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	data, _ := json.MarshalIndent(outbound, "", "  ")
	t.Logf("SS SIP002 outbound:\n%s", string(data))

	settings := outbound["settings"].(map[string]interface{})
	if settings["method"] != "aes-256-gcm" {
		t.Errorf("method 不匹配: %v", settings["method"])
	}
	if settings["password"] != "mypassword" {
		t.Errorf("password 不匹配: %v", settings["password"])
	}
	if settings["address"] != "1.2.3.4" {
		t.Errorf("address 不匹配: %v", settings["address"])
	}
}

func TestSSURI_Legacy(t *testing.T) {
	// 旧格式: ss://BASE64(method:password@host:port)
	raw := base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pass123@2.3.4.5:443"))
	node := "ss://" + raw
	_, outbound, err := proxy.ParseProxyNode(node)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	settings := outbound["settings"].(map[string]interface{})
	if settings["method"] != "chacha20-ietf-poly1305" {
		t.Errorf("method 不匹配: %v", settings["method"])
	}
	if settings["address"] != "2.3.4.5" {
		t.Errorf("address 不匹配: %v", settings["address"])
	}
	t.Logf("SS legacy outbound OK: %v", settings)
}

func TestSSR_Unsupported(t *testing.T) {
	node := "ssr://somebase64data"
	_, _, err := proxy.ParseProxyNode(node)
	if err == nil {
		t.Fatal("期望 SSR 返回错误，但没有")
	}
	t.Logf("SSR 正确返回错误: %v", err)
}

func TestHysteria2URI(t *testing.T) {
	node := "hysteria2://mypassword@example.com:443?sni=example.com&insecure=1"
	outbound, err := proxy.BuildSingBoxOutbound(node)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	data, _ := json.MarshalIndent(outbound, "", "  ")
	t.Logf("hysteria2 URI outbound:\n%s", string(data))

	if outbound["type"] != "hysteria2" {
		t.Errorf("type 期望 hysteria2，得到 %v", outbound["type"])
	}
	if outbound["server"] != "example.com" {
		t.Errorf("server 不匹配: %v", outbound["server"])
	}
	if outbound["password"] != "mypassword" {
		t.Errorf("password 不匹配: %v", outbound["password"])
	}
	tls := outbound["tls"].(map[string]interface{})
	if tls["insecure"] != true {
		t.Errorf("insecure 期望 true，得到 %v", tls["insecure"])
	}
}

func TestHysteria2ClashYAML(t *testing.T) {
	node := `- name: HY2节点
  type: hysteria2
  server: example.com
  port: 443
  password: testpass
  sni: example.com
  skip-cert-verify: false`

	outbound, err := proxy.BuildSingBoxOutbound(node)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	data, _ := json.MarshalIndent(outbound, "", "  ")
	t.Logf("hysteria2 clash outbound:\n%s", string(data))

	if outbound["type"] != "hysteria2" {
		t.Errorf("type 期望 hysteria2，得到 %v", outbound["type"])
	}
	if outbound["server"] != "example.com" {
		t.Errorf("server 不匹配: %v", outbound["server"])
	}
	tls := outbound["tls"].(map[string]interface{})
	if tls["server_name"] != "example.com" {
		t.Errorf("server_name 不匹配: %v", tls["server_name"])
	}
}

func TestIsSingBoxProtocol(t *testing.T) {
	cases := []struct {
		input    string
		expected bool
	}{
		{"hysteria2://pass@host:443", true},
		{"hysteria://pass@host:443", true},
		{"- name: n\n  type: hysteria2\n  server: h\n  port: 443", true},
		{"- name: n\n  type: tuic\n  server: h\n  port: 443", true},
		{"vmess://xxx", false},
		{"trojan://pass@host:443", false},
		{"socks5://127.0.0.1:1080", false},
	}
	for _, c := range cases {
		got := proxy.IsSingBoxProtocol(c.input)
		if got != c.expected {
			t.Errorf("IsSingBoxProtocol(%q) = %v, 期望 %v", c.input[:min(30, len(c.input))], got, c.expected)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
