package api

import (
	"net/http"

	"github.com/VenkatGGG/Browser-use/pkg/httpx"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/dashboard" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Browser Use Control Room</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Sora:wght@400;600;700;800&family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet">
  <style>
    :root {
      --bg: #f3efe5;
      --panel: #fffaf1;
      --ink: #1f2a2e;
      --muted: #5f6c71;
      --brand: #1e7a70;
      --brand-soft: #d9f0ea;
      --accent: #df6a32;
      --warn: #c84f2a;
      --ok: #2b8856;
      --shadow: 0 22px 48px rgba(28, 42, 46, 0.12);
      --ring: rgba(30, 122, 112, 0.3);
      --border: rgba(26, 41, 45, 0.12);
    }

    * { box-sizing: border-box; }

    html, body { margin: 0; padding: 0; }

    body {
      font-family: "Sora", sans-serif;
      color: var(--ink);
      min-height: 100vh;
      background:
        radial-gradient(1200px 500px at -20% -10%, #d8e8e2 0%, transparent 55%),
        radial-gradient(1200px 700px at 120% 0%, #f2dfc8 0%, transparent 55%),
        linear-gradient(140deg, #f7f2e9 0%, #efe7d7 100%);
      overflow-x: hidden;
    }

    body::before {
      content: "";
      position: fixed;
      inset: 0;
      pointer-events: none;
      background-image: radial-gradient(rgba(25, 36, 40, 0.08) 0.6px, transparent 0.6px);
      background-size: 8px 8px;
      opacity: 0.28;
    }

    .shell {
      position: relative;
      z-index: 1;
      max-width: 1320px;
      margin: 0 auto;
      padding: 28px 18px 36px;
    }

    .hero {
      display: grid;
      grid-template-columns: 1.25fr 1fr;
      gap: 18px;
      margin-bottom: 18px;
    }

    .hero-card {
      border: 1px solid var(--border);
      border-radius: 22px;
      background: linear-gradient(160deg, rgba(255, 251, 242, 0.95), rgba(255, 247, 231, 0.92));
      box-shadow: var(--shadow);
      padding: 20px;
      backdrop-filter: blur(5px);
    }

    .title {
      margin: 0 0 8px;
      font-size: clamp(26px, 4vw, 40px);
      line-height: 1.06;
      letter-spacing: -0.02em;
    }

    .subtitle {
      margin: 0;
      color: var(--muted);
      font-size: 14px;
      max-width: 62ch;
    }

    .stats {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 12px;
      margin-top: 16px;
    }

    .stat {
      background: #fff;
      border: 1px solid var(--border);
      border-radius: 14px;
      padding: 12px;
      min-height: 88px;
    }

    .stat .label {
      font-size: 11px;
      color: var(--muted);
      text-transform: uppercase;
      letter-spacing: 0.11em;
    }

    .stat .value {
      margin-top: 8px;
      font-size: 30px;
      font-weight: 700;
      line-height: 1;
    }

    .pulse {
      width: 10px;
      height: 10px;
      border-radius: 999px;
      background: var(--ok);
      box-shadow: 0 0 0 0 rgba(43, 136, 86, 0.45);
      animation: pulse 1.8s infinite;
      display: inline-block;
      margin-right: 8px;
      transform: translateY(1px);
    }

    @keyframes pulse {
      0% { box-shadow: 0 0 0 0 rgba(43, 136, 86, 0.45); }
      70% { box-shadow: 0 0 0 10px rgba(43, 136, 86, 0); }
      100% { box-shadow: 0 0 0 0 rgba(43, 136, 86, 0); }
    }

    .clock {
      font-family: "IBM Plex Mono", monospace;
      font-size: 12px;
      color: #4a5960;
      background: rgba(255, 255, 255, 0.75);
      border: 1px solid var(--border);
      padding: 8px 10px;
      border-radius: 10px;
      text-align: right;
    }

    .layout {
      display: grid;
      grid-template-columns: 380px 1fr;
      gap: 18px;
      align-items: start;
    }

    .panel {
      border: 1px solid var(--border);
      border-radius: 20px;
      background: rgba(255, 250, 241, 0.92);
      box-shadow: var(--shadow);
      padding: 18px;
      backdrop-filter: blur(6px);
    }

    .panel h2 {
      margin: 0 0 6px;
      font-size: 18px;
      letter-spacing: -0.01em;
    }

    .panel p {
      margin: 0 0 14px;
      color: var(--muted);
      font-size: 12px;
    }

    .form-grid {
      display: grid;
      gap: 10px;
    }

    .label {
      font-size: 11px;
      text-transform: uppercase;
      letter-spacing: 0.09em;
      color: #58666d;
      margin-bottom: 4px;
      display: inline-block;
    }

    input, textarea {
      width: 100%;
      border: 1px solid var(--border);
      background: #ffffff;
      color: var(--ink);
      font-family: "IBM Plex Mono", monospace;
      font-size: 13px;
      border-radius: 12px;
      padding: 10px 12px;
      outline: none;
      transition: border-color 120ms, box-shadow 120ms;
    }

    input:focus, textarea:focus {
      border-color: var(--brand);
      box-shadow: 0 0 0 3px var(--ring);
    }

    textarea {
      min-height: 106px;
      resize: vertical;
    }

    .row {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 10px;
    }

    .btn {
      border: 1px solid transparent;
      border-radius: 12px;
      padding: 11px 14px;
      font-family: "Sora", sans-serif;
      font-size: 13px;
      font-weight: 600;
      cursor: pointer;
      transition: transform 100ms ease, box-shadow 120ms ease, filter 120ms;
    }

    .btn:active { transform: translateY(1px); }

    .btn-primary {
      color: #fff;
      background: linear-gradient(135deg, #1d7d72 0%, #175a53 100%);
      box-shadow: 0 8px 18px rgba(23, 90, 83, 0.3);
    }

    .btn-secondary {
      color: #223337;
      background: #fff;
      border-color: var(--border);
    }

    .btn:hover { filter: brightness(1.04); }

    .hint {
      margin-top: 10px;
      font-size: 12px;
      color: var(--muted);
      line-height: 1.4;
    }

    .status-box {
      margin-top: 10px;
      border: 1px dashed var(--border);
      border-radius: 12px;
      padding: 10px;
      font-size: 12px;
      color: #45545a;
      min-height: 46px;
      line-height: 1.4;
      background: rgba(255, 255, 255, 0.7);
      word-break: break-word;
    }

    .content-grid {
      display: grid;
      grid-template-columns: 1fr;
      gap: 18px;
    }

    .nodes {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
      gap: 12px;
    }

    .node {
      border-radius: 14px;
      border: 1px solid var(--border);
      background: #fff;
      padding: 12px;
      min-height: 104px;
    }

    .node .id {
      font-family: "IBM Plex Mono", monospace;
      font-size: 12px;
      color: #223338;
      margin-bottom: 6px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .pill {
      display: inline-flex;
      align-items: center;
      border-radius: 999px;
      padding: 4px 9px;
      font-size: 11px;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0.05em;
    }

    .pill-ready { background: #dff5e8; color: #1e7a4c; }
    .pill-warming { background: #f6eed9; color: #886412; }
    .pill-leased { background: #dbeff7; color: #1f6784; }
    .pill-dead { background: #f7e1db; color: #8f3a22; }
    .pill-draining { background: #efe7f7; color: #6c478f; }
    .pill-queued { background: #efe8d8; color: #805e1f; }
    .pill-running { background: #dcefff; color: #195f86; }
    .pill-completed { background: #dff5e8; color: #1d7a4d; }
    .pill-failed { background: #f9dfd9; color: #9a3117; }

    .mono { font-family: "IBM Plex Mono", monospace; }

    .task-wrap {
      overflow-x: auto;
      border-radius: 14px;
      border: 1px solid var(--border);
      background: #fff;
    }

    table {
      width: 100%;
      border-collapse: collapse;
      min-width: 860px;
    }

    th, td {
      text-align: left;
      padding: 11px 12px;
      border-bottom: 1px solid rgba(28, 41, 45, 0.09);
      vertical-align: middle;
      font-size: 12px;
    }

    th {
      font-size: 11px;
      text-transform: uppercase;
      letter-spacing: 0.08em;
      color: #59686f;
      background: rgba(249, 245, 234, 0.8);
      position: sticky;
      top: 0;
      z-index: 2;
    }

    tr:hover td {
      background: rgba(246, 251, 250, 0.7);
    }

    .link {
      color: #1b6f9a;
      text-decoration: none;
      font-weight: 600;
    }

    .link:hover { text-decoration: underline; }

    .empty {
      border: 1px dashed var(--border);
      border-radius: 14px;
      padding: 22px;
      text-align: center;
      color: #5d6a70;
      font-size: 13px;
      background: rgba(255, 255, 255, 0.62);
    }

    .fade-up {
      opacity: 0;
      transform: translateY(8px);
      animation: fadeUp 350ms ease forwards;
    }

    .delay-1 { animation-delay: 80ms; }
    .delay-2 { animation-delay: 160ms; }

    @keyframes fadeUp {
      to { opacity: 1; transform: translateY(0); }
    }

    .modal {
      position: fixed;
      inset: 0;
      background: rgba(22, 30, 34, 0.6);
      display: none;
      align-items: center;
      justify-content: center;
      padding: 20px;
      z-index: 9;
    }

    .modal.show { display: flex; }

    .modal-card {
      width: min(1080px, 100%);
      max-height: 92vh;
      background: #fdf9ef;
      border: 1px solid var(--border);
      border-radius: 14px;
      box-shadow: var(--shadow);
      overflow: hidden;
    }

    .modal-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 10px;
      padding: 10px 12px;
      border-bottom: 1px solid var(--border);
      font-size: 12px;
    }

    .close {
      border: 0;
      background: #fff;
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 6px 9px;
      cursor: pointer;
    }

    .modal img {
      display: block;
      width: 100%;
      height: auto;
      max-height: calc(92vh - 60px);
      object-fit: contain;
      background: #f6f1e6;
    }

    @media (max-width: 1100px) {
      .hero { grid-template-columns: 1fr; }
      .layout { grid-template-columns: 1fr; }
      .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    }

    @media (max-width: 620px) {
      .shell { padding: 14px 10px 26px; }
      .stats { grid-template-columns: 1fr 1fr; }
      .row { grid-template-columns: 1fr; }
      .hero-card, .panel { border-radius: 16px; }
      .stat .value { font-size: 24px; }
    }
  </style>
</head>
<body>
  <div class="shell">
    <section class="hero fade-up">
      <div class="hero-card">
        <h1 class="title">Browser Use Control Room</h1>
        <p class="subtitle">Live node fleet health, task execution status, and on-demand task dispatch in one local-first dashboard.</p>
        <div class="stats">
          <div class="stat">
            <div class="label">Nodes</div>
            <div class="value" id="stat-nodes">0</div>
          </div>
          <div class="stat">
            <div class="label">Ready Nodes</div>
            <div class="value" id="stat-ready">0</div>
          </div>
          <div class="stat">
            <div class="label">Running Tasks</div>
            <div class="value" id="stat-running">0</div>
          </div>
          <div class="stat">
            <div class="label">Success Rate</div>
            <div class="value" id="stat-success">0%</div>
          </div>
        </div>
      </div>
      <div class="hero-card">
        <div style="display:flex;justify-content:space-between;align-items:center;gap:12px">
          <h2 style="margin:0;font-size:17px"><span class="pulse"></span>Live Polling</h2>
          <div class="clock" id="clock">-</div>
        </div>
        <p style="margin-top:10px">Refreshing every 3 seconds. Dashboard pulls <span class="mono">/v1/nodes</span> and <span class="mono">/v1/tasks?limit=80</span>.</p>
        <div class="status-box" id="refresh-status">Waiting for first refresh.</div>
      </div>
    </section>

    <section class="layout">
      <aside class="panel fade-up delay-1">
        <h2>Create Task</h2>
        <p>Submit deterministic actions or a plain goal and let the node planner generate actions.</p>
        <form id="task-form" class="form-grid">
          <div>
            <label class="label" for="tenant-id">Tenant ID</label>
            <input id="tenant-id" value="dashboard" autocomplete="off">
          </div>
          <div class="row">
            <div>
              <label class="label" for="session-id">Session ID</label>
              <input id="session-id" placeholder="sess_...">
            </div>
            <div style="display:flex;align-items:end">
              <button type="button" class="btn btn-secondary" id="create-session-btn" style="width:100%">Create Session</button>
            </div>
          </div>
          <div>
            <label class="label" for="task-url">URL</label>
            <input id="task-url" value="https://duckduckgo.com" autocomplete="off">
          </div>
          <div>
            <label class="label" for="task-goal">Goal</label>
            <input id="task-goal" value="search for browser use" autocomplete="off">
          </div>
          <div class="row">
            <div>
              <label class="label" for="task-retries">Max Retries</label>
              <input id="task-retries" type="number" min="0" max="10" value="1">
            </div>
            <div style="display:flex;align-items:end">
              <button type="submit" class="btn btn-primary" style="width:100%">Queue Task</button>
            </div>
          </div>
          <div>
            <label class="label" for="task-actions">Actions JSON (optional)</label>
            <textarea id="task-actions" placeholder='[{"type":"wait_for","selector":"input[name=\"q\"]","timeout_ms":8000}]'></textarea>
          </div>
        </form>
        <div class="hint">If actions JSON is empty, planner mode can derive actions from the goal.</div>
        <div class="status-box" id="create-status">No task submitted yet.</div>
      </aside>

      <div class="content-grid">
        <section class="panel fade-up delay-1">
          <h2>Node Fleet</h2>
          <p>Registered node agents and heartbeat state.</p>
          <div id="nodes" class="nodes"></div>
        </section>

        <section class="panel fade-up delay-2">
          <h2>Recent Tasks</h2>
          <p>Newest tasks first. Click artifact links to preview screenshots.</p>
          <div id="tasks-wrap" class="task-wrap">
            <table>
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Status</th>
                  <th>Goal</th>
                  <th>URL</th>
                  <th>Attempt</th>
                  <th>Node</th>
                  <th>Created</th>
                  <th>Artifact</th>
                </tr>
              </thead>
              <tbody id="tasks-body"></tbody>
            </table>
          </div>
          <div id="tasks-empty" class="empty" style="display:none">No tasks found yet.</div>
        </section>
      </div>
    </section>
  </div>

  <div id="image-modal" class="modal">
    <div class="modal-card">
      <div class="modal-header">
        <strong class="mono" id="image-title">Screenshot</strong>
        <button class="close" id="image-close">Close</button>
      </div>
      <img id="image-preview" alt="Task screenshot preview">
    </div>
  </div>

  <script>
    const state = {
      nodes: [],
      tasks: [],
      lastCreatedTaskID: ""
    };

    const nodesEl = document.getElementById("nodes");
    const tasksBodyEl = document.getElementById("tasks-body");
    const tasksEmptyEl = document.getElementById("tasks-empty");
    const refreshStatusEl = document.getElementById("refresh-status");
    const createStatusEl = document.getElementById("create-status");
    const imageModalEl = document.getElementById("image-modal");
    const imagePreviewEl = document.getElementById("image-preview");
    const imageTitleEl = document.getElementById("image-title");

    function escapeHTML(value) {
      return String(value || "")
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;")
        .replaceAll('"', "&quot;")
        .replaceAll("'", "&#39;");
    }

    function fmtTime(value) {
      if (!value) return "-";
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) return "-";
      return date.toLocaleString();
    }

    function statusPillClass(status) {
      const normalized = String(status || "").toLowerCase();
      if (normalized === "ready") return "pill-ready";
      if (normalized === "warming") return "pill-warming";
      if (normalized === "leased") return "pill-leased";
      if (normalized === "draining") return "pill-draining";
      if (normalized === "dead") return "pill-dead";
      if (normalized === "queued") return "pill-queued";
      if (normalized === "running") return "pill-running";
      if (normalized === "completed") return "pill-completed";
      if (normalized === "failed") return "pill-failed";
      return "pill-warming";
    }

    function taskSuccessRate(tasks) {
      const terminal = tasks.filter((task) => task.status === "completed" || task.status === "failed");
      if (terminal.length === 0) return "0%";
      const completed = terminal.filter((task) => task.status === "completed").length;
      const ratio = Math.round((completed / terminal.length) * 100);
      return ratio + "%";
    }

    function updateStats() {
      const nodeCount = state.nodes.length;
      const readyCount = state.nodes.filter((node) => node.state === "ready").length;
      const runningCount = state.tasks.filter((task) => task.status === "running").length;

      document.getElementById("stat-nodes").textContent = String(nodeCount);
      document.getElementById("stat-ready").textContent = String(readyCount);
      document.getElementById("stat-running").textContent = String(runningCount);
      document.getElementById("stat-success").textContent = taskSuccessRate(state.tasks);
      document.getElementById("clock").textContent = new Date().toLocaleString();
    }

    function renderNodes() {
      if (!state.nodes.length) {
        nodesEl.innerHTML = '<div class="empty" style="grid-column:1/-1">No nodes registered yet.</div>';
        return;
      }

      nodesEl.innerHTML = state.nodes.map((node) => {
        return (
          '<article class="node">' +
            '<div class="id" title="' + escapeHTML(node.id) + '">' + escapeHTML(node.id) + '</div>' +
            '<div style="margin-bottom:8px"><span class="pill ' + statusPillClass(node.state) + '">' + escapeHTML(node.state || "unknown") + '</span></div>' +
            '<div class="mono" style="font-size:11px;color:#45545a;line-height:1.4">' +
              '<div>' + escapeHTML(node.address || "-") + '</div>' +
              '<div>v' + escapeHTML(node.version || "dev") + '</div>' +
              '<div>HB: ' + escapeHTML(fmtTime(node.last_heartbeat)) + '</div>' +
            '</div>' +
          '</article>'
        );
      }).join("");
    }

    function renderTasks() {
      if (!state.tasks.length) {
        tasksBodyEl.innerHTML = "";
        tasksEmptyEl.style.display = "block";
        document.getElementById("tasks-wrap").style.display = "none";
        return;
      }
      tasksEmptyEl.style.display = "none";
      document.getElementById("tasks-wrap").style.display = "block";

      tasksBodyEl.innerHTML = state.tasks.map((task) => {
        const isLast = task.id === state.lastCreatedTaskID;
        const idStyle = isLast ? ' style="font-weight:700;color:#145d55"' : "";
        const artifactCell = task.screenshot_artifact_url
          ? '<a class="link" href="' + escapeHTML(task.screenshot_artifact_url) + '" data-artifact="' + escapeHTML(task.screenshot_artifact_url) + '">open</a>'
          : "-";

        return (
          "<tr>" +
            "<td class=\"mono\"" + idStyle + " title=\"" + escapeHTML(task.id) + "\">" + escapeHTML(task.id || "-") + "</td>" +
            "<td><span class=\"pill " + statusPillClass(task.status) + "\">" + escapeHTML(task.status || "unknown") + "</span></td>" +
            "<td title=\"" + escapeHTML(task.goal || "-") + "\">" + escapeHTML(task.goal || "-") + "</td>" +
            "<td title=\"" + escapeHTML(task.final_url || task.url || "-") + "\">" + escapeHTML(task.final_url || task.url || "-") + "</td>" +
            "<td class=\"mono\">" + escapeHTML(task.attempt) + "/" + escapeHTML(task.max_retries) + "</td>" +
            "<td class=\"mono\">" + escapeHTML(task.node_id || "-") + "</td>" +
            "<td class=\"mono\">" + escapeHTML(fmtTime(task.created_at)) + "</td>" +
            "<td>" + artifactCell + "</td>" +
          "</tr>"
        );
      }).join("");
    }

    async function requestJSON(url, options) {
      const response = await fetch(url, options);
      const text = await response.text();
      let payload = {};
      if (text) {
        try {
          payload = JSON.parse(text);
        } catch (_error) {
          throw new Error("Invalid JSON from " + url + ": " + text.slice(0, 120));
        }
      }

      if (!response.ok) {
        const message = payload.message || payload.error || ("Request failed with status " + response.status);
        throw new Error(message);
      }
      return payload;
    }

    async function refreshDashboard() {
      try {
        const [nodeResp, taskResp] = await Promise.all([
          requestJSON("/v1/nodes"),
          requestJSON("/v1/tasks?limit=80")
        ]);

        state.nodes = Array.isArray(nodeResp.nodes) ? nodeResp.nodes : [];
        state.tasks = Array.isArray(taskResp.tasks) ? taskResp.tasks : [];

        updateStats();
        renderNodes();
        renderTasks();
        refreshStatusEl.textContent = "Last refresh: " + new Date().toLocaleTimeString();
      } catch (error) {
        refreshStatusEl.textContent = "Refresh failed: " + error.message;
      }
    }

    async function createSession() {
      const tenantID = document.getElementById("tenant-id").value.trim() || "dashboard";
      const payload = { tenant_id: tenantID };
      const created = await requestJSON("/v1/sessions", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload)
      });
      document.getElementById("session-id").value = created.id || "";
      return created.id || "";
    }

    document.getElementById("create-session-btn").addEventListener("click", async () => {
      const button = document.getElementById("create-session-btn");
      const previous = button.textContent;
      button.disabled = true;
      button.textContent = "Creating...";
      try {
        const sessionID = await createSession();
        createStatusEl.textContent = "Created session: " + sessionID;
      } catch (error) {
        createStatusEl.textContent = "Session creation failed: " + error.message;
      } finally {
        button.disabled = false;
        button.textContent = previous;
      }
    });

    document.getElementById("task-form").addEventListener("submit", async (event) => {
      event.preventDefault();
      const submitButton = event.submitter;
      const sessionInput = document.getElementById("session-id");
      const urlInput = document.getElementById("task-url").value.trim();
      const goalInput = document.getElementById("task-goal").value.trim();
      const retriesInput = Number.parseInt(document.getElementById("task-retries").value, 10);
      const actionsRaw = document.getElementById("task-actions").value.trim();

      if (!urlInput) {
        createStatusEl.textContent = "URL is required.";
        return;
      }

      let sessionID = sessionInput.value.trim();
      if (!sessionID) {
        try {
          sessionID = await createSession();
        } catch (error) {
          createStatusEl.textContent = "Task blocked: failed to auto-create session: " + error.message;
          return;
        }
      }

      let actions = [];
      if (actionsRaw) {
        try {
          actions = JSON.parse(actionsRaw);
          if (!Array.isArray(actions)) {
            throw new Error("actions must be a JSON array");
          }
        } catch (error) {
          createStatusEl.textContent = "Invalid actions JSON: " + error.message;
          return;
        }
      }

      const payload = {
        session_id: sessionID,
        url: urlInput,
        goal: goalInput,
        max_retries: Number.isFinite(retriesInput) ? retriesInput : 1
      };
      if (actions.length > 0) {
        payload.actions = actions;
      }

      const previousLabel = submitButton.textContent;
      submitButton.disabled = true;
      submitButton.textContent = "Queuing...";
      try {
        const task = await requestJSON("/v1/tasks", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload)
        });
        state.lastCreatedTaskID = task.id || "";
        createStatusEl.innerHTML = "Queued task <span class=\"mono\">" + escapeHTML(task.id || "-") + "</span> with status <strong>" + escapeHTML(task.status || "-") + "</strong>.";
        await refreshDashboard();
      } catch (error) {
        createStatusEl.textContent = "Task queue failed: " + error.message;
      } finally {
        submitButton.disabled = false;
        submitButton.textContent = previousLabel;
      }
    });

    document.addEventListener("click", (event) => {
      const target = event.target.closest("[data-artifact]");
      if (!target) return;
      event.preventDefault();
      const url = target.getAttribute("data-artifact");
      if (!url) return;
      imagePreviewEl.src = url;
      imageTitleEl.textContent = url;
      imageModalEl.classList.add("show");
    });

    function closeModal() {
      imageModalEl.classList.remove("show");
      imagePreviewEl.src = "";
    }

    document.getElementById("image-close").addEventListener("click", closeModal);
    imageModalEl.addEventListener("click", (event) => {
      if (event.target === imageModalEl) closeModal();
    });
    document.addEventListener("keydown", (event) => {
      if (event.key === "Escape") closeModal();
    });

    refreshDashboard();
    setInterval(refreshDashboard, 3000);
  </script>
</body>
</html>`
