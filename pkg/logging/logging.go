package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime/debug"
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

	// appendBuf will append s to buf until it is "filled" (based on MaxFormPrefix)
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
			} else {
				if !appendBuf(url.QueryEscape(v)) {
					return buf.String()
				}
			}
		}
	}
	return buf.String()
}

const ApacheFormatPattern = "%s - - [%s] \"%s %d %d\" %f %s\n"

type ApacheLogRecord struct {
	http.ResponseWriter `json:"-"`

	IP            string    `json:"remoteAddr,omitempty"`
	Time          time.Time `json:"time,omitempty"`
	Method        string    `json:"method,omitempty"`
	URI           string    `json:"path,omitempty"`
	Protocol      string    `json:"protocol,omitempty"`
	Status        int       `json:"status,omitempty"`
	ResponseBytes int64     `json:"responseBytes,omitempty"`
	ElapsedTime   float64   `json:"duration,omitempty"`
	FormPrefix    string    `json:"query,omitempty"`
}

func (r *ApacheLogRecord) Log(out io.Writer) {
	timeFormatted := r.Time.Format("02/Jan/2006 15:04:05")
	requestLine := fmt.Sprintf("%s %s %s", r.Method, r.URI, r.Protocol)
	fmt.Fprintf(out, ApacheFormatPattern, r.IP, timeFormatted, requestLine, r.Status, r.ResponseBytes,
		r.ElapsedTime, r.FormPrefix)
}

func (r *ApacheLogRecord) LogJson(out io.Writer) {
	data, err := json.Marshal(r)
	if err == nil {
		out.Write(append(data, byte(10)))
	}
}

func (r *ApacheLogRecord) Write(p []byte) (int, error) {
	written, err := r.ResponseWriter.Write(p)
	r.ResponseBytes += int64(written)
	return written, err
}

func (r *ApacheLogRecord) WriteHeader(status int) {
	r.Status = status
	r.ResponseWriter.WriteHeader(status)
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
			// Just return a stack trace always
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

	record := &ApacheLogRecord{
		ResponseWriter: rw,
		IP:             clientIP,
		Method:         r.Method,
		URI:            r.URL.Path,
		Protocol:       r.Proto,
		Status:         http.StatusOK,
		FormPrefix:     FormPrefix(r.Form),
	}

	startTime := time.Now()
	if err := h.runHandler(record, r); err != nil {
		// If we have an error we want to clear any Content-Encoding that may have been set
		// as we are just going to write direct
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
