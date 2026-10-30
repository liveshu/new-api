package controller

// BEGIN: 固定组预算、继承、Auto 和真实任务提交循环的集中回归。
import (
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFixedGroupRetryBudget(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previousDB, previousPolicy := model.DB, model.CurrentRequestPolicy()
	previousRetries := common.RetryTimes
	common.OptionMapRWMutex.Lock()
	previousOptions := maps.Clone(common.OptionMap)
	common.OptionMap = maps.Clone(previousPolicy.Options)
	common.OptionMapRWMutex.Unlock()
	model.DB = db
	t.Cleanup(func() {
		require.NoError(t, model.UpdateRequestPolicyOptions(previousPolicy.Options))
		model.DB = previousDB
		common.RetryTimes = previousRetries
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, model.UpdateRequestPolicyOptions(map[string]string{"RetryTimes": "2", "GroupRetryTimes": `{"zero":0,"vip":5}`}))
	for _, tc := range []struct {
		group string
		want  int
	}{
		{"zero", 0}, {"vip", 5}, {"default", 2}, {"auto", 2}, {"", 2},
	} {
		t.Run(tc.group, func(t *testing.T) {
			param := &service.RetryParam{TokenGroup: tc.group}
			param.InitRetryBudget()
			assert.Equal(t, tc.want, param.MaxRetries())
		})
	}

	fixed := &service.RetryParam{TokenGroup: "vip"}
	auto := &service.RetryParam{TokenGroup: "auto"}
	fixed.InitRetryBudget()
	auto.InitRetryBudget()
	require.NoError(t, model.UpdateRequestPolicyOptions(map[string]string{"RetryTimes": "3", "GroupRetryTimes": `{"zero":0,"vip":1}`}))
	assert.Equal(t, 5, fixed.MaxRetries(), "in-flight explicit override stays stable")
	assert.Equal(t, 3, auto.MaxRetries(), "Auto continues to read the current global default")
	updated := &service.RetryParam{TokenGroup: "vip"}
	updated.InitRetryBudget()
	assert.Equal(t, 1, updated.MaxRetries())

	for _, tc := range []struct {
		group   string
		noRetry bool
		want    int
	}{
		{"zero", false, 1}, {"vip", false, 2}, {"default", false, 4}, {"auto", false, 4}, {"vip", true, 1},
	} {
		t.Run(tc.group+"/task", func(t *testing.T) {
			ctx := taskSubmissionTestContext()
			defer common.CleanupBodyStorage(ctx)
			info := taskSubmissionRelayInfo(nil)
			info.TokenGroup = tc.group
			info.LockedChannel = &model.Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Key: "test-key", Name: "test"}
			attempts := 0
			_, taskErr := executeTaskSubmissionWith(ctx, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
				attempts++
				return nil, &dto.TaskError{Message: "unavailable", StatusCode: http.StatusServiceUnavailable, LocalError: true, NoRetry: tc.noRetry}
			})
			require.NotNil(t, taskErr)
			assert.Equal(t, tc.want, attempts)
			if tc.group == "vip" && !tc.noRetry {
				events := service.RequestPolicy(ctx).Events()
				require.NotEmpty(t, events)
				assert.Equal(t, "group_retry", events[len(events)-1].Decision.Source)
			}
		})
	}
}

func TestFixedGroupRetryTransports(t *testing.T) {
	for _, transport := range []string{"http", "websocket"} {
		for _, tc := range []struct {
			name, global, overrides string
			want                    int64
		}{
			{"override", "0", `{"default":2}`, 3},
			{"zero", "2", `{"default":0}`, 1},
			{"inherit", "1", `{}`, 2},
		} {
			t.Run(transport+"/"+tc.name, func(t *testing.T) {
				fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {
					t.Error("unavailable upstream must not accept a websocket")
				})
				require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
				previousPolicy, previousRetries := model.CurrentRequestPolicy(), common.RetryTimes
				common.OptionMapRWMutex.Lock()
				previousOptions := maps.Clone(common.OptionMap)
				common.OptionMap = maps.Clone(previousPolicy.Options)
				common.OptionMapRWMutex.Unlock()
				t.Cleanup(func() {
					require.NoError(t, model.UpdateRequestPolicyOptions(previousPolicy.Options))
					common.RetryTimes = previousRetries
					common.OptionMapRWMutex.Lock()
					common.OptionMap = previousOptions
					common.OptionMapRWMutex.Unlock()
				})
				require.NoError(t, model.UpdateRequestPolicyOptions(map[string]string{"RetryTimes": tc.global, "GroupRetryTimes": tc.overrides}))
				var attempts atomic.Int64
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					attempts.Add(1)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"error":{"message":"unavailable","type":"server_error"}}`)
				}))
				defer upstream.Close()
				require.NoError(t, model.DB.Model(fixture.channel).Update("base_url", upstream.URL).Error)
				if transport == "websocket" {
					require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
					assert.Equal(t, "error", readResponsesWSTestEvent(t, fixture.client)["type"])
				} else {
					request, err := http.NewRequest(http.MethodPost, fixture.gatewayURL+"/v1/responses", strings.NewReader(`{"model":"ws-billing","input":"hello"}`))
					require.NoError(t, err)
					request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
					request.Header.Set("Content-Type", "application/json")
					client := &http.Client{Timeout: 5 * time.Second}
					response, err := client.Do(request)
					require.NoError(t, err)
					_, readErr := io.Copy(io.Discard, response.Body)
					require.NoError(t, response.Body.Close())
					require.NoError(t, readErr)
					assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
					select {
					case <-fixture.httpDone:
					case <-time.After(5 * time.Second):
						t.Fatal("HTTP relay did not finish")
					}
				}
				fixture.closeAndWait(t)
				assert.Equal(t, tc.want, attempts.Load())
			})
		}
	}
}

// END: 固定分组重试集中回归。
