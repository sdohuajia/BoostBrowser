//go:build !windows

package backend

func (a *App) startBrowserRuntimeReconciler()    {}
func (a *App) reconcileBrowserRuntimeStateOnce() {}
