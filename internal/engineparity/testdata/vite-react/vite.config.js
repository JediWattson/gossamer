import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    target: "es2020",
    minify: true,
    rollupOptions: {
      input: "src/main.jsx",
      output: {
        entryFileNames: "react-taskboard-19.2.7.production.module.js",
        chunkFileNames: "react-taskboard-[name]-19.2.7.production.module.js",
        manualChunks(id) {
          if (id.includes("node_modules/react/") ||
              id.includes("node_modules/react-dom/") ||
              id.includes("node_modules/scheduler/")) {
            return "runtime";
          }
        }
      }
    }
  }
});
