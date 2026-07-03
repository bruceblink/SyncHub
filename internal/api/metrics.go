package api

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type requestMetricKey struct {
	method string
	path   string
	status int
}

type requestMetricValue struct {
	count           int64
	durationSeconds float64
}

type requestMetrics struct {
	mu       sync.Mutex
	requests map[requestMetricKey]requestMetricValue
}

func newRequestMetrics() *requestMetrics {
	return &requestMetrics{requests: map[requestMetricKey]requestMetricValue{}}
}

func requestMetricsMiddleware(metrics *requestMetrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		if metrics == nil {
			return
		}
		path := requestLogPath(c)
		if path == "/metrics" {
			return
		}
		metrics.record(c.Request.Method, path, c.Writer.Status(), time.Since(started))
	}
}

func (m *requestMetrics) record(method, path string, status int, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := requestMetricKey{method: method, path: path, status: status}
	value := m.requests[key]
	value.count++
	value.durationSeconds += duration.Seconds()
	m.requests[key] = value
}

func (m *requestMetrics) writePrometheus(w io.Writer) {
	m.mu.Lock()
	snapshots := make([]struct {
		key   requestMetricKey
		value requestMetricValue
	}, 0, len(m.requests))
	for key, value := range m.requests {
		snapshots = append(snapshots, struct {
			key   requestMetricKey
			value requestMetricValue
		}{key: key, value: value})
	}
	m.mu.Unlock()
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].key.method != snapshots[j].key.method {
			return snapshots[i].key.method < snapshots[j].key.method
		}
		if snapshots[i].key.path != snapshots[j].key.path {
			return snapshots[i].key.path < snapshots[j].key.path
		}
		return snapshots[i].key.status < snapshots[j].key.status
	})

	fmt.Fprintln(w, "# HELP synchub_http_requests_total Total HTTP requests handled by the API.")
	fmt.Fprintln(w, "# TYPE synchub_http_requests_total counter")
	for _, snapshot := range snapshots {
		fmt.Fprintf(w, "synchub_http_requests_total{method=\"%s\",path=\"%s\",status=\"%s\"} %d\n",
			prometheusEscape(snapshot.key.method),
			prometheusEscape(snapshot.key.path),
			prometheusEscape(strconv.Itoa(snapshot.key.status)),
			snapshot.value.count,
		)
	}
	fmt.Fprintln(w, "# HELP synchub_http_request_duration_seconds_total Total HTTP request duration in seconds.")
	fmt.Fprintln(w, "# TYPE synchub_http_request_duration_seconds_total counter")
	for _, snapshot := range snapshots {
		fmt.Fprintf(w, "synchub_http_request_duration_seconds_total{method=\"%s\",path=\"%s\",status=\"%s\"} %.9f\n",
			prometheusEscape(snapshot.key.method),
			prometheusEscape(snapshot.key.path),
			prometheusEscape(strconv.Itoa(snapshot.key.status)),
			snapshot.value.durationSeconds,
		)
	}
}

func (s *Server) metricsHandler(c *gin.Context) {
	c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if s.metrics == nil {
		c.String(http.StatusOK, "")
		return
	}
	c.Status(http.StatusOK)
	s.metrics.writePrometheus(c.Writer)
}

func prometheusEscape(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	return value
}
