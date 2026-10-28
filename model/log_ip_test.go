package model

import (
	"fmt"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestRequestLogsAlwaysRecordIP(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "log_ip_test_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousDB, previousLogDB := DB, LOG_DB
			previousRedis, previousConsume, previousExport := common.RedisEnabled, common.LogConsumeEnabled, common.DataExportEnabled
			previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
			DB, LOG_DB = db, db
			common.RedisEnabled, common.LogConsumeEnabled, common.DataExportEnabled = false, true, false
			common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.RedisEnabled, common.LogConsumeEnabled, common.DataExportEnabled = previousRedis, previousConsume, previousExport
				common.SetDatabaseTypes(previousMainType, previousLogType)
				assert.NoError(t, db.Migrator().DropTable(&Log{}, &User{}))
				assert.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&User{}, &Log{}))
			var version string
			versionQuery := "SELECT version()"
			if dialect == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database=%s version=%s", dialect, version)

			for i, tc := range []struct {
				name    string
				setting string
				remote  string
				wantIP  string
			}{
				{"user disables IP recording", `{"record_ip_log":false}`, "203.0.113.10:1234", "203.0.113.10"},
				{"user enables IP recording", `{"record_ip_log":true}`, "203.0.113.11:1234", "203.0.113.11"},
				{"default settings and IPv6", "", "[2001:db8::1]:1234", "2001:db8::1"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					userID := i + 1
					require.NoError(t, db.Create(&User{Id: userID, Username: fmt.Sprintf("ip-user-%d", userID), AffCode: fmt.Sprintf("ip%d", userID), Setting: tc.setting}).Error)
					ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
					ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
					ctx.Request.RemoteAddr = tc.remote
					RecordErrorLog(ctx, userID, 1, "test-model", "test-token", "upstream unavailable", 1, 0, false, "default", NewLogOther())
					RecordConsumeLog(ctx, userID, RecordConsumeLogParams{ModelName: "test-model", Quota: 17, Other: NewLogOther()})

					var logs []Log
					require.NoError(t, db.Where("user_id = ?", userID).Order("id").Find(&logs).Error)
					require.Len(t, logs, 2)
					assert.Equal(t, LogTypeError, logs[0].Type)
					assert.Equal(t, LogTypeConsume, logs[1].Type)
					for _, log := range logs {
						assert.Equal(t, tc.wantIP, log.Ip)
					}
					assert.Equal(t, 17, logs[1].Quota)
				})
			}
		})
	}
}
