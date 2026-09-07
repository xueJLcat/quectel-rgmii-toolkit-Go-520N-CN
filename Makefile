# =====================================================================
# simpleadmin-go 构建 Makefile（位于仓库根目录）
#
# 与现有 Windows 批处理脚本等价（编译参数一一对应）：
#   - go-build/build_simpleadmin_go.bat    -> make arm
#     GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 GOTOOLCHAIN=local
#     go build -buildvcs=false -trimpath -ldflags "-s -w" \
#         -o development/simpleadmin/simpleadmin-httpd.armv7 ./cmd/simpleadmin-httpd
#   - windows-test/build_windows_test.bat  -> make windows
#     GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOTOOLCHAIN=local
#     go build -buildvcs=false -trimpath -ldflags "-s -w" \
#         -o windows-test/bin/simpleadmin-httpd-windows-amd64.exe ./cmd/simpleadmin-httpd
#
# 可覆盖变量（示例：make GO=/usr/local/go/bin/go GOTARGETS=./cmd/simpleadmin-httpd）：
#   GO        - go 命令，默认取 PATH 中的 go（不硬性要求特定小版本）
#   GOFMT     - gofmt 命令，默认取 PATH 中的 gofmt
#   GOTARGETS - go build 的目标包，默认 ./cmd/simpleadmin-httpd
# =====================================================================

GO        ?= go
GOFMT     ?= gofmt
GOTARGETS ?= ./cmd/simpleadmin-httpd

# 版本号：优先取 git 标签/提交号（如 2.97 或 abc1234-dirty），否则用默认值。
# 构建时注入 main.appVersion，服务运行期间把 HTML 里的 ?v=__SA_VERSION__
# 占位符替换为该版本号，发版不再需要手工改前端缓存参数。
# 覆盖示例：make arm VERSION=2.97
# git 不可用(源码 tarball/非仓库拷贝)时的回退值与 version.go 的内置
# 默认(appVersion)保持一致，避免同一代码出现两个"默认版本"。
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 2.98)

# Go 模块目录（含 go.mod，模块名 simpleadmin-httpd）
GO_SRC := go-build/simpleadmin-go

# 产物路径（与 .bat 中的输出位置一致）
ARM_OUT_DIR := development/simpleadmin
ARM_OUT     := $(ARM_OUT_DIR)/simpleadmin-httpd.armv7
WIN_OUT_DIR := windows-test/bin
WIN_OUT     := $(WIN_OUT_DIR)/simpleadmin-httpd-windows-amd64.exe

# 与 .bat 相同的构建参数：不写入 VCS 信息、裁剪路径、去掉符号表与 DWARF 调试信息；
# 另外注入版本号到 main.appVersion
GO_BUILD_FLAGS := -buildvcs=false -trimpath -ldflags "-s -w -X main.appVersion=$(VERSION)"

.PHONY: all arm windows test vet fmt-check clean \
	web-build web web-test web-e2e web-size web-dev dev-mock-build dev-mock

# 默认目标：同时产出 ARM 嵌入式版与 Windows 本地测试版
all: arm windows

# ARMv7 交叉编译（嵌入式设备用），等价于 go-build/build_simpleadmin_go.bat
arm:
	@mkdir -p "$(ARM_OUT_DIR)"
	cd "$(GO_SRC)" && GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 GOTOOLCHAIN=local GOEXPERIMENT= \
		$(GO) build $(GO_BUILD_FLAGS) -o "$(CURDIR)/$(ARM_OUT)" $(GOTARGETS)

# Windows amd64 本地测试版，等价于 windows-test/build_windows_test.bat
windows:
	@mkdir -p "$(WIN_OUT_DIR)"
	cd "$(GO_SRC)" && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOTOOLCHAIN=local GOEXPERIMENT= \
		$(GO) build $(GO_BUILD_FLAGS) -o "$(CURDIR)/$(WIN_OUT)" $(GOTARGETS)

# 运行单元测试（-count=1 禁用测试缓存）
test:
	cd "$(GO_SRC)" && $(GO) test -count=1 ./...

# 静态检查
vet:
	cd "$(GO_SRC)" && $(GO) vet ./...

# 格式检查：gofmt -l 有任何输出（即存在未格式化文件）则判定失败
fmt-check:
	@out=$$($(GOFMT) -l "$(GO_SRC)"); \
	if [ -n "$$out" ]; then \
		echo "[错误] 以下文件未通过 gofmt 格式化："; \
		echo "$$out"; \
		exit 1; \
	else \
		echo "[通过] gofmt 格式检查"; \
	fi

# ---------------------------------------------------------------------
# React 前端(Vite + React 19 + Tailwind CSS v4)
# 源码:development/simpleadmin/frontend-react/
# 产物:frontend-react/dist/(vite build 输出),make web 经 scripts/sync-www.sh
#      同步到 development/simpleadmin/www/ 由 Go 后端静态托管。
# 注意:www/config/ 是后端运行时写入目录(get_theme.json、get_language.json,
#      见 server_config.go),同步脚本整目录保留;www/index.html 为后端启动校验项。
# ---------------------------------------------------------------------
REACT_DIR := development/simpleadmin/frontend-react
WWW_DIR   := development/simpleadmin/www
SYNC_WWW  := $(REACT_DIR)/scripts/sync-www.sh
DEV_MOCK  := $(WIN_OUT_DIR)/simpleadmin-httpd-dev-mock

# 只构建 React 产物到 dist/(node_modules 缺失时先 npm ci),不改动 www
web-build:
	cd "$(REACT_DIR)" && { [ -d node_modules ] || npm ci --no-audit --no-fund; } && npm run build

# 构建并同步:此目标会用 React 产物替换 www(保留 config/)
web: web-build
	cd "$(REACT_DIR)" && "$(CURDIR)/$(SYNC_WWW)" dist ../www

# React 前端静态检查与单元测试(typecheck + eslint + vitest)
web-test:
	cd "$(REACT_DIR)" && npm run typecheck && npm run lint && npm test

# React 前端 E2E 测试(Playwright;webServer 自行构建并直连 Go 二进制,
# 不依赖本 Makefile 其他目标;需先 npx playwright install chromium)
web-e2e:
	cd "$(REACT_DIR)" && npx playwright test

# 前端产物体积预算闸门(检查 www;CI 中对构建产物用 --dist dist,
# 见 scripts/check-size.mjs 头部注释)
web-size:
	node "$(REACT_DIR)/scripts/check-size.mjs"

# 前端开发服务器(前台运行;Vite dev server :5173,/api、/console 已代理到 127.0.0.1:18080)
web-dev:
	cd "$(REACT_DIR)" && npm run dev

# linux 原生 mock 开发二进制(GOARCH 动态取本机;参数对齐 windows-test/run_windows_test.bat)
dev-mock-build:
	@mkdir -p "$(WIN_OUT_DIR)"
	cd "$(GO_SRC)" && GOOS=linux GOARCH=$$($(GO) env GOARCH) CGO_ENABLED=0 GOTOOLCHAIN=local GOEXPERIMENT= \
		$(GO) build $(GO_BUILD_FLAGS) -o "$(CURDIR)/$(DEV_MOCK)" $(GOTARGETS)

# 前台运行 mock 开发服务器(http://127.0.0.1:18080,静态目录 www;
# auth 文件不存在时自动写入默认 admin:admin)
dev-mock: dev-mock-build
	@mkdir -p windows-test/data
	@[ -f windows-test/data/simpleadmin.auth ] || echo "admin:admin" > windows-test/data/simpleadmin.auth
	"./$(DEV_MOCK)" -mock -no-tls -http 127.0.0.1:18080 -static "$(WWW_DIR)" -auth-file windows-test/data/simpleadmin.auth

# 清理构建产物
clean:
	rm -f "$(ARM_OUT)" "$(WIN_OUT)"
