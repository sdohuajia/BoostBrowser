package proxy

import "testing"

func TestSingBoxRegisterBridgeStoresNewBridge(t *testing.T) {
	manager := &SingBoxManager{
		Bridges: make(map[string]*SingBoxBridge),
	}
	bridge := &SingBoxBridge{
		NodeKey: "node-a",
		Port:    21001,
		Running: true,
	}

	socksURL, reused := manager.registerBridge("node-a", bridge)
	if reused {
		t.Fatalf("expected new bridge registration, got reused with %q", socksURL)
	}
	if socksURL != "" {
		t.Fatalf("expected empty socksURL for new bridge registration, got %q", socksURL)
	}
	if manager.Bridges["node-a"] != bridge {
		t.Fatalf("bridge was not stored in manager")
	}
}

func TestSingBoxRegisterBridgeIgnoresSamePointer(t *testing.T) {
	manager := &SingBoxManager{
		Bridges: make(map[string]*SingBoxBridge),
	}
	bridge := &SingBoxBridge{
		NodeKey: "node-a",
		Port:    21001,
		Running: true,
	}
	manager.Bridges["node-a"] = bridge

	socksURL, reused := manager.registerBridge("node-a", bridge)
	if reused {
		t.Fatalf("same bridge pointer must not be treated as duplicate, got reused with %q", socksURL)
	}
	if socksURL != "" {
		t.Fatalf("expected empty socksURL when registering same pointer, got %q", socksURL)
	}
	if manager.Bridges["node-a"] != bridge {
		t.Fatalf("bridge mapping changed unexpectedly")
	}
	if bridge.Stopping {
		t.Fatalf("same bridge pointer should not be marked as stopping")
	}
}
