import { defineConfig } from "vite";
import solid from "vite-plugin-solid";

export default defineConfig({
  plugins: [solid({ dev: false, hot: false })],
  build: {
    target: "es2020",
    minify: true,
    lib: {
      entry: "src/main.jsx",
      name: "GossamerSolidFixture",
      formats: ["iife"],
      fileName: () => "solid-counter-1.9.14.production.js"
    },
    rollupOptions: {
      output: {
        inlineDynamicImports: true
      }
    }
  }
});
