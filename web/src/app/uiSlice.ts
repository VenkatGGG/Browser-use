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
}

const initialState: UIState = {
  selectedTaskID: "",
  statusFilter: "all",
  query: "",
  taskLimit: 120,
  refreshMs: 3000,
  live: true,
  sort: "newest"
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
  setSort
} = uiSlice.actions;

export default uiSlice.reducer;

