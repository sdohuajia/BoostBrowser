package backend

import (
	"errors"
	"testing"
)

func TestCompleteOKXWalletImportStopsVerifiedInstance(t *testing.T) {
	result := &OKXWalletImportResult{}
	item := OKXWalletImportItem{ProfileID: "profile-1"}
	var stoppedProfileID string

	completeOKXWalletImport(result, &item, func(profileID string) error {
		stoppedProfileID = profileID
		return nil
	})

	if stoppedProfileID != "profile-1" {
		t.Fatalf("stopped profile = %q, want profile-1", stoppedProfileID)
	}
	if item.Status != "success" || result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("unexpected completion result: %#v, %#v", item, result)
	}
}

func TestCompleteOKXWalletImportReportsCloseFailure(t *testing.T) {
	result := &OKXWalletImportResult{}
	item := OKXWalletImportItem{ProfileID: "profile-1"}

	completeOKXWalletImport(result, &item, func(string) error {
		return errors.New("browser still running")
	})

	if item.Status != "close_failed" || result.Succeeded != 0 || result.Failed != 1 {
		t.Fatalf("unexpected close failure result: %#v, %#v", item, result)
	}
}
