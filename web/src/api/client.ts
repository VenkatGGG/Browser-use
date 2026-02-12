import type {
  DirectReplaysResponse,
  NodeItem,
  ReplayChainResponse,
  SessionItem,
  TaskItem,
  TaskStats
} from "./types";

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, init);
  const text = await response.text();
  const payload = text ? (JSON.parse(text) as unknown) : {};
  if (!response.ok) {
    const message =
      typeof payload === "object" && payload && "message" in payload
        ? String((payload as { message?: unknown }).message ?? `request failed: ${response.status}`)
        : `request failed: ${response.status}`;
    throw new Error(message);
  }
  return payload as T;
}

export async function fetchNodes(): Promise<NodeItem[]> {
  const data = await requestJSON<{ nodes?: NodeItem[] }>("/v1/nodes");
  return Array.isArray(data.nodes) ? data.nodes : [];
}

export async function fetchTasks(limit: number): Promise<TaskItem[]> {
  const data = await requestJSON<{ tasks?: TaskItem[] }>(`/v1/tasks?limit=${encodeURIComponent(String(limit))}`);
  return Array.isArray(data.tasks) ? data.tasks : [];
}

export async function fetchTaskStats(): Promise<TaskStats> {
  return requestJSON<TaskStats>("/v1/tasks/stats?limit=1000");
}

export async function replayTask(taskID: string, freshSession: boolean): Promise<TaskItem> {
  return requestJSON<TaskItem>(`/v1/tasks/${encodeURIComponent(taskID)}/replay`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(freshSession ? { create_new_session: true } : {})
  });
}

export async function cancelTask(taskID: string): Promise<TaskItem> {
  return requestJSON<TaskItem>(`/v1/tasks/${encodeURIComponent(taskID)}/cancel`, {
    method: "POST"
  });
}

export async function fetchReplayChain(taskID: string): Promise<ReplayChainResponse> {
  return requestJSON<ReplayChainResponse>(`/v1/tasks/${encodeURIComponent(taskID)}/replay_chain?max_depth=20`);
}

export async function fetchDirectReplays(taskID: string): Promise<DirectReplaysResponse> {
  return requestJSON<DirectReplaysResponse>(`/v1/tasks/${encodeURIComponent(taskID)}/replays?limit=200`);
}

export async function runNodeAction(nodeID: string, action: "drain" | "activate" | "recycle"): Promise<NodeItem> {
  return requestJSON<NodeItem>(`/v1/nodes/${encodeURIComponent(nodeID)}/${encodeURIComponent(action)}`, {
    method: "POST"
  });
}

export async function createSession(tenantID: string): Promise<SessionItem> {
  return requestJSON<SessionItem>("/v1/sessions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ tenant_id: tenantID })
  });
}

export async function createTask(input: {
  session_id: string;
  url: string;
  goal: string;
  max_retries?: number;
  actions?: Array<Record<string, unknown>>;
}): Promise<TaskItem> {
  return requestJSON<TaskItem>("/v1/tasks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input)
  });
}

