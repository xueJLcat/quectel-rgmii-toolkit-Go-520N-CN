#!/bin/bash

set -u

PKG_DIR="/tmp/development"
SIMPLEADMIN_SRC="$PKG_DIR/simpleadmin"

SIMPLEADMIN_DIR="/usrdata/simpleadmin"
TTL_VALUE_FILE="$SIMPLEADMIN_DIR/ttlvalue"
AT_DEVICES_FILE="$SIMPLEADMIN_DIR/at_devices.conf"
SYSTEMD_UNIT="simpleadmin-httpd.service"
SYSTEMD_DIR=""
WANTS_DIR=""
SERVICE_UNIT_INSTALLED="0"
FALLBACK_PID_FILE="$SIMPLEADMIN_DIR/simpleadmin-httpd.pid"
FALLBACK_START_SCRIPT="$SIMPLEADMIN_DIR/start_simpleadmin.sh"
FALLBACK_STOP_SCRIPT="$SIMPLEADMIN_DIR/stop_simpleadmin.sh"
PORT_PREPARE_SCRIPT="$SIMPLEADMIN_DIR/prepare_simpleadmin_ports.sh"
FALLBACK_LOG_FILE="/tmp/simpleadmin-httpd.log"
POST_BOOT_FILE="/etc/init.post_boot.sh"
POST_BOOT_LOG_FILE="/tmp/simpleadmin-postboot.log"
REBOOT_MARKER_FILE="/tmp/simpleadmin-reboot-required"
INSTALL_RESULT_FILE="/tmp/simpleadmin-install-result.env"
POST_BOOT_BEGIN="# BEGIN SIMPLEADMIN GO AUTOSTART"
POST_BOOT_END="# END SIMPLEADMIN GO AUTOSTART"
ROOT_BIN="/usrdata/root/bin"
MOBILEAP_HELPER_SRC="$SIMPLEADMIN_SRC/mobileap_bridge0_mac.sh"
MOBILEAP_HELPER_SCRIPT="$SIMPLEADMIN_DIR/mobileap_bridge0_mac.sh"
MOBILEAP_RESULT_FILE="/tmp/simpleadmin-mobileap-result.env"
MOBILEAP_CFG_TOUCHED="0"

log() {
    echo "[信息] $*"
}

warn() {
    echo "[警告] $*"
}

write_install_failure_result() {
    local msg="$1"
    {
        echo "INSTALL_STATUS=FAIL"
        echo "REBOOT_REQUIRED=0"
        echo "INSTALL_ERROR=$msg"
    } > "$INSTALL_RESULT_FILE" 2>/dev/null || true
}

write_install_success_result() {
    if [ -f "$INSTALL_RESULT_FILE" ]; then
        grep -q '^INSTALL_STATUS=' "$INSTALL_RESULT_FILE" 2>/dev/null || echo "INSTALL_STATUS=OK" >> "$INSTALL_RESULT_FILE"
    else
        {
            echo "INSTALL_STATUS=OK"
            echo "REBOOT_REQUIRED=0"
        } > "$INSTALL_RESULT_FILE" 2>/dev/null || true
    fi
}

fail() {
    write_install_failure_result "$*"
    echo "[错误] $*" >&2
    exit 1
}

remount_rw() {
    mount -o remount,rw / >/dev/null 2>&1 || true
}

remount_ro() {
    mount -o remount,ro / >/dev/null 2>&1 || true
}

require_file() {
    [ -f "$1" ] || fail "缺少文件: $1"
}

is_valid_auth_file() {
    local file="$1"
    local line=""

    [ -s "$file" ] || return 1
    line="$(grep -v '^[[:space:]]*#' "$file" 2>/dev/null | grep -v '^[[:space:]]*$' | head -n 1 || true)"
    case "$line" in
        *:*)
            [ -n "${line%%:*}" ] || return 1
            [ -n "${line#*:}" ] || return 1
            return 0
            ;;
    esac
    return 1
}

write_default_auth_file() {
    umask 077
    printf 'admin:admin\n' > "$SIMPLEADMIN_DIR/simpleadmin.auth"
    chmod 600 "$SIMPLEADMIN_DIR/simpleadmin.auth"
}

is_writable_dir() {
    local dir="$1"
    [ -d "$dir" ] || mkdir -p "$dir" 2>/dev/null || return 1
    [ -w "$dir" ]
}

find_systemd_dir() {
    local dir="/lib/systemd/system"

    if is_writable_dir "$dir"; then
        echo "$dir"
        return 0
    fi

    return 1
}

remove_unit_files_from_dir() {
    local dir="$1"
    local unit="$2"

    [ -n "$dir" ] || return 0
    rm -f "$dir/$unit" "$dir/multi-user.target.wants/$unit" 2>/dev/null || true
}

remove_systemd_unit_files() {
    remove_unit_files_from_dir /etc/systemd/system "$SYSTEMD_UNIT"
    remove_unit_files_from_dir /lib/systemd/system "$SYSTEMD_UNIT"
}

remove_stale_etc_unit() {
    remove_unit_files_from_dir /etc/systemd/system "$SYSTEMD_UNIT"
}

link_unit() {
    local unit="$1"

    [ -n "$SYSTEMD_DIR" ] || return 1
    [ -n "$WANTS_DIR" ] || return 1
    mkdir -p "$WANTS_DIR" 2>/dev/null || return 1
    ln -sf "$SYSTEMD_DIR/$unit" "$WANTS_DIR/$unit"
}

remove_post_boot_autostart() {
    [ -f "$POST_BOOT_FILE" ] || return 0
    sed -i "/$POST_BOOT_BEGIN/,/$POST_BOOT_END/d" "$POST_BOOT_FILE" 2>/dev/null || true
}

install_post_boot_autostart() {
    local parent=""

    parent="$(dirname "$POST_BOOT_FILE")"
    if [ ! -d "$parent" ] || [ ! -w "$parent" ]; then
        warn "post_boot 目录不可写，未安装自启动: $parent"
        return 1
    fi

    if [ ! -f "$POST_BOOT_FILE" ]; then
        warn "未找到 post_boot 脚本，跳过自启动安装: $POST_BOOT_FILE"
        return 1
    fi

    if [ ! -w "$POST_BOOT_FILE" ]; then
        warn "post_boot 脚本不可写，未安装自启动: $POST_BOOT_FILE"
        return 1
    fi

    remove_post_boot_autostart
    cat >> "$POST_BOOT_FILE" <<EOF
$POST_BOOT_BEGIN
(
    sleep 3
    if [ -x "$FALLBACK_START_SCRIPT" ]; then
        if command -v iptables >/dev/null 2>&1; then
            iptables -C INPUT -p tcp --dport 80 -j ACCEPT >/dev/null 2>&1 || iptables -I INPUT -p tcp --dport 80 -j ACCEPT >/dev/null 2>&1 || true
        fi
        "$FALLBACK_START_SCRIPT" >> "$POST_BOOT_LOG_FILE" 2>&1
    fi
) &
$POST_BOOT_END
EOF
    chmod +x "$POST_BOOT_FILE" 2>/dev/null || true
    log "post_boot 自启动已安装: $POST_BOOT_FILE"
}

install_at_device_config() {
    mkdir -p "$SIMPLEADMIN_DIR"
    log "正在写入 AT 设备配置：直接使用 /dev/smd11"
    cat > "$AT_DEVICES_FILE" <<'EOF'
/dev/smd11
EOF
    chmod 0644 "$AT_DEVICES_FILE"
}

cleanup_legacy_frontend_sources() {
    log "正在清理旧版上传遗留的前端构建源码（frontend / node_modules）"
    rm -rf "$SIMPLEADMIN_SRC/frontend" 2>/dev/null || true
    rm -rf "$SIMPLEADMIN_DIR/frontend" 2>/dev/null || true
    rm -rf "$SIMPLEADMIN_DIR/node_modules" 2>/dev/null || true
}

cleanup_legacy_at_bridges() {
    ps 2>/dev/null | awk '
        $0 ~ "cat /dev/ttyIN" { print $1 }
        $0 ~ "cat /dev/smd11 > /dev/ttyIN" { print $1 }
        $0 ~ "socat" && ($0 ~ "link=/dev/ttyIN" || $0 ~ "link=/dev/ttyOUT") { print $1 }
    ' | while read pid; do
        case "$pid" in
            ""|*[!0-9]*) continue ;;
        esac
        kill "$pid" >/dev/null 2>&1 || true
        sleep 1
        kill -9 "$pid" >/dev/null 2>&1 || true
    done
    rm -f /dev/ttyIN /dev/ttyOUT 2>/dev/null || true
}

stop_existing_simpleadmin_runtime() {
    local pid=""

    log "正在停止旧的 SimpleAdmin 运行实例"
    systemctl stop "$SYSTEMD_UNIT" >/dev/null 2>&1 || true
    systemctl disable "$SYSTEMD_UNIT" >/dev/null 2>&1 || true

    if [ -x "$FALLBACK_STOP_SCRIPT" ]; then
        "$FALLBACK_STOP_SCRIPT" >/dev/null 2>&1 || true
    fi

    if [ -f "$FALLBACK_PID_FILE" ]; then
        pid="$(cat "$FALLBACK_PID_FILE" 2>/dev/null || true)"
        case "$pid" in
            ""|*[!0-9]*) ;;
            *)
                # PID 文件在持久分区(/usrdata),掉电重启后残留的 PID 可能被
                # 任意厂商进程复用:kill 前必须按 /proc cmdline 校验身份,
                # 否则会误杀无辜进程。
                cmd="$(tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null || true)"
                case "$cmd" in
                    *simpleadmin-httpd*) kill "$pid" >/dev/null 2>&1 || true ;;
                esac
                ;;
        esac
        rm -f "$FALLBACK_PID_FILE"
    fi

    killall simpleadmin-httpd >/dev/null 2>&1 || true
    sleep 1
    if pidof simpleadmin-httpd >/dev/null 2>&1 || ps 2>/dev/null | grep '[s]impleadmin-httpd' >/dev/null 2>&1; then
        killall -9 simpleadmin-httpd >/dev/null 2>&1 || true
        sleep 1
    fi
    cleanup_legacy_at_bridges
    remove_systemd_unit_files
    systemctl daemon-reload >/dev/null 2>&1 || true
    systemctl reset-failed >/dev/null 2>&1 || true
}

install_ttl_state() {
    log "正在初始化 Go 原生 TTL 配置"
    local ttl_value="0"

    if [ -f "$TTL_VALUE_FILE" ]; then
        ttl_value="$(grep -o "[0-9]\{1,3\}" "$TTL_VALUE_FILE" 2>/dev/null | head -n 1 || true)"
    fi

    case "$ttl_value" in
        ""|*[!0-9]*) ttl_value="0" ;;
    esac
    if [ "$ttl_value" -gt 255 ]; then
        ttl_value="0"
    fi

    mkdir -p "$SIMPLEADMIN_DIR"
    echo "$ttl_value" > "$TTL_VALUE_FILE"
    chmod 0644 "$TTL_VALUE_FILE"
}

install_mobileap_helper_script() {
    require_file "$MOBILEAP_HELPER_SRC"
    cp -f "$MOBILEAP_HELPER_SRC" "$MOBILEAP_HELPER_SCRIPT"
    chmod +x "$MOBILEAP_HELPER_SCRIPT"
}

maybe_install_bridge0_mac_config() {
    MOBILEAP_CFG_TOUCHED="0"
    rm -f "$MOBILEAP_RESULT_FILE" 2>/dev/null || true

    if [ ! -x "$MOBILEAP_HELPER_SCRIPT" ]; then
        warn "mobileap bridge0 MAC 辅助脚本缺失或不可执行: $MOBILEAP_HELPER_SCRIPT"
        return 0
    fi

    SIMPLEADMIN_DIR="$SIMPLEADMIN_DIR" \
    MOBILEAP_RESULT_FILE="$MOBILEAP_RESULT_FILE" \
    SIMPLEADMIN_FIX_BRIDGE0_MAC="${SIMPLEADMIN_FIX_BRIDGE0_MAC:-1}" \
    "$MOBILEAP_HELPER_SCRIPT" || warn "mobileap bridge0 MAC 辅助脚本执行失败"

    if [ -f "$MOBILEAP_RESULT_FILE" ]; then
        # shellcheck disable=SC1090
        . "$MOBILEAP_RESULT_FILE" 2>/dev/null || true
    fi

    case "${MOBILEAP_CFG_TOUCHED:-0}" in
        1) MOBILEAP_CFG_TOUCHED="1" ;;
        *) MOBILEAP_CFG_TOUCHED="0" ;;
    esac
}

install_simpleadmin_files() {
    log "正在安装 SimpleAdmin Go 运行文件"
    require_file "$SIMPLEADMIN_SRC/simpleadmin-httpd.armv7"
    require_file "$SIMPLEADMIN_SRC/systemd/simpleadmin-httpd.service"
    require_file "$SIMPLEADMIN_SRC/simplepasswd"
    require_file "$MOBILEAP_HELPER_SRC"
    # www 目录必须完整:下方先删旧 www 再复制,若复制失败(空间不足/staging
    # 不完整),二进制的 validateStaticDir 会因缺 index.html 直接 log.Fatalf,
    # 独立安装(adb/手动)又没有 deploy 的备份回滚兜底,服务将永久起不来。
    require_file "$SIMPLEADMIN_SRC/www/index.html"

    mkdir -p "$SIMPLEADMIN_DIR" "$SIMPLEADMIN_DIR/www" "$ROOT_BIN" || fail "无法创建安装目录"

    rm -f /tmp/simpleadmin.auth.backup
    if is_valid_auth_file "$SIMPLEADMIN_DIR/simpleadmin.auth"; then
        cp -f "$SIMPLEADMIN_DIR/simpleadmin.auth" /tmp/simpleadmin.auth.backup
    elif [ -f "$SIMPLEADMIN_DIR/simpleadmin.auth" ]; then
        warn "现有认证文件为空或格式无效，已重置为默认 admin/admin"
    fi

    # 保留运行时偏好目录 www/config(get_theme.json/get_language.json 由后端
    # 运行时写入):整体替换 www 会把用户主题/语言在每次升级后重置回仓库
    # 默认值,与本地 sync-www.sh 的"config 已存在绝不改动"行为不一致。
    rm -rf /tmp/simpleadmin-www-config.backup
    if [ -d "$SIMPLEADMIN_DIR/www/config" ]; then
        cp -a "$SIMPLEADMIN_DIR/www/config" /tmp/simpleadmin-www-config.backup 2>/dev/null \
            || warn "备份 www/config 失败,本次升级将重置主题/语言偏好"
    fi

    rm -rf "$SIMPLEADMIN_DIR/www" "$SIMPLEADMIN_DIR/console" "$SIMPLEADMIN_DIR/systemd"
    mkdir -p "$SIMPLEADMIN_DIR/systemd"

    # 二进制先写临时文件再 mv(同目录原子替换):cp -f 直写在中途 ENOSPC/
    # 掉电时留下截断二进制,旧版已被覆盖、新版不完整,两边都起不来。
    cp -f "$SIMPLEADMIN_SRC/simpleadmin-httpd.armv7" "$SIMPLEADMIN_DIR/simpleadmin-httpd.tmp.$$" \
        || fail "复制二进制失败(检查 /usrdata 剩余空间)"
    chmod +x "$SIMPLEADMIN_DIR/simpleadmin-httpd.tmp.$$"
    mv -f "$SIMPLEADMIN_DIR/simpleadmin-httpd.tmp.$$" "$SIMPLEADMIN_DIR/simpleadmin-httpd" \
        || fail "替换二进制失败"

    cp -rf "$SIMPLEADMIN_SRC/www" "$SIMPLEADMIN_DIR/www" \
        || fail "复制 www 失败(检查 /usrdata 剩余空间)"
    if [ ! -f "$SIMPLEADMIN_DIR/www/index.html" ]; then
        fail "www 复制不完整(缺 index.html),服务将无法启动"
    fi
    if [ -d /tmp/simpleadmin-www-config.backup ]; then
        mkdir -p "$SIMPLEADMIN_DIR/www/config"
        cp -a /tmp/simpleadmin-www-config.backup/. "$SIMPLEADMIN_DIR/www/config/" 2>/dev/null \
            || warn "恢复 www/config 失败,主题/语言偏好可能被重置"
        rm -rf /tmp/simpleadmin-www-config.backup
    fi

    cp -f "$SIMPLEADMIN_SRC/simplepasswd" "$ROOT_BIN/simplepasswd" \
        || warn "复制 simplepasswd 失败(不影响主服务)"
    chmod +x "$ROOT_BIN/simplepasswd" 2>/dev/null || true

    install_mobileap_helper_script

    if [ -f /tmp/simpleadmin.auth.backup ]; then
        cp -f /tmp/simpleadmin.auth.backup "$SIMPLEADMIN_DIR/simpleadmin.auth"
        rm -f /tmp/simpleadmin.auth.backup
    elif ! is_valid_auth_file "$SIMPLEADMIN_DIR/simpleadmin.auth"; then
        write_default_auth_file
    fi
    chmod 600 "$SIMPLEADMIN_DIR/simpleadmin.auth"

    cp -f "$SIMPLEADMIN_SRC/systemd/$SYSTEMD_UNIT" "$SIMPLEADMIN_DIR/systemd/$SYSTEMD_UNIT" \
        || fail "复制 systemd 单元失败"
    install_fallback_scripts
    install_systemd_unit || warn "systemd 服务无法安装到 /lib/systemd/system；如果服务无法启动，将使用 post_boot 兜底"
}

install_fallback_scripts() {
    cat > "$PORT_PREPARE_SCRIPT" <<'EOF'
#!/bin/sh

port80_listener_inodes() {
    awk 'NR > 1 && $2 ~ /:0050$/ && $4 == "0A" { print $10 }' /proc/net/tcp /proc/net/tcp6 2>/dev/null | sort -u
}

port80_owner_pids() {
    local inode fd link pid
    for inode in $(port80_listener_inodes); do
        for fd in /proc/[0-9]*/fd/*; do
            [ -e "$fd" ] || continue
            link="$(readlink "$fd" 2>/dev/null || true)"
            [ "$link" = "socket:[$inode]" ] || continue
            pid="${fd#/proc/}"
            pid="${pid%%/*}"
            echo "$pid"
        done
    done | sort -u
}

cmdline_for_pid() {
    local pid="$1"
    if [ -r "/proc/$pid/cmdline" ]; then
        tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null
    elif [ -r "/proc/$pid/comm" ]; then
        cat "/proc/$pid/comm" 2>/dev/null
    else
        echo "unknown"
    fi
}

is_simpleadmin_related_cmd() {
    case "$1" in
        *simpleadmin*|*SimpleAdmin*|*ZBIMS*|*zbims*) return 0 ;;
    esac
    return 1
}

is_factory_lighttpd_cmd() {
    case "$1" in
        *"lighttpd"*"/data/lighttpd.conf"*) return 0 ;;
    esac
    return 1
}

stop_pid_soft_then_hard() {
    local pid="$1"
    case "$pid" in
        ""|*[!0-9]*) return 0 ;;
    esac
    kill "$pid" >/dev/null 2>&1 || true
}

stop_known_web_conflicts() {
    local pid cmd

    for pid in $(pidof lighttpd 2>/dev/null || true); do
        cmd="$(cmdline_for_pid "$pid")"
        if is_factory_lighttpd_cmd "$cmd"; then
            echo "[信息] 正在停止原厂 lighttpd Web 服务以释放 80 端口: pid=$pid"
            stop_pid_soft_then_hard "$pid"
        fi
    done

    for pid in $(port80_owner_pids); do
        cmd="$(cmdline_for_pid "$pid")"
        if is_simpleadmin_related_cmd "$cmd" || is_factory_lighttpd_cmd "$cmd"; then
            stop_pid_soft_then_hard "$pid"
        fi
    done

    sleep 1

    for pid in $(pidof lighttpd 2>/dev/null || true); do
        cmd="$(cmdline_for_pid "$pid")"
        if is_factory_lighttpd_cmd "$cmd"; then
            kill -9 "$pid" >/dev/null 2>&1 || true
        fi
    done

    for pid in $(port80_owner_pids); do
        cmd="$(cmdline_for_pid "$pid")"
        if is_simpleadmin_related_cmd "$cmd" || is_factory_lighttpd_cmd "$cmd"; then
            kill -9 "$pid" >/dev/null 2>&1 || true
        fi
    done
}

describe_port80_owners() {
    local pid cmd
    for pid in $(port80_owner_pids); do
        cmd="$(cmdline_for_pid "$pid")"
        echo "pid=$pid cmd=$cmd"
    done
}

wait_for_port80_free() {
    local attempt owners
    for attempt in 1 2 3 4 5; do
        owners="$(port80_owner_pids)"
        [ -z "$owners" ] && return 0
        stop_known_web_conflicts
        sleep 1
    done

    owners="$(port80_owner_pids)"
    [ -z "$owners" ] && return 0

    echo "[错误] 80 端口仍被占用，SimpleAdmin Go 无法绑定 :80" >&2
    describe_port80_owners >&2
    return 1
}

stop_known_web_conflicts
wait_for_port80_free
EOF

    cat > "$FALLBACK_START_SCRIPT" <<'EOF'
#!/bin/sh
SIMPLEADMIN_DIR="/usrdata/simpleadmin"
PID_FILE="$SIMPLEADMIN_DIR/simpleadmin-httpd.pid"
LOG_FILE="/tmp/simpleadmin-httpd.log"
BIN="$SIMPLEADMIN_DIR/simpleadmin-httpd"
PORT_PREPARE_SCRIPT="$SIMPLEADMIN_DIR/prepare_simpleadmin_ports.sh"

open_simpleadmin_port() {
    if command -v iptables >/dev/null 2>&1; then
        iptables -C INPUT -p tcp --dport 80 -j ACCEPT >/dev/null 2>&1 || iptables -I INPUT -p tcp --dport 80 -j ACCEPT >/dev/null 2>&1 || true
    fi
}

cleanup_legacy_at_bridges() {
    ps 2>/dev/null | awk '
        $0 ~ "cat /dev/ttyIN" { print $1 }
        $0 ~ "cat /dev/smd11 > /dev/ttyIN" { print $1 }
        $0 ~ "socat" && ($0 ~ "link=/dev/ttyIN" || $0 ~ "link=/dev/ttyOUT") { print $1 }
    ' | while read pid; do
        case "$pid" in
            ""|*[!0-9]*) continue ;;
        esac
        kill "$pid" >/dev/null 2>&1 || true
        sleep 1
        kill -9 "$pid" >/dev/null 2>&1 || true
    done
    rm -f /dev/ttyIN /dev/ttyOUT 2>/dev/null || true
}

if [ ! -x "$BIN" ]; then
    echo "[错误] simpleadmin-httpd 缺失或不可执行: $BIN" >&2
    exit 1
fi

# 主服务进程身份判定:PID 文件在持久分区,掉电重启后残留 PID 可能被任意
# 厂商进程复用;__smd-reader 子进程的 argv[0] 又是同一二进制路径,pidof 会
# 一并命中。两种误认都会让启动脚本把"没在运行"判成"已在运行"(拒绝启动,
# post_boot 兜底静默失效)或把错误 PID 写进 PID 文件(停止脚本误杀)。
pid_is_simpleadmin_server() {
    _cand="$1"
    [ -n "$_cand" ] || return 1
    [ -r "/proc/$_cand/cmdline" ] || return 1
    _cmd="$(tr '\000' ' ' < "/proc/$_cand/cmdline" 2>/dev/null)"
    case "$_cmd" in
        *simpleadmin-httpd*) ;;
        *) return 1 ;;
    esac
    case "$_cmd" in
        *__smd-reader*) return 1 ;;
    esac
    return 0
}

if [ -f "$PID_FILE" ]; then
    OLD_PID="$(cat "$PID_FILE" 2>/dev/null || true)"
    if pid_is_simpleadmin_server "$OLD_PID" && kill -0 "$OLD_PID" 2>/dev/null; then
        echo "[OK] simpleadmin-httpd already running: $OLD_PID"
        exit 0
    fi
    rm -f "$PID_FILE"
fi

RUNNING_PID=""
for _cand in $(pidof simpleadmin-httpd 2>/dev/null); do
    if pid_is_simpleadmin_server "$_cand"; then
        RUNNING_PID="$_cand"
        break
    fi
done
if [ -z "$RUNNING_PID" ]; then
    for _cand in $(ps 2>/dev/null | grep '[s]impleadmin-httpd' | awk '{print $1}'); do
        if pid_is_simpleadmin_server "$_cand"; then
            RUNNING_PID="$_cand"
            break
        fi
    done
fi
if [ -n "$RUNNING_PID" ]; then
    echo "$RUNNING_PID" > "$PID_FILE"
    echo "[OK] simpleadmin-httpd already running: $RUNNING_PID"
    exit 0
fi

port80_listener_inodes() {
    awk 'NR > 1 && $2 ~ /:0050$/ && $4 == "0A" { print $10 }' /proc/net/tcp /proc/net/tcp6 2>/dev/null | sort -u
}

port80_owner_pids() {
    local inode fd link pid
    for inode in $(port80_listener_inodes); do
        for fd in /proc/[0-9]*/fd/*; do
            [ -e "$fd" ] || continue
            link="$(readlink "$fd" 2>/dev/null || true)"
            [ "$link" = "socket:[$inode]" ] || continue
            pid="${fd#/proc/}"
            pid="${pid%%/*}"
            echo "$pid"
        done
    done | sort -u
}

cmdline_for_pid() {
    local pid="$1"
    if [ -r "/proc/$pid/cmdline" ]; then
        tr '\000' ' ' < "/proc/$pid/cmdline" 2>/dev/null
    elif [ -r "/proc/$pid/comm" ]; then
        cat "/proc/$pid/comm" 2>/dev/null
    else
        echo "unknown"
    fi
}

is_simpleadmin_related_cmd() {
    case "$1" in
        *simpleadmin*|*SimpleAdmin*|*ZBIMS*|*zbims*) return 0 ;;
    esac
    return 1
}

stop_simpleadmin_port80_conflicts() {
    local pid cmd
    for pid in $(port80_owner_pids); do
        cmd="$(cmdline_for_pid "$pid")"
        if is_simpleadmin_related_cmd "$cmd"; then
            kill "$pid" >/dev/null 2>&1 || true
        fi
    done
}

force_stop_simpleadmin_port80_conflicts() {
    local pid cmd
    for pid in $(port80_owner_pids); do
        cmd="$(cmdline_for_pid "$pid")"
        if is_simpleadmin_related_cmd "$cmd"; then
            kill -9 "$pid" >/dev/null 2>&1 || true
        fi
    done
}

describe_port80_owners() {
    local pid cmd
    for pid in $(port80_owner_pids); do
        cmd="$(cmdline_for_pid "$pid")"
        echo "pid=$pid cmd=$cmd"
    done
}

wait_for_port80_free() {
    local owners=""
    local attempt=""

    for attempt in 1 2 3 4 5; do
        owners="$(port80_owner_pids)"
        [ -z "$owners" ] && return 0
        stop_simpleadmin_port80_conflicts
        sleep 1
    done

    owners="$(port80_owner_pids)"
    if [ -n "$owners" ]; then
        force_stop_simpleadmin_port80_conflicts
        sleep 1
    fi

    owners="$(port80_owner_pids)"
    if [ -z "$owners" ]; then
        return 0
    fi

    echo "[错误] 80 端口已被占用，SimpleAdmin Go 无法绑定 :80" >&2
    describe_port80_owners >&2
    return 1
}

if [ -x "$PORT_PREPARE_SCRIPT" ]; then
    "$PORT_PREPARE_SCRIPT" || exit 1
fi
killall simpleadmin-httpd >/dev/null 2>&1 || true
cleanup_legacy_at_bridges
open_simpleadmin_port
if ! wait_for_port80_free; then
    exit 1
fi
: > "$LOG_FILE"
nohup "$BIN" \
    -http :80 \
    -static "$SIMPLEADMIN_DIR/www" \
    -auth-file "$SIMPLEADMIN_DIR/simpleadmin.auth" \
    -at-devices-file "$SIMPLEADMIN_DIR/at_devices.conf" \
    >> "$LOG_FILE" 2>&1 &
echo $! > "$PID_FILE"
sleep 1
PID="$(cat "$PID_FILE" 2>/dev/null || true)"
if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    echo "[OK] simpleadmin-httpd started by nohup: $PID"
    exit 0
fi

echo "[错误] simpleadmin-httpd 后台启动失败，日志如下:" >&2
cat "$LOG_FILE" >&2 2>/dev/null || true
exit 1
EOF

    cat > "$FALLBACK_STOP_SCRIPT" <<'EOF'
#!/bin/sh
SIMPLEADMIN_DIR="/usrdata/simpleadmin"
PID_FILE="$SIMPLEADMIN_DIR/simpleadmin-httpd.pid"

if [ -f "$PID_FILE" ]; then
    PID="$(cat "$PID_FILE" 2>/dev/null || true)"
    # 持久分区残留 PID 可能已被无关进程复用:按 /proc cmdline 校验身份再杀。
    if [ -n "$PID" ] && [ -r "/proc/$PID/cmdline" ]; then
        case "$(tr '\000' ' ' < "/proc/$PID/cmdline" 2>/dev/null)" in
            *simpleadmin-httpd*) kill "$PID" >/dev/null 2>&1 || true ;;
        esac
    fi
    rm -f "$PID_FILE"
fi
killall simpleadmin-httpd >/dev/null 2>&1 || true
sleep 1
if pidof simpleadmin-httpd >/dev/null 2>&1 || ps 2>/dev/null | grep '[s]impleadmin-httpd' >/dev/null 2>&1; then
    killall -9 simpleadmin-httpd >/dev/null 2>&1 || true
    sleep 1
fi
ps 2>/dev/null | awk '
    $0 ~ "cat /dev/ttyIN" { print $1 }
    $0 ~ "cat /dev/smd11 > /dev/ttyIN" { print $1 }
    $0 ~ "socat" && ($0 ~ "link=/dev/ttyIN" || $0 ~ "link=/dev/ttyOUT") { print $1 }
' | while read pid; do
    case "$pid" in
        ""|*[!0-9]*) continue ;;
    esac
    kill "$pid" >/dev/null 2>&1 || true
    sleep 1
    kill -9 "$pid" >/dev/null 2>&1 || true
done
rm -f /dev/ttyIN /dev/ttyOUT 2>/dev/null || true
exit 0
EOF

    chmod +x "$FALLBACK_START_SCRIPT" "$FALLBACK_STOP_SCRIPT" "$PORT_PREPARE_SCRIPT"
}

install_systemd_unit() {
    SYSTEMD_DIR="$(find_systemd_dir || true)"
    [ -n "$SYSTEMD_DIR" ] || return 1
    WANTS_DIR="$SYSTEMD_DIR/multi-user.target.wants"

    mkdir -p "$WANTS_DIR" 2>/dev/null || return 1
    remove_stale_etc_unit
    cp -f "$SIMPLEADMIN_DIR/systemd/$SYSTEMD_UNIT" "$SYSTEMD_DIR/$SYSTEMD_UNIT" || return 1
    link_unit "$SYSTEMD_UNIT" || return 1
    SERVICE_UNIT_INSTALLED="1"
    log "systemd 服务已安装: $SYSTEMD_DIR/$SYSTEMD_UNIT"
    return 0
}

start_fallback_service() {
    log "正在使用 /usrdata 后台模式启动 SimpleAdmin Go"
    "$FALLBACK_STOP_SCRIPT" >/dev/null 2>&1 || true
    if "$FALLBACK_START_SCRIPT" >/tmp/simpleadmin-fallback-start.out 2>&1; then
        log "后台进程启动完成"
    else
        if grep -Eq "80 端口已被占用|80 端口仍被占用|port 80 is already in use" /tmp/simpleadmin-fallback-start.out 2>/dev/null; then
            warn "80 端口已被占用，SimpleAdmin Go 无法绑定 :80"
            cat /tmp/simpleadmin-fallback-start.out 2>/dev/null || true
            fail "请先停止占用 80 端口的旧 Web 服务后重新安装"
        fi
        warn "后台启动失败，详情请查看 /tmp/simpleadmin-fallback-start.out"
        fail "simpleadmin-httpd 后台启动失败"
    fi
}

# probe_local_http 本机 HTTP 探活(带重试):探测公开端点 /api/module_model
# (免认证、纯常量应答、不产生 AT 流量),既证明端口在听、又证明应答者是
# simpleadmin-httpd 而不是复活的厂商 Web 服务。启动路径包含 iptables 重放
# 等前置动作,监听建立需要数秒,故重试 6 次 × 1s。无 wget/curl 时降级为
# 信任 systemd 状态(返回 0)。
probe_local_http() {
    _i=0
    while [ "$_i" -lt 6 ]; do
        if command -v wget >/dev/null 2>&1; then
            wget -q -O /dev/null -T 2 "http://127.0.0.1/api/module_model" 2>/dev/null && return 0
        elif command -v curl >/dev/null 2>&1; then
            curl -sf -m 2 -o /dev/null "http://127.0.0.1/api/module_model" 2>/dev/null && return 0
        else
            return 0
        fi
        _i=$((_i + 1))
        sleep 1
    done
    return 1
}

restart_services() {
    if [ "$SERVICE_UNIT_INSTALLED" = "1" ] && command -v systemctl >/dev/null 2>&1; then
        log "正在重载 systemd 并启动 SimpleAdmin Go 服务（AT 调试关闭）"
        systemctl daemon-reload >/dev/null 2>&1 || true
        systemctl enable "$SYSTEMD_UNIT" >/dev/null 2>&1 || true
        systemctl restart "$SYSTEMD_UNIT" >/dev/null 2>&1 || warn "服务启动失败或当前设备不支持: $SYSTEMD_UNIT"
        # is-active 对 activating(auto-restart)同样返回 0:崩溃循环中的服务
        # 会被误报"安装成功"。要求 SubState=running 且本机 HTTP 探活通过。
        if systemctl is-active "$SYSTEMD_UNIT" >/dev/null 2>&1 \
           && systemctl show -p SubState "$SYSTEMD_UNIT" 2>/dev/null | grep -q '=running' \
           && probe_local_http; then
            echo "[成功] $SYSTEMD_UNIT 已启动"
            # systemd 接管成功后必须清掉历史 post_boot 兜底块:两者共存时
            # 每次开机 post_boot 拉起的 nohup 实例会与单元竞争 80 端口,
            # 被 ExecStartPre 反复击杀,PID 文件也会被写成 systemd 进程。
            remove_post_boot_autostart
            return 0
        fi
        warn "$SYSTEMD_UNIT 未达到 running/探活未通过，改用 post_boot 自启动"
    fi

    if [ "$SERVICE_UNIT_INSTALLED" != "1" ]; then
        warn "systemd 服务未安装到 /lib/systemd/system，改用 post_boot 自启动"
    fi
    systemctl stop "$SYSTEMD_UNIT" >/dev/null 2>&1 || true
    systemctl disable "$SYSTEMD_UNIT" >/dev/null 2>&1 || true
    remove_systemd_unit_files
    systemctl daemon-reload >/dev/null 2>&1 || true
    install_post_boot_autostart || warn "post_boot 自启动未安装；如果 systemd 自启动不可用，重启后需要手动启动"
    start_fallback_service
    log "正在检查服务启动状态:"
    if ps | grep '[s]impleadmin-httpd' >/dev/null 2>&1; then
        echo "[成功] simpleadmin-httpd 进程已运行"
    else
        fail "simpleadmin-httpd 启动失败"
    fi
}

reset_install_runtime_markers() {
    rm -f "$REBOOT_MARKER_FILE" "$INSTALL_RESULT_FILE" 2>/dev/null || true
}

write_reboot_marker_if_mobileap_cfg_touched() {
    rm -f "$REBOOT_MARKER_FILE" "$INSTALL_RESULT_FILE" 2>/dev/null || true
    if [ "${MOBILEAP_CFG_TOUCHED:-0}" != "1" ]; then
        echo "REBOOT_REQUIRED=0" > "$INSTALL_RESULT_FILE" 2>/dev/null || true
        return 0
    fi

    log "QCMAP mobileap 配置已处理，安装完成后需要重启"
    echo "mobileap_cfg" > "$REBOOT_MARKER_FILE" 2>/dev/null || warn "写入重启标记失败: $REBOOT_MARKER_FILE"
    {
        echo "REBOOT_REQUIRED=1"
        echo "REBOOT_REASON=mobileap_cfg"
    } > "$INSTALL_RESULT_FILE" 2>/dev/null || warn "写入安装结果失败: $INSTALL_RESULT_FILE"
    sync
}

main() {
    [ -d "$SIMPLEADMIN_SRC" ] || fail "安装包不完整: $SIMPLEADMIN_SRC"
    log "开始安装 SimpleAdmin Go 和 Go 原生 SMD AT 服务"
    # 任何退出路径(含 fail)都恢复根文件系统只读:旧实现只在成功路径
    # remount_ro,失败退出后 / 保持 rw,厂商只读假设被破坏且无人恢复。
    trap remount_ro EXIT
    reset_install_runtime_markers
    remount_rw
    stop_existing_simpleadmin_runtime
    cleanup_legacy_frontend_sources
    install_simpleadmin_files
    install_at_device_config
    install_ttl_state
    restart_services
    # QCMAP 配置在服务启动成功之后才修改:旧顺序先改再启动,启动失败 fail
    # 退出时 deploy 回滚只恢复二进制/www,QCMAP 已带着新 MAC/EarlyEthMode
    # 留在设备上无人回滚,且 REBOOT_REQUIRED 提示被 FAIL 结果覆盖丢失。
    maybe_install_bridge0_mac_config
    remount_ro
    write_reboot_marker_if_mobileap_cfg_touched
    write_install_success_result
    log "安装完成。默认登录账号: admin / admin"
}

main "$@"
