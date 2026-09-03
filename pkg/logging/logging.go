package logging

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

var MaxFormPrefix = 256

func SetMaxFormPrefix(i int) {
	MaxFormPrefix = i
}

func FormPrefix(form url.Values) string {
	var buf strings.Builder

	appendBuf := func(s string) bool {
		if buf.Len()+len(s) >= MaxFormPrefix {
			remaining := MaxFormPrefix - buf.Len()
			if remaining > 0 {
				buf.WriteString(s[:remaining])
			}
			return false
		}

		buf.WriteString(s)
		return true
	}
	for k, values := range form {
		keyEscaped := url.QueryEscape(k)
		for _, v := range values {
			if buf.Len() >= MaxFormPrefix {
				return buf.String()
			}
			if buf.Len() > 0 {
				buf.WriteByte('&')
			}
			if !appendBuf(keyEscaped) {
				return buf.String()
			}
			buf.WriteByte('=')
			if buf.Len()+len(v) >= MaxFormPrefix {
				remaining := MaxFormPrefix - buf.Len()
				if remaining > 0 {
					if !appendBuf(url.QueryEscape(v[:remaining])) {
						return buf.String()
					}
				}
			} else if !appendBuf(url.QueryEscape(v)) {
				return buf.String()
			}
		}
	}
	return buf.String()
}

const ApacheFormatPattern = "%s - - [%s] \"%s %d %d\" %f %s\n"

type ApacheLogRecord struct {
	http.ResponseWriter `json:"-"`

	IP                    string    `json:"remoteAddr,omitempty"`
	Time                  time.Time `json:"time,omitempty"`
	Method                string    `json:"method,omitempty"`
	URI                   string    `json:"path,omitempty"`
	Protocol              string    `json:"protocol,omitempty"`
	Status                int       `json:"status,omitempty"`
	ResponseBytes         int64     `json:"responseBytes,omitempty"`
	ElapsedTime           float64   `json:"duration,omitempty"`
	FormPrefix            string    `json:"query,omitempty"`
	ResponseBody          any       `json:"responseBody,omitempty"`
	ResponseBodyTruncated bool      `json:"responseBodyTruncated,omitempty"`

	responseBody      limitedBuffer
	responseBodyGzip  bool
	responseBodyLimit int64
}

type limitedBuffer struct {
	bytes.Buffer
	limit     int64
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= int64(b.Len()) {
		b.truncated = b.truncated || len(p) > 0
		return len(p), nil
	}
	remaining := b.limit - int64(b.Len())
	if int64(len(p)) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, err := b.Buffer.Write(p)
	return len(p), err
}

func responseBodyLoggingConfig() (bool, int64) {
	if strings.ToLower(os.Getenv("PROMXY_RESPONSE_BODY_LOGGING")) != "true" {
		return false, 0
	}

	const defaultLimit = 64 * 1024
	limit, err := strconv.ParseInt(os.Getenv("PROMXY_RESPONSE_BODY_MAX_BYTES"), 10, 64)
	if err != nil || limit <= 0 {
		return true, defaultLimit
	}
	return true, limit
}

func (r *ApacheLogRecord) Write(p []byte) (int, error) {
	if r.responseBodyLimit > 0 && !r.responseBodyGzip {
		r.responseBodyGzip = strings.Contains(strings.ToLower(r.ResponseWriter.Header().Get("Content-Encoding")), "gzip")
	}
	written, err := r.ResponseWriter.Write(p)
	r.ResponseBytes += int64(written)
	if r.responseBodyLimit > 0 {
		r.responseBody.Write(p[:written])
	}
	return written, err
}

func (r *ApacheLogRecord) WriteHeader(status int) {
	r.Status = status
	r.ResponseWriter.WriteHeader(status)
	if r.responseBodyLimit > 0 {
		r.responseBodyGzip = strings.Contains(strings.ToLower(r.ResponseWriter.Header().Get("Content-Encoding")), "gzip")
	}
}

func (r *ApacheLogRecord) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *ApacheLogRecord) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("http.Hijacker is not supported")
	}
	return h.Hijack()
}

func (r *ApacheLogRecord) Push(target string, opts *http.PushOptions) error {
	p, ok := r.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

func (r *ApacheLogRecord) ReadFrom(src io.Reader) (int64, error) {
	if r.responseBodyLimit <= 0 {
		n, err := io.Copy(r.ResponseWriter, src)
		r.ResponseBytes += n
		return n, err
	}

	tee := io.TeeReader(src, &responseCapture{record: r})
	n, err := io.Copy(r.ResponseWriter, tee)
	r.ResponseBytes += n
	return n, err
}

type responseCapture struct {
	record *ApacheLogRecord
}

func (c *responseCapture) Write(p []byte) (int, error) {
	_, err := c.record.responseBody.Write(p)
	return len(p), err
}

func (r *ApacheLogRecord) responseBodyValue() any {
	if r.responseBody.Len() == 0 {
		return nil
	}
	body := r.responseBody.Bytes()
	if r.responseBodyGzip && len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return fmt.Sprintf("[error decompressing response body: %v]", err)
		}
		decompressed, err := io.ReadAll(io.LimitReader(reader, r.responseBodyLimit+1))
		reader.Close()
		if err != nil {
			return fmt.Sprintf("[error reading decompressed response body: %v]", err)
		}
		if int64(len(decompressed)) > r.responseBodyLimit {
			r.ResponseBodyTruncated = true
			decompressed = decompressed[:r.responseBodyLimit]
		}
		body = decompressed
	}

	var value any
	if json.Unmarshal(body, &value) == nil {
		if os.Getenv("PROMXY_REDACT_SUCCESS_RESPONSE_RESULTS") == "true" {
			if response, ok := value.(map[string]any); ok && response["status"] == "success" {
				if data, ok := response["data"].(map[string]any); ok {
					delete(data, "result")
				}
			}
		}
		return value
	}
	return string(body)
}

func (r *ApacheLogRecord) Log(out io.Writer) {
	timeFormatted := r.Time.Format("02/Jan/2006 15:04:05")
	requestLine := fmt.Sprintf("%s %s %s", r.Method, r.URI, r.Protocol)
	fmt.Fprintf(out, ApacheFormatPattern, r.IP, timeFormatted, requestLine, r.Status, r.ResponseBytes,
		r.ElapsedTime, r.FormPrefix)
}

func (r *ApacheLogRecord) LogJson(out io.Writer) {
	if r.responseBodyLimit > 0 {
		r.ResponseBody = r.responseBodyValue()
		r.ResponseBodyTruncated = r.ResponseBodyTruncated || r.responseBody.truncated
	}
	data, err := json.Marshal(r)
	if err == nil {
		out.Write(append(data, byte(10)))
	}
}

type LogRecordHandler func(*ApacheLogRecord)

func LogToWriter(out io.Writer) LogRecordHandler {
	return func(l *ApacheLogRecord) {
		l.Log(out)
	}
}

func LogJsonToWriter(out io.Writer) LogRecordHandler {
	return func(l *ApacheLogRecord) {
		l.LogJson(out)
	}
}

type ApacheLoggingHandler struct {
	handler     http.Handler
	logHandlers []LogRecordHandler
}

func NewApacheLoggingHandler(handler http.Handler, logHandlers ...LogRecordHandler) http.Handler {
	return &ApacheLoggingHandler{
		handler:     handler,
		logHandlers: logHandlers,
	}
}

func (h *ApacheLoggingHandler) runHandler(rw http.ResponseWriter, r *http.Request) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = errors.Wrap(errors.New(string(debug.Stack())), "Error running handler")
		}
	}()
	h.handler.ServeHTTP(rw, r)
	return
}

func (h *ApacheLoggingHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	clientIP := r.RemoteAddr
	if colon := strings.LastIndex(clientIP, ":"); colon != -1 {
		clientIP = clientIP[:colon]
	}

	r.ParseForm()
	enabled, limit := responseBodyLoggingConfig()
	record := &ApacheLogRecord{
		ResponseWriter: rw,
		IP:             clientIP,
		Method:         r.Method,
		URI:            r.URL.Path,
		Protocol:       r.Proto,
		Status:         http.StatusOK,
		FormPrefix:     FormPrefix(r.Form),
	}
	if enabled {
		record.responseBodyLimit = limit
	}

	startTime := time.Now()
	if err := h.runHandler(record, r); err != nil {
		rw.Header().Del("Content-Encoding")
		http.Error(record, err.Error(), http.StatusInternalServerError)
	}
	finishTime := time.Now()

	record.Time = finishTime.UTC()
	record.ElapsedTime = finishTime.Sub(startTime).Seconds()

	for _, logHandler := range h.logHandlers {
		logHandler(record)
	}
}
