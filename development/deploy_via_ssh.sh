#!/usr/bin/env bash
# deploy_via_ssh.sh — 经 SSH/SCP 部署 SimpleAdmin Go 到设备(不依赖 adb/Windows)。
#
# 流程:本地构建(make arm)→ 远端备份 → 上传安装包 → 执行 install_simpleadmin_go.sh
#       → 部署后验证 → 验证失败自动回滚备份 → 验证通过后删除备份释放空间。
#
# 用法:
#   development/deploy_via_ssh.sh [选项]
#     -y, --yes         跳过确认提示
#     -n, --dry-run     只打印将执行的动作,不实际执行
#         --skip-build  跳过本地构建(使用现有 simpleadmin-httpd.armv7)
#         --rollback    不部署,回滚到设备上最近一次部署备份后即退出
#                       (备份在部署成功后自动删除,仅备份尚存时可用)
#     -h, --help        帮助
#
# 环境变量(均可覆盖):
#   CPE_HOST     设备地址,默认 192.168.5.1
#   CPE_USER     SSH 用户,默认 root(仅密钥登录)
#   CPE_PORT     SSH 端口,默认 22
#   CPE_SSH_KEY  SSH 私钥,默认 ~/.ssh/cpe_id_ed25519
#
# 说明:
#   - 安装期间 Web 管理界面中断约 5~15 秒;设备的蜂窝网络/DHCP/DNS 由 QCMAP 与
#     dnsmasq 负责,不受 simpleadmin-httpd 重启影响。
#   - 备份保存在设备 /usrdata/simpleadmin/.deploy-backup/(二进制 + 上一版前端),
#     部署成功后自动删除以释放空间;--rollback 仅在备份尚存
#     (异常中断/删除失败)时可用。
#   - 安装脚本可能输出 REBOOT_REQUIRED=1(QCMAP mobileap 配置被修改时),
#     本脚本只提示、不自动重启设备。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CPE_HOST="${CPE_HOST:-192.168.5.1}"
CPE_USER="${CPE_USER:-root}"
CPE_PORT="${CPE_PORT:-22}"
CPE_SSH_KEY="${CPE_SSH_KEY:-$HOME/.ssh/cpe_id_ed25519}"

# 设备端路径(与 install_simpleadmin_go.sh 约定一致)
REMOTE_PKG_DIR="/tmp/development"
REMOTE_INSTALL_DIR="/usrdata/simpleadmin"
REMOTE_BACKUP_DIR="$REMOTE_INSTALL_DIR/.deploy-backup"
REMOTE_UNIT="simpleadmin-httpd.service"
REMOTE_RESULT_ENV="/tmp/simpleadmin-install-result.env"

# 本地产物路径
PKG_SRC="$REPO_ROOT/development/simpleadmin"
INSTALL_SCRIPT="$REPO_ROOT/development/install_simpleadmin_go.sh"
ARM_BIN="$PKG_SRC/simpleadmin-httpd.armv7"

ASSUME_YES=0
DRY_RUN=0
SKIP_BUILD=0
DO_ROLLBACK=0

log()  { echo "[信息] $*"; }
warn() { echo "[警告] $*" >&2; }
fail() { echo "[错误] $*" >&2; exit 1; }

usage() {
    sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
    case "$1" in
        -y|--yes) ASSUME_YES=1 ;;
        -n|--dry-run) DRY_RUN=1 ;;
        --skip-build) SKIP_BUILD=1 ;;
        --rollback) DO_ROLLBACK=1 ;;
        -h|--help) usage; exit 0 ;;
        *) usage; fail "未知参数: $1" ;;
    esac
    shift
done

SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=10 -o IdentitiesOnly=yes
          -o StrictHostKeyChecking=accept-new -o ServerAliveInterval=15
          -p "$CPE_PORT" -i "$CPE_SSH_KEY")

ssh_cpe() { ssh "${SSH_OPTS[@]}" "$CPE_USER@$CPE_HOST" "$@"; }

# 中断提示:安装阶段 SSH 断链/Ctrl-C 时 set -e 直接退出,设备可能处于半装
# 状态;明确告知备份位置与 --rollback 恢复入口,避免操作者不知情。
trap 'warn "部署中断:设备可能处于半装状态;若备份尚存($REMOTE_BACKUP_DIR),可执行 $0 --rollback 恢复"' INT TERM

# 文件传输不使用 scp:设备端 dropbear 无 sftp-server,新版 OpenSSH scp
# 默认走 SFTP 协议会失败。改用 tar/cat over ssh(busybox 全兼容)。
push_file() { # push_file <本地文件> <远端路径>
    local src="$1" dst="$2"
    if [ "$DRY_RUN" = 1 ]; then
        echo "[演练] push_file $src → $CPE_HOST:$dst"
        return 0
    fi
    ssh_cpe "mkdir -p \"\$(dirname '$dst')\" && cat > '$dst'" < "$src"
}

push_dir() { # push_dir <本地目录> <远端目录>(整体替换远端目录)
    local src="$1" dst="$2"
    if [ "$DRY_RUN" = 1 ]; then
        echo "[演练] push_dir $src → $CPE_HOST:$dst"
        return 0
    fi
    tar -C "$src" -cf - . | ssh_cpe "rm -rf '$dst' && mkdir -p '$dst' && tar -C '$dst' -xf -"
}

run() {
    if [ "$DRY_RUN" = 1 ]; then
        echo "[演练] $*"
    else
        "$@"
    fi
}

# ---------------------------------------------------------------- 本地预检
local_preflight() {
    [ -f "$CPE_SSH_KEY" ] || fail "SSH 私钥不存在: $CPE_SSH_KEY"
    command -v ssh >/dev/null || fail "缺少 ssh 命令"
    command -v tar >/dev/null || fail "缺少 tar 命令(传输用)"

    if [ "$SKIP_BUILD" != 1 ]; then
        log "本地构建 ARMv7 二进制(make arm)…"
        run make -C "$REPO_ROOT" arm
    fi

    local f
    for f in "$ARM_BIN" \
             "$PKG_SRC/www/index.html" \
             "$PKG_SRC/systemd/$REMOTE_UNIT" \
             "$PKG_SRC/simplepasswd" \
             "$PKG_SRC/mobileap_bridge0_mac.sh" \
             "$INSTALL_SCRIPT"; do
        [ -f "$f" ] || fail "缺少产物文件: $f"
    done
    # ARM 可执行文件校验:魔数 + e_machine。只查魔数挡不住架构错误——
    # --skip-build 时误用 linux-amd64 产物同样是 ELF,推上设备后
    # exec format error,要走完整个部署+回滚周期才失败。
    if [ "$DRY_RUN" != 1 ]; then
        head -c 20 "$ARM_BIN" | grep -q ELF || fail "二进制不是 ELF 格式: $ARM_BIN"
        # e_machine 位于 ELF 头偏移 0x12(2 字节小端),EM_ARM=40(十进制)
        # =0x0028 → 小端字节序 "2800"。
        machine="$(od -An -j18 -N2 -tx1 "$ARM_BIN" | tr -d ' \n')"
        [ "$machine" = "2800" ] || fail "二进制不是 ARM 架构(e_machine 字节=$machine, want 2800/EM_ARM): $ARM_BIN"
    fi
    log "本地预检通过: $ARM_BIN"
}

# ---------------------------------------------------------------- 远端备份
remote_backup() {
    log "在设备上备份当前版本 → $REMOTE_BACKUP_DIR"
    # 远端必须 set -e 且逐项校验:旧实现 cp 失败(如 /usrdata 空间不足)被
    # 静默吞掉、末尾 echo 恒成功,部署继续推进;验证失败进入回滚时才发现
    # "无可用备份",设备停在新版本半装、旧版本无法恢复的不可逆状态。
    run ssh_cpe "
        set -eu
        mkdir -p '$REMOTE_BACKUP_DIR'
        if [ -f '$REMOTE_INSTALL_DIR/simpleadmin-httpd' ]; then
            cp -f '$REMOTE_INSTALL_DIR/simpleadmin-httpd' '$REMOTE_BACKUP_DIR/simpleadmin-httpd'
            [ -s '$REMOTE_BACKUP_DIR/simpleadmin-httpd' ] || { echo '[错误] 备份二进制为空(检查 /usrdata 空间)' >&2; exit 1; }
        fi
        if [ -d '$REMOTE_INSTALL_DIR/www' ]; then
            rm -rf '$REMOTE_BACKUP_DIR/www'
            cp -a '$REMOTE_INSTALL_DIR/www' '$REMOTE_BACKUP_DIR/www'
            [ -f '$REMOTE_BACKUP_DIR/www/index.html' ] || { echo '[错误] 备份 www 不完整' >&2; exit 1; }
        fi
        date '+%Y-%m-%d %H:%M:%S' > '$REMOTE_BACKUP_DIR/backup-time'
        echo '[信息] 备份完成'
    "
}

# ---------------------------------------------------------------- 回滚
rollback() {
    log "回滚到最近一次部署备份…"
    ssh_cpe "
        set -u
        [ -f '$REMOTE_BACKUP_DIR/simpleadmin-httpd' ] || { echo '[错误] 无可用的备份二进制' >&2; exit 1; }
        cp -f '$REMOTE_BACKUP_DIR/simpleadmin-httpd' '$REMOTE_INSTALL_DIR/simpleadmin-httpd'
        chmod +x '$REMOTE_INSTALL_DIR/simpleadmin-httpd'
        if [ -d '$REMOTE_BACKUP_DIR/www' ]; then
            rm -rf '$REMOTE_INSTALL_DIR/www'
            cp -a '$REMOTE_BACKUP_DIR/www' '$REMOTE_INSTALL_DIR/www'
        fi
        systemctl restart '$REMOTE_UNIT' >/dev/null 2>&1 || {
            # systemctl 不可用/失败时走安装脚本落盘的停止+启动脚本。
            # 旧兜底 killall -HUP 是致命错误:Go 进程只注册了 SIGINT/SIGTERM,
            # SIGHUP 走默认动作直接终止进程,而 fallback 模式(systemctl 恰好
            # 不可用的场景)没有任何机制再拉起,回滚反而把服务打死。
            '$REMOTE_INSTALL_DIR/stop_simpleadmin.sh' >/dev/null 2>&1 || true
            '$REMOTE_INSTALL_DIR/start_simpleadmin.sh' >/dev/null 2>&1 || true
        }
        sleep 2
        if systemctl is-active '$REMOTE_UNIT' >/dev/null 2>&1 || pidof simpleadmin-httpd >/dev/null 2>&1; then
            echo '[成功] 已回滚,服务运行中'
        else
            echo '[错误] 回滚后服务未运行,请手动检查' >&2
            exit 1
        fi
    "
}

# ---------------------------------------------------------------- 上传与安装
upload_and_install() {
    log "上传安装包 → $CPE_HOST:$REMOTE_PKG_DIR"
    run ssh_cpe "rm -rf '$REMOTE_PKG_DIR' && mkdir -p '$REMOTE_PKG_DIR/simpleadmin'"
    push_file "$INSTALL_SCRIPT" "$REMOTE_PKG_DIR/install_simpleadmin_go.sh"
    push_file "$ARM_BIN" "$REMOTE_PKG_DIR/simpleadmin/simpleadmin-httpd.armv7"
    push_dir "$PKG_SRC/www" "$REMOTE_PKG_DIR/simpleadmin/www"
    push_dir "$PKG_SRC/systemd" "$REMOTE_PKG_DIR/simpleadmin/systemd"
    push_file "$PKG_SRC/simplepasswd" "$REMOTE_PKG_DIR/simpleadmin/simplepasswd"
    push_file "$PKG_SRC/mobileap_bridge0_mac.sh" "$REMOTE_PKG_DIR/simpleadmin/mobileap_bridge0_mac.sh"
    if [ "$DRY_RUN" != 1 ]; then
        ssh_cpe "chmod +x '$REMOTE_PKG_DIR/install_simpleadmin_go.sh' '$REMOTE_PKG_DIR/simpleadmin/mobileap_bridge0_mac.sh'"
    fi

    log "设备端执行安装脚本(期间 Web 界面短暂中断)…"
    local rc=0
    if [ "$DRY_RUN" != 1 ]; then
        ssh_cpe "chmod +x '$REMOTE_PKG_DIR/install_simpleadmin_go.sh' && bash '$REMOTE_PKG_DIR/install_simpleadmin_go.sh'" || rc=$?
    fi

    local result=""
    if [ "$DRY_RUN" != 1 ]; then
        result="$(ssh_cpe "cat '$REMOTE_RESULT_ENV' 2>/dev/null || true" || true)"
        log "安装结果文件: ${result:-<空>}"
    fi

    if [ "$DRY_RUN" != 1 ] && { [ "$rc" != 0 ] || echo "$result" | grep -q "INSTALL_STATUS=FAIL"; }; then
        warn "安装脚本执行失败,准备回滚"
        rollback || true
        fail "部署失败(已回滚)。安装输出见上方。"
    fi

    if echo "$result" | grep -q "REBOOT_REQUIRED=1"; then
        warn "安装脚本提示需要重启设备(REBOOT_REQUIRED=1,QCMAP mobileap 配置有变更)。"
        warn "本脚本不会自动重启;请在确认业务空闲时手动重启设备。"
    fi
}

# ---------------------------------------------------------------- 部署后验证
verify_deployment() {
    log "部署后验证…"
    local ok=1

    if [ "$DRY_RUN" = 1 ]; then
        echo "[演练] 跳过验证"
        return 0
    fi

    # 1) 服务存活:systemd 要求 SubState=running(is-active 对崩溃循环中的
    #    activating 也返回 0);pidof 兜底必须排除 __smd-reader 子进程——它的
    #    argv[0] 是同一二进制路径,busybox pidof 按 basename 会一并命中,
    #    主进程已死仅剩毫秒级孤儿子进程时会误判存活。
    sleep 3
    if ssh_cpe "
        if systemctl is-active '$REMOTE_UNIT' >/dev/null 2>&1 \
           && systemctl show -p SubState '$REMOTE_UNIT' 2>/dev/null | grep -q '=running'; then
            exit 0
        fi
        for p in \$(pidof simpleadmin-httpd 2>/dev/null); do
            cmd=\$(tr '\000' ' ' < /proc/\$p/cmdline 2>/dev/null)
            case \"\$cmd\" in
                *simpleadmin-httpd*)
                    case \"\$cmd\" in
                        *__smd-reader*) ;;
                        *) exit 0 ;;
                    esac ;;
            esac
        done
        exit 1
    "; then
        log "✓ 服务运行中"
    else
        warn "✗ 服务未运行"
        ok=0
    fi

    # 2) AT 代理 Socket(新版本特性,顺带确认新二进制已生效)
    if ssh_cpe "test -S '$REMOTE_INSTALL_DIR/at_proxy.sock'"; then
        log "✓ AT 代理 Socket 存在"
    else
        warn "✗ AT 代理 Socket 缺失(可能仍是旧版本二进制)"
        ok=0
    fi

    # 3) HTTP 可达且应答者确实是 SimpleAdmin:只看状态码时,服务已死而厂商
    #    守护把 lighttpd 重新拉回 80 端口的场景同样返回 200/302(假阳性)。
    #    跟随重定向取登录页,校验前端专有标识(标题含 RG520N-CN)。
    local code body_tmp
    body_tmp="$(mktemp)"
    code="$(curl -sL -m 8 -o "$body_tmp" -w '%{http_code}' "http://$CPE_HOST/" 2>/dev/null || true)"
    [ -n "$code" ] || code=000
    case "$code" in
        200|301|302|303)
            if grep -q "RG520N-CN" "$body_tmp" 2>/dev/null; then
                log "✓ HTTP 可达且为 SimpleAdmin 前端(status=$code)"
            else
                warn "✗ :80 应答者不是 SimpleAdmin 前端(status=$code,可能被厂商 Web 服务占用)"
                ok=0
            fi
            ;;
        *) warn "✗ HTTP 不可达(status=$code)"; ok=0 ;;
    esac
    rm -f "$body_tmp"

    # 4) 设备端前端与本地包一致:旧判据 grep __SA_VERSION__ 无效——占位符由
    #    新二进制在服务端运行时替换,无论 www 新旧 served HTML 都不含占位符,
    #    检查恒过;且 pipefail 下 grep -q 命中即退会让 curl 收 SIGPIPE,
    #    管道非零反而走 ✓ 分支(判定方向反转)。改为比对构建产物指纹。
    local local_sum remote_sum
    local_sum="$(md5sum "$PKG_SRC/www/index.html" | awk '{print $1}')"
    remote_sum="$(ssh_cpe "md5sum '$REMOTE_INSTALL_DIR/www/index.html' 2>/dev/null" | awk '{print $1}')"
    if [ -n "$remote_sum" ] && [ "$local_sum" = "$remote_sum" ]; then
        log "✓ 设备端前端与本地包一致"
    else
        warn "✗ 设备端前端指纹不符(local=$local_sum remote=${remote_sum:-<读取失败>})"
        ok=0
    fi

    # 5) 状态接口不再返回误导性错误(经 at-client 验证 AT 通道)
    if ssh_cpe "'$REMOTE_INSTALL_DIR/simpleadmin-httpd' at-client ATI >/dev/null 2>&1"; then
        log "✓ at-client ATI 正常(退出码 0)"
    else
        warn "✗ at-client ATI 异常(不影响部署判定,供参考)"
    fi

    if [ "$ok" != 1 ]; then
        warn "验证未全部通过,执行回滚…"
        if rollback; then
            fail "部署失败(已自动回滚到上一版本)"
        else
            fail "部署失败且回滚失败,请手动检查设备!"
        fi
    fi
    log "部署验证通过"
}

cleanup_staging() {
    if [ "$DRY_RUN" != 1 ]; then
        ssh_cpe "rm -rf '$REMOTE_PKG_DIR'" >/dev/null 2>&1 || true
    fi
}

# 部署成功后删除设备上的备份,释放 /usrdata 空间(用户要求)。
# 回滚窗口随之关闭:--rollback 仅在异常中断残留备份时可用。
cleanup_backup() {
    if [ "$DRY_RUN" = 1 ]; then
        log "[dry-run] 将删除设备备份 $REMOTE_BACKUP_DIR"
        return
    fi
    log "部署成功,删除设备备份释放空间 → $REMOTE_BACKUP_DIR"
    ssh_cpe "rm -rf '$REMOTE_BACKUP_DIR'" || warn "备份删除失败(不影响部署结果),可手动清理"
}

# ---------------------------------------------------------------- 主流程
main() {
    log "目标设备: $CPE_USER@$CPE_HOST:$CPE_PORT(密钥: $CPE_SSH_KEY)"

    if [ "$DO_ROLLBACK" = 1 ]; then
        if [ "$ASSUME_YES" != 1 ]; then
            read -r -p "确认回滚 $CPE_HOST 到最近一次部署备份? [y/N] " ans
            case "$ans" in y|Y|yes|YES) ;; *) fail "已取消" ;; esac
        fi
        rollback
        exit 0
    fi

    local_preflight

    # 远端连通性预检
    if [ "$DRY_RUN" != 1 ]; then
        ssh_cpe "echo ok" >/dev/null || fail "无法通过 SSH 连接 $CPE_USER@$CPE_HOST(检查网络/密钥)"
        log "SSH 连通性正常"
    fi

    if [ "$ASSUME_YES" != 1 ] && [ "$DRY_RUN" != 1 ]; then
        read -r -p "确认部署到 $CPE_HOST? (Web 界面将中断约 5~15 秒) [y/N] " ans
        case "$ans" in y|Y|yes|YES) ;; *) fail "已取消" ;; esac
    fi

    remote_backup
    upload_and_install
    verify_deployment
    cleanup_backup
    cleanup_staging

    log "部署完成: http://$CPE_HOST/ (账号见设备 $REMOTE_INSTALL_DIR/simpleadmin.auth)"
}

main "$@"
