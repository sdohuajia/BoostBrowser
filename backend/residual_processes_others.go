//go:build !windows
// +build !windows

package backend

func killResidualRuntimeProcesses(appRoot string) error {
	return nil
}
