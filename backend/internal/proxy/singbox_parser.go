package proxy

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// IsSingBoxProtocol 判断是否为 sing-box 支持的协议（hysteria2/tuic）
func IsSingBoxProtocol(proxyConfig string) bool {
	l := strings.ToLower(strings.TrimSpace(proxyConfig))
	if strings.HasPrefix(l, "hysteria2://") || strings.HasPrefix(l, "hysteria://") {
		return true
	}
	// Clash YAML 格式
	if strings.Contains(l, "type: hysteria2") || strings.Contains(l, "type:hysteria2") ||
		strings.Contains(l, "type: hysteria") || strings.Contains(l, "type:hysteria") ||
		strings.Contains(l, "type: tuic") || strings.Contains(l, "type:tuic") {
		return true
	}
	return false
}

// BuildSingBoxOutbound 解析节点配置，返回 sing-box outbound map
func BuildSingBoxOutbound(node string) (map[string]interface{}, error) {
	src := strings.TrimSpace(node)
	l := strings.ToLower(src)

	if strings.HasPrefix(l, "hysteria2://") || strings.HasPrefix(l, "hysteria://") {
		return parseHysteria2URI(src)
	}

	// Clash YAML 格式
	if strings.Contains(l, "type:") || strings.Contains(l, "proxies:") {
		return parseClashSingBoxNode(src)
	}

	return nil, fmt.Errorf("不支持的 sing-box 节点格式")
}

// parseHysteria2URI 解析 hysteria2:// URI
// 格式: hysteria2://password@host:port?sni=xxx&insecure=1
func parseHysteria2URI(node string) (map[string]interface{}, error) {
	// 统一为 hysteria2://
	if strings.HasPrefix(strings.ToLower(node), "hysteria://") {
		node = "hysteria2://" + node[len("hysteria://"):]
	}

	u, err := url.Parse(node)
	if err != nil {
		return nil, fmt.Errorf("hysteria2 URI 解析失败: %v", err)
	}

	host := u.Hostname()
	portStr := u.Port()
	port, _ := strconv.Atoi(portStr)
	password := u.User.Username()
	if password == "" {
		// 有些格式把密码放在 userinfo 里不带 @
		password = strings.TrimPrefix(u.Host, "@")
	}

	q := u.Query()
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer")
	}
	insecure := q.Get("insecure") == "1" || strings.ToLower(q.Get("insecure")) == "true"
	obfsPassword := q.Get("obfs-password")

	if host == "" || port == 0 {
		return nil, fmt.Errorf("hysteria2 节点信息不完整: host=%s port=%d", host, port)
	}

	out := map[string]interface{}{
		"type":        "hysteria2",
		"tag":         "proxy-out",
		"server":      host,
		"server_port": port,
		"password":    password,
		"tls": map[string]interface{}{
			"enabled":  true,
			"insecure": insecure,
		},
	}

	if sni != "" {
		out["tls"].(map[string]interface{})["server_name"] = sni
	}

	if obfsPassword != "" {
		out["obfs"] = map[string]interface{}{
			"type":     "salamander",
			"password": obfsPassword,
		}
	}

	return out, nil
}

// parseClashSingBoxNode 解析 Clash YAML 格式的 sing-box 节点
func parseClashSingBoxNode(src string) (map[string]interface{}, error) {
	// 复用已有的 YAML 解析基础设施
	var payload interface{}
	if err := yaml.Unmarshal([]byte(src), &payload); err != nil {
		return nil, fmt.Errorf("YAML 解析失败: %v", err)
	}

	nodeMap := pickClashNode(payload)
	if nodeMap == nil {
		return nil, fmt.Errorf("节点解析失败")
	}

	nodeType := strings.ToLower(getMapString(nodeMap, "type"))
	switch nodeType {
	case "hysteria2", "hysteria":
		return buildSingBoxHysteria2FromClash(nodeMap)
	case "tuic":
		return buildSingBoxTUICFromClash(nodeMap)
	default:
		return nil, fmt.Errorf("不支持的 sing-box 节点类型: %s", nodeType)
	}
}

func buildSingBoxHysteria2FromClash(node map[string]interface{}) (map[string]interface{}, error) {
	host := getMapString(node, "server")
	port := getMapInt(node, "port")
	password := getMapString(node, "password")
	sni := getMapString(node, "sni")
	if sni == "" {
		sni = getMapString(node, "servername")
	}
	skipVerify := getMapBool(node, "skip-cert-verify")

	if host == "" || port == 0 {
		return nil, fmt.Errorf("hysteria2 节点信息不完整")
	}

	tls := map[string]interface{}{
		"enabled":  true,
		"insecure": skipVerify,
	}
	if sni != "" {
		tls["server_name"] = sni
	}

	out := map[string]interface{}{
		"type":        "hysteria2",
		"tag":         "proxy-out",
		"server":      host,
		"server_port": port,
		"password":    password,
		"tls":         tls,
	}

	// 带宽限制（可选）
	if up := getMapString(node, "up"); up != "" {
		out["up_mbps"] = parseBandwidthMbps(up)
	}
	if down := getMapString(node, "down"); down != "" {
		out["down_mbps"] = parseBandwidthMbps(down)
	}

	// obfs
	if obfsPassword := getMapString(node, "obfs-password"); obfsPassword != "" {
		out["obfs"] = map[string]interface{}{
			"type":     "salamander",
			"password": obfsPassword,
		}
	}

	return out, nil
}

func buildSingBoxTUICFromClash(node map[string]interface{}) (map[string]interface{}, error) {
	host := getMapString(node, "server")
	port := getMapInt(node, "port")
	uuid := getMapString(node, "uuid")
	password := getMapString(node, "password")
	sni := getMapString(node, "sni")
	skipVerify := getMapBool(node, "skip-cert-verify")

	if host == "" || port == 0 {
		return nil, fmt.Errorf("tuic 节点信息不完整")
	}

	tls := map[string]interface{}{
		"enabled":  true,
		"insecure": skipVerify,
	}
	if sni != "" {
		tls["server_name"] = sni
	}

	// alpn
	if alpnRaw, ok := node["alpn"]; ok {
		if alpnList := toStringSlice(alpnRaw); len(alpnList) > 0 {
			tls["alpn"] = alpnList
		}
	}

	return map[string]interface{}{
		"type":               "tuic",
		"tag":                "proxy-out",
		"server":             host,
		"server_port":        port,
		"uuid":               uuid,
		"password":           password,
		"congestion_control": "bbr",
		"tls":                tls,
	}, nil
}

// parseBandwidthMbps 解析带宽字符串，返回 Mbps 整数
// 支持: "100 Mbps", "100", "100M"
func parseBandwidthMbps(s string) int {
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.TrimSuffix(s, "BPS")
	s = strings.TrimSuffix(s, "B")
	s = strings.TrimSuffix(s, "M")
	n, _ := strconv.Atoi(s)
	return n
}

// toStringSlice 将 interface{} 转为 []string
func toStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	if arr, ok := v.([]interface{}); ok {
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	if s, ok := v.(string); ok && s != "" {
		return []string{s}
	}
	return nil
}
