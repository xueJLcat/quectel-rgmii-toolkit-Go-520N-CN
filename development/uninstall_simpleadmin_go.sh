#!/bin/bash

set -u

SYSTEMD_DIRS="/etc/systemd/system /lib/systemd/system"
SIMPLEADMIN_DIR="/usrdata/simpleadmin"
ROOT_BIN="/usrdata/root/bin"
POST_BOOT_FILE="/etc/init.post_boot.sh"
POST_BOOT_BEGIN="# BEGIN SIMPLEADMIN GO AUTOSTART"
POST_BOOT_END="# END SIMPLEADMIN GO AUTOSTART"

log() {
    echo "[信息] $*"
}

warn() {
    echo "[警告] $*"
}

remount_rw() {
    mount -o remount,rw / >/dev/null 2>&1 || true
}

remount_ro() {
    mount -o remount,ro / >/dev/null 2>&1 || true
}

stop_service() {
    systemctl stop "$1" >/dev/null 2>&1 || true
    systemctl disable "$1" >/dev/null 2>&1 || true
}

stop_fallback_process() {
    if [ -x "$SIMPLEADMIN_DIR/stop_simpleadmin.sh" ]; then
        "$SIMPLEADMIN_DIR/stop_simpleadmin.sh" >/dev/null 2>&1 || true
    fi
    if [ -f "$SIMPLEADMIN_DIR/simpleadmin-httpd.pid" ]; then
        local pid=""
        pid="$(cat "$SIMPLEADMIN_DIR/simpleadmin-httpd.pid" 2>/dev/null || true)"
        [ -n "$pid" ] && kill "$pid" >/dev/null 2>&1 || true
        rm -f "$SIMPLEADMIN_DIR/simpleadmin-httpd.pid"
    fi
    killall simpleadmin-httpd >/dev/null 2>&1 || true
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

remove_post_boot_autostart() {
    [ -f "$POST_BOOT_FILE" ] || return 0
    sed -i "/$POST_BOOT_BEGIN/,/$POST_BOOT_END/d" "$POST_BOOT_FILE" 2>/dev/null || true
}

remove_unit() {
    local unit="$1"
    local dir=""

    stop_service "$unit"
    for dir in $SYSTEMD_DIRS; do
        rm -f "$dir/$unit" "$dir/multi-user.target.wants/$unit" 2>/dev/null || true
    done
}

clear_go_ttl_rules() {
    if [ -x "$SIMPLEADMIN_DIR/simpleadmin-httpd" ]; then
        "$SIMPLEADMIN_DIR/simpleadmin-httpd" ttl off >/dev/null 2>&1 || warn "清理 Go 原生 TTL 规则失败"
    fi
}

clear_dnsmasq_customization() {
    # 依赖 AT 通道,必须在服务停止之后、二进制删除之前执行。
    if [ -x "$SIMPLEADMIN_DIR/simpleadmin-httpd" ]; then
        "$SIMPLEADMIN_DIR/simpleadmin-httpd" dnsmasq-cleanup >/dev/null 2>&1 || warn "清理 dnsmasq 自定义配置失败(可在安装前从界面删除静态绑定/恢复运营商 DNS)"
    fi
    rm -f "$SIMPLEADMIN_DIR/dns_upstream.conf" "$SIMPLEADMIN_DIR/mac_bind.conf"
    rm -rf "$SIMPLEADMIN_DIR/dnsmasq.d" "$SIMPLEADMIN_DIR/backup"
}

remove_simpleadmin_go_files() {
    log "正在卸载 SimpleAdmin Go 服务和当前版本文件"
    clear_go_ttl_rules
    stop_fallback_process
    remove_post_boot_autostart

    remove_unit simpleadmin-httpd.service
    clear_dnsmasq_customization
    rm -f "$ROOT_BIN/simplepasswd"
    rm -f "$SIMPLEADMIN_DIR/simpleadmin-httpd"
    rm -f "$SIMPLEADMIN_DIR/simpleadmin.auth"
    rm -f "$SIMPLEADMIN_DIR/server.crt"
    rm -f "$SIMPLEADMIN_DIR/server.key"
    rm -f "$SIMPLEADMIN_DIR/zbims-ca.crt"
    rm -f "$SIMPLEADMIN_DIR/zbims-ca.key"
    rm -f "$SIMPLEADMIN_DIR/at_devices.conf"
    rm -f "$SIMPLEADMIN_DIR/ttlvalue"
    rm -f "$SIMPLEADMIN_DIR/bridge0_mac"
    rm -f "$SIMPLEADMIN_DIR/mobileap_bridge0_mac.sh"
    rm -f "$SIMPLEADMIN_DIR/socat-armel-static"
    rm -f "$SIMPLEADMIN_DIR/simpleadmin-httpd.pid"
    rm -f "$SIMPLEADMIN_DIR/start_simpleadmin.sh"
    rm -f "$SIMPLEADMIN_DIR/stop_simpleadmin.sh"
    # 各功能页状态文件(约定:新增状态文件必须随卸载清理,否则重装时旧设置静默复活):
    # 防火墙规则、看门狗、定时重启、时间同步、短信转发
    rm -f "$SIMPLEADMIN_DIR/firewall_ports.conf"
    rm -f "$SIMPLEADMIN_DIR/watchdog.conf"
    rm -f "$SIMPLEADMIN_DIR/scheduler.conf"
    rm -f "$SIMPLEADMIN_DIR/scheduler.state"
    rm -f "$SIMPLEADMIN_DIR/timesync.conf"
    rm -f "$SIMPLEADMIN_DIR/sms_webhook.conf"
    # 安装脚本生成的端口准备脚本与部署备份;异常退出残留的 AT 代理 Socket
    rm -f "$SIMPLEADMIN_DIR/prepare_simpleadmin_ports.sh"
    rm -f "$SIMPLEADMIN_DIR/at_proxy.sock"
    rm -rf "$SIMPLEADMIN_DIR/.deploy-backup"
    rm -rf "$SIMPLEADMIN_DIR/www"
    rm -rf "$SIMPLEADMIN_DIR/systemd"
    rm -rf "$SIMPLEADMIN_DIR/frontend"
    rm -rf "$SIMPLEADMIN_DIR/node_modules"

    rmdir "$SIMPLEADMIN_DIR" >/dev/null 2>&1 || true
}

reload_systemd() {
    log "正在重载 systemd 状态"
    systemctl daemon-reload >/dev/null 2>&1 || true
    systemctl reset-failed >/dev/null 2>&1 || true
}

main() {
    log "开始卸载 SimpleAdmin Go 二进制版本"
    remount_rw
    remove_simpleadmin_go_files
    reload_systemd
    remount_ro
    log "卸载完成"
}

main "$@"
