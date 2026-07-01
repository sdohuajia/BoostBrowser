package browser

import (
	"boost-browser/backend/internal/config"
	"testing"
)

func TestResolveProfileProxyBindingBySourceAndName(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr := NewManager(cfg, "")
	mgr.ProxyDAO = &proxyDAOStub{
		list: []Proxy{
			{
				ProxyId:     "new-p1",
				ProxyName:   "香港-01",
				ProxyConfig: "socks5://127.0.0.1:1080",
				SourceID:    "src-hk",
				SourceURL:   "https://example.com/sub",
			},
		},
	}

	profile := &Profile{
		ProfileId:         "pf-1",
		ProxyId:           "old-missing-id",
		ProxyConfig:       "socks5://127.0.0.1:2080",
		ProxyBindSourceID: "src-hk",
		ProxyBindName:     "香港-01",
	}

	changed, boundInPool, mode := mgr.ResolveProfileProxyBinding(profile)
	if !changed {
		t.Fatalf("expected profile binding to change")
	}
	if !boundInPool {
		t.Fatalf("expected profile to be rebound in pool")
	}
	if mode != "source_id+name" && mode != "proxy_id" {
		t.Fatalf("unexpected bind mode: %s", mode)
	}
	if profile.ProxyId != "new-p1" {
		t.Fatalf("unexpected rebound proxy id: %s", profile.ProxyId)
	}
	if profile.ProxyConfig != "socks5://127.0.0.1:1080" {
		t.Fatalf("unexpected rebound proxy config: %s", profile.ProxyConfig)
	}
	if profile.ProxyBindUpdatedAt == "" {
		t.Fatalf("expected bind updated time to be set")
	}
}

func TestResolveProfileProxyBindingAmbiguousNameNoRebind(t *testing.T) {
	cfg := config.DefaultConfig()
	mgr := NewManager(cfg, "")
	mgr.ProxyDAO = &proxyDAOStub{
		list: []Proxy{
			{ProxyId: "p1", ProxyName: "重复节点", ProxyConfig: "socks5://127.0.0.1:1080", SourceID: "src-a"},
			{ProxyId: "p2", ProxyName: "重复节点", ProxyConfig: "socks5://127.0.0.1:2080", SourceID: "src-b"},
		},
	}

	profile := &Profile{
		ProfileId:     "pf-2",
		ProxyId:       "old-missing-id",
		ProxyBindName: "重复节点",
	}

	changed, boundInPool, mode := mgr.ResolveProfileProxyBinding(profile)
	if changed {
		t.Fatalf("did not expect binding to change")
	}
	if boundInPool {
		t.Fatalf("did not expect ambiguous name to bind")
	}
	if mode != "" {
		t.Fatalf("expected empty mode, got=%s", mode)
	}
	if profile.ProxyId != "old-missing-id" {
		t.Fatalf("proxy id should remain unchanged, got=%s", profile.ProxyId)
	}
}
