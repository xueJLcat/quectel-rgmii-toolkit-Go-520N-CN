// Playwright E2E 配置:对 Go 后端 mock 模式(仅 127.0.0.1:18080)跑通全部用例。
// 安全红线:webServer/用例只访问本机 mock 服务器,不触真机(192.168.5.1)与外网;
// 浏览器以 --no-proxy-server 启动,规避环境代理(本环境存在悬挂的外网代理变量)。
//
// webServer 不走 make(并行代理正在修改 Makefile),直接拼接 go build + 运行命令
// (等效 make dev-mock);相对路径以本配置所在目录(frontend-react/)为基准。
// GOTOOLCHAIN=local GOPROXY=off:构建强制离线,不外联。
import { defineConfig } from "@playwright/test";

import { AUTH_STATE_PATH } from "./e2e/helpers";

const BASE_URL = "http://127.0.0.1:18080";

// 本环境存在外网代理变量(http_proxy 等),会劫持 webServer 就绪探测(127.0.0.1),
// 导致 reuseExistingServer 误判为未启动;统一为本机地址声明代理旁路(只增不减)。
// 浏览器侧另以 --no-proxy-server 启动,双保险确保流量仅限本机。
process.env.NO_PROXY = ["127.0.0.1", "localhost", process.env.NO_PROXY]
  .filter(Boolean)
  .join(",");
process.env.no_proxy = process.env.NO_PROXY;

const WEB_SERVER_COMMAND = [
  "set -e",
  "cd ../../../go-build/simpleadmin-go",
  "GOTOOLCHAIN=local GOPROXY=off CGO_ENABLED=0 go build -o ../../windows-test/bin/simpleadmin-httpd-dev-mock ./cmd/simpleadmin-httpd",
  "cd ../..",
  "exec ./windows-test/bin/simpleadmin-httpd-dev-mock -mock -no-tls -http 127.0.0.1:18080 -static development/simpleadmin/www -auth-file windows-test/data/simpleadmin.auth",
].join(" && ");

export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: true,
  reporter: "list",
  use: {
    baseURL: BASE_URL,
    viewport: { width: 1440, height: 900 },
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    launchOptions: { args: ["--no-proxy-server"] },
  },
  webServer: {
    command: WEB_SERVER_COMMAND,
    url: `${BASE_URL}/login.html`,
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
  },
  projects: [
    { name: "setup", testMatch: /auth\.setup\.ts/ },
    {
      name: "chromium",
      use: { storageState: AUTH_STATE_PATH },
      dependencies: ["setup"],
      testIgnore: /auth\.setup\.ts/,
    },
  ],
});
