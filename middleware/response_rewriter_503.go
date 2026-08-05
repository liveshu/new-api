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

		body := bw.buf.Bytes()
		localGroup := resolveLocalGroup(c)

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
