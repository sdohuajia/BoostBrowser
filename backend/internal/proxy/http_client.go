package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"boost-browser/backend/internal/config"

	xproxy "golang.org/x/net/proxy"
)

// buildProxyHTTPClient 根据代理配置构建 HTTP 客户端，统一用于测速/健康检测场景。
func buildProxyHTTPClient(
	src string,
	proxyId string,
	proxies []config.BrowserProxy,
	xrayMgr *XrayManager,
	singboxMgr *SingBoxManager,
	timeout time.Duration,
) (*http.Client, error) {
	l := strings.ToLower(strings.TrimSpace(src))
	if l == "" || l == "direct://" {
		return &http.Client{Timeout: timeout}, nil
	}

	if IsSingBoxProtocol(src) {
		if singboxMgr == nil {
			return nil, fmt.Errorf("sing-box 管理器未初始化")
		}
		socks5Addr, err := singboxMgr.EnsureBridge(src, proxies, proxyId)
		if err != nil {
			return nil, fmt.Errorf("sing-box 桥接启动失败: %w", err)
		}
		return buildSocks5HTTPClient(strings.TrimPrefix(socks5Addr, "socks5://"), timeout)
	}

	if RequiresBridge(src, proxies, proxyId) {
		if xrayMgr == nil {
			return nil, fmt.Errorf("xray 管理器未初始化")
		}
		socks5Addr, err := xrayMgr.EnsureBridge(src, proxies, proxyId)
		if err != nil {
			return nil, fmt.Errorf("xray 桥接启动失败: %w", err)
		}
		return buildSocks5HTTPClient(strings.TrimPrefix(socks5Addr, "socks5://"), timeout)
	}

	if strings.HasPrefix(l, "socks5://") {
		u, err := url.Parse(src)
		if err != nil {
			return nil, fmt.Errorf("SOCKS5 地址解析失败: %w", err)
		}
		var auth *xproxy.Auth
		if u.User != nil {
			pass, _ := u.User.Password()
			auth = &xproxy.Auth{
				User:     u.User.Username(),
				Password: pass,
			}
		}
		dialer, err := xproxy.SOCKS5("tcp", u.Host, auth, xproxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("SOCKS5 dialer 创建失败: %w", err)
		}
		contextDialer, ok := dialer.(xproxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("SOCKS5 dialer 不支持 ContextDialer")
		}
		transport := &http.Transport{DialContext: contextDialer.DialContext}
		return &http.Client{Transport: transport, Timeout: timeout}, nil
	}

	proxyURL, err := url.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("代理地址解析失败: %w", err)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	if strings.EqualFold(proxyURL.Scheme, "https") {
		// 这里的 TLS 是“客户端 -> HTTPS 代理服务器”这一层，不是访问目标站点 my.ippure.com 的 TLS。
		// 很多住宅/机房代理会给 IP:port 返回域名证书或自签证书，证书里没有 IP SAN，
		// Go 默认会报：x509: cannot validate certificate for x.x.x.x because it doesn't contain any IP SANs。
		// 当前项目 Windows Go 版本没有 http.Transport.ProxyTLSClientConfig，所以用 DialTLSContext
		// 只在拨号地址等于代理服务器地址时放宽校验；其它 HTTPS 连接仍走 Go 默认校验。
		transport.DialTLSContext = insecureHTTPSProxyDialTLSContext(proxyURL.Host)
	}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

func buildSocks5HTTPClient(socks5Host string, timeout time.Duration) (*http.Client, error) {
	dialer, err := xproxy.SOCKS5("tcp", socks5Host, nil, xproxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("SOCKS5 dialer 创建失败: %w", err)
	}
	contextDialer, ok := dialer.(xproxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("SOCKS5 dialer 不支持 ContextDialer")
	}
	transport := &http.Transport{DialContext: contextDialer.DialContext}
	return &http.Client{Transport: transport, Timeout: timeout}, nil
}

func insecureHTTPSProxyDialTLSContext(proxyHost string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialer := &net.Dialer{}
		if deadline, ok := ctx.Deadline(); ok {
			dialer.Deadline = deadline
		}

		serverName := tlsServerName(addr)
		insecureProxyLayer := strings.EqualFold(addr, proxyHost)
		conn, err := tls.DialWithDialer(dialer, network, addr, &tls.Config{
			ServerName:         serverName,
			InsecureSkipVerify: insecureProxyLayer, // #nosec G402 -- only for HTTPS proxy layer with IP/self-signed proxy certs.
		})
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
}

func tlsServerName(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return strings.Trim(host, "[]")
}
