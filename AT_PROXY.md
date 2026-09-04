# SimpleAdmin AT 代理(at-client)

> 摘要:SSH 调用 at-client 经 Unix Socket 代理执行 AT,免停服、不抢占串口。

## 概述

- SimpleAdmin 主进程 `simpleadmin-httpd`(systemd 单元 `simpleadmin-httpd.service`,设备 `192.168.5.1` 上以 root 运行)独占唯一 AT 通道 `/dev/smd11`(子进程 `__smd-reader`)。
- 主进程启动时同时在 Unix Socket 上提供 **AT 代理**(受限 AT 执行能力):
  - 缺省路径 `/usrdata/simpleadmin/at_proxy.sock`,可用启动参数 `-at-proxy-sock` 覆盖。
  - 配套客户端子命令 `simpleadmin-httpd at-client` 只连接该 Socket,不直接访问串口。
- 代理与界面/缓存共享同一把全局锁,**串行执行**:外部程序无需停止服务、不会与主进程抢占通道。
- 支持**交互事务**:`AT+CMGS`(发短信)/`AT+CMGW` 等"提示符 + 报文"命令通过 `--payload` 单请求完成(见"交互事务"节)。
- ⚠️ **不要直接读写 `/dev/smd11`**:会被主进程 `__smd-reader` 抢占,裸读写与服务互相干扰。
- 设备端二进制路径:`/usrdata/simpleadmin/simpleadmin-httpd`。
- 服务以 `-mock` 运行时代理同样可用(返回预制响应,供本地预览)。

## 前置条件

- 设备 `192.168.5.1` 可达,仅密钥 SSH 登录(`root`,密码已禁)。
- 服务运行中:以 `systemctl status simpleadmin-httpd` 确认。
  - **服务未运行时 at-client 退出码 2**(代理 Socket 不存在);遇到退出码 2 先确认 `systemctl status simpleadmin-httpd`。
- Socket 缺省路径 `/usrdata/simpleadmin/at_proxy.sock` 存在(主进程启动时创建;SIGTERM 自动清理,异常退出残留的 socket 文件在下次启动时探活后自动清理)。

## 用法

```bash
simpleadmin-httpd at-client [--sock <socket 路径>] [--timeout-ms <毫秒>] [--payload <报文体>] <AT 命令...>
```

- 命令体 = 位置参数拼接后去首尾空白。
- 支持组合命令(分号分隔),如 `AT+QMAP="WWAN";+QENG="servingcell"`。
- `--payload` 非空时按**交互事务**执行(见下文"交互事务"节)。
- 通常经 SSH 远程调用(设备端二进制路径 `/usrdata/simpleadmin/simpleadmin-httpd`):

```bash
ssh root@192.168.5.1 "/usrdata/simpleadmin/simpleadmin-httpd at-client ATI"
```

## 参数

| 参数 | 缺省 | 说明 |
|---|---|---|
| `--sock <路径>` | `/usrdata/simpleadmin/at_proxy.sock` | AT 代理 Unix Socket 路径,需与主进程 `-at-proxy-sock` 一致 |
| `--timeout-ms <毫秒>` | `0` | AT 执行超时。`0` = 服务端按命令类型自动:普通命令约 1 秒,扫网类 120 秒,MPDN_RULE 查询 10 秒;上限 `180000`(超出截断为上限;负数按请求错误处理,退出码 2)。**交互事务模式下忽略**(事务内含固定预算) |
| `--payload <报文体>` | 空 | 交互事务报文体:服务端等模块 `> ` 提示符出现后发送,并自动在末尾补 Ctrl-Z(`0x1A`)。用于 `AT+CMGS`(发短信)、`AT+CMGW`(存短信)等"提示符 + 报文"形态的命令 |
| `<AT 命令...>` | 必填 | 位置参数拼接为命令体;命令为空按用法错误处理,退出码 2 |

## 退出码

| 退出码 | 含义 | 说明 |
|---|---|---|
| `0` | 成功 | 原始 transcript 原样输出到 stdout |
| `1` | 业务/AT 执行失败 | 代理报 exec 错,或 transcript 非 OK 收尾(如 `ERROR`、`+CME ERROR`);此时 transcript 仍完整输出到 stdout,stderr 附一行提示 |
| `2` | 代理不可连接 / 格式错误 / 用法错误 | 服务未运行、socket 不存在、请求格式错误、参数用法错误等;错误信息在 stderr |

## 输出约定

- 退出码 `0`/`1`:原始 transcript **原样**写入 stdout,不增删字符;transcript 内部行分隔为 CRLF(`\r\n`)。
- 退出码 `1`:transcript(如有)仍完整输出到 stdout;stderr 仅追加一行失败提示。
- 退出码 `2`:无 transcript;错误信息只写 stderr。
- 调用方自行解析,以 transcript 收尾行(`OK` / `ERROR` / `+CME ERROR: ...`)判断成败。
- ⚠️ 勿绕过代理直接读写 `/dev/smd11`(被主进程 `__smd-reader` 抢占)。

## 示例

### 1. ATI(成功,退出码 0)

```bash
ssh root@192.168.5.1 "/usrdata/simpleadmin/simpleadmin-httpd at-client ATI"
```

stdout 预期形态(transcript 为 CRLF 分隔,型号/版本为占位符):

```text
ATI
Quectel
<型号>
Revision: <版本>
OK
```

退出码 `0`。

### 2. 含引号的查询(SSH 双层转义)

```bash
ssh root@192.168.5.1 "/usrdata/simpleadmin/simpleadmin-httpd at-client 'AT+QMAP=\"WWAN\"'"
```

stdout 预期形态:

```text
AT+QMAP="WWAN"
+QMAP: "WWAN",<...>
OK
```

### 3. 自定义超时

```bash
ssh root@192.168.5.1 "/usrdata/simpleadmin/simpleadmin-httpd at-client --timeout-ms 10000 AT+CSQ"
```

### 4. AT 执行失败(退出码 1)

命令被模组拒绝时,transcript 仍完整输出到 stdout:

```text
<命令回显>
ERROR
```

(或 `+CME ERROR: <代码>` 收尾);stderr 附一行提示;退出码 `1`。

### 5. 服务未运行(退出码 2)

```bash
ssh root@192.168.5.1 "/usrdata/simpleadmin/simpleadmin-httpd at-client ATI"
```

stderr 输出 `连接 AT 代理失败: <socket 连接错误>`;退出码 `2` → 先确认 `systemctl status simpleadmin-httpd`。

## 交互事务(AT+CMGS 发短信等)

`AT+CMGS`/`AT+CMGW` 是两阶段命令:发命令 → 模块回 `> ` 提示符 → 发送报文体 + Ctrl-Z(`0x1A`)→ 返回 `+CMGS: <mr>` + OK。代理把该时序封装在**单个请求**内:请求携带 `payload` 字段(客户端 `--payload`),服务端复用短信页生产验证过的事务执行器(全局锁 → 等待提示符 10 秒 → 写报文体 + Ctrl-Z → 等待终结 60 秒;失败自动发 ESC 让模块退出输入态)。调用方无需感知提示符。

规则:

- `payload` 非空才启用交互事务;为空按普通命令执行。
- 报文体末尾的 Ctrl-Z 由服务端**自动补**,调用方不要自带。
- `payload` ≤ `4096` 字节;不允许包含 ESC(`0x1B`,会令模块提前退出输入态),违反按请求错误处理(退出码 2)。
- `payload` 可含换行(JSON 以 `\n` 转义,不影响单行分帧);其余内容原样发送。
- 事务超时固定:等提示符 10 秒、等终结 60 秒;`--timeout-ms` 在该模式下被忽略。
- 与全局锁串行:事务期间其他 AT 请求(界面/缓存/代理)排队等待。

### 示例 1:文本模式发短信(需先切文本模式)

```bash
ssh root@192.168.5.1 "/usrdata/simpleadmin/simpleadmin-httpd at-client --payload '测试短信内容' 'AT+CMGF=1;+CMGS=\"+8613800138000\"'"
```

stdout 预期形态:

```text
> 
+CMGS: <消息引用号>
OK
```

退出码 `0`;若以 `+CMS ERROR: <代码>` 收尾则退出码 `1`(transcript 仍完整输出)。

### 示例 2:PDU 模式(报文体为 TPDU 十六进制串)

命令用 `AT+CMGF=0;+CMGS=<N>`,`<N>` = TPDU 八位组数(不含 SMSC 字段);payload 传 PDU 十六进制字符串。PDU 构造与长短信分段较复杂,设备 Web 短信页已内置该能力,外部程序建议优先使用上面的文本模式:

```bash
ssh root@192.168.5.1 "/usrdata/simpleadmin/simpleadmin-httpd at-client --payload '<TPDU十六进制串>' 'AT+CMGF=0;+CMGS=<N>'"
```

## 错误处理

| 现象 | 退出码 | 原因 | 处置 |
|---|---|---|---|
| stderr `连接 AT 代理失败: ...` | 2 | 服务未运行,或 socket 路径不符 | 确认 `systemctl status simpleadmin-httpd`;核对主进程 `-at-proxy-sock` 与客户端 `--sock` 一致 |
| stderr `请求错误: ...` | 2 | 协议错误:命令为空、命令超 4096 字节、`timeout_ms` 为负、请求 JSON 非法、`payload` 超 4096 字节、`payload` 含 ESC | 修正请求参数 |
| stderr `AT 执行失败: ...` | 1 | 代理报 exec 错(含交互事务等提示符超时、写报文体失败) | 重试或检查模组状态 |
| transcript 以 `ERROR`/`+CME ERROR` 收尾(已原样输出到 stdout) | 1 | 模组拒绝该命令 | 检查命令语法、SIM 与注册状态 |
| 交互事务 transcript 以 `+CMS ERROR: <代码>` 收尾 | 1 | 短信发送被网络/模块拒绝 | 按 3GPP 27.005 附录查代码含义(如 500=未知错误、304=无效 PDU 参数) |

## 限制

- 单条命令 ≤ `4096` 字节;`payload` ≤ `4096` 字节;单请求 ≤ `64KB`。
- 命令中的 CR/LF 会被剥离;`payload` 原样保留(换行经 JSON 转义)。
- `payload` 不允许包含 ESC(`0x1B`);末尾 Ctrl-Z(`0x1A`)由服务端自动补。
- 交互事务超时固定(提示符 10 秒、终结 60 秒),`--timeout-ms` 在该模式下忽略。
- socket 权限 `0666`:本地任意用户可连,信任边界 = 本机。
- 串行执行:与界面/缓存共享同一把全局锁,无并发加速;交互事务期间其他请求排队。
- socket 生命周期:主进程收到 SIGTERM 自动清理;异常退出残留的 socket 文件在下次启动时探活后自动清理。
- `-mock` 模式返回预制响应,仅供本地预览(交互事务返回固定 `> ` + `+CMGS: 1` + OK)。

## 协议细节(自行实现客户端参考)

- 传输:Unix 流式 Socket,缺省 `/usrdata/simpleadmin/at_proxy.sock`。
- 交互模型:单连接单请求;服务端写回响应后主动关闭连接。
- 分帧:行分隔 JSON;请求为一行 JSON 以 `\n` 结尾,响应同样为一行 JSON。
- 服务端读超时 10 秒(只连接不发数据会被断开);单请求按 64KB 截断。

请求(普通命令):

```json
{"command":"ATI","timeout_ms":0}
```

请求(交互事务,如发短信;`payload` 非空即启用,末尾 Ctrl-Z 由服务端自动补):

```json
{"command":"AT+CMGF=1;+CMGS=\"+8613800138000\"","payload":"短信内容","timeout_ms":0}
```

响应(成功;`response` 为原始 transcript,内含 `\r\n`):

```json
{"ok":true,"response":"ATI\r\nQuectel\r\n<型号>\r\nRevision: <版本>\r\nOK\r\n"}
```

响应(失败):

```json
{"ok":false,"error":"<错误信息>","code":"protocol"}
```

| `code` | 含义 |
|---|---|
| `protocol` | 请求格式/连接层错误:JSON 非法、空行、命令为空、命令超 4096 字节、`timeout_ms` 为负、请求超 64KB |
| `exec` | AT 执行失败 |

`timeout_ms` 语义:`0` = 服务端按命令类型自动;负数 = protocol 错误;大于 `180000` 截断为 `180000`。
`payload` 语义:可选;非空 = 交互事务(等 `> ` 提示符后发送,服务端自动补 Ctrl-Z);≤4096 字节;含 ESC 为 protocol 错误。

客户端步骤:

1. 连接 socket(建议连接超时 ≤5 秒;连接失败即服务未运行,对应退出码 2)。
2. 写入一行请求 JSON + `\n`(payload 含换行时由 JSON 转义,保持单行)。
3. 读到 `\n` 为止并解析 JSON(响应读取上限建议 1MB;交互事务最长约 70 秒,读取超时建议 ≥90 秒)。
4. `ok=true`:`response` 即原始 transcript,调用方自行解析收尾(OK/ERROR)。
5. `ok=false`:按 `code` 分类处理。

最小 Python 参考实现:

```python
import json, socket

SOCK = "/usrdata/simpleadmin/at_proxy.sock"

def at_exec(command: str, timeout_ms: int = 0, payload: str = "") -> dict:
    s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    s.settimeout(400)  # 覆盖服务端最坏情况:锁等待 200 秒 + 执行 180 秒
    s.connect(SOCK)
    req = {"command": command, "timeout_ms": timeout_ms}
    if payload:
        req["payload"] = payload  # 交互事务:等 "> " 提示符后发送,Ctrl-Z 服务端自动补
    s.sendall((json.dumps(req, ensure_ascii=False) + "\n").encode())
    buf = b""
    while not buf.endswith(b"\n"):
        chunk = s.recv(65536)
        if not chunk:
            break
        buf += chunk
    s.close()
    return json.loads(buf)  # {"ok":true,"response":...} 或 {"ok":false,"error":...,"code":...}
```
