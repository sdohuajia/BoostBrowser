package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureProfileCloakWebStoreHelperWritesProfileEndpoint(t *testing.T) {
	appRoot := t.TempDir()
	userDataDir := filepath.Join(t.TempDir(), "profile")

	shared, err := ensureEmbeddedCloakExtensions(appRoot)
	if err != nil {
		t.Fatalf("ensure shared helper: %v", err)
	}
	if _, err := os.Stat(filepath.Join(shared, "manifest.json")); err != nil {
		t.Fatalf("shared manifest missing: %v", err)
	}

	profileID := "profile-123"
	helper, err := ensureProfileCloakWebStoreHelper(appRoot, userDataDir, profileID, 19876, "X-Boost-Api-Key", "secret")
	if err != nil {
		t.Fatalf("ensure profile helper: %v", err)
	}
	if helper == shared {
		t.Fatalf("profile helper must not reuse shared helper path")
	}
	if _, err := os.Stat(filepath.Join(helper, "manifest.json")); err != nil {
		t.Fatalf("profile manifest missing: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(helper, "boost_endpoint.json"))
	if err != nil {
		t.Fatalf("endpoint missing: %v", err)
	}
	var endpoint map[string]any
	if err := json.Unmarshal(data, &endpoint); err != nil {
		t.Fatalf("bad endpoint json: %v", err)
	}
	if endpoint["profileId"] != profileID {
		t.Fatalf("profileId not written: %#v", endpoint)
	}
	if endpoint["port"].(float64) != 19876 {
		t.Fatalf("port not written: %#v", endpoint)
	}
}
