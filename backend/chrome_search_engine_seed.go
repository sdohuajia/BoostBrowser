package backend

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"boost-browser/backend/internal/logger"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// seedDefaultSearchEngine 在 Chrome 启动前把 Google 设为默认搜索引擎，
// 并清掉 keywords 里会在设置页暴露出来的其它搜索引擎。
//
// 背景：cloak 内核（基于 ungoogled-chromium）出厂自带的 keywords 表里，
// 默认搜索引擎被设成 "No Search"（url=http://{searchTerms}），导致地址栏
// typing 非 URL 文本时直接走 http://<text>。原版 Google 入口被混淆成
// 9oo91e.qjz9zk，无法直接使用。
//
// 命令行 --search-provider-* 在 cloak 这个 fork 上不生效，
// HKCU 企业策略也被屏蔽。可行路径只剩两步同时做：
//  1. 往 Default/Web Data 的 keywords 表 INSERT 一条干净的 Google
//     （UUID4 sync_guid，is_active=1）。
//  2. 改 Default/Preferences 让 default_search_provider 指向它，
//     字段名必须用 mirrored_template_url_data（不是 template_url_data）
//     和 default_search_provider.guid（不是 synced_guid）—— 这两个名字
//     是 cloak 内核 UI 操作时实际写出来的，错一个就会被回退 No Search。
//
// 该 profile 的 protection.macs 为空、super_mac 为 None，
// 当前没有 HMAC 校验，外部直接改 JSON 安全。
func seedDefaultSearchEngine(userDataDir string) {
	seedDefaultSearchEngineOnce(userDataDir)
}

func seedDefaultSearchEngineWithRetry(userDataDir string, maxAttempts int, delay time.Duration) {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	if delay <= 0 {
		delay = 1500 * time.Millisecond
	}
	log := logger.New("SearchEngineSeed")
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if seedDefaultSearchEngineOnce(userDataDir) {
			return
		}
		if attempt >= maxAttempts {
			log.Info("多次尝试后仍未完成默认搜索引擎静态修正，将在下次启动继续重试",
				logger.F("user_data_dir", userDataDir),
				logger.F("attempts", maxAttempts),
			)
			return
		}
		time.Sleep(delay)
	}
}

func seedDefaultSearchEngineOnce(userDataDir string) bool {
	if strings.TrimSpace(userDataDir) == "" {
		return false
	}
	log := logger.New("SearchEngineSeed")

	defaultDir := filepath.Join(userDataDir, "Default")
	webDataPath := filepath.Join(defaultDir, "Web Data")
	prefsPath := filepath.Join(defaultDir, "Preferences")

	// Web Data 不存在说明这是首次启动，留给 Chrome 自己创建后下次再 seed。
	if _, err := os.Stat(webDataPath); err != nil {
		log.Debug("Web Data not yet present, skipping seed",
			logger.F("path", webDataPath),
		)
		return false
	}

	googleID, googleGUID, err := ensureGoogleKeywordRow(webDataPath)
	if err != nil {
		log.Debug("ensureGoogleKeywordRow failed",
			logger.F("path", webDataPath),
			logger.F("error", err.Error()),
		)
		return false
	}

	if err := patchPreferencesPointToGoogle(prefsPath, googleID, googleGUID); err != nil {
		log.Debug("patchPreferencesPointToGoogle failed",
			logger.F("path", prefsPath),
			logger.F("error", err.Error()),
		)
		return false
	}

	log.Info("已清理搜索引擎列表并设置默认搜索引擎为 Google",
		logger.F("user_data_dir", userDataDir),
		logger.F("keyword_id", googleID),
		logger.F("sync_guid", googleGUID),
	)
	return true
}

// ensureGoogleKeywordRow 确保 Web Data 的 keywords 表里只保留一条 Google 条目。
// 已存在则复用现有 Google 行；不存在则插入。随后清理其它普通搜索引擎，
// 保留 @书签/@历史记录 这类站内快捷词。
func ensureGoogleKeywordRow(webDataPath string) (int64, string, error) {
	// modernc sqlite driver；不开 WAL，避免与 Chrome 自己持有的连接抢锁。
	db, err := sql.Open("sqlite", webDataPath+"?_pragma=busy_timeout(2000)")
	if err != nil {
		return 0, "", err
	}
	defer db.Close()

	// cloak 内核会把 google.com 字面量从 keywords 表里清掉（de-google 扫描），
	// 所以 INSERT 必须用 cloak 自己的混淆域名 9oo91e.qjz9zk —— 网络层会自动
	// 重写回 google.com。和 prepopulated id=10 (Gemini) / id=12 (Google AI 模式)
	// 同样用法。
	const googleURL = "https://www.9oo91e.qjz9zk/search?q={searchTerms}"
	const googleSuggestURL = "https://www.9oo91e.qjz9zk/complete/search?client=chrome&q={searchTerms}"
	const googleFavicon = "https://www.9oo91e.qjz9zk/favicon.ico"
	const googleKeyword = "9oo91e.qjz9zk"

	// 按 short_name 匹配（不绑定 URL，避免 cloak 域名变更时重复插入死行）。
	var existingID int64
	var existingGUID string
	err = db.QueryRow(
		`SELECT id, sync_guid FROM keywords
         WHERE short_name='Google' AND prepopulate_id=0 LIMIT 1`,
	).Scan(&existingID, &existingGUID)
	if err == nil && existingID > 0 {
		if strings.TrimSpace(existingGUID) == "" {
			existingGUID = uuid.NewString()
		}
		_, _ = db.Exec(`UPDATE keywords
			SET is_active=1,
			    keyword=?,
			    short_name='Google',
			    favicon_url=?,
			    url=?,
			    suggest_url=?,
			    input_encodings='UTF-8',
			    prepopulate_id=0,
			    safe_for_autoreplace=0,
			    sync_guid=?
			WHERE id=?`,
			googleKeyword,
			googleFavicon,
			googleURL,
			googleSuggestURL,
			existingGUID,
			existingID,
		)
		if err := removeNonGoogleKeywordRows(db, existingID); err != nil {
			return 0, "", err
		}
		return existingID, existingGUID, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return 0, "", err
	}

	// 插入新行。Chrome epoch 是 1601-01-01 微秒，转成 Unix 微秒 + offset。
	now := time.Now().UnixMicro() + 11644473600000000
	guid := uuid.NewString()

	res, err := db.Exec(`
        INSERT INTO keywords (
            short_name, keyword, favicon_url, url, safe_for_autoreplace,
            originating_url, date_created, usage_count, input_encodings,
            suggest_url, prepopulate_id, created_by_policy, last_modified,
            sync_guid, alternate_urls, image_url, search_url_post_params,
            suggest_url_post_params, image_url_post_params, new_tab_url,
            last_visited, created_from_play_api, is_active, starter_pack_id,
            enforced_by_policy, featured_by_policy
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `,
		"Google",
		googleKeyword,
		googleFavicon,
		googleURL,
		0, // safe_for_autoreplace=0：标记为持久化用户条目，防止 cloak 把它当成 prepopulated 自动 GC
		"",
		now,
		0,
		"UTF-8",
		googleSuggestURL,
		0, // prepopulate_id=0 标记自定义条目
		0,
		now,
		guid,
		"",
		"",
		"",
		"",
		"",
		"",
		0,
		0,
		1, // is_active=1
		0,
		0,
		0,
	)
	if err != nil {
		return 0, "", err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, "", err
	}
	if err := removeNonGoogleKeywordRows(db, id); err != nil {
		return 0, "", err
	}
	return id, guid, nil
}

func removeNonGoogleKeywordRows(db *sql.DB, keepID int64) error {
	_, err := db.Exec(`DELETE FROM keywords
		WHERE id <> ?
		  AND (
			COALESCE(keyword, '') NOT LIKE '@%'
			OR lower(COALESCE(short_name, '')) IN ('google ai 模式', 'gemini')
		  )`, keepID)
	return err
}

// patchPreferencesPointToGoogle 让 Preferences 的 default_search_provider
// 指向 keywords 表里那条 Google。
//
// 关键：cloak 内核 UI 操作实际写的字段名是
//
//	default_search_provider_data.mirrored_template_url_data
//	default_search_provider.guid
//
// 不是 Chrome 标准文档里的 template_url_data / synced_guid。
func patchPreferencesPointToGoogle(prefsPath string, googleID int64, googleGUID string) error {
	if strings.TrimSpace(prefsPath) == "" {
		return nil
	}
	if _, err := os.Stat(prefsPath); err != nil {
		return err
	}
	data, err := os.ReadFile(prefsPath)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		data = []byte("{}")
	}
	var prefs map[string]any
	if err := json.Unmarshal(data, &prefs); err != nil {
		return err
	}

	idStr := strconvFormatInt(googleID)
	changed := false

	// === default_search_provider_data.mirrored_template_url_data ===
	dspData := ensureJSONMap(prefs, "default_search_provider_data")
	mirrored, _ := dspData["mirrored_template_url_data"].(map[string]any)

	wantMirrored := func() bool {
		if mirrored == nil {
			return true
		}
		if asJSONString(mirrored["url"]) != "https://www.9oo91e.qjz9zk/search?q={searchTerms}" {
			return true
		}
		if asJSONString(mirrored["synced_guid"]) != googleGUID {
			return true
		}
		return false
	}()
	if wantMirrored {
		mirrored = map[string]any{
			"alternate_urls":              []any{},
			"created_from_play_api":       false,
			"date_created":                "0",
			"enforced_by_policy":          false,
			"favicon_url":                 "https://www.9oo91e.qjz9zk/favicon.ico",
			"featured_by_policy":          false,
			"id":                          idStr,
			"input_encodings":             []any{"UTF-8"},
			"is_active":                   float64(1),
			"keyword":                     "9oo91e.qjz9zk",
			"last_modified":               "0",
			"last_visited":                "0",
			"new_tab_url":                 "",
			"originating_url":             "",
			"policy_origin":               float64(0),
			"prepopulate_id":              float64(0),
			"safe_for_autoreplace":        false,
			"search_url_post_params":      "",
			"short_name":                  "Google",
			"starter_pack_id":             float64(0),
			"suggestions_url":             "https://www.9oo91e.qjz9zk/complete/search?client=chrome&q={searchTerms}",
			"suggestions_url_post_params": "",
			"synced_guid":                 googleGUID,
			"url":                         "https://www.9oo91e.qjz9zk/search?q={searchTerms}",
			"usage_count":                 float64(0),
		}
		dspData["mirrored_template_url_data"] = mirrored
		// 旧字段顺手清掉，避免误导。
		delete(dspData, "template_url_data")
		changed = true
	}

	// === default_search_provider（旧字段）===
	provider := ensureJSONMap(prefs, "default_search_provider")
	wantProvider := provider["enabled"] != true ||
		asJSONString(provider["guid"]) != googleGUID ||
		asJSONString(provider["search_url"]) != "https://www.9oo91e.qjz9zk/search?q={searchTerms}"
	if wantProvider {
		provider["enabled"] = true
		provider["name"] = "Google"
		provider["keyword"] = "9oo91e.qjz9zk"
		provider["search_url"] = "https://www.9oo91e.qjz9zk/search?q={searchTerms}"
		provider["suggest_url"] = "https://www.9oo91e.qjz9zk/complete/search?client=chrome&q={searchTerms}"
		provider["icon_url"] = "https://www.9oo91e.qjz9zk/favicon.ico"
		provider["encodings"] = "UTF-8"
		provider["id"] = idStr
		provider["prepopulate_id"] = "0"
		provider["guid"] = googleGUID // 关键：cloak 用 guid，不是 synced_guid
		provider["reset_occurred"] = false
		// 清掉我之前误写的字段
		delete(provider, "synced_guid")
		changed = true
	}

	if !changed {
		return nil
	}
	out, err := json.MarshalIndent(prefs, "", "   ")
	if err != nil {
		return err
	}
	return os.WriteFile(prefsPath, out, 0644)
}

// strconvFormatInt 内联一个简单的 int64 → 字符串，避免引新 import。
func strconvFormatInt(v int64) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
