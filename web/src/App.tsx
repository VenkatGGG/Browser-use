import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  cancelTask,
  createTask,
  fetchDirectReplays,
  fetchNodes,
  fetchReplayChain,
  fetchTasks,
  fetchTaskStats,
  replayTask,
  runNodeAction
} from "./api/client";
import { useAppDispatch, useAppSelector } from "./app/hooks";
import {
  clearQuickFilters,
  setArtifactsOnly,
  setBlockersOnly,
  setFailuresOnly,
  setLive,
  setQuery,
  setRefreshMs,
  setSelectedTaskID,
  setSort,
  setStatusFilter,
  setTaskLimit
} from "./app/uiSlice";
import type { NodeItem, TaskItem, TaskTraceStep } from "./api/types";

function statusClass(status: string): string {
  const s = status.toLowerCase();
  if (s === "queued") return "queued";
  if (s === "running") return "running";
  if (s === "completed") return "completed";
  if (s === "failed") return "failed";
  if (s === "canceled") return "canceled";
  return "unknown";
}

function isCancelable(task: TaskItem): boolean {
  const s = String(task.status || "").toLowerCase();
  return s === "queued" || s === "running";
}

function hasArtifact(task: TaskItem): boolean {
  if (task.screenshot_artifact_url) return true;
  return Boolean(task.trace?.some((step) => Boolean(step.screenshot_artifact_url)));
}

function fmt(value?: string): string {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  return date.toLocaleString();
}

function compact(value: string | undefined, max: number): string {
  const text = String(value || "");
  if (text.length <= max) return text;
  return `${text.slice(0, max - 3)}...`;
}

const ACTION_ICONS: Record<string, string> = {
  scroll: "⬇",
  click: "🖱",
  type: "⌨",
  wait: "⏱",
  wait_for: "👁",
  extract_text: "📋",
  press_enter: "⏎",
  submit_search: "🔍",
  wait_for_url_contains: "🔗",
};

const ACTION_LABELS: Record<string, string> = {
  scroll: "Scroll",
  click: "Click",
  type: "Type",
  wait: "Wait",
  wait_for: "Wait for element",
  extract_text: "Extract text",
  press_enter: "Press Enter",
  submit_search: "Submit search",
  wait_for_url_contains: "Wait for URL",
};

function stepIcon(type: string): string {
  return ACTION_ICONS[type] || "⚡";
}

function stepLabel(type: string): string {
  return ACTION_LABELS[type] || type;
}

function stepSummary(step: TaskTraceStep): string {
  const action = step.action;
  if (!action) return "Unknown action";
  const type = String(action.type || "unknown");
  const icon = stepIcon(type);
  const label = stepLabel(type);

  switch (type) {
    case "scroll": {
      const dir = action.text || "down";
      const px = action.pixels ? `${action.pixels}px` : "";
      return `${icon} ${label} ${dir}${px ? ` ${px}` : ""}`;
    }
    case "click":
      return `${icon} ${label} on ${compact(action.selector || "element", 50)}`;
    case "type":
      return `${icon} ${label} "${compact(action.text || "", 30)}" into ${compact(action.selector || "input", 30)}`;
    case "wait":
      return `${icon} ${label} ${action.delay_ms || 0}ms`;
    case "wait_for":
      return `${icon} ${label} ${compact(action.selector || "", 50)}`;
    case "extract_text":
      return `${icon} ${label} from ${compact(action.selector || "", 50)}`;
    case "press_enter":
      return `${icon} ${label} on ${compact(action.selector || "input", 50)}`;
    case "wait_for_url_contains":
      return `${icon} ${label} "${compact(action.text || "", 40)}"`;
    default:
      if (action.selector) return `${icon} ${type} ${compact(action.selector, 42)}`;
      if (action.text) return `${icon} ${type} "${compact(action.text, 42)}"`;
      return `${icon} ${type}`;
  }
}

function statusIcon(status: string): string {
  switch (status) {
    case "succeeded": return "✅";
    case "failed": return "❌";
    case "running": return "⏳";
    case "skipped": return "⏭";
    default: return "◻️";
  }
}

function durationLabel(ms: number | undefined): string {
  if (ms == null) return "-";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export function App() {
  const dispatch = useAppDispatch();
  const queryClient = useQueryClient();
  const ui = useAppSelector((state) => state.ui);
  const [tenantID, setTenantID] = useState("dashboard");
  const [taskURL, setTaskURL] = useState("https://duckduckgo.com");
  const [taskGoal, setTaskGoal] = useState("search for browser use");
  const [taskRetries, setTaskRetries] = useState(1);
  const [taskActionsJSON, setTaskActionsJSON] = useState("");
  const [composeStatus, setComposeStatus] = useState("No task submitted yet.");
  const [previewImageURL, setPreviewImageURL] = useState("");
  const [expandedSteps, setExpandedSteps] = useState<Set<number>>(new Set());

  function toggleStep(idx: number) {
    setExpandedSteps((prev) => {
      const next = new Set(prev);
      if (next.has(idx)) next.delete(idx);
      else next.add(idx);
      return next;
    });
  }

  const nodesQuery = useQuery({
    queryKey: ["nodes"],
    queryFn: fetchNodes,
    refetchInterval: ui.live ? ui.refreshMs : false
  });
  const tasksQuery = useQuery({
    queryKey: ["tasks", ui.taskLimit],
    queryFn: () => fetchTasks(ui.taskLimit),
    refetchInterval: ui.live ? ui.refreshMs : false
  });
  const statsQuery = useQuery({
    queryKey: ["task-stats"],
    queryFn: fetchTaskStats,
    refetchInterval: ui.live ? ui.refreshMs : false
  });

  const replayMutation = useMutation({
    mutationFn: ({ id, fresh }: { id: string; fresh: boolean }) => replayTask(id, fresh),
    onSuccess: (created) => {
      dispatch(setSelectedTaskID(created.id));
      void queryClient.invalidateQueries({ queryKey: ["tasks"] });
      void queryClient.invalidateQueries({ queryKey: ["task-stats"] });
    }
  });
  const cancelMutation = useMutation({
    mutationFn: (id: string) => cancelTask(id),
    onSuccess: (updated) => {
      dispatch(setSelectedTaskID(updated.id));
      void queryClient.invalidateQueries({ queryKey: ["tasks"] });
      void queryClient.invalidateQueries({ queryKey: ["task-stats"] });
    }
  });
  const nodeActionMutation = useMutation({
    mutationFn: ({ id, action }: { id: string; action: "drain" | "activate" | "recycle" }) => runNodeAction(id, action),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["nodes"] });
    }
  });
  const createTaskMutation = useMutation({
    mutationFn: createTask,
    onSuccess: (created) => {
      dispatch(setSelectedTaskID(created.id));
      setComposeStatus(`Queued task ${created.id} (${created.status}) with session ${created.session_id}.`);
      void queryClient.invalidateQueries({ queryKey: ["tasks"] });
      void queryClient.invalidateQueries({ queryKey: ["task-stats"] });
    },
    onError: (error) => {
      setComposeStatus(`Task queue failed: ${(error as Error).message}`);
    }
  });

  const tasks = tasksQuery.data ?? [];
  const filtered = useMemo(() => {
    const q = ui.query.trim().toLowerCase();
    const rows = tasks.filter((task) => {
      if (ui.statusFilter !== "all" && String(task.status).toLowerCase() !== String(ui.statusFilter).toLowerCase()) {
        return false;
      }
      if (ui.failuresOnly && String(task.status || "").toLowerCase() !== "failed") {
        return false;
      }
      if (ui.blockersOnly && !String(task.blocker_type || "").trim()) {
        return false;
      }
      if (ui.artifactsOnly && !hasArtifact(task)) {
        return false;
      }
      if (!q) return true;
      const blob = [task.id, task.goal, task.url, task.final_url, task.node_id, task.error_message, task.blocker_type, task.blocker_message]
        .map((x) => String(x || "").toLowerCase())
        .join(" ");
      return blob.includes(q);
    });
    rows.sort((a, b) => {
      const at = new Date(a.created_at || 0).getTime();
      const bt = new Date(b.created_at || 0).getTime();
      return ui.sort === "oldest" ? at - bt : bt - at;
    });
    return rows;
  }, [tasks, ui.artifactsOnly, ui.blockersOnly, ui.failuresOnly, ui.query, ui.sort, ui.statusFilter]);

  const selectedTask = useMemo(
    () => tasks.find((task) => task.id === ui.selectedTaskID) ?? null,
    [tasks, ui.selectedTaskID]
  );
  const selectedTaskID = selectedTask?.id ?? "";

  const replayChainQuery = useQuery({
    queryKey: ["replay-chain", selectedTaskID],
    queryFn: () => fetchReplayChain(selectedTaskID),
    enabled: Boolean(selectedTaskID)
  });
  const directReplaysQuery = useQuery({
    queryKey: ["direct-replays", selectedTaskID],
    queryFn: () => fetchDirectReplays(selectedTaskID),
    enabled: Boolean(selectedTaskID)
  });

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const tag = (event.target as HTMLElement | null)?.tagName?.toLowerCase();
      if (tag === "input" || tag === "textarea" || tag === "select") return;
      if (!filtered.length) return;

      const currentIndex = filtered.findIndex((task) => task.id === ui.selectedTaskID);
      if (event.key.toLowerCase() === "j") {
        event.preventDefault();
        const next = currentIndex < 0 ? 0 : Math.min(filtered.length - 1, currentIndex + 1);
        dispatch(setSelectedTaskID(filtered[next].id));
      } else if (event.key.toLowerCase() === "k") {
        event.preventDefault();
        const next = currentIndex < 0 ? filtered.length - 1 : Math.max(0, currentIndex - 1);
        dispatch(setSelectedTaskID(filtered[next].id));
      } else if (event.key.toLowerCase() === "r" && selectedTask) {
        event.preventDefault();
        replayMutation.mutate({ id: selectedTask.id, fresh: event.shiftKey });
      } else if (event.key.toLowerCase() === "c" && selectedTask && isCancelable(selectedTask)) {
        event.preventDefault();
        cancelMutation.mutate(selectedTask.id);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [cancelMutation, dispatch, filtered, replayMutation, selectedTask, ui.selectedTaskID]);

  const statusCounts = statsQuery.data?.status_counts ?? {};
  const nodes = nodesQuery.data ?? [];
  const replayChainIDs = (replayChainQuery.data?.tasks ?? []).map((item) => item.id).filter(Boolean);
  const directReplayIDs = (directReplaysQuery.data?.tasks ?? []).map((item) => item.id).filter(Boolean);
  const failures = useMemo(
    () => tasks.filter((task) => String(task.status).toLowerCase() === "failed").slice(0, 8),
    [tasks]
  );

  async function onSubmitTask(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const url = taskURL.trim();
    if (!url) {
      setComposeStatus("URL is required.");
      return;
    }
    const goal = taskGoal.trim();
    let actions: Array<Record<string, unknown>> | undefined;
    const raw = taskActionsJSON.trim();
    if (raw) {
      try {
        const parsed = JSON.parse(raw) as unknown;
        if (!Array.isArray(parsed)) {
          throw new Error("actions must be a JSON array");
        }
        actions = parsed as Array<Record<string, unknown>>;
      } catch (error) {
        setComposeStatus(`Invalid actions JSON: ${(error as Error).message}`);
        return;
      }
    }
    createTaskMutation.mutate({
      tenant_id: tenantID.trim() || "dashboard",
      url,
      goal,
      max_retries: taskRetries,
      actions
    });
  }

  return (
    <main className="page">
      <header className="header">
        <h1>Browser-use</h1>
        <p>Task orchestration dashboard — live polling, execution trace, node fleet control.</p>
      </header>

      <section className="controls">
        <label>
          Status
          <select value={ui.statusFilter} onChange={(e) => dispatch(setStatusFilter(e.target.value))}>
            <option value="all">all</option>
            <option value="queued">queued</option>
            <option value="running">running</option>
            <option value="completed">completed</option>
            <option value="failed">failed</option>
            <option value="canceled">canceled</option>
          </select>
        </label>
        <label>
          Search
          <input value={ui.query} onChange={(e) => dispatch(setQuery(e.target.value))} placeholder="task id, goal, url, node, error" />
        </label>
        <label>
          Limit
          <select value={ui.taskLimit} onChange={(e) => dispatch(setTaskLimit(Number(e.target.value)))}>
            <option value={80}>80</option>
            <option value={120}>120</option>
            <option value={200}>200</option>
          </select>
        </label>
        <label>
          Refresh
          <select value={ui.refreshMs} onChange={(e) => dispatch(setRefreshMs(Number(e.target.value)))}>
            <option value={1000}>1s</option>
            <option value={3000}>3s</option>
            <option value={5000}>5s</option>
            <option value={10000}>10s</option>
          </select>
        </label>
        <label>
          Sort
          <select value={ui.sort} onChange={(e) => dispatch(setSort(e.target.value as "newest" | "oldest"))}>
            <option value="newest">newest</option>
            <option value="oldest">oldest</option>
          </select>
        </label>
        <button onClick={() => dispatch(setLive(!ui.live))}>{ui.live ? "Pause" : "Resume"} Polling</button>
      </section>
      <section className="quickFilters">
        <label>
          <input
            type="checkbox"
            checked={ui.failuresOnly}
            onChange={(e) => dispatch(setFailuresOnly(e.target.checked))}
          />
          Failed only
        </label>
        <label>
          <input
            type="checkbox"
            checked={ui.blockersOnly}
            onChange={(e) => dispatch(setBlockersOnly(e.target.checked))}
          />
          Blockers only
        </label>
        <label>
          <input
            type="checkbox"
            checked={ui.artifactsOnly}
            onChange={(e) => dispatch(setArtifactsOnly(e.target.checked))}
          />
          Artifacts only
        </label>
        <button type="button" onClick={() => dispatch(clearQuickFilters())}>
          Clear quick filters
        </button>
      </section>

      <section className="kpis">
        <div data-label="Nodes"><span className="kpiValue">{nodes.length}</span></div>
        <div data-label="Queued"><span className="kpiValue">{statusCounts.queued ?? 0}</span></div>
        <div data-label="Running"><span className="kpiValue">{statusCounts.running ?? 0}</span></div>
        <div data-label="Completed"><span className="kpiValue">{statusCounts.completed ?? 0}</span></div>
        <div data-label="Failed"><span className="kpiValue">{statusCounts.failed ?? 0}</span></div>
        <div data-label="Canceled"><span className="kpiValue">{statusCounts.canceled ?? 0}</span></div>
      </section>

      <section className="composePane">
        <h2>Create Task</h2>
        <form className="composeForm" onSubmit={onSubmitTask}>
          <label>
            Tenant ID
            <input value={tenantID} onChange={(e) => setTenantID(e.target.value)} />
          </label>
          <p className="composeHint">A new session is auto-created for each queued task.</p>
          <label>
            URL
            <input value={taskURL} onChange={(e) => setTaskURL(e.target.value)} />
          </label>
          <label>
            Goal
            <input value={taskGoal} onChange={(e) => setTaskGoal(e.target.value)} />
          </label>
          <label>
            Max Retries
            <input
              type="number"
              min={0}
              max={10}
              value={taskRetries}
              onChange={(e) => setTaskRetries(Number(e.target.value) || 0)}
            />
          </label>
          <label>
            Actions JSON (optional)
            <textarea
              value={taskActionsJSON}
              onChange={(e) => setTaskActionsJSON(e.target.value)}
              placeholder='[{"type":"wait_for","selector":"input[name=\"q\"]","timeout_ms":8000}]'
            />
          </label>
          <button type="submit" disabled={createTaskMutation.isPending}>
            {createTaskMutation.isPending ? "Queuing..." : "Queue Task"}
          </button>
        </form>
        <div className="composeStatus">{composeStatus}</div>
      </section>

      <section className="nodesPane">
        <h2>Node Fleet</h2>
        <div className="nodeGrid">
          {nodes.map((node: NodeItem) => (
            <article key={node.id} className="nodeCard">
              <div className="nodeHead">
                <strong>{node.id}</strong>
                <span className={`pill ${statusClass(String(node.state || ""))}`}>{node.state}</span>
              </div>
              <div className="nodeMeta">
                <div>addr: {node.address}</div>
                <div>ver: {node.version}</div>
                <div>hb: {fmt(node.last_heartbeat)}</div>
              </div>
              <div className="actions">
                <button onClick={() => nodeActionMutation.mutate({ id: node.id, action: "drain" })}>Drain</button>
                <button onClick={() => nodeActionMutation.mutate({ id: node.id, action: "activate" })}>Activate</button>
                <button
                  className="danger"
                  onClick={() => nodeActionMutation.mutate({ id: node.id, action: "recycle" })}
                >
                  Recycle
                </button>
              </div>
            </article>
          ))}
        </div>
      </section>

      <section className="layout">
        <div className="tablePane">
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Status</th>
                <th>Goal</th>
                <th>URL</th>
                <th>Node</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((task) => (
                <tr
                  key={task.id}
                  className={ui.selectedTaskID === task.id ? "selected" : ""}
                  onClick={() => dispatch(setSelectedTaskID(task.id))}
                >
                  <td>{task.id}</td>
                  <td>
                    <span className={`pill ${statusClass(String(task.status || ""))}`}>{task.status}</span>
                  </td>
                  <td>{task.goal}</td>
                  <td>{task.final_url || task.url}</td>
                  <td>{task.node_id || "-"}</td>
                  <td>
                    <div className="actions">
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          replayMutation.mutate({ id: task.id, fresh: false });
                        }}
                      >
                        Replay
                      </button>
                      {task.screenshot_artifact_url ? (
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            window.open(task.screenshot_artifact_url!, "_blank");
                          }}
                        >
                          Screenshot
                        </button>
                      ) : null}
                      {isCancelable(task) ? (
                        <button
                          className="danger"
                          onClick={(e) => {
                            e.stopPropagation();
                            cancelMutation.mutate(task.id);
                          }}
                        >
                          Cancel
                        </button>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <aside className="detailPane">
          {selectedTask ? (
            <>
              <h2>{selectedTask.id}</h2>
              <p>
                <strong>Status:</strong> {selectedTask.status}
              </p>
              <p>
                <strong>Goal:</strong> {selectedTask.goal}
              </p>
              <p>
                <strong>Error:</strong> {selectedTask.error_message || "-"}
              </p>
              <p>
                <strong>Source Task:</strong> {selectedTask.source_task_id || "-"}
              </p>
              <p>
                <strong>Replay Chain:</strong> {replayChainIDs.length ? replayChainIDs.join(" <- ") : "-"}
              </p>
              <p>
                <strong>Direct Replays:</strong> {directReplayIDs.length ? directReplayIDs.join(", ") : "-"}
              </p>
              <p>
                <strong>Created:</strong> {selectedTask.created_at || "-"}
              </p>
              <p>
                <strong>Started:</strong> {selectedTask.started_at || "-"}
              </p>
              <p>
                <strong>Completed:</strong> {selectedTask.completed_at || "-"}
              </p>
              <p>
                <strong>Extracted:</strong>{" "}
                {selectedTask.extracted_outputs && selectedTask.extracted_outputs.length
                  ? selectedTask.extracted_outputs.join(" | ")
                  : "-"}
              </p>
              <div className="actions">
                <button onClick={() => replayMutation.mutate({ id: selectedTask.id, fresh: false })}>Replay</button>
                <button onClick={() => replayMutation.mutate({ id: selectedTask.id, fresh: true })}>Replay Fresh</button>
                {selectedTask.screenshot_artifact_url ? (
                  <button onClick={() => window.open(selectedTask.screenshot_artifact_url!, "_blank")}>
                    View Screenshot
                  </button>
                ) : null}
                {isCancelable(selectedTask) ? (
                  <button className="danger" onClick={() => cancelMutation.mutate(selectedTask.id)}>
                    Cancel
                  </button>
                ) : null}
              </div>
              <div className="tracePane">
                <h3>Execution Trace</h3>
                {selectedTask.trace && selectedTask.trace.length ? (
                  <>
                    {/* Progress bar */}
                    <div className="traceProgress">
                      {selectedTask.trace.map((step, idx) => {
                        const s = String(step.status || "unknown");
                        return <div key={idx} className={`traceProgressSegment ${s === "succeeded" ? "segmentSuccess" : s === "failed" ? "segmentFail" : "segmentOther"}`} title={`Step ${step.index || idx + 1}: ${s}`} />;
                      })}
                    </div>
                    <div className="traceStats">
                      <span>{selectedTask.trace.filter(s => s.status === "succeeded").length}/{selectedTask.trace.length} succeeded</span>
                      <span>Total: {durationLabel(selectedTask.trace.reduce((sum, s) => sum + (s.duration_ms || 0), 0))}</span>
                    </div>
                    <ul className="traceList">
                      {selectedTask.trace.map((step, idx) => {
                        const isExpanded = expandedSteps.has(idx);
                        const s = String(step.status || "unknown");
                        const action = step.action;
                        return (
                          <li key={`${selectedTask.id}-step-${idx}`}
                            className={`traceStep ${s === "succeeded" ? "traceStepSuccess" : s === "failed" ? "traceStepFail" : "traceStepOther"}`}>
                            <div className="traceTop" onClick={() => toggleStep(idx)} role="button" tabIndex={0}>
                              <span className="traceStepHeader">
                                <span className="traceStepNumber">#{step.index || idx + 1}</span>
                                <span className="traceStatusIcon">{statusIcon(s)}</span>
                                <span className="traceStepLabel">{stepSummary(step)}</span>
                              </span>
                              <span className="traceStepMeta">
                                <span className="traceDuration">{durationLabel(step.duration_ms)}</span>
                                <span className="traceChevron">{isExpanded ? "▼" : "▶"}</span>
                              </span>
                            </div>
                            {/* Detail badges — always visible */}
                            {action && (
                              <div className="traceBadges">
                                {action.pixels ? <code className="traceBadge">pixels: {action.pixels}</code> : null}
                                {action.delay_ms ? <code className="traceBadge">delay: {action.delay_ms}ms</code> : null}
                                {action.timeout_ms ? <code className="traceBadge">timeout: {action.timeout_ms}ms</code> : null}
                                {action.selector ? <code className="traceBadge selectorBadge" title={action.selector}>{compact(action.selector, 40)}</code> : null}
                              </div>
                            )}
                            {/* Expanded detail */}
                            {isExpanded && (
                              <div className="traceDetail">
                                {step.output_text ? <div className="traceOutput"><strong>Output:</strong> {compact(step.output_text, 300)}</div> : null}
                                {step.error ? <div className="traceError"><strong>Error:</strong> {compact(step.error, 300)}</div> : null}
                                <div className="traceTime">
                                  <span>Start: {fmt(step.started_at)}</span>
                                  <span>End: {fmt(step.completed_at)}</span>
                                  <span>Duration: {durationLabel(step.duration_ms)}</span>
                                </div>
                                {step.screenshot_artifact_url ? (
                                  <button type="button" className="traceScreenshotBtn" onClick={() => window.open(step.screenshot_artifact_url || "", "_blank")}>
                                    📸 View Screenshot
                                  </button>
                                ) : null}
                              </div>
                            )}
                          </li>
                        );
                      })}
                    </ul>
                  </>
                ) : (
                  <p className="traceEmpty">No trace steps recorded.</p>
                )}
              </div>
            </>
          ) : (
            <p>Select a task.</p>
          )}
        </aside>
      </section>

      <section className="failuresPane">
        <h2>Recent Failures</h2>
        {failures.length ? (
          <div className="failureList">
            {failures.map((task) => (
              <button
                key={task.id}
                className="failureItem"
                type="button"
                onClick={() => dispatch(setSelectedTaskID(task.id))}
              >
                <strong>{task.id}</strong>
                <span>{compact(task.goal, 120)}</span>
                <span className="failureError">{compact(task.error_message || "-", 160)}</span>
              </button>
            ))}
          </div>
        ) : (
          <p>No failed tasks in the current window.</p>
        )}
      </section>

      {previewImageURL ? (
        <div className="modalOverlay" onClick={() => setPreviewImageURL("")}>
          <div className="modalCard" onClick={(event) => event.stopPropagation()}>
            <div className="modalHead">
              <strong>Screenshot Preview</strong>
              <button type="button" onClick={() => setPreviewImageURL("")}>
                Close
              </button>
            </div>
            <img src={previewImageURL} alt="Task screenshot preview" />
          </div>
        </div>
      ) : null}
    </main>
  );
}
