export type NodeState = "warming" | "ready" | "leased" | "draining" | "dead" | string;

export interface NodeItem {
  id: string;
  address: string;
  version: string;
  state: NodeState;
  booted_at?: string;
  last_heartbeat?: string;
  leased_until?: string;
  created_at?: string;
  updated_at?: string;
}

export type TaskStatus = "queued" | "running" | "completed" | "failed" | "canceled" | string;

export interface TaskAction {
  type: string;
  selector?: string;
  text?: string;
  pixels?: number;
  timeout_ms?: number;
  delay_ms?: number;
}

export interface TaskItem {
  id: string;
  source_task_id?: string;
  session_id: string;
  url: string;
  goal: string;
  actions?: TaskAction[];
  status: TaskStatus;
  attempt?: number;
  max_retries?: number;
  node_id?: string;
  final_url?: string;
  blocker_type?: string;
  blocker_message?: string;
  error_message?: string;
  screenshot_artifact_url?: string;
  created_at?: string;
  started_at?: string;
  completed_at?: string;
}

export interface TaskStats {
  status_counts?: Record<string, number>;
  blocker_counts?: Record<string, number>;
  success_rate_percent?: number;
  block_rate_percent?: number;
  totals?: {
    tasks?: number;
    terminal?: number;
    blocked?: number;
  };
}

