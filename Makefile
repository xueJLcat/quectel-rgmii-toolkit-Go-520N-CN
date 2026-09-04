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
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 2.97)

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

.PHONY: all arm windows test vet fmt-check smoke css css-check clean

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

# 前端模块装配冒烟测试（需要 node；按 index.html 顺序执行全部模块脚本）
smoke:
	node windows-test/frontend_smoke.js

# ---------------------------------------------------------------------
# 前端样式（Tailwind CSS v4）
# 源码:development/simpleadmin/frontend/(input.css + src/components-*.css)
# 产物:development/simpleadmin/www/css/tailwind.css(提交入库,设备端直接服务)
# 修改前端源码后必须执行 make css 并提交产物;css-check 校验产物与源码一致。
# 依赖安装:优先 `npm ci`(按 package-lock.json 精确还原,保证构建可复现)。
# ---------------------------------------------------------------------
FRONTEND_DIR := development/simpleadmin/frontend
CSS_OUT      := development/simpleadmin/www/css/tailwind.css
NPM_INSTALL  := cd "$(FRONTEND_DIR)" && \
	{ [ -f package-lock.json ] && npm ci --no-audit --no-fund --silent || npm install --no-audit --no-fund --silent; }

css:
	$(NPM_INSTALL) && npx tailwindcss -i input.css -o ../www/css/tailwind.css --minify

css-check:
	$(NPM_INSTALL) && npx tailwindcss -i input.css -o /tmp/simpleadmin-tailwind-check.css --minify && \
		diff -u "$(CURDIR)/$(CSS_OUT)" /tmp/simpleadmin-tailwind-check.css

# 清理构建产物
clean:
	rm -f "$(ARM_OUT)" "$(WIN_OUT)"
