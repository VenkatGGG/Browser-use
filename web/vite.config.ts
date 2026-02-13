import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    globals: true
  },
  server: {
    port: 5173,
    proxy: {
      "/v1": "http://localhost:8080",
      "/sessions": "http://localhost:8080",
      "/task": "http://localhost:8080",
      "/tasks": "http://localhost:8080",
      "/metrics": "http://localhost:8080",
      "/healthz": "http://localhost:8080",
      "/artifacts": "http://localhost:8080"
    }
  }
});
