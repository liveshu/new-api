package relay

import (
	"regexp"

	"github.com/QuantumNous/new-api/relaykit/types"
)

var zhGroupPattern = regexp.MustCompile(`分组\s+(.+?)\s+下模型`)
var enGroupPattern = regexp.MustCompile(`under group\s+(.+?)\s*\(`)

func RewriteUpstreamGroup(err *types.NewAPIError, localGroup string) {
	if err == nil || localGroup == "" {
		return
	}
	msg := err.Error()
	if msg == "" {
		return
	}

	// 只处理 "No available channel" / "可用渠道不存在" 这类 model not found 错误
	replaced := false
	if enGroupPattern.MatchString(msg) {
		msg = enGroupPattern.ReplaceAllString(msg, "under group "+localGroup+" (")
		replaced = true
	}
	if zhGroupPattern.MatchString(msg) {
		msg = zhGroupPattern.ReplaceAllString(msg, "分组 "+localGroup+" 下模型")
		replaced = true
	}
	if !replaced {
		return
	}

	err.SetMessage(msg)

	switch re := err.RelayError.(type) {
	case types.OpenAIError:
		re.Message = msg
		re.Code = "model_not_found"
		err.RelayError = re
	case types.ClaudeError:
		re.Message = msg
		re.Type = "model_not_found"
		err.RelayError = re
	}
}
