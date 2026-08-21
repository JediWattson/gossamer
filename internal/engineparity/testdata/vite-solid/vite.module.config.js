import { defineConfig } from "vite";
import solid from "vite-plugin-solid";

export default defineConfig({
  plugins: [solid({ dev: false, hot: false })],
  build: {
    target: "es2020",
    minify: true,
    emptyOutDir: false,
    rollupOptions: {
      input: "src/main.jsx",
      output: {
        entryFileNames: "solid-parity-1.9.14.production.module.js",
        chunkFileNames: "solid-runtime-1.9.14.production.module.js",
        manualChunks(id) {
          if (id.includes("node_modules/solid-js/")) {
            return "solid-runtime";
          }
        }
      }
    }
  }
});
