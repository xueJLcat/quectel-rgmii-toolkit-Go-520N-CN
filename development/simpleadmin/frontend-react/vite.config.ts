import { fileURLToPath, URL } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

const at = (p: string) => fileURLToPath(new URL(p, import.meta.url));

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": at("./src") },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      input: {
        main: at("./index.html"),
        login: at("./login.html"),
      },
      output: {
        manualChunks(id) {
          if (!id.includes("node_modules")) return undefined;
          if (id.includes("echarts") || id.includes("zrender")) return "echarts";
          if (id.includes("@xterm")) return "xterm";
          if (id.includes("i18next")) return "i18n";
          if (id.includes("@tanstack")) return "query";
          if (id.includes("/motion/") || id.includes("framer-motion")) return "motion";
          // 必须锚定包目录:裸 "/react-dom/" 会误吞 @floating-ui/react-dom,
          // 令 react-vendor 反向依赖 vendor(floating-ui/dom)形成 chunk 循环,
          // sonner 顶层 React.createElement 触发 TDZ,整站白屏。
          if (
            id.includes("/node_modules/react/") ||
            id.includes("/node_modules/react-dom/") ||
            id.includes("/node_modules/scheduler/") ||
            id.includes("react-router")
          )
            return "react-vendor";
          return "vendor";
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://127.0.0.1:18080", ws: true },
      "/console": { target: "http://127.0.0.1:18080", ws: true },
    },
  },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: "./vitest.setup.ts",
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
