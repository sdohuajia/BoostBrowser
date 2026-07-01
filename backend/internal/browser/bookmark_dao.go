package browser

import (
	"database/sql"
	"fmt"

	"boost-browser/backend/internal/config"
)

// BookmarkDAO 默认书签持久化接口
type BookmarkDAO interface {
	List() ([]config.BrowserBookmark, error)
	ReplaceAll(bookmarks []config.BrowserBookmark) error
}

// SQLiteBookmarkDAO 基于 SQLite 的 BookmarkDAO 实现
type SQLiteBookmarkDAO struct {
	db *sql.DB
}

// NewSQLiteBookmarkDAO 创建 SQLiteBookmarkDAO
func NewSQLiteBookmarkDAO(db *sql.DB) *SQLiteBookmarkDAO {
	return &SQLiteBookmarkDAO{db: db}
}

// List 查询所有默认书签，按 sort_order 升序
func (d *SQLiteBookmarkDAO) List() ([]config.BrowserBookmark, error) {
	rows, err := d.db.Query(`
		SELECT name, url FROM browser_bookmarks ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("查询书签列表失败: %w", err)
	}
	defer rows.Close()

	var list []config.BrowserBookmark
	for rows.Next() {
		var b config.BrowserBookmark
		if err := rows.Scan(&b.Name, &b.URL); err != nil {
			return nil, fmt.Errorf("读取书签行失败: %w", err)
		}
		list = append(list, b)
	}
	return list, rows.Err()
}

// ReplaceAll 原子替换全部书签（事务保证）
func (d *SQLiteBookmarkDAO) ReplaceAll(bookmarks []config.BrowserBookmark) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM browser_bookmarks`); err != nil {
		return fmt.Errorf("清空书签失败: %w", err)
	}
	for i, b := range bookmarks {
		if b.Name == "" || b.URL == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO browser_bookmarks (name, url, sort_order) VALUES (?, ?, ?)`,
			b.Name, b.URL, i,
		); err != nil {
			return fmt.Errorf("插入书签失败: %w", err)
		}
	}
	return tx.Commit()
}
