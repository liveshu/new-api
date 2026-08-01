package middleware

import (
	"bytes"
	"net/http"
	"regexp"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

// 匹配中文格式：分组 xxx 下模型
// 例：分组 default 下模型 gpt-4 无可用渠道
var zhGroupPattern = regexp.MustCompile(`(分组\s+)(\S+)(\s+下模型)`)

// 匹配英文格式：under group xxx
// 例：No available channel for model gpt-4 under group default
var enGroupPattern = regexp.MustCompile(`(under group\s+)(\S+)`)

// ResponseRewriter 拦截上游透传的 503 错误响应，
// 自动从当前请求 context 中获取本站点分组名，替换上游的分组名。
//
// 分组名来源（按优先级）：
//   - ContextKeyUsingGroup：auth 中间件设置，来自 token 指定或用户默认分组
//   - ContextKeyUserGroup：用户主分组（fallback）
//   - ContextKeyAutoGroup：auto 模式下实际解析到的分组（fallback）
//
// 无需传参，中间件自动从 gin context 读取。
func ResponseRewriter() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 用自定义 writer 捕获响应内容到 buffer
		bw := &responseBuffer{
			ResponseWriter: c.Writer,
			buf:            &bytes.Buffer{},
		}
		c.Writer = bw

		c.Next()
		// 此时所有 handler 和 defer 块都已执行完毕，
		// 响应内容（包括 c.JSON 写入的）已缓存到 bw.buf 中

		// 只对 503 做检查和替换
		if bw.status != http.StatusServiceUnavailable {
			writeOriginal(bw)
			return
		}

		body := bw.buf.Bytes()

		// 从 context 获取本站点的分组名
		localGroup := resolveLocalGroup(c)
		if localGroup == "" {
			writeOriginal(bw)
			return
		}

		rewritten := false
		if zhGroupPattern.Match(body) {
			body = zhGroupPattern.ReplaceAll(body, []byte("${1}"+localGroup+"${3}"))
			rewritten = true
		}
		if enGroupPattern.Match(body) {
			body = enGroupPattern.ReplaceAll(body, []byte("${1}"+localGroup))
			rewritten = true
		}

		if rewritten {
			bw.Header().Set("Content-Length", strconv.Itoa(len(body)))
			bw.ResponseWriter.WriteHeader(bw.status)
			bw.ResponseWriter.Write(body)
			return
		}

		writeOriginal(bw)
	}
}

// resolveLocalGroup 按优先级获取本站点应展示的分组名
func resolveLocalGroup(c *gin.Context) string {
	// 优先取当前请求使用的分组（auth 中间件设置）
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group != "" && group != "auto" {
		return group
	}

	// auto 模式下，尝试取 distributor 实际解析出的分组
	if group == "auto" {
		autoGroup := common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
		if autoGroup != "" {
			return "auto(" + autoGroup + ")"
		}
		return "auto"
	}

	// fallback：取用户主分组
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	if userGroup != "" {
		return userGroup
	}

	return ""
}

func writeOriginal(bw *responseBuffer) {
	if bw.status > 0 {
		bw.ResponseWriter.WriteHeader(bw.status)
	}
	bw.ResponseWriter.Write(bw.buf.Bytes())
}

// responseBuffer 拦截写入 gin.ResponseWriter 的数据
type responseBuffer struct {
	gin.ResponseWriter
	buf    *bytes.Buffer
	status int
}

func (w *responseBuffer) WriteHeader(code int) {
	w.status = code
}

func (w *responseBuffer) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}
