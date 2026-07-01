package backend

import (
	"boost-browser/backend/internal/config"
	"boost-browser/backend/internal/logger"
)

type BrowserBookmark = config.BrowserBookmark

var defaultBookmarkList = []BrowserBookmark{
	{Name: "Google", URL: "https://www.google.com/"},
	{Name: "Gmail", URL: "https://mail.google.com/"},
	{Name: "Claude", URL: "https://claude.ai/"},
	{Name: "ChatGPT", URL: "https://chatgpt.com/"},
	{Name: "YouTube", URL: "https://www.youtube.com/"},
}

// BookmarkList 获取默认书签列表（优先 SQLite，降级 config.yaml）
func (a *App) BookmarkList() []BrowserBookmark {
	if a.browserMgr.BookmarkDAO != nil {
		list, err := a.browserMgr.BookmarkDAO.List()
		if err == nil && len(list) > 0 {
			return list
		}
	}
	if len(a.config.Browser.DefaultBookmarks) > 0 {
		return append([]BrowserBookmark{}, a.config.Browser.DefaultBookmarks...)
	}
	return append([]BrowserBookmark{}, defaultBookmarkList...)
}

// BookmarkSave 保存默认书签列表（优先 SQLite，降级 config.yaml）
func (a *App) BookmarkSave(items []BrowserBookmark) error {
	log := logger.New("Bookmark")
	valid := make([]BrowserBookmark, 0, len(items))
	for _, item := range items {
		if item.Name != "" && item.URL != "" {
			valid = append(valid, item)
		}
	}

	if a.browserMgr.BookmarkDAO != nil {
		if err := a.browserMgr.BookmarkDAO.ReplaceAll(valid); err != nil {
			log.Error("书签保存到数据库失败", logger.F("error", err.Error()))
			return err
		}
		log.Info("书签已保存到数据库", logger.F("count", len(valid)))
		return nil
	}

	// 降级：写入 config.yaml
	a.config.Browser.DefaultBookmarks = valid
	if err := a.config.Save(a.resolveAppPath("config.yaml")); err != nil {
		log.Error("书签保存失败", logger.F("error", err.Error()))
		return err
	}
	log.Info("书签已保存到 config.yaml", logger.F("count", len(valid)))
	return nil
}

// BookmarkReset 恢复默认书签
func (a *App) BookmarkReset() error {
	return a.BookmarkSave(append([]BrowserBookmark{}, defaultBookmarkList...))
}
