package backend

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSeedDefaultSearchEngineCleansNonGoogleEntries(t *testing.T) {
	dir := t.TempDir()
	defaultDir := filepath.Join(dir, "Default")
	if err := os.MkdirAll(defaultDir, 0755); err != nil {
		t.Fatal(err)
	}

	prefsPath := filepath.Join(defaultDir, "Preferences")
	if err := os.WriteFile(prefsPath, []byte(`{"profile":{},"browser":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	webDataPath := filepath.Join(defaultDir, "Web Data")
	db := mustOpenTestKeywordsDB(t, webDataPath)
	defer db.Close()

	mustExecTestDB(t, db, `INSERT INTO keywords (id, short_name, keyword, favicon_url, url, safe_for_autoreplace, originating_url, date_created, usage_count, input_encodings, suggest_url, prepopulate_id, created_by_policy, last_modified, sync_guid, alternate_urls, image_url, search_url_post_params, suggest_url_post_params, image_url_post_params, new_tab_url, last_visited, created_from_play_api, is_active, starter_pack_id, enforced_by_policy, featured_by_policy) VALUES
		(2, '百度', 'baidu.com', '', 'https://www.baidu.com/#ie={inputEncoding}&wd={searchTerms}', 1, '', 0, 0, 'UTF-8', '', 0, 0, 0, 'baidu-guid', '', '', '', '', '', '', 0, 0, 0, 0, 0, 0),
		(3, 'Microsoft Bing', 'bing.com', '', 'https://www.bing.com/search?q={searchTerms}', 1, '', 0, 0, 'UTF-8', '', 0, 0, 0, 'bing-guid', '', '', '', '', '', '', 0, 0, 0, 0, 0, 0),
		(6, 'No Search', 'nosearch', '', 'http://{searchTerms}', 1, '', 0, 0, 'UTF-8', '', 0, 0, 0, 'nosearch-guid', '', '', '', '', '', '', 0, 0, 0, 0, 0, 0),
		(7, '书签', '@书签', '', 'chrome://bookmarks/?q={searchTerms}', 1, '', 0, 0, 'UTF-8', '', 1, 0, 0, 'bookmark-guid', '', '', '', '', '', '', 0, 0, 1, 0, 0, 0),
		(10, 'Gemini', '@gemini', '', 'https://www.9oo91e.qjz9zk/app?q={searchTerms}', 1, '', 0, 0, 'UTF-8', '', 10, 0, 0, 'gemini-guid', '', '', '', '', '', '', 0, 0, 1, 0, 0, 0)`)

	seedDefaultSearchEngine(dir)

	var rows []struct {
		ID      int64
		Name    string
		Keyword string
		URL     string
		Active  int
		GUID    string
	}
	resultRows, err := db.Query(`SELECT id, short_name, keyword, url, is_active, sync_guid FROM keywords ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer resultRows.Close()
	for resultRows.Next() {
		var row struct {
			ID      int64
			Name    string
			Keyword string
			URL     string
			Active  int
			GUID    string
		}
		if err := resultRows.Scan(&row.ID, &row.Name, &row.Keyword, &row.URL, &row.Active, &row.GUID); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if err := resultRows.Err(); err != nil {
		t.Fatal(err)
	}

	if len(rows) != 2 {
		t.Fatalf("keywords row count=%d, want 2; rows=%#v", len(rows), rows)
	}
	var googleRow *struct {
		ID      int64
		Name    string
		Keyword string
		URL     string
		Active  int
		GUID    string
	}
	var bookmarkRow *struct {
		ID      int64
		Name    string
		Keyword string
		URL     string
		Active  int
		GUID    string
	}
	for i := range rows {
		row := &rows[i]
		if row.Name == "Google" {
			googleRow = row
		}
		if row.Keyword == "@书签" {
			bookmarkRow = row
		}
	}
	if googleRow == nil || googleRow.Keyword != "9oo91e.qjz9zk" || googleRow.Active != 1 || googleRow.GUID == "" {
		t.Fatalf("google row not normalized: %#v", rows)
	}
	if bookmarkRow == nil {
		t.Fatalf("site-search shortcut should remain: %#v", rows)
	}

	prefsData, err := os.ReadFile(prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	var prefs map[string]any
	if err := json.Unmarshal(prefsData, &prefs); err != nil {
		t.Fatal(err)
	}
	provider := prefs["default_search_provider"].(map[string]any)
	if provider["name"] != "Google" || provider["keyword"] != "9oo91e.qjz9zk" {
		t.Fatalf("default_search_provider not patched to Google: %#v", provider)
	}
	dspData := prefs["default_search_provider_data"].(map[string]any)
	mirrored := dspData["mirrored_template_url_data"].(map[string]any)
	if mirrored["short_name"] != "Google" || mirrored["keyword"] != "9oo91e.qjz9zk" {
		t.Fatalf("mirrored_template_url_data not patched to Google: %#v", mirrored)
	}
}

func TestSeedDefaultSearchEngineWithRetryWaitsForWebData(t *testing.T) {
	dir := t.TempDir()

	go func() {
		time.Sleep(20 * time.Millisecond)
		defaultDir := filepath.Join(dir, "Default")
		if err := os.MkdirAll(defaultDir, 0755); err != nil {
			t.Errorf("mkdir default dir failed: %v", err)
			return
		}
		prefsPath := filepath.Join(defaultDir, "Preferences")
		if err := os.WriteFile(prefsPath, []byte(`{"profile":{},"browser":{}}`), 0644); err != nil {
			t.Errorf("write preferences failed: %v", err)
			return
		}

		// Build the sqlite file off to the side and only publish it as "Web Data"
		// after all writes are closed. Otherwise the retry loop can see the file
		// half-created and race this test goroutine, causing a flaky SQLITE_BUSY.
		tmpWebDataPath := filepath.Join(defaultDir, "Web Data.tmp")
		db := mustOpenTestKeywordsDB(t, tmpWebDataPath)
		mustExecTestDB(t, db, `INSERT INTO keywords (id, short_name, keyword, favicon_url, url, safe_for_autoreplace, originating_url, date_created, usage_count, input_encodings, suggest_url, prepopulate_id, created_by_policy, last_modified, sync_guid, alternate_urls, image_url, search_url_post_params, suggest_url_post_params, image_url_post_params, new_tab_url, last_visited, created_from_play_api, is_active, starter_pack_id, enforced_by_policy, featured_by_policy) VALUES
			(6, 'No Search', 'nosearch', '', 'http://{searchTerms}', 1, '', 0, 0, 'UTF-8', '', 0, 0, 0, 'nosearch-guid', '', '', '', '', '', '', 0, 0, 0, 0, 0, 0)`)
		if err := db.Close(); err != nil {
			t.Errorf("close temp Web Data failed: %v", err)
			return
		}
		if err := os.Rename(tmpWebDataPath, filepath.Join(defaultDir, "Web Data")); err != nil {
			t.Errorf("publish Web Data failed: %v", err)
			return
		}
	}()

	seedDefaultSearchEngineWithRetry(dir, 10, 10*time.Millisecond)

	prefsPath := filepath.Join(dir, "Default", "Preferences")
	prefsData, err := os.ReadFile(prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	var prefs map[string]any
	if err := json.Unmarshal(prefsData, &prefs); err != nil {
		t.Fatal(err)
	}
	provider := prefs["default_search_provider"].(map[string]any)
	if provider["name"] != "Google" || provider["keyword"] != "9oo91e.qjz9zk" {
		t.Fatalf("default_search_provider not patched after retry: %#v", provider)
	}
}

func mustOpenTestKeywordsDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	mustExecTestDB(t, db, `CREATE TABLE keywords (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		short_name TEXT,
		keyword TEXT,
		favicon_url TEXT,
		url TEXT,
		safe_for_autoreplace INTEGER,
		originating_url TEXT,
		date_created INTEGER,
		usage_count INTEGER,
		input_encodings TEXT,
		suggest_url TEXT,
		prepopulate_id INTEGER,
		created_by_policy INTEGER,
		last_modified INTEGER,
		sync_guid TEXT,
		alternate_urls TEXT,
		image_url TEXT,
		search_url_post_params TEXT,
		suggest_url_post_params TEXT,
		image_url_post_params TEXT,
		new_tab_url TEXT,
		last_visited INTEGER,
		created_from_play_api INTEGER,
		is_active INTEGER,
		starter_pack_id INTEGER,
		enforced_by_policy INTEGER,
		featured_by_policy INTEGER
	)`)
	return db
}

func mustExecTestDB(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
