//go:build windows

package backend

import "testing"

func TestHubSDKProfileIDIsStableAndNamespaced(t *testing.T) {
	const hwnd = int64(987654321)
	got := hubSDKProfileID(hwnd)
	want := "hubsdk:987654321"
	if got != want {
		t.Fatalf("hubSDKProfileID(%d) = %q, want %q", hwnd, got, want)
	}
	if got == "987654321" {
		t.Fatal("HubSDK IDs must be namespaced so they cannot collide with local profile IDs")
	}
}
