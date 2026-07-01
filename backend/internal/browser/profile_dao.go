package browser

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ProfileDAO 实例配置持久化接口
type ProfileDAO interface {
	List() ([]*Profile, error)
	GetById(profileId string) (*Profile, error)
	Upsert(profile *Profile) error
	Delete(profileId string) error
}

// SQLiteProfileDAO 基于 SQLite 的 ProfileDAO 实现
type SQLiteProfileDAO struct {
	db *sql.DB
}

// NewSQLiteProfileDAO 创建 SQLiteProfileDAO
func NewSQLiteProfileDAO(db *sql.DB) *SQLiteProfileDAO {
	return &SQLiteProfileDAO{db: db}
}

// List 查询所有实例配置，按创建时间升序
func (d *SQLiteProfileDAO) List() ([]*Profile, error) {
	rows, err := d.db.Query(`
		SELECT profile_id, profile_name, user_data_dir, core_id,
		       fingerprint_args, proxy_id, proxy_config,
		       COALESCE(proxy_bind_source_id, ''), COALESCE(proxy_bind_source_url, ''),
		       COALESCE(proxy_bind_name, ''), COALESCE(proxy_bind_updated_at, ''),
		       launch_args, COALESCE(last_tabs, '[]'),
		       tags, keywords, group_id, created_at, updated_at,
		       COALESCE(last_window_x, 0), COALESCE(last_window_y, 0),
		       COALESCE(last_window_width, 0), COALESCE(last_window_height, 0)
		FROM browser_profiles ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("查询实例列表失败: %w", err)
	}
	defer rows.Close()

	var list []*Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// GetById 根据 profileId 查询单个实例
func (d *SQLiteProfileDAO) GetById(profileId string) (*Profile, error) {
	row := d.db.QueryRow(`
		SELECT profile_id, profile_name, user_data_dir, core_id,
		       fingerprint_args, proxy_id, proxy_config,
		       COALESCE(proxy_bind_source_id, ''), COALESCE(proxy_bind_source_url, ''),
		       COALESCE(proxy_bind_name, ''), COALESCE(proxy_bind_updated_at, ''),
		       launch_args, COALESCE(last_tabs, '[]'),
		       tags, keywords, group_id, created_at, updated_at,
		       COALESCE(last_window_x, 0), COALESCE(last_window_y, 0),
		       COALESCE(last_window_width, 0), COALESCE(last_window_height, 0)
		FROM browser_profiles WHERE profile_id = ?`, profileId)
	p, err := scanProfile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("实例不存在: %s", profileId)
	}
	return p, err
}

// Upsert 新增或更新实例配置
func (d *SQLiteProfileDAO) Upsert(profile *Profile) error {
	fingerprintArgs, _ := json.Marshal(profile.FingerprintArgs)
	launchArgs, _ := json.Marshal(profile.LaunchArgs)
	lastTabs, _ := json.Marshal(profile.LastTabs)
	tags, _ := json.Marshal(profile.Tags)
	keywords, _ := json.Marshal(profile.Keywords)

	now := time.Now().Format(time.RFC3339)
	if profile.CreatedAt == "" {
		profile.CreatedAt = now
	}
	if profile.UpdatedAt == "" {
		profile.UpdatedAt = now
	}

	_, err := d.db.Exec(`
		INSERT INTO browser_profiles
		  (profile_id, profile_name, user_data_dir, core_id, fingerprint_args,
		   proxy_id, proxy_config, proxy_bind_source_id, proxy_bind_source_url, proxy_bind_name, proxy_bind_updated_at,
		   launch_args, last_tabs, tags, keywords, group_id, created_at, updated_at,
		   last_window_x, last_window_y, last_window_width, last_window_height)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(profile_id) DO UPDATE SET
		  profile_name     = excluded.profile_name,
		  user_data_dir    = excluded.user_data_dir,
		  core_id          = excluded.core_id,
		  fingerprint_args = excluded.fingerprint_args,
		  proxy_id         = excluded.proxy_id,
		  proxy_config     = excluded.proxy_config,
		  proxy_bind_source_id = excluded.proxy_bind_source_id,
		  proxy_bind_source_url = excluded.proxy_bind_source_url,
		  proxy_bind_name = excluded.proxy_bind_name,
		  proxy_bind_updated_at = excluded.proxy_bind_updated_at,
		  launch_args      = excluded.launch_args,
		  last_tabs        = excluded.last_tabs,
		  tags             = excluded.tags,
		  keywords         = excluded.keywords,
		  group_id         = excluded.group_id,
		  updated_at       = excluded.updated_at,
		  last_window_x      = excluded.last_window_x,
		  last_window_y      = excluded.last_window_y,
		  last_window_width  = excluded.last_window_width,
		  last_window_height = excluded.last_window_height`,
		profile.ProfileId, profile.ProfileName, profile.UserDataDir, profile.CoreId,
		string(fingerprintArgs), profile.ProxyId, profile.ProxyConfig,
		profile.ProxyBindSourceID, profile.ProxyBindSourceURL, profile.ProxyBindName, profile.ProxyBindUpdatedAt,
		string(launchArgs), string(lastTabs), string(tags), string(keywords), profile.GroupId,
		profile.CreatedAt, profile.UpdatedAt,
		profile.LastWindowX, profile.LastWindowY, profile.LastWindowWidth, profile.LastWindowHeight,
	)
	if err != nil {
		return fmt.Errorf("保存实例配置失败: %w", err)
	}
	return nil
}

// Delete 删除实例配置
func (d *SQLiteProfileDAO) Delete(profileId string) error {
	_, err := d.db.Exec(`DELETE FROM browser_profiles WHERE profile_id = ?`, profileId)
	if err != nil {
		return fmt.Errorf("删除实例配置失败: %w", err)
	}
	return nil
}

// ListByGroup 按分组筛选实例
// groupId 为空字符串时返回未分组的实例
// includeChildren=true 时同时包含 childGroupIds 中的子分组实例
func (d *SQLiteProfileDAO) ListByGroup(groupId string, includeChildren bool, childGroupIds []string) ([]*Profile, error) {
	var rows *sql.Rows
	var err error

	if includeChildren && len(childGroupIds) > 0 {
		// 构建 IN 子句，包含当前分组和所有子分组
		allIds := append([]string{groupId}, childGroupIds...)
		inClause := ""
		args := make([]interface{}, len(allIds))
		for i, id := range allIds {
			if i > 0 {
				inClause += ","
			}
			inClause += "?"
			args[i] = id
		}
		rows, err = d.db.Query(fmt.Sprintf(`
			SELECT profile_id, profile_name, user_data_dir, core_id,
			       fingerprint_args, proxy_id, proxy_config,
			       COALESCE(proxy_bind_source_id, ''), COALESCE(proxy_bind_source_url, ''),
			       COALESCE(proxy_bind_name, ''), COALESCE(proxy_bind_updated_at, ''),
			       launch_args, COALESCE(last_tabs, '[]'),
			       tags, keywords, group_id, created_at, updated_at,
			       COALESCE(last_window_x, 0), COALESCE(last_window_y, 0),
			       COALESCE(last_window_width, 0), COALESCE(last_window_height, 0)
			FROM browser_profiles WHERE group_id IN (%s) ORDER BY created_at ASC`, inClause), args...)
	} else {
		// 仅查询指定分组
		rows, err = d.db.Query(`
			SELECT profile_id, profile_name, user_data_dir, core_id,
			       fingerprint_args, proxy_id, proxy_config,
			       COALESCE(proxy_bind_source_id, ''), COALESCE(proxy_bind_source_url, ''),
			       COALESCE(proxy_bind_name, ''), COALESCE(proxy_bind_updated_at, ''),
			       launch_args, COALESCE(last_tabs, '[]'),
			       tags, keywords, group_id, created_at, updated_at,
			       COALESCE(last_window_x, 0), COALESCE(last_window_y, 0),
			       COALESCE(last_window_width, 0), COALESCE(last_window_height, 0)
			FROM browser_profiles WHERE group_id = ? ORDER BY created_at ASC`, groupId)
	}

	if err != nil {
		return nil, fmt.Errorf("按分组查询实例失败: %w", err)
	}
	defer rows.Close()

	var list []*Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// MoveToGroup 批量移动实例到分组
func (d *SQLiteProfileDAO) MoveToGroup(profileIds []string, groupId string) error {
	if len(profileIds) == 0 {
		return nil
	}
	inClause := ""
	args := make([]interface{}, len(profileIds)+1)
	args[0] = groupId
	for i, id := range profileIds {
		if i > 0 {
			inClause += ","
		}
		inClause += "?"
		args[i+1] = id
	}
	_, err := d.db.Exec(fmt.Sprintf(`UPDATE browser_profiles SET group_id = ? WHERE profile_id IN (%s)`, inClause), args...)
	if err != nil {
		return fmt.Errorf("批量移动实例失败: %w", err)
	}
	return nil
}

// scanner 统一扫描接口，兼容 *sql.Row 和 *sql.Rows
type scanner interface {
	Scan(dest ...any) error
}

func scanProfile(s scanner) (*Profile, error) {
	var (
		fingerprintArgsJSON, launchArgsJSON, lastTabsJSON, tagsJSON, keywordsJSON string
		p                                                                         Profile
	)
	err := s.Scan(
		&p.ProfileId, &p.ProfileName, &p.UserDataDir, &p.CoreId,
		&fingerprintArgsJSON, &p.ProxyId, &p.ProxyConfig,
		&p.ProxyBindSourceID, &p.ProxyBindSourceURL, &p.ProxyBindName, &p.ProxyBindUpdatedAt,
		&launchArgsJSON, &lastTabsJSON, &tagsJSON, &keywordsJSON, &p.GroupId,
		&p.CreatedAt, &p.UpdatedAt,
		&p.LastWindowX, &p.LastWindowY, &p.LastWindowWidth, &p.LastWindowHeight,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(fingerprintArgsJSON), &p.FingerprintArgs)
	_ = json.Unmarshal([]byte(launchArgsJSON), &p.LaunchArgs)
	_ = json.Unmarshal([]byte(lastTabsJSON), &p.LastTabs)
	_ = json.Unmarshal([]byte(tagsJSON), &p.Tags)
	_ = json.Unmarshal([]byte(keywordsJSON), &p.Keywords)
	if p.FingerprintArgs == nil {
		p.FingerprintArgs = []string{}
	}
	if p.LaunchArgs == nil {
		p.LaunchArgs = []string{}
	}
	if p.LastTabs == nil {
		p.LastTabs = []string{}
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if p.Keywords == nil {
		p.Keywords = []string{}
	}
	return &p, nil
}
