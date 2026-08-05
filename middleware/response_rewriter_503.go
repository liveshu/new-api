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

var zhGroupPattern = regexp.MustCompile(`分组\s+(.+?)\s+下模型`)
var enGroupPattern = regexp.MustCompile(`under group\s+(.+?)\s*\(`)

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

		// 非流式响应，检查是否需要重写
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
			// 如果之前有缓冲的数据，先发送出去
			if w.buf.Len() > 0 {
				w.ResponseWriter.WriteHeader(w.status)
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
	// 不要立即写入 ResponseWriter，等 Write 时判断
}

func (w *flushableBuffer) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

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
