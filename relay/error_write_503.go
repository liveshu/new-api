package relay

import (
	"regexp"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// 匹配中文格式：分组 xxx 下模型
var zhGroupPattern = regexp.MustCompile(`分组\s+(.+?)\s+下模型`)

// 匹配英文格式：under group xxx (distributor)
var enGroupPattern = regexp.MustCompile(`under group\s+(.+?)\s*\(`)

// RewriteUpstreamGroup 将错误信息中的上游分组名替换为本站分组名
func RewriteUpstreamGroup(err *types.NewAPIError, localGroup string) {
	if err == nil || localGroup == "" {
		return
	}

	msg := err.Error()
	if msg == "" {
		return
	}

	// 英文格式替换
	if enGroupPattern.MatchString(msg) {
		msg = enGroupPattern.ReplaceAllString(msg, "under group "+localGroup+" (")
	}

	// 中文格式替换
	if zhGroupPattern.MatchString(msg) {
		msg = zhGroupPattern.ReplaceAllString(msg, "分组 "+localGroup+" 下模型")
	}

	err.SetMessage(msg)

	// ↓↓↓ 补这里：同步更新 RelayError，客户端实际读的是这个 ↓↓↓
	switch re := err.RelayError.(type) {
	case types.OpenAIError:
		re.Message = msg
		err.RelayError = re
	case types.ClaudeError:
		re.Message = msg
		err.RelayError = re
	}
}
