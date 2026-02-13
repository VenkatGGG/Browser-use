import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Provider } from "react-redux";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { configureStore } from "@reduxjs/toolkit";
import { describe, it, expect, vi, beforeEach } from "vitest";
import uiReducer from "./app/uiSlice";
import { App } from "./App";

// Mock all API client functions so no real HTTP requests fire from jsdom.
vi.mock("./api/client", () => ({
  fetchNodes: vi.fn().mockResolvedValue([]),
  fetchTasks: vi.fn().mockResolvedValue([]),
  fetchTaskStats: vi.fn().mockResolvedValue({ status_counts: {} }),
  replayTask: vi.fn().mockResolvedValue({ id: "t-1", status: "queued" }),
  cancelTask: vi.fn().mockResolvedValue({ id: "t-1", status: "canceled" }),
  fetchReplayChain: vi.fn().mockResolvedValue({ tasks: [] }),
  fetchDirectReplays: vi.fn().mockResolvedValue({ tasks: [] }),
  runNodeAction: vi.fn().mockResolvedValue({ id: "n-1", state: "ready" }),
  createSession: vi.fn().mockResolvedValue({ id: "sess_1", tenant_id: "dashboard" }),
  createTask: vi.fn().mockResolvedValue({ id: "t-new", status: "queued" })
}));

function renderApp() {
  const store = configureStore({ reducer: { ui: uiReducer } });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } }
  });
  return {
    store,
    ...render(
      <Provider store={store}>
        <QueryClientProvider client={queryClient}>
          <App />
        </QueryClientProvider>
      </Provider>
    )
  };
}

describe("App smoke tests", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders the dashboard header", () => {
    renderApp();
    expect(screen.getByText(/Browser-use Dashboard/i)).toBeDefined();
  });

  it("renders KPI section with zero counts", () => {
    renderApp();
    expect(screen.getByText(/Nodes:/)).toBeDefined();
    expect(screen.getByText(/Queued:/)).toBeDefined();
    expect(screen.getByText(/Running:/)).toBeDefined();
  });

  it("renders the task compose form with required inputs", () => {
    renderApp();
    const composeSection = screen.getByText("Create Task").closest("section")!;
    expect(composeSection).toBeDefined();
    expect(within(composeSection).getByText("Tenant ID")).toBeDefined();
    expect(within(composeSection).getByText("URL")).toBeDefined();
    expect(within(composeSection).getByText("Goal")).toBeDefined();
    expect(within(composeSection).getByRole("button", { name: /Queue Task/i })).toBeDefined();
  });

  it("renders the node fleet section", () => {
    renderApp();
    expect(screen.getByText("Node Fleet")).toBeDefined();
  });
});

describe("Quick filter toggles", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("toggles the Failed only checkbox", async () => {
    const user = userEvent.setup();
    renderApp();
    const checkbox = screen.getByRole("checkbox", { name: /Failed only/i });
    expect((checkbox as HTMLInputElement).checked).toBe(false);
    await user.click(checkbox);
    expect((checkbox as HTMLInputElement).checked).toBe(true);
  });

  it("toggles the Blockers only checkbox", async () => {
    const user = userEvent.setup();
    renderApp();
    const checkbox = screen.getByRole("checkbox", { name: /Blockers only/i });
    expect((checkbox as HTMLInputElement).checked).toBe(false);
    await user.click(checkbox);
    expect((checkbox as HTMLInputElement).checked).toBe(true);
  });

  it("toggles the Artifacts only checkbox", async () => {
    const user = userEvent.setup();
    renderApp();
    const checkbox = screen.getByRole("checkbox", { name: /Artifacts only/i });
    expect((checkbox as HTMLInputElement).checked).toBe(false);
    await user.click(checkbox);
    expect((checkbox as HTMLInputElement).checked).toBe(true);
  });

  it("clears all quick filters with the clear button", async () => {
    const user = userEvent.setup();
    renderApp();
    const failedCb = screen.getByRole("checkbox", { name: /Failed only/i });
    const blockersCb = screen.getByRole("checkbox", { name: /Blockers only/i });
    const artifactsCb = screen.getByRole("checkbox", { name: /Artifacts only/i });

    await user.click(failedCb);
    await user.click(blockersCb);
    await user.click(artifactsCb);
    expect((failedCb as HTMLInputElement).checked).toBe(true);
    expect((blockersCb as HTMLInputElement).checked).toBe(true);
    expect((artifactsCb as HTMLInputElement).checked).toBe(true);

    await user.click(screen.getByRole("button", { name: /Clear quick filters/i }));
    expect((failedCb as HTMLInputElement).checked).toBe(false);
    expect((blockersCb as HTMLInputElement).checked).toBe(false);
    expect((artifactsCb as HTMLInputElement).checked).toBe(false);
  });
});

describe("Controls section", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("toggles polling with the Pause/Resume button", async () => {
    const user = userEvent.setup();
    renderApp();
    const btn = screen.getByRole("button", { name: /Pause Polling/i });
    expect(btn.textContent).toContain("Pause");
    await user.click(btn);
    expect(btn.textContent).toContain("Resume");
  });

  it("renders sort and status filter dropdowns", () => {
    renderApp();
    const statusElements = screen.getAllByText("Status");
    const controlsSection = statusElements[0].closest("section")!;
    expect(within(controlsSection).getByText("Sort")).toBeDefined();
    expect(within(controlsSection).getByText("Limit")).toBeDefined();
    expect(within(controlsSection).getByText("Refresh")).toBeDefined();
  });
});
