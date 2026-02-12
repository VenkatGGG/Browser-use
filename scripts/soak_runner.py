#!/usr/bin/env python3
"""Local soak runner for Browser-use orchestrator."""

from __future__ import annotations

import argparse
import json
import sys
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timezone
from typing import Any, Dict, List


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run local soak test against Browser-use API.")
    parser.add_argument("--base-url", default="http://localhost:8080", help="Orchestrator base URL.")
    parser.add_argument("--tenant-id", default="soak", help="Tenant ID for session creation.")
    parser.add_argument("--tasks", type=int, default=40, help="Total tasks to enqueue.")
    parser.add_argument("--submit-workers", type=int, default=8, help="Concurrent submit workers.")
    parser.add_argument("--poll-interval", type=float, default=1.0, help="Polling interval in seconds.")
    parser.add_argument("--timeout-seconds", type=int, default=300, help="Max wait time for terminal states.")
    parser.add_argument("--url", default="https://example.com", help="Task URL.")
    parser.add_argument("--goal", default="open page and capture screenshot", help="Task goal.")
    parser.add_argument(
        "--goal-template",
        default="",
        help='Optional goal template with "{i}" placeholder, e.g. "search for browser use {i}".',
    )
    parser.add_argument("--max-fail-rate", type=float, default=1.0, help="Fail if failed/tasks exceeds this ratio.")
    parser.add_argument("--max-block-rate", type=float, default=1.0, help="Fail if blocked/failed exceeds this ratio.")
    return parser.parse_args()


def now_utc() -> datetime:
    return datetime.now(timezone.utc)


def parse_time(value: str) -> datetime | None:
    raw = (value or "").strip()
    if not raw:
        return None
    if raw.endswith("Z"):
        raw = raw[:-1] + "+00:00"
    try:
        return datetime.fromisoformat(raw)
    except ValueError:
        return None


def request_json(url: str, method: str = "GET", body: Dict[str, Any] | None = None) -> Dict[str, Any]:
    raw = None
    headers = {"Content-Type": "application/json"}
    if body is not None:
        raw = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(url=url, method=method, data=raw, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            payload = resp.read().decode("utf-8")
            return json.loads(payload) if payload else {}
    except urllib.error.HTTPError as exc:
        body_text = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"{method} {url} failed: {exc.code} {body_text}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"{method} {url} failed: {exc}") from exc


def create_session(base_url: str, tenant_id: str) -> str:
    payload = request_json(f"{base_url}/v1/sessions", method="POST", body={"tenant_id": tenant_id})
    session_id = str(payload.get("id", "")).strip()
    if not session_id:
        raise RuntimeError("session create response missing id")
    return session_id


def submit_task(base_url: str, session_id: str, url: str, goal: str) -> Dict[str, Any]:
    body = {"session_id": session_id, "url": url, "goal": goal, "wait_for_completion": False}
    payload = request_json(f"{base_url}/v1/tasks", method="POST", body=body)
    task_id = str(payload.get("id", "")).strip()
    if not task_id:
        raise RuntimeError(f"task create response missing id: {payload}")
    return payload


def fetch_task(base_url: str, task_id: str) -> Dict[str, Any]:
    return request_json(f"{base_url}/v1/tasks/{task_id}")


def run() -> int:
    args = parse_args()
    base_url = args.base_url.rstrip("/")

    start = now_utc()
    print(f"[soak] creating session tenant={args.tenant_id}")
    session_id = create_session(base_url, args.tenant_id)
    print(f"[soak] session_id={session_id}")

    submit_results: List[Dict[str, Any]] = []
    tasks = max(1, args.tasks)
    workers = max(1, args.submit_workers)

    print(f"[soak] submitting tasks count={tasks} workers={workers}")
    with ThreadPoolExecutor(max_workers=workers) as pool:
        futures = []
        for i in range(tasks):
            goal = args.goal_template.format(i=i) if args.goal_template else args.goal
            futures.append(pool.submit(submit_task, base_url, session_id, args.url, goal))
        for future in as_completed(futures):
            submit_results.append(future.result())

    task_ids = [str(item["id"]) for item in submit_results]
    states: Dict[str, Dict[str, Any]] = {task_id: {"status": "queued"} for task_id in task_ids}
    pending = set(task_ids)
    deadline = time.time() + max(5, args.timeout_seconds)

    print(f"[soak] polling terminal state for {len(task_ids)} task(s)")
    while pending and time.time() < deadline:
        done_now: List[str] = []
        for task_id in list(pending):
            payload = fetch_task(base_url, task_id)
            states[task_id] = payload
            status = str(payload.get("status", "")).strip().lower()
            if status in {"completed", "failed"}:
                done_now.append(task_id)
        for task_id in done_now:
            pending.discard(task_id)
        if pending:
            time.sleep(max(0.2, args.poll_interval))

    elapsed = (now_utc() - start).total_seconds()
    completed = 0
    failed = 0
    blocked = 0
    timeouts = len(pending)
    durations: List[float] = []
    for task_id, payload in states.items():
        status = str(payload.get("status", "")).strip().lower()
        if status == "completed":
            completed += 1
        elif status == "failed":
            failed += 1
            if str(payload.get("blocker_type", "")).strip():
                blocked += 1
        started_at = parse_time(str(payload.get("started_at", "")))
        completed_at = parse_time(str(payload.get("completed_at", "")))
        if started_at and completed_at:
            delta = (completed_at - started_at).total_seconds()
            if delta >= 0:
                durations.append(delta)
        if task_id in pending:
            print(f"[soak] timeout task_id={task_id}")

    total = len(task_ids)
    fail_rate = (failed / total) if total else 0.0
    block_rate = (blocked / failed) if failed else 0.0
    avg_duration = (sum(durations) / len(durations)) if durations else 0.0
    p95 = 0.0
    if durations:
        values = sorted(durations)
        idx = max(0, min(len(values) - 1, int(round(0.95 * (len(values) - 1)))))
        p95 = values[idx]

    print("")
    print("[soak] summary")
    print(f"  total={total}")
    print(f"  completed={completed}")
    print(f"  failed={failed}")
    print(f"  blocked={blocked}")
    print(f"  timeout={timeouts}")
    print(f"  fail_rate={fail_rate:.3f}")
    print(f"  block_rate={block_rate:.3f}")
    print(f"  avg_duration_seconds={avg_duration:.2f}")
    print(f"  p95_duration_seconds={p95:.2f}")
    print(f"  elapsed_seconds={elapsed:.2f}")

    if timeouts > 0:
        print("[soak] result=FAIL reason=timeout", file=sys.stderr)
        return 2
    if fail_rate > args.max_fail_rate:
        print(
            f"[soak] result=FAIL reason=fail_rate_exceeded threshold={args.max_fail_rate:.3f} actual={fail_rate:.3f}",
            file=sys.stderr,
        )
        return 3
    if block_rate > args.max_block_rate:
        print(
            f"[soak] result=FAIL reason=block_rate_exceeded threshold={args.max_block_rate:.3f} actual={block_rate:.3f}",
            file=sys.stderr,
        )
        return 4
    print("[soak] result=PASS")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(run())
    except Exception as exc:  # pylint: disable=broad-except
        print(f"[soak] fatal: {exc}", file=sys.stderr)
        sys.exit(1)
