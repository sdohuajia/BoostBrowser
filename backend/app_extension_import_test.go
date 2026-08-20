package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRemoveExtensionDirFromLaunchArgs(t *testing.T) {
	target := `C:\\BoostBrowserTest\\extensions\\imported\\mcohilncbfahbmgdjkbpemcciiolgcge`
	other := `C:\\BoostBrowserTest\\extensions\\imported\\nkbihfbeogaeaoehlefnkodbefgpgknn`
	args := []string{
		"--disable-sync",
		"--load-extension=" + target + "," + other,
	}
	next, changed := removeExtensionDirFromLaunchArgs(args, target)
	if !changed {
		t.Fatalf("expected target extension to be removed")
	}
	want := []string{
		"--disable-sync",
		"--load-extension=" + other,
	}
	if !reflect.DeepEqual(next, want) {
		t.Fatalf("unexpected args after removal\nwant: %#v\n got: %#v", want, next)
	}
}

func TestExtractExtensionIDFromEncodedCRXURL(t *testing.T) {
	input := "https://clients2.google.com/service/update2/crx?response=redirect&acceptformat=crx2,crx3&prodversion=146.0.7680.177&x=id%3Dmcohilncbfahbmgdjkbpemcciiolgcge%26installsource%3Dondemand%26uc"
	if got, want := extractExtensionID(input), "mcohilncbfahbmgdjkbpemcciiolgcge"; got != want {
		t.Fatalf("extractExtensionID() = %q, want %q", got, want)
	}
}

func TestExtractExtensionIDFromChromeWebStoreURL(t *testing.T) {
	input := "https://chromewebstore.google.com/detail/okx-wallet/mcohilncbfahbmgdjkbpemcciiolgcge"
	if got, want := extractExtensionID(input), "mcohilncbfahbmgdjkbpemcciiolgcge"; got != want {
		t.Fatalf("extractExtensionID() = %q, want %q", got, want)
	}
}

func TestCleanupStaleManagedUnpackedExtensionsRemovesOldDuplicateByManifestName(t *testing.T) {
	root := t.TempDir()
	userDataDir := filepath.Join(root, "profile")
	profileDir := filepath.Join(userDataDir, "Default")
	activeExt := filepath.Join(root, "extensions", "imported", "nkbihfbeogaeaoehlefnkodbefgpgknn")
	oldExt := filepath.Join(root, "extensions", "imported", "old-metamask")
	if err := os.MkdirAll(activeExt, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activeExt, "manifest.json"), []byte(`{"name":"MetaMask","version":"1.0","manifest_version":3}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(profileDir, "Extensions", "oldid"), 0755); err != nil {
		t.Fatal(err)
	}
	prefs := map[string]any{
		"extensions": map[string]any{
			"settings": map[string]any{
				"oldid": map[string]any{
					"path":     oldExt,
					"manifest": map[string]any{"name": "MetaMask"},
				},
				"keepid": map[string]any{
					"path":     activeExt,
					"manifest": map[string]any{"name": "MetaMask"},
				},
				"webstoreid": map[string]any{
					"manifest": map[string]any{"name": "Unrelated Web Store Extension"},
				},
			},
		},
	}
	data, _ := json.Marshal(prefs)
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "Preferences"), data, 0644); err != nil {
		t.Fatal(err)
	}

	cleanupStaleManagedUnpackedExtensions(userDataDir, []string{"--load-extension=" + activeExt}, root)

	outData, err := os.ReadFile(filepath.Join(profileDir, "Preferences"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(outData, &out); err != nil {
		t.Fatal(err)
	}
	settings := out["extensions"].(map[string]any)["settings"].(map[string]any)
	if _, ok := settings["oldid"]; ok {
		t.Fatalf("old duplicate unpacked extension setting was not removed")
	}
	if _, ok := settings["keepid"]; !ok {
		t.Fatalf("active extension setting should be kept")
	}
	if _, ok := settings["webstoreid"]; !ok {
		t.Fatalf("extension without path should be kept")
	}
	if _, err := os.Stat(filepath.Join(profileDir, "Extensions", "oldid")); !os.IsNotExist(err) {
		t.Fatalf("old extension profile data should be removed")
	}
}

func TestCleanupRemovedManagedExtensionRemovesPinnedAndProfileData(t *testing.T) {
	root := t.TempDir()
	userDataDir := filepath.Join(root, "profile")
	profileDir := filepath.Join(userDataDir, "Default")
	extID := "mcohilncbfahbmgdjkbpemcciiolgcge"
	extDir := filepath.Join(root, "extensions", "imported", extID)
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, "manifest.json"), []byte(`{"name":"OKX Wallet","version":"1.0","manifest_version":3}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(profileDir, "Extensions", extID), 0755); err != nil {
		t.Fatal(err)
	}
	prefs := map[string]any{
		"extensions": map[string]any{
			"settings": map[string]any{
				extID: map[string]any{
					"path":     extDir,
					"manifest": map[string]any{"name": "OKX Wallet"},
				},
			},
			"pinned_extensions": []any{extID, "otherext"},
		},
	}
	data, _ := json.Marshal(prefs)
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "Preferences"), data, 0644); err != nil {
		t.Fatal(err)
	}

	cleanupRemovedManagedExtension(userDataDir, extDir, extID, root)

	outData, err := os.ReadFile(filepath.Join(profileDir, "Preferences"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(outData, &out); err != nil {
		t.Fatal(err)
	}
	extensions := out["extensions"].(map[string]any)
	settings := extensions["settings"].(map[string]any)
	if _, ok := settings[extID]; ok {
		t.Fatalf("removed extension should be deleted from settings")
	}
	pinned := extensions["pinned_extensions"].([]any)
	if len(pinned) != 1 || pinned[0].(string) != "otherext" {
		t.Fatalf("unexpected pinned_extensions after cleanup: %#v", pinned)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "Extensions", extID)); !os.IsNotExist(err) {
		t.Fatalf("removed extension profile data should be deleted")
	}
}

func TestCleanupStaleManagedUnpackedExtensionsRemovesPathlessWebStoreDuplicate(t *testing.T) {
	root := t.TempDir()
	userDataDir := filepath.Join(root, "profile")
	profileDir := filepath.Join(userDataDir, "Default")
	activeID := "mcohilncbfahbmgdjkbpemcciiolgcge"
	staleID := "ajmpgkcoippjhgcipkhbbajcinebaohc"
	activeExt := filepath.Join(root, "extensions", "imported", activeID)
	if err := os.MkdirAll(filepath.Join(profileDir, "Extensions", staleID), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(activeExt, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activeExt, "manifest.json"), []byte(`{"name":"OKX Wallet","version":"1.0","manifest_version":3}`), 0644); err != nil {
		t.Fatal(err)
	}
	prefs := map[string]any{
		"extensions": map[string]any{
			"settings": map[string]any{
				activeID: map[string]any{
					"path":     activeExt,
					"manifest": map[string]any{"name": "OKX Wallet"},
				},
				staleID: map[string]any{
					"manifest": map[string]any{"name": "OKX Wallet"},
				},
			},
			"pinned_extensions": []any{activeID, staleID},
		},
	}
	data, _ := json.Marshal(prefs)
	if err := os.WriteFile(filepath.Join(profileDir, "Preferences"), data, 0644); err != nil {
		t.Fatal(err)
	}

	cleanupStaleManagedUnpackedExtensions(userDataDir, []string{"--load-extension=" + activeExt}, root)

	outData, err := os.ReadFile(filepath.Join(profileDir, "Preferences"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(outData, &out); err != nil {
		t.Fatal(err)
	}
	extensions := out["extensions"].(map[string]any)
	settings := extensions["settings"].(map[string]any)
	if _, ok := settings[staleID]; ok {
		t.Fatalf("pathless Web Store duplicate was not removed")
	}
	if _, ok := settings[activeID]; !ok {
		t.Fatalf("active managed extension should be kept")
	}
	pinned := extensions["pinned_extensions"].([]any)
	if len(pinned) != 1 || pinned[0].(string) != activeID {
		t.Fatalf("unexpected pinned_extensions after cleanup: %#v", pinned)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "Extensions", staleID)); !os.IsNotExist(err) {
		t.Fatalf("stale Web Store extension profile data should be removed")
	}
}
