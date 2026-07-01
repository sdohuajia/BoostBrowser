//go:build !windows

package backend

func ensureBrowserUserDataDirReadyForFreshLaunch(chromeBinaryPath string, userDataDir string) error {
	return nil
}
