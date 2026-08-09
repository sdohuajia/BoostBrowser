//go:build !windows

package backend

func setBadgeForInstance(pid int, displayNumber int) error {
	return nil
}
