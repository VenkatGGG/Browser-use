import { createSlice, type PayloadAction } from "@reduxjs/toolkit";
import type { TaskStatus } from "../api/types";

type SortOrder = "newest" | "oldest";

interface UIState {
  selectedTaskID: string;
  statusFilter: "all" | TaskStatus;
  query: string;
  taskLimit: number;
  refreshMs: number;
  live: boolean;
  sort: SortOrder;
  artifactsOnly: boolean;
  blockersOnly: boolean;
  failuresOnly: boolean;
}

const initialState: UIState = {
  selectedTaskID: "",
  statusFilter: "all",
  query: "",
  taskLimit: 120,
  refreshMs: 3000,
  live: true,
  sort: "newest",
  artifactsOnly: false,
  blockersOnly: false,
  failuresOnly: false
};

const uiSlice = createSlice({
  name: "ui",
  initialState,
  reducers: {
    setSelectedTaskID(state, action: PayloadAction<string>) {
      state.selectedTaskID = action.payload;
    },
    setStatusFilter(state, action: PayloadAction<UIState["statusFilter"]>) {
      state.statusFilter = action.payload;
    },
    setQuery(state, action: PayloadAction<string>) {
      state.query = action.payload;
    },
    setTaskLimit(state, action: PayloadAction<number>) {
      state.taskLimit = action.payload;
    },
    setRefreshMs(state, action: PayloadAction<number>) {
      state.refreshMs = action.payload;
    },
    setLive(state, action: PayloadAction<boolean>) {
      state.live = action.payload;
    },
    setSort(state, action: PayloadAction<SortOrder>) {
      state.sort = action.payload;
    },
    setArtifactsOnly(state, action: PayloadAction<boolean>) {
      state.artifactsOnly = action.payload;
    },
    setBlockersOnly(state, action: PayloadAction<boolean>) {
      state.blockersOnly = action.payload;
    },
    setFailuresOnly(state, action: PayloadAction<boolean>) {
      state.failuresOnly = action.payload;
    },
    clearQuickFilters(state) {
      state.artifactsOnly = false;
      state.blockersOnly = false;
      state.failuresOnly = false;
    }
  }
});

export const {
  setSelectedTaskID,
  setStatusFilter,
  setQuery,
  setTaskLimit,
  setRefreshMs,
  setLive,
  setSort,
  setArtifactsOnly,
  setBlockersOnly,
  setFailuresOnly,
  clearQuickFilters
} = uiSlice.actions;

export default uiSlice.reducer;

