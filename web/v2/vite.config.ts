import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// Production builds use a relative base so one artifact runs at any mount
// point: the Go binary serves the panel with a <base href> matching its
// configured --base-path, and every relative asset and API URL follows it (see
// docs/nginx.md). A fixed base here would force a rebuild per deployment, which
// is exactly what the runtime setting exists to avoid.
export default defineConfig(({ command }) => ({
  base: command === "build" ? "./" : "/",
  plugins: [vue()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: true
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:9876"
    }
  }
}));
