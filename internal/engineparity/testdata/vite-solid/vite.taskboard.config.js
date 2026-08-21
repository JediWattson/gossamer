import { defineConfig } from "vite";
import solid from "vite-plugin-solid";

export default defineConfig({
  plugins: [solid({ dev: false, hot: false })],
  build: {
    target: "es2020",
    minify: true,
    emptyOutDir: false,
    rollupOptions: {
      input: "src/taskboard/main.jsx",
      output: {
        entryFileNames: "solid-taskboard-1.9.14.production.module.js",
        chunkFileNames: "solid-taskboard-[name]-1.9.14.production.module.js",
        manualChunks(id) {
          if (id.includes("node_modules/solid-js/")) {
            return "runtime";
          }
        }
      }
    }
  }
});
