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

		if bw.streamed {
			return
		}

		if bw.status != http.StatusServiceUnavailable {
			if bw.status > 0 {
				bw.ResponseWriter.WriteHeader(bw.status)
			}
			_, _ = bw.ResponseWriter.Write(bw.buf.Bytes())
			return
		}

		body := bw.buf.Bytes()
		localGroup := resolveLocalGroup(c)

		// 如果 localGroup 为空，使用用户的分组作为 fallback
		if localGroup == "" {
			userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
			if userGroup != "" {
				localGroup = userGroup
			}
		}

		if localGroup == "" {
			if bw.status > 0 {
				bw.ResponseWriter.WriteHeader(bw.status)
			}
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
			if bw.status > 0 {
				bw.ResponseWriter.WriteHeader(bw.status)
			}
			_, _ = bw.ResponseWriter.Write(body)
			return
		}

		if bw.status > 0 {
			bw.ResponseWriter.WriteHeader(bw.status)
		}
		_, _ = bw.ResponseWriter.Write(body)
	}
}

type flushableBuffer struct {
	gin.ResponseWriter
	buf      *bytes.Buffer
	status   int
	streamed bool
}

func (w *flushableBuffer) Write(b []byte) (int, error) {
	if !w.streamed {
		contentType := w.ResponseWriter.Header().Get("Content-Type")
		if strings.Contains(contentType, "text/event-stream") {
			w.streamed = true
			if w.status > 0 {
				w.ResponseWriter.WriteHeader(w.status)
			}
			if w.buf.Len() > 0 {
				w.ResponseWriter.Write(w.buf.Bytes())
				w.buf.Reset()
			}
		}
	}

	if w.streamed {
		n, err := w.ResponseWriter.Write(b)
		if f, ok := w.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
		return n, err
	}

	return w.buf.Write(b)
}

func (w *flushableBuffer) WriteHeader(code int) {
	w.status = code
	if w.streamed {
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *flushableBuffer) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
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
