package backend

import (
	"net/url"
	"strings"
	"sync"
	"time"
)

const browserLastTabsSnapshotInterval = 2 * time.Second

type lastTabsTracker struct {
	profileId string
	debugPort int
	stopCh    chan struct{}
	stopOnce  sync.Once
}

func (t *lastTabsTracker) stop() {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() { close(t.stopCh) })
}

var (
	lastTabsTrackersMu sync.Mutex
	lastTabsTrackers   = map[string]*lastTabsTracker{}
)

// captureRestorableTabsViaCDP returns the ordinary web page tabs that should be
// reopened the next time this profile starts. Internal browser/extension pages
// are intentionally ignored so wallet popups, chrome://settings seed tabs and
// about:blank do not get resurrected.
func captureRestorableTabsViaCDP(debugPort int) []string {
	if debugPort <= 0 || !canConnectDebugPort(debugPort, 250*time.Millisecond) {
		return nil
	}
	targets, err := listCDPTargets(debugPort)
	if err != nil {
		return nil
	}
	return restorableTabURLsFromTargets(targets)
}

func restorableTabURLsFromTargets(targets []cdpTarget) []string {
	out := make([]string, 0, len(targets))
	seen := map[string]struct{}{}
	for _, target := range targets {
		if strings.TrimSpace(strings.ToLower(target.Type)) != "page" {
			continue
		}
		raw := strings.TrimSpace(target.URL)
		if !isRestorableTabURL(raw) {
			continue
		}
		if _, exists := seen[raw]; exists {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	return out
}

func isRestorableTabURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Host == "" {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return scheme == "http" || scheme == "https"
}

func sameLastTabs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// updateProfileLastTabsLocked updates and persists profile.LastTabs. Caller must
// hold a.browserMgr.Mutex.
func (a *App) updateProfileLastTabsLocked(profile *BrowserProfile, tabs []string) {
	if a == nil || a.browserMgr == nil || profile == nil || len(tabs) == 0 {
		return
	}
	if sameLastTabs(profile.LastTabs, tabs) {
		return
	}
	profile.LastTabs = append([]string{}, tabs...)
	profile.UpdatedAt = time.Now().Format(time.RFC3339)
	if a.browserMgr.ProfileDAO != nil {
		_ = a.browserMgr.ProfileDAO.Upsert(profile)
		return
	}
	_ = a.browserMgr.SaveProfiles()
}

func (a *App) startLastTabsTracker(profileId string, debugPort int) {
	if a == nil || a.browserMgr == nil || profileId == "" || debugPort <= 0 {
		return
	}

	lastTabsTrackersMu.Lock()
	if old, ok := lastTabsTrackers[profileId]; ok {
		old.stop()
		delete(lastTabsTrackers, profileId)
	}
	t := &lastTabsTracker{
		profileId: profileId,
		debugPort: debugPort,
		stopCh:    make(chan struct{}),
	}
	lastTabsTrackers[profileId] = t
	lastTabsTrackersMu.Unlock()

	go func(tracker *lastTabsTracker) {
		defer func() { _ = recover() }()
		lastSaved := []string(nil)
		timer := time.NewTimer(800 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-tracker.stopCh:
			return
		case <-timer.C:
		}

		ticker := time.NewTicker(browserLastTabsSnapshotInterval)
		defer ticker.Stop()
		for {
			select {
			case <-tracker.stopCh:
				return
			case <-ticker.C:
			}

			a.browserMgr.Mutex.Lock()
			profile, exists := a.browserMgr.Profiles[profileId]
			if !exists || profile == nil || !profile.Running || profile.DebugPort != debugPort || !profile.DebugReady {
				a.browserMgr.Mutex.Unlock()
				return
			}
			a.browserMgr.Mutex.Unlock()

			tabs := captureRestorableTabsViaCDP(debugPort)
			if len(tabs) == 0 || sameLastTabs(lastSaved, tabs) {
				continue
			}

			a.browserMgr.Mutex.Lock()
			profile, exists = a.browserMgr.Profiles[profileId]
			if exists && profile != nil && profile.Running && profile.DebugPort == debugPort {
				a.updateProfileLastTabsLocked(profile, tabs)
				lastSaved = append([]string{}, tabs...)
			}
			a.browserMgr.Mutex.Unlock()
		}
	}(t)
}

func stopLastTabsTracker(profileId string) {
	if profileId == "" {
		return
	}

	lastTabsTrackersMu.Lock()
	t, ok := lastTabsTrackers[profileId]
	if ok {
		delete(lastTabsTrackers, profileId)
	}
	lastTabsTrackersMu.Unlock()
	if ok {
		t.stop()
	}
}
