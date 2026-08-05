package middleware

import (
	"bytes"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

// 匹配中文格式：分组 xxx 下模型
var zhGroupPattern = regexp.MustCompile(`分组\s+(.+?)\s+下模型`)

// 匹配英文格式：under group xxx (distributor)
var enGroupPattern = regexp.MustCompile(`under group\s+(.+?)\s*\(`)

// ResponseRewriter 拦截上游透传的 503 错误响应，
// 自动从当前请求 context 中获取本站点分组名，替换上游的分组名。
//
// 支持流式和非流式响应：
// - 流式响应（text/event-stream）：检测到后立即切换为直写模式，实时 flush
// - 非流式响应：缓冲后统一处理，重写错误信息中的分组名
func ResponseRewriter() gin.HandlerFunc {
	return func(c *gin.Context) {
		bw := &flushableBuffer{
			ResponseWriter: c.Writer,
			buf:            &bytes.Buffer{},
		}
		c.Writer = bw
		c.Next()

		// 如果是流式响应，数据已经通过 Flush 实时发送了，直接返回
		if bw.streamed {
			return
		}

		// 只对 503 做检查和替换
		if bw.status != http.StatusServiceUnavailable {
			bw.ResponseWriter.WriteHeader(bw.status)
			_, _ = bw.ResponseWriter.Write(bw.buf.Bytes())
			return
		}

		body := bw.buf.Bytes()
		localGroup := resolveLocalGroup(c)
		if localGroup == "" {
			bw.ResponseWriter.WriteHeader(bw.status)
			_, _ = bw.ResponseWriter.Write(body)
			return
		}

		rewritten := false
		if matches := enGroupPattern.FindSubmatch(body); len(matches) >= 2 {
			body = bytes.Replace(body, matches[1], []byte(localGroup), 1)
			rewritten = true
		}
		if matches := zhGroupPattern.FindSubmatch(body); len(matches) >= 2 {
			body = bytes.Replace(body, matches[1], []byte(localGroup), 1)
			rewritten = true
		}

		if rewritten {
			body = bytes.Replace(body, []byte(" ("), []byte(" ("), -1)
			bw.Header().Set("Content-Length", strconv.Itoa(len(body)))
			bw.ResponseWriter.WriteHeader(bw.status)
			_, _ = bw.ResponseWriter.Write(body)
			return
		}

		bw.ResponseWriter.WriteHeader(bw.status)
		_, _ = bw.ResponseWriter.Write(body)
	}
}

// flushableBuffer 是一个可 flush 的 response buffer
// 当检测到流式响应时，数据会直接 flush 到客户端
// 非流式响应会被缓冲，用于后续重写
type flushableBuffer struct {
	gin.ResponseWriter
	buf      *bytes.Buffer
	status   int
	streamed bool // 标记是否已检测到流式响应并实时发送
}

func (w *flushableBuffer) Write(b []byte) (int, error) {
	// 检查是否是流式响应（首次写入时检测）
	if !w.streamed {
		contentType := w.ResponseWriter.Header().Get("Content-Type")
		if strings.Contains(contentType, "text/event-stream") {
			w.streamed = true
			// 先写入状态码
			w.ResponseWriter.WriteHeader(w.status)
			// 如果之前有缓冲的数据，先发送出去
			if w.buf.Len() > 0 {
				w.ResponseWriter.Write(w.buf.Bytes())
				w.buf.Reset()
			}
		}
	}

	// 如果是流式响应，直接写入并 flush
	if w.streamed {
		n, err := w.ResponseWriter.Write(b)
		if f, ok := w.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
		return n, err
	}

	// 非流式，缓冲起来
	return w.buf.Write(b)
}

func (w *flushableBuffer) WriteHeader(code int) {
	w.status = code
	// 如果是流式响应，立即写入状态码
	if w.streamed {
		w.ResponseWriter.WriteHeader(code)
	}
	// 非流式响应延迟写入，等最后统一处理
}

func (w *flushableBuffer) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// resolveLocalGroup 按优先级获取本站点应展示的分组名
func resolveLocalGroup(c *gin.Context) string {
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group != "" && group != "auto" {
		return group
	}
	if group == "auto" {
		autoGroup := common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
		if autoGroup != "" {
			return "auto(" + autoGroup + ")"
		}
		return "auto"
	}
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	if userGroup != "" {
		return userGroup
	}
	return ""
}
