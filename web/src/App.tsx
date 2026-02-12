import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  cancelTask,
  createSession,
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

function stepSummary(step: TaskTraceStep): string {
  const action = step.action;
  if (!action) return "unknown action";
  const type = String(action.type || "unknown");
  if (action.selector) return `${type} ${action.selector}`;
  if (action.text) return `${type} "${compact(action.text, 42)}"`;
  return type;
}

export function App() {
  const dispatch = useAppDispatch();
  const queryClient = useQueryClient();
  const ui = useAppSelector((state) => state.ui);
  const [tenantID, setTenantID] = useState("dashboard");
  const [sessionID, setSessionID] = useState("");
  const [taskURL, setTaskURL] = useState("https://duckduckgo.com");
  const [taskGoal, setTaskGoal] = useState("search for browser use");
  const [taskRetries, setTaskRetries] = useState(1);
  const [taskActionsJSON, setTaskActionsJSON] = useState("");
  const [composeStatus, setComposeStatus] = useState("No task submitted yet.");
  const [previewImageURL, setPreviewImageURL] = useState("");

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
  const createSessionMutation = useMutation({
    mutationFn: (tenant: string) => createSession(tenant),
    onSuccess: (session) => {
      setSessionID(session.id);
      setComposeStatus(`Created session ${session.id}`);
    },
    onError: (error) => {
      setComposeStatus(`Create session failed: ${(error as Error).message}`);
    }
  });
  const createTaskMutation = useMutation({
    mutationFn: createTask,
    onSuccess: (created) => {
      dispatch(setSelectedTaskID(created.id));
      setComposeStatus(`Queued task ${created.id} (${created.status}).`);
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
      if (!q) return true;
      const blob = [task.id, task.goal, task.url, task.final_url, task.node_id, task.error_message]
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
  }, [tasks, ui.query, ui.sort, ui.statusFilter]);

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

  async function ensureSession(): Promise<string> {
    const existing = sessionID.trim();
    if (existing) return existing;
    const created = await createSession(tenantID.trim() || "dashboard");
    setSessionID(created.id);
    return created.id;
  }

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
    try {
      const sid = await ensureSession();
      createTaskMutation.mutate({
        session_id: sid,
        url,
        goal,
        max_retries: taskRetries,
        actions
      });
    } catch (error) {
      setComposeStatus(`Task queue failed: ${(error as Error).message}`);
    }
  }

  return (
    <main className="page">
      <header className="header">
        <h1>Browser-use Dashboard (TS Migration)</h1>
        <p>Redux handles local UI state. TanStack Query handles polling, caching, lineage fetches, and mutations.</p>
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

      <section className="kpis">
        <div>Nodes: {nodes.length}</div>
        <div>Queued: {statusCounts.queued ?? 0}</div>
        <div>Running: {statusCounts.running ?? 0}</div>
        <div>Completed: {statusCounts.completed ?? 0}</div>
        <div>Failed: {statusCounts.failed ?? 0}</div>
        <div>Canceled: {statusCounts.canceled ?? 0}</div>
      </section>

      <section className="composePane">
        <h2>Create Task</h2>
        <form className="composeForm" onSubmit={onSubmitTask}>
          <label>
            Tenant ID
            <input value={tenantID} onChange={(e) => setTenantID(e.target.value)} />
          </label>
          <div className="sessionRow">
            <label>
              Session ID
              <input value={sessionID} onChange={(e) => setSessionID(e.target.value)} placeholder="sess_..." />
            </label>
            <button
              type="button"
              onClick={() => createSessionMutation.mutate(tenantID.trim() || "dashboard")}
              disabled={createSessionMutation.isPending}
            >
              {createSessionMutation.isPending ? "Creating..." : "Create Session"}
            </button>
          </div>
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
                {isCancelable(selectedTask) ? (
                  <button className="danger" onClick={() => cancelMutation.mutate(selectedTask.id)}>
                    Cancel
                  </button>
                ) : null}
              </div>
              <div className="tracePane">
                <h3>Execution Trace</h3>
                {selectedTask.trace && selectedTask.trace.length ? (
                  <ul className="traceList">
                    {selectedTask.trace.map((step, idx) => (
                      <li key={`${selectedTask.id}-step-${idx}`} className="traceStep">
                        <div className="traceTop">
                          <span>
                            #{step.index || idx + 1} - {stepSummary(step)}
                          </span>
                          <span className={`pill ${statusClass(String(step.status || "unknown"))}`}>{step.status || "unknown"}</span>
                        </div>
                        {step.output_text ? <div>output: {compact(step.output_text, 200)}</div> : null}
                        {step.error ? <div className="traceError">error: {compact(step.error, 200)}</div> : null}
                        <div className="traceTime">
                          start: {fmt(step.started_at)} | end: {fmt(step.completed_at)} | duration: {step.duration_ms ?? 0}ms
                        </div>
                        {step.screenshot_artifact_url ? (
                          <button type="button" onClick={() => setPreviewImageURL(step.screenshot_artifact_url || "")}>
                            screenshot
                          </button>
                        ) : null}
                      </li>
                    ))}
                  </ul>
                ) : (
                  <p>No trace available.</p>
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

