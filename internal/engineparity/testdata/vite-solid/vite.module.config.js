import { defineConfig } from "vite";
import solid from "vite-plugin-solid";

export default defineConfig({
  plugins: [solid({ dev: false, hot: false })],
  build: {
    target: "es2020",
    minify: true,
    emptyOutDir: false,
    lib: {
      entry: "src/main.jsx",
      formats: ["es"],
      fileName: () => "solid-parity-1.9.14.production.module.js"
    }
  }
});
