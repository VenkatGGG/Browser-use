package api

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/VenkatGGG/Browser-use/internal/task"
	"github.com/VenkatGGG/Browser-use/pkg/httpx"
)

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	limit := 500
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		if parsed > 2000 {
			parsed = 2000
		}
		limit = parsed
	}

	items, err := s.tasks.ListRecent(r.Context(), limit)
	if err != nil {
		http.Error(w, "tasks metrics failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	nodes, err := s.nodes.List(r.Context())
	if err != nil {
		http.Error(w, "nodes metrics failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	statusCounts := map[string]int{
		string(task.StatusQueued):    0,
		string(task.StatusRunning):   0,
		string(task.StatusCompleted): 0,
		string(task.StatusFailed):    0,
		string(task.StatusCanceled):  0,
	}
	blockerCounts := make(map[string]int)
	terminal := 0
	completed := 0
	failed := 0
	blocked := 0
	sandboxCrashes := 0
	stepLatenciesMS := make([]int64, 0, len(items)*2)
	for _, item := range items {
		status := strings.TrimSpace(string(item.Status))
		if status == "" {
			status = "unknown"
		}
		statusCounts[status]++
		if item.Status == task.StatusCompleted || item.Status == task.StatusFailed || item.Status == task.StatusCanceled {
			terminal++
		}
		if item.Status == task.StatusCompleted {
			completed++
		}
		if item.Status == task.StatusFailed {
			failed++
			if isSandboxCrashError(item.ErrorMessage) {
				sandboxCrashes++
			}
		}
		if blocker := strings.TrimSpace(item.BlockerType); blocker != "" {
			blocked++
			blockerCounts[blocker]++
		}
		for _, step := range item.Trace {
			if step.DurationMS > 0 {
				stepLatenciesMS = append(stepLatenciesMS, step.DurationMS)
			}
		}
	}

	nodeStateCounts := map[string]int{}
	for _, node := range nodes {
		state := strings.TrimSpace(string(node.State))
		if state == "" {
			state = "unknown"
		}
		nodeStateCounts[state]++
	}

	successRate := 0.0
	if terminal > 0 {
		successRate = (float64(completed) / float64(terminal)) * 100.0
	}
	blockRate := 0.0
	if failed > 0 {
		blockRate = (float64(blocked) / float64(failed)) * 100.0
	}
	sandboxCrashRate := 0.0
	if failed > 0 {
		sandboxCrashRate = (float64(sandboxCrashes) / float64(failed)) * 100.0
	}
	p95StepLatency := percentile(stepLatenciesMS, 95)

	var b strings.Builder
	fmt.Fprintln(&b, "# HELP browseruse_tasks_window_size Number of recent tasks used for metrics")
	fmt.Fprintln(&b, "# TYPE browseruse_tasks_window_size gauge")
	fmt.Fprintf(&b, "browseruse_tasks_window_size %d\n", len(items))

	fmt.Fprintln(&b, "# HELP browseruse_tasks_status_total Task count by status")
	fmt.Fprintln(&b, "# TYPE browseruse_tasks_status_total gauge")
	for _, key := range sortedIntMapKeys(statusCounts) {
		fmt.Fprintf(&b, "browseruse_tasks_status_total{status=%q} %d\n", metricLabelEscape(key), statusCounts[key])
	}

	fmt.Fprintln(&b, "# HELP browseruse_tasks_terminal_total Terminal tasks in metrics window")
	fmt.Fprintln(&b, "# TYPE browseruse_tasks_terminal_total gauge")
	fmt.Fprintf(&b, "browseruse_tasks_terminal_total %d\n", terminal)
	fmt.Fprintln(&b, "# HELP browseruse_tasks_completed_total Completed tasks in metrics window")
	fmt.Fprintln(&b, "# TYPE browseruse_tasks_completed_total gauge")
	fmt.Fprintf(&b, "browseruse_tasks_completed_total %d\n", completed)
	fmt.Fprintln(&b, "# HELP browseruse_tasks_failed_total Failed tasks in metrics window")
	fmt.Fprintln(&b, "# TYPE browseruse_tasks_failed_total gauge")
	fmt.Fprintf(&b, "browseruse_tasks_failed_total %d\n", failed)
	fmt.Fprintln(&b, "# HELP browseruse_tasks_blocked_total Blocker-tagged tasks in metrics window")
	fmt.Fprintln(&b, "# TYPE browseruse_tasks_blocked_total gauge")
	fmt.Fprintf(&b, "browseruse_tasks_blocked_total %d\n", blocked)
	fmt.Fprintln(&b, "# HELP browseruse_tasks_success_rate_percent Success rate percent over terminal tasks")
	fmt.Fprintln(&b, "# TYPE browseruse_tasks_success_rate_percent gauge")
	fmt.Fprintf(&b, "browseruse_tasks_success_rate_percent %.2f\n", successRate)
	fmt.Fprintln(&b, "# HELP browseruse_tasks_block_rate_percent Blocked/failed percent")
	fmt.Fprintln(&b, "# TYPE browseruse_tasks_block_rate_percent gauge")
	fmt.Fprintf(&b, "browseruse_tasks_block_rate_percent %.2f\n", blockRate)
	fmt.Fprintln(&b, "# HELP browseruse_tasks_sandbox_crash_total Sandbox-like execution crashes in failed tasks window")
	fmt.Fprintln(&b, "# TYPE browseruse_tasks_sandbox_crash_total gauge")
	fmt.Fprintf(&b, "browseruse_tasks_sandbox_crash_total %d\n", sandboxCrashes)
	fmt.Fprintln(&b, "# HELP browseruse_tasks_sandbox_crash_rate_percent Sandbox-like crash rate among failed tasks")
	fmt.Fprintln(&b, "# TYPE browseruse_tasks_sandbox_crash_rate_percent gauge")
	fmt.Fprintf(&b, "browseruse_tasks_sandbox_crash_rate_percent %.2f\n", sandboxCrashRate)
	fmt.Fprintln(&b, "# HELP browseruse_tasks_p95_step_latency_ms p95 action step latency in milliseconds")
	fmt.Fprintln(&b, "# TYPE browseruse_tasks_p95_step_latency_ms gauge")
	fmt.Fprintf(&b, "browseruse_tasks_p95_step_latency_ms %d\n", p95StepLatency)

	fmt.Fprintln(&b, "# HELP browseruse_tasks_blocker_total Task blocker count by blocker type")
	fmt.Fprintln(&b, "# TYPE browseruse_tasks_blocker_total gauge")
	for _, key := range sortedIntMapKeys(blockerCounts) {
		fmt.Fprintf(&b, "browseruse_tasks_blocker_total{blocker_type=%q} %d\n", metricLabelEscape(key), blockerCounts[key])
	}

	fmt.Fprintln(&b, "# HELP browseruse_nodes_total Number of registered nodes")
	fmt.Fprintln(&b, "# TYPE browseruse_nodes_total gauge")
	fmt.Fprintf(&b, "browseruse_nodes_total %d\n", len(nodes))
	fmt.Fprintln(&b, "# HELP browseruse_nodes_state_total Node count by state")
	fmt.Fprintln(&b, "# TYPE browseruse_nodes_state_total gauge")
	for _, key := range sortedIntMapKeys(nodeStateCounts) {
		fmt.Fprintf(&b, "browseruse_nodes_state_total{state=%q} %d\n", metricLabelEscape(key), nodeStateCounts[key])
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

func sortedIntMapKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func metricLabelEscape(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(escaped, `"`, `\"`)
}

func percentile(values []int64, p int) int64 {
	if len(values) == 0 {
		return 0
	}
	if p <= 0 {
		p = 1
	}
	if p > 100 {
		p = 100
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := int(math.Ceil((float64(p)/100.0)*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func isSandboxCrashError(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	signals := []string{
		"connection refused",
		"connection reset",
		"dial tcp",
		"no such host",
		"bad gateway",
		"eof",
		"unexpected status 5",
		"sandbox crashed",
		"chromium crashed",
		"browser process",
	}
	for _, signal := range signals {
		if strings.Contains(msg, signal) {
			return true
		}
	}
	return false
}
