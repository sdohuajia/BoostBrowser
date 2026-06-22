package launchcode_test

// Feature: instance-launch-code, Property 2: persistence round-trip
// Validates: Requirements 1.3, 5.1, 5.3

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"boost-browser/backend/internal/launchcode"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	_ "modernc.org/sqlite"
)

// newTestDB 创建内存 SQLite 数据库并执行建表迁移
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS launch_codes (
		profile_id TEXT PRIMARY KEY,
		code       TEXT NOT NULL UNIQUE,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_launch_codes_code ON launch_codes(code)`)
	if err != nil {
		t.Fatalf("建索引失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// newFileTestDB 创建基于文件的 SQLite 数据库（用于需要独立隔离的测试）
func newFileTestDB(t *testing.T) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "launchcode_test_*.db")
	if err != nil {
		t.Fatalf("创建临时数据库文件失败: %v", err)
	}
	f.Close()
	dbPath := f.Name()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS launch_codes (
		profile_id TEXT PRIMARY KEY,
		code       TEXT NOT NULL UNIQUE,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	_, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_launch_codes_code ON launch_codes(code)`)
	if err != nil {
		t.Fatalf("建索引失败: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		os.Remove(dbPath)
	})
	return db
}

// TestProperty2_PersistenceRoundTrip
// Property 2: 持久化 Round-Trip
// 对于任意 ProfileId 和 LaunchCode，Upsert 后：
//   - FindProfileId(code) 返回相同的 profileId
//   - FindCode(profileId) 返回相同的 code
func TestProperty2_PersistenceRoundTrip(t *testing.T) {
	properties := gopter.NewProperties(gopter.DefaultTestParameters())

	properties.Property("Upsert 后 FindProfileId 返回正确 profileId", prop.ForAll(
		func(profileId, code string) bool {
			db := newFileTestDB(t)
			dao := launchcode.NewSQLiteLaunchCodeDAO(db)

			if err := dao.Upsert(profileId, code); err != nil {
				return false
			}
			got, err := dao.FindProfileId(code)
			return err == nil && got == profileId
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 && len(s) <= 64 }),
		gen.RegexMatch(`[A-Z0-9]{6}`),
	))

	properties.Property("Upsert 后 FindCode 返回正确 code", prop.ForAll(
		func(profileId, code string) bool {
			db := newFileTestDB(t)
			dao := launchcode.NewSQLiteLaunchCodeDAO(db)

			if err := dao.Upsert(profileId, code); err != nil {
				return false
			}
			got, err := dao.FindCode(profileId)
			return err == nil && got == code
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 && len(s) <= 64 }),
		gen.RegexMatch(`[A-Z0-9]{6}`),
	))

	properties.Property("Upsert 幂等：相同 profileId 更新 code 后查询返回新 code", prop.ForAll(
		func(profileId, code1, code2 string) bool {
			if code1 == code2 {
				return true // 跳过相同 code 的情况
			}
			db := newFileTestDB(t)
			dao := launchcode.NewSQLiteLaunchCodeDAO(db)

			if err := dao.Upsert(profileId, code1); err != nil {
				return false
			}
			if err := dao.Upsert(profileId, code2); err != nil {
				return false
			}
			got, err := dao.FindCode(profileId)
			return err == nil && got == code2
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 && len(s) <= 64 }),
		gen.RegexMatch(`[A-Z0-9]{6}`),
		gen.RegexMatch(`[A-Z0-9]{6}`),
	))

	properties.TestingRun(t)
}

// TestProperty2_DeleteRemovesMapping
// Property 2 补充：Delete 后查询应返回 not found
func TestProperty2_DeleteRemovesMapping(t *testing.T) {
	properties := gopter.NewProperties(gopter.DefaultTestParameters())

	properties.Property("Delete 后 FindCode 返回错误", prop.ForAll(
		func(profileId, code string) bool {
			db := newFileTestDB(t)
			dao := launchcode.NewSQLiteLaunchCodeDAO(db)

			if err := dao.Upsert(profileId, code); err != nil {
				return false
			}
			if err := dao.Delete(profileId); err != nil {
				return false
			}
			_, err := dao.FindCode(profileId)
			return err != nil
		},
		gen.AlphaString().SuchThat(func(s string) bool { return len(s) > 0 && len(s) <= 64 }),
		gen.RegexMatch(`[A-Z0-9]{6}`),
	))

	properties.TestingRun(t)
}

// TestProperty2_LoadAllRoundTrip
// Property 2 补充：LoadAll 返回所有已写入的映射
func TestProperty2_LoadAllRoundTrip(t *testing.T) {
	db := newTestDB(t)
	dao := launchcode.NewSQLiteLaunchCodeDAO(db)

	// 写入一批映射
	entries := map[string]string{}
	for i := 0; i < 10; i++ {
		profileId := fmt.Sprintf("profile-%02d", i)
		code := fmt.Sprintf("CODE%02d", i)
		entries[profileId] = code
		if err := dao.Upsert(profileId, code); err != nil {
			t.Fatalf("Upsert 失败: %v", err)
		}
	}

	loaded, err := dao.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll 失败: %v", err)
	}

	for profileId, code := range entries {
		got, ok := loaded[profileId]
		if !ok {
			t.Errorf("LoadAll 缺少 profileId=%s", profileId)
			continue
		}
		if got != code {
			t.Errorf("LoadAll profileId=%s: 期望 code=%s，实际=%s", profileId, code, got)
		}
	}
}
