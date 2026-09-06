#!/usr/bin/env bash
# =====================================================================
# sync-www.sh —— 将 React 前端构建产物(dist)同步到后端静态目录(www)
#
# 用法: sync-www.sh <dist目录> <www目录>
#   - dist 必须已存在(先执行 npm run build / make web-build)
#   - www 不存在时自动创建
#   - www/config 为后端运行时写入目录(get_theme.json / get_language.json),
#     同步时整目录保留不动;若缺失则创建并写入默认值
#   - 同步完成后校验 www/index.html 存在(后端启动校验项),否则退出非零
#
# 幂等:重复执行结果一致。只依赖 bash 内建与 cp/rm/mkdir/find(设备与 CI
# 环境最小化,不用 rsync)。
# =====================================================================
set -euo pipefail

if [ $# -ne 2 ]; then
	echo "用法: $0 <dist目录> <www目录>" >&2
	exit 2
fi

DIST="$1"
WWW="$2"

if [ ! -d "$DIST" ]; then
	echo "[错误] dist 目录不存在: $DIST(请先执行构建)" >&2
	exit 1
fi

# www 不存在则创建
mkdir -p "$WWW"

# 删除 www 下除 config 外的全部内容(config 不存在时也不报错)
find "$WWW" -mindepth 1 -maxdepth 1 ! -name config -exec rm -rf {} +

# dist/. 全量复制进 www
cp -a "$DIST"/. "$WWW"/

# 确保 www/config 存在;缺失则创建并写入默认配置(已存在时绝不改动)
if [ ! -d "$WWW/config" ]; then
	mkdir -p "$WWW/config"
	echo '{"theme":"light"}' > "$WWW/config/get_theme.json"
	echo '{"language":"zh-CN"}' > "$WWW/config/get_language.json"
fi

# 最终校验:后端启动要求 www/index.html 存在
if [ ! -f "$WWW/index.html" ]; then
	echo "[错误] 同步后未找到 $WWW/index.html,dist 产物不完整" >&2
	exit 1
fi

echo "[完成] 已同步 $DIST -> $WWW(config/ 已保留)"
