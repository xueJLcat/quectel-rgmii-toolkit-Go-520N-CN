(function (global) {
  'use strict';

  const root = global.SimpleAdmin || {};

  root.Lang = root.Lang || (function () {
    const storageKey = 'simpleadmin.language';
    const defaultLanguage = 'zh-CN';
    const supportedLanguages = ['zh-CN', 'en'];
    let currentLanguage = defaultLanguage;

    const translations = {
      en: {
        '首页': 'Home',
        '网络': 'Network',
        '设置': 'Settings',
        '短信': 'SMS',
        '控制台': 'Console',
        '设备信息': 'Device Info',
        '设备信息读取失败': 'Failed to load device info',
        '总览': 'Overview',
        '蜂窝网络': 'Cellular Network',
        '系统设置': 'System Settings',
        '短信服务': 'SMS Service',
        '退出登录': 'Log Out',
        '菜单': 'Menu',
        '暗夜模式': 'Night Mode',
        '浅色模式': 'Light Mode',
        '温度': 'Temperature',
        'SIM 卡': 'SIM Card',
        '信号百分比': 'Signal Percentage',
        '互联网连接': 'Internet Connection',
        'CPU 使用率': 'CPU Usage',
        'RAM 使用量': 'RAM Usage',
        '实时负载': 'Live Load',
        '网络信息': 'Network Info',
        '精简显示': 'Compact View',
        '完整显示': 'Full View',
        '激活 SIM': 'Active SIM',
        '运营商': 'Carrier',
        '网络模式': 'Network Mode',
        '频段': 'Bands',
        '带宽': 'Bandwidth',
        '在线时长': 'Uptime',
        '刷新频率（最少 2 秒）': 'Refresh rate (minimum 2 seconds)',
        '信号信息': 'Signal Info',
        '信号评估': 'Signal Assessment',
        '累计流量': 'Total Traffic',
        '速率': 'Speed',
        '更新时间': 'Last Updated',
        '信号与流量趋势': 'Signal & Traffic Trend',
        '暂无历史数据，页面保持打开后将自动积累': 'No history yet. Keep this page open to accumulate data.',
        '信号强度': 'Signal',
        '下载速率': 'Download Rate',
        '上传速率': 'Upload Rate',
        '时间范围': 'Time Range',
        '刷新': 'Refresh',
        '制造商': 'Manufacturer',
        '型号名称': 'Model Name',
        '固件版本': 'Firmware Version',
        '电话号码': 'Phone Number',
        '局域网IP': 'LAN IP',
        '广域网IPv': 'WAN IPv',
        '版本': 'Version',
        '更新': 'Update',
        '访问': 'Visit',
        '代码库': 'repository',
        '或': 'or',
        '文档': 'documentation',
        '以获取更多信息。版权所有。': 'for more information. Copyright.',
        '重启': 'Reboot',
        '取消': 'Cancel',
        '正在禁用 IP 透传，网口会重启，请等待倒计时结束。': 'Disabling IP passthrough. The network port will restart. Please wait for the countdown to finish.',
        '加载中...': 'Loading...',
        'AT 终端': 'AT Terminal',
        'AT 命令': 'AT Commands',
        'AT 命令输出': 'AT Command Output',
        '用分号（;）分隔多个命令，模块会按组合命令一次性处理，示例：AT+CFUN?;+CCID': 'Separate multiple commands with semicolons (;). The module processes them as one compound command. Example: AT+CFUN?;+CCID',
        '提交': 'Submit',
        '清除': 'Clear',
        '重置 AT&F': 'Reset AT&F',
        '重置': 'Reset',
        'IP 透传': 'IP Passthrough',
        '当前：已启用': 'Current: Enabled',
        '当前：未启用': 'Current: Disabled',
        '当前：': 'Current:',
        '未指定': 'Unspecified',
        'USB 协议': 'USB Protocol',
        'ECM (推荐)': 'ECM (Recommended)',
        '更改': 'Change',
        'DMZ 设置': 'DMZ Settings',
        '启用': 'Enable',
        '禁用': 'Disable',
        'LAN IP 设置': 'LAN IP Settings',
        '保存': 'Save',
        '其他设置': 'Other Settings',
        'TTL 设置': 'TTL Settings',
        'TTL 已激活': 'TTL Enabled',
        'TTL 未激活': 'TTL Disabled',
        '设置 TTL 值为 0 以禁用。': 'Set TTL value to 0 to disable.',
        '界面语言': 'Interface Language',
        '默认主题': 'Default Theme',
        '顶部的切换按钮只影响当前浏览器；此默认值在新浏览器访问或清除缓存后生效。': 'The toggle button at the top only affects the current browser; this default applies to new browsers or after clearing the cache.',
        '中文': 'Chinese',
        '已保存': 'Saved',
        '保存失败': 'Save failed',
        '语言已保存。': 'Language saved.',
        '登录密码': 'Login Password',
        '自动化': 'Automation',
        '断网自愈看门狗': 'Offline Self-Healing Watchdog',
        '连续失败次数': 'Failures',
        '冷却分钟': 'Cooldown (min)',
        '每日定时重启': 'Daily Scheduled Reboot',
        '重启时刻': 'Reboot Time',
        '启用看门狗': 'Enable Watchdog',
        '启用定时重启': 'Enable Scheduled Reboot',
        '检测间隔（分钟）': 'Check Interval (minutes)',
        '自愈动作': 'Recovery Action',
        '直接重启模块': 'Reboot Module Directly',
        '先重注册网络，仍失败再重启': 'Re-register network first, reboot if still offline',
        '探测目标': 'Probe Targets',
        '每行一个，格式 地址 或 地址:端口，留空使用内置目标': 'One per line, host or host:port; leave empty to use built-in targets',
        '留空时使用内置公共 DNS 探测（223.5.5.5 / 1.1.1.1 / 8.8.8.8，端口 53）；自定义目标最多 4 个，非法条目会被忽略。': 'When empty, built-in public DNS is used (223.5.5.5 / 1.1.1.1 / 8.8.8.8, port 53). Up to 4 custom targets; invalid entries are ignored.',
        '保存看门狗设置': 'Save Watchdog Settings',
        '保存定时重启': 'Save Scheduled Reboot',
        '轮询中': 'Polling',
        '连续失败': 'Consecutive Failures',
        '上次自愈': 'Last Recovery',
        '从未': 'Never',
        '无线电重注册': 'Radio Re-registration',
        '重启模块': 'Module Reboot',
        '检测间隔必须是 1-30 的整数': 'Check interval must be an integer between 1 and 30',
        '看门狗配置加载失败': 'Failed to load watchdog settings',
        '定时重启配置加载失败': 'Failed to load scheduled reboot settings',
        '启用后按检测间隔探测联网，连续失败达到阈值且过了冷却时间时执行自愈动作；禁用后轮询器停止。': 'When enabled, connectivity is probed at the check interval; the recovery action runs after consecutive failures reach the threshold and the cooldown has passed. Disabling stops the poller.',
        '时间同步': 'Time Sync',
        '启用时间同步': 'Enable Time Sync',
        '同步间隔（分钟）': 'Sync Interval (minutes)',
        'NTP 服务器': 'NTP Server',
        '启用后按同步间隔轮询 NTP 服务器（单一源）并直接校正系统时间；禁用后轮询器停止。': 'When enabled, the NTP server (single source) is polled at the sync interval and the system clock is corrected directly. Disabling stops the poller.',
        '缺省使用阿里云 NTP 服务器 ntp.aliyun.com。': 'Defaults to the Aliyun NTP server ntp.aliyun.com.',
        '上次同步': 'Last Sync',
        '系统时间': 'System Time',
        '时间同步配置加载失败': 'Failed to load time sync settings',
        '保存时间同步': 'Save Time Sync',
        '手动同步一次': 'Sync Now',
        '同步中…': 'Syncing…',
        '同步间隔必须是 1-1440 的整数': 'Sync interval must be an integer between 1 and 1440',
        'NTP 服务器格式无效': 'Invalid NTP server format',
        '时间同步成功': 'Time synced',
        '时间同步失败': 'Time sync failed',
        '失败': 'Failed',
        '偏差': 'offset',
        '系统监控': 'System Monitor',
        '内存': 'Memory',
        '负载': 'Load Average',
        '运行时长': 'Uptime',
        '进程数': 'Processes',
        '进程 Top 20': 'Process Top 20',
        '排序': 'Sort By',
        '进程排序方式': 'Process sort order',
        '按 CPU': 'By CPU',
        '按内存': 'By Memory',
        '用户': 'User',
        '进程名': 'Process',
        '内存%': 'Mem%',
        '暂无进程数据': 'No process data',
        '系统监控数据加载失败': 'Failed to load system monitor data',
        '新短信转发到 Webhook': 'Forward New SMS to Webhook',
        'https:// 开头的回调地址': 'Callback URL starting with https://',
        '当前密码': 'Current Password',
        '新密码': 'New Password',
        '确认新密码': 'Confirm New Password',
        '修改密码': 'Change Password',
        '更改登录密码': 'Change Login Password',
        '请输入当前密码和新密码': 'Enter the current password and new password',
        '两次输入的新密码不一致': 'The new passwords do not match',
        '密码已保存，请使用新密码重新登录。': 'Password saved. Please log in again with the new password.',
        '密码保存失败': 'Password save failed',
        '当前密码不正确': 'Current password is incorrect',
        '新密码不能为空': 'New password cannot be empty',
        '密码确认不一致': 'Password confirmation mismatch',
        '请输入当前登录密码': 'Enter current login password',
        '请输入新的登录密码': 'Enter new login password',
        '请再次输入新的登录密码': 'Enter new login password again',
        '网络工具': 'Network Tools',
        'IP 协议类型': 'IP Protocol Type',
        '保存更改': 'Save Changes',
        '小区锁定': 'Cell Lock',
        '选择扫描模式': 'Select scan mode',
        '当前：选择扫描模式': 'Current: Select scan mode',
        '全面扫描': 'Full Scan',
        '仅LTE': 'LTE Only',
        '仅NR5G': 'NR5G Only',
        '开始扫描': 'Start Scan',
        '扫描中... 请稍候.': 'Scanning... Please wait.',
        '锁定': 'Lock',
        '小区扫描需要几分钟完成，请勿离开此页面。': 'Cell scanning may take a few minutes. Do not leave this page.',
        '小区数量': 'Number of Cells',
        '锁定LTE小区': 'Lock LTE Cell',
        '锁定NR5G-SA小区': 'Lock NR5G-SA Cell',
        '解锁LTE小区': 'Unlock LTE Cell',
        '解锁NR5G-SA小区': 'Unlock NR5G-SA Cell',
        '初始化网络...': 'Initializing network...',
        '锁定频段': 'Band Lock',
        '频段锁定': 'Band Lock',
        '复选框将会在这里生成': 'Checkboxes will be generated here',
        '获取支持的频段中...': 'Getting supported bands...',
        '锁定当前选中': 'Lock Selected',
        '启用所有': 'Enable All',
        '小区扫描': 'Cell Scan',
        '小区扫描将扫描您所在区域的所有LTE和NR5G-SA小区。扫描可能会断开您的网络连接，并需要几分钟完成。': 'Cell scan will scan all LTE and NR5G-SA cells in your area. The scan may interrupt your network connection and may take a few minutes.',
        '选择': 'Select',
        '频点': 'ARFCN',
        '信号': 'Signal',
        '收件箱': 'Inbox',
        '读取短信...': 'Reading SMS...',
        '无短信': 'No SMS',
        '全选': 'Select All',
        '取消选中': 'Deselect',
        '发件人:': 'Sender:',
        '发件人': 'Sender',
        '内容': 'Content',
        '时间': 'Time',
        '日期和时间:': 'Date and Time:',
        '删除': 'Delete',
        '发信息': 'Send Message',
        '收件人号码': 'Recipient Number',
        '短信内容': 'Message Content',
        '发送短信': 'Send SMS',
        '正在注销...': 'Logging out...',
        '主导航': 'Main navigation',
        '切换导航': 'Toggle navigation',
        'DMZ IP 地址': 'DMZ IP Address',
        'TTL 值': 'TTL Value',
        '网关 IP 地址': 'Gateway IP Address',
        '起始地址': 'Start Address',
        '结束地址': 'End Address',
        '输入收件人号码': 'Enter recipient number',
        '输入短信内容': 'Enter message content',
        '优秀': 'Excellent',
        '良好': 'Good',
        '一般': 'Fair',
        '差': 'Poor',
        '无信号': 'No Signal',
        '已激活': 'Active',
        '未激活': 'Inactive',
        '已连接': 'Connected',
        '未连接': 'Disconnected',
        '测试模式': 'Test Mode',
        '未知时间': 'Unknown Time',
        '未插卡': 'No SIM',
        '没有提供新的 IMEI。': 'No new IMEI was provided.',
        'IMEI 无效': 'Invalid IMEI',
        '新的 IMEI 与当前 IMEI 相同。': 'The new IMEI is the same as the current IMEI.',
        'IMEI 与当前 IMEI 相同': 'The IMEI is the same as the current IMEI',
        '请输入新的 IMEI': 'Enter new IMEI',
        '获取IMEI中...': 'Getting IMEI...',
        '请输入所有必填字段': 'Please fill in all required fields',
        '请输入至少一个有效的earfcn和pci对': 'Enter at least one valid EARFCN and PCI pair',
        '请输入要锁定的小区数量': 'Enter the number of cells to lock',
        '请至少选择一个小区进行锁定.': 'Please select at least one cell to lock.',
        '最多只能选择 10 条小区进行锁定': 'You can select up to 10 cells to lock',
        '选择的网络模式无效': 'Invalid network mode selection',
        '没有选中任何频段，请选择至少一个频段！': 'No bands selected. Select at least one band.',
        '没有做出更改': 'No changes were made',
        '某些必填字段缺失，请检查小区数据': 'Some required fields are missing. Check the cell data.',
        '网关 IP 地址格式无效！': 'Invalid gateway IP address format.',
        '请输入有效的网关 IP 地址和起始、结束 IP 地址！': 'Enter a valid gateway IP address, start address, and end address.',
        '未指定 IP 透传模式': 'IP passthrough mode is not specified',
        '无效的 IP 透传模式': 'Invalid IP passthrough mode',
        '未指定 USB 网络模式': 'USB network mode is not specified',
        'USB 网络模式无效': 'Invalid USB network mode',
        '未检测到 SIM 卡': 'No SIM card detected',
        '短信发送成功！': 'SMS sent successfully.',
        '未知错误': 'Unknown error',
        '没有有效的短信索引': 'No valid SMS index',
        '短信索引未正确初始化或为空': 'SMS indexes were not initialized correctly or are empty',
        '自动': 'Auto',
        '选择首选网络': 'Select preferred network',
        'NR5G模式控制': 'NR5G Mode Control',
        '获取中...': 'Loading...',
        '已启用': 'Enabled',
        '未启用': 'Disabled',
        '已禁用': 'Disabled',
        '禁用NR5G-NSA': 'Disable NR5G-NSA',
        '禁用NR5G-SA': 'Disable NR5G-SA',
        '解锁LTE': 'Unlock LTE',
        '解锁NR5G-SA': 'Unlock NR5G-SA',
        '活动频段:': 'Active Bands:',
        '开始小区扫描': 'Start Cell Scan',
        '空': 'Empty',
        '数据': 'Data',
        '网络已断开': 'Network disconnected',
        '正在解析LTE数据': 'Parsing LTE data',
        '正在解析NR5G-SA数据': 'Parsing NR5G-SA data',
        'PCC PCI值:': 'PCC PCI:',
        'SCC PCI值:': 'SCC PCI:',
        '天': 'days',
        '小时': 'hours',
        '分钟': 'minutes',
        '未锁定': 'Unlocked',
        '已锁定4G': '4G Locked',
        '已锁定5G': '5G Locked',
        '已锁定4G和5G': '4G and 5G Locked',
        '未禁用': 'Not Disabled',
        '禁用NSA': 'NSA Disabled',
        '禁用SA': 'SA Disabled',
        '获取频段中...': 'Getting bands...',
        '全部选中': 'Select All',
        '中国移动': 'China Mobile',
        '中国联通': 'China Unicom',
        '中国电信': 'China Telecom',
        '中国广电': 'China Broadnet',
        '中国铁通': 'China Tietong',
        '中国卫通': 'China Satcom',
        '国家电网': 'State Grid',
        '号码或内容不能为空': 'Phone number or message cannot be empty',
        '短信发送失败：': 'SMS sending failed:',
        'AT命令执行结果:': 'AT command result:',
        '发送AT命令失败:': 'Failed to send AT command:',
        '错误:': 'Error:',
        '锁频/锁小区配置档': 'Band/Cell Locking Profiles',
        '档名': 'Profile Name',
        '输入档名': 'Enter profile name',
        '已有配置档': 'Saved Profiles',
        '选择配置档': 'Select a profile',
        '保存当前配置': 'Save Current Config',
        '应用': 'Apply',
        '请输入档名': 'Please enter a profile name',
        '已存在同名配置档': 'A profile with the same name already exists',
        '配置档已保存': 'Profile saved',
        '配置档保存失败': 'Failed to save profile',
        '请先选择配置档': 'Please select a profile first',
        '配置档已应用，仅填充界面，未自动提交': 'Profile applied. It only fills the form and is not submitted automatically',
        '确定删除该配置档？': 'Delete this profile?',
        '配置档已删除': 'Profile deleted',
        '提示：': 'Notice: ',
        '操作失败': 'Operation failed',
        '请输入有效的 IP 地址！': 'Please enter a valid IP address.',
        '连续失败次数必须是 1-60 的整数': 'Failures must be an integer between 1 and 60',
        '冷却分钟必须是 1-1440 的整数': 'Cooldown minutes must be an integer between 1 and 1440',
        '重启时刻格式无效，应为 HH:MM': 'Invalid reboot time format, expected HH:MM',
        '网络设置': 'Network Settings',
        '网络详情': 'Network Details',
        'WAN / LAN 地址': 'WAN / LAN Addresses',
        'LAN 网关': 'LAN Gateway',
        '接口状态': 'Interface Status',
        '接口': 'Interface',
        '累计上行': 'Total Upload',
        '累计下行': 'Total Download',
        '当前速率': 'Current Rate',
        '局域网设备': 'LAN Devices',
        '在线设备': 'Devices Online',
        '主机名': 'Hostname',
        '剩余租期': 'Lease Remaining',
        '暂无接口数据': 'No interface data',
        '暂无在线设备': 'No online devices',
        '网络详情加载失败': 'Failed to load network details',
        '不足1分钟': '<1 min',
        'AT命令': 'AT Commands',
        '当前状态': 'Current Status',
        '常用命令': 'Quick Commands',
        '命令历史': 'Command History',
        '暂无历史记录': 'No history yet',
        '复制': 'Copy',
        '已复制': 'Copied',
        '复制失败': 'Copy failed',
        '危险操作': 'Danger Zone',
        '确认重置': 'Confirm Reset',
        '设备操作': 'Device Actions',
        '重启设备': 'Reboot Device',
        '重启调制解调器,约需 40 秒恢复。': 'Reboots the modem. It takes about 40 seconds to recover.',
        '状态获取中...': 'Loading status...',
        '状态获取失败': 'Failed to load status',
        '网络连接失败，请检查网络后重试': 'Network connection failed. Please check your network and try again.',
        'DNS 代理': 'DNS Proxy',
        'DNS 代理设置已下发': 'DNS proxy setting submitted',
        'DNS V6 代理开关': 'DNS V6 proxy toggle',
        '系统托管': 'System managed',
        'DNS V4 由系统内部管理，始终生效，不支持查询或修改。': 'DNS V4 is managed internally by the system and is always active. It cannot be queried or modified.',
        'IMEI 设置': 'IMEI Settings',
        '当前 IMEI：': 'Current IMEI:',
        'LAN IP 段': 'LAN IP Range',
        '↑ / ↓ 键可翻阅历史命令': 'Use the ↑ / ↓ keys to browse command history',
        '这将把调制解调器 AT 配置恢复出厂。继续？': 'This will restore the modem AT configuration to factory defaults. Continue?',
        '这将修改 IMEI 并重启调制解调器。': 'This will change the IMEI and reboot the modem.',
        '成功': 'Success',
        '起始/结束地址必须是 1-254 的整数！': 'The start/end address must be an integer between 1 and 254.',
        '启用 DNS': 'Enable DNS',
        '禁用 DNS': 'Disable DNS',
        '模块信息': 'Module Info',
        '服务小区': 'Serving Cell',
        '信号详情': 'Signal Details',
        '天线信号': 'Antenna Signals',
        '信号数据读取失败': 'Failed to load signal data',
        '网络制式': 'Network Mode',
        '更新于': 'Updated at',
        '物理小区标识 (PCI)': 'Physical Cell ID (PCI)',
        '小区 ID': 'Cell ID',
        '天线1 (PRX)': 'Antenna 1 (PRX)',
        '天线2 (DRX)': 'Antenna 2 (DRX)',
        '天线3 (RX2)': 'Antenna 3 (RX2)',
        '天线4 (RX3)': 'Antenna 4 (RX3)',
        '载波聚合': 'Carrier Aggregation',
        '聚合状态': 'Aggregation Status',
        '角色': 'Role',
        '主载波 (PCC)': 'Primary Carrier (PCC)',
        '辅载波 (SCC)': 'Secondary Carrier (SCC)',
        '单载波(无聚合)': 'Single carrier (no aggregation)',
        '载波聚合生效': 'Carrier aggregation active',
        '载波': 'carriers',
        'LAN 配置': 'LAN Config',
        '确认并重启': 'Confirm and Reboot',
        '这将把调制解调器 AT 配置恢复出厂。': 'This will restore the modem AT configuration to factory defaults.',
        '重试': 'Retry',
        '重载': 'Reload',
        '系统状态获取失败': 'Failed to load system status',
        'TTL 状态获取失败': 'Failed to load TTL status',
        '已本地生效，服务器保存失败': 'Applied locally, but saving on the server failed',
        '退出并重新登录': 'Log out and sign in again',
        '扫描失败，请重试': 'Scan failed, please retry',
        '扫描中…最长约 3 分钟,请勿离开页面': 'Scanning... may take up to 3 minutes, please stay on this page',
        '扫描完成,模块未返回任何小区(该固件扫描功能可能受限)': 'Scan finished but the module returned no cells (scanning may be limited by this firmware)',
        '未扫描到小区': 'No cells found',
        '邻区扫描': 'Neighbour Cell Scan',
        '模块尚未就绪,请稍后重试': 'Module not ready yet, please retry shortly',
        '读取设置失败': 'Failed to load settings',
        '清除历史': 'Clear History',
        '模块就绪中…': 'Module warming up...',
        'AT 数据读取失败，请检查模块或稍后重试': 'Failed to read AT data, please check the module or retry later',
        '扫描失败：AT 无有效应答，请检查模块或稍后重试': 'Scan failed: no valid AT response, please check the module or retry later',
        '待重启生效': 'Pending reboot to take effect',
        '发送失败': 'Send failed',
        '删除失败': 'Delete failed',
        '数据更新失败': 'Data update failed',
        '操作失败，已取消后续步骤': 'Operation failed, follow-up steps cancelled',
        '状态尚未就绪，请稍候': 'Status not ready yet, please wait',
        '网络设置保存失败': 'Failed to save network settings',
        '锁定失败': 'Lock failed',
        '解锁失败': 'Unlock failed',
        '保存失败，请重试': 'Save failed, please retry',
        '短信发送失败': 'SMS sending failed',
        '自动化设置已回读同步': 'Automation settings re-synced',
        '看门狗、定时重启或 Webhook 保存失败': 'Failed to save watchdog, scheduler or webhook',
        '会话已过期': 'Session expired',
        '重新登录': 'Sign in again',
        '加载中': 'Loading',
        'LTE 小区与 NR5G 小区将依次锁定': 'LTE cells and NR5G cell will be locked in sequence',
        '参数必须为纯数字': 'Parameters must be digits only',
        '起始地址不能大于结束地址': 'Start address cannot be greater than end address',
        '网关 IP 地址末段必须在 1-254 之间': 'Last octet of gateway IP must be 1-254',
        '防火墙': 'Firewall',
        '防火墙状态': 'Firewall Status',
        '阻止端口': 'Blocked Ports',
        '阻止规则数': 'Blocking Rules',
        '状态': 'Status',
        '端口': 'Port',
        '操作': 'Actions',
        '添加': 'Add',
        '应用更改': 'Apply Changes',
        '清空': 'Clear All',
        '未配置阻止端口': 'No blocked ports configured',
        '有未应用的更改': 'You have unapplied changes',
        '防火墙状态加载失败': 'Failed to load firewall status',
        '端口必须是 1-65535 的数字': 'Port must be a number between 1 and 65535',
        '该端口已在列表中': 'This port is already in the list',
        '已应用': 'Applied',
        '应用失败': 'Failed to apply',
        '在 bridge0、eth0、tailscale0 接口上放行已配置的端口，其余接口的对应入站连接将被阻止。': 'Configured ports are allowed on the bridge0, eth0 and tailscale0 interfaces; matching inbound connections on all other interfaces are blocked.',
        '放行': 'Allow',
        '阻止': 'Block',
        '类型': 'Type',
        '端口规则': 'Port Rules',
        '未配置端口规则': 'No port rules configured',
        '所有防火墙规则': 'All Firewall Rules',
        '数据包': 'Packets',
        '字节': 'Bytes',
        '目标': 'Target',
        '协议': 'Protocol',
        '入接口': 'In',
        '出接口': 'Out',
        '源地址': 'Source',
        '目的地址': 'Destination',
        '详情': 'Details',
        '默认策略': 'Default policy',
        '暂无规则': 'No rules',
        '暂无规则数据': 'No rule data available',
        '管理规则数': 'Managed Rules',
        '应用后立即生效；配置已持久化，设备重启后自动恢复。同一端口不能同时阻止和放行。': 'Changes take effect immediately and persist across reboots. The same port cannot be both blocked and allowed.',
        '恢复全部': 'Restore All',
        '短信详情': 'SMS Details',
        '关闭': 'Close',
        '暂无可用频段': 'No bands available',
        '例如 8080': 'e.g. 8080',
        '找不到对应的小区数据，请重新扫描': 'Cell data not found. Please scan again.',
        '频段模式无效': 'Invalid band mode',
        '无': 'None',
        '监控': 'Monitoring',
        '通信': 'Communication',
        '工具': 'Tools',
        '系统': 'System',
        '界面偏好': 'Interface Preferences',
        '账户安全': 'Account Security',
        '短信转发': 'SMS Forwarding',
        '重启中…请稍候,请勿关闭页面': 'Rebooting... Please wait, do not close this page.',
        '确认': 'Confirm',
        '重启调制解调器': 'Reboot Modem',
        '这将重启调制解调器。继续吗?': 'This will reboot the modem. Continue?',
        '重启倒计时结束': 'Reboot countdown finished',
        '重启失败': 'Reboot failed',
        'AT命令重启': 'AT Command Reboot',
        '通过 AT 指令（AT+CFUN=1,1）重启蜂窝模块，约需 40 秒恢复，设备本身保持运行。': 'Reboots the cellular module via AT command (AT+CFUN=1,1); takes about 40 seconds to recover. The device itself keeps running.',
        '将通过 AT 指令（AT+CFUN=1,1）重启蜂窝模块，约需 40 秒恢复。继续吗?': 'This will reboot the cellular module via AT command (AT+CFUN=1,1); takes about 40 seconds to recover. Continue?',
        '设备重启': 'Device Reboot',
        '重启整台设备（reboot），期间管理界面不可用，约需 60 秒恢复。': 'Reboots the entire device (reboot); the admin interface is unavailable for about 60 seconds.',
        '这将重启整台设备，期间管理界面不可用，约需 60 秒恢复。继续吗?': 'This will reboot the entire device; the admin interface is unavailable for about 60 seconds. Continue?',
        '设备重启中…请稍候,请勿关闭页面': 'Rebooting device… please wait, do not close this page.',
        '关机': 'Power Off',
        '设备断电关机（poweroff），关机后必须手动通电才能恢复。': 'Powers off the device (poweroff); manual power-on is required to recover.',
        '这将使设备断电关机，关机后设备无法自行恢复，必须手动通电开机。继续吗?': 'This will power off the device. It cannot recover by itself and must be powered on manually. Continue?',
        '关机命令未送达，设备仍在运行，请重试': 'The power-off command was not delivered and the device is still running. Please retry.',
        '设备已关机': 'Device Powered Off',
        '设备已断电关机，管理界面不再可用。如需继续使用，请手动为设备通电开机。': 'The device has been powered off and the admin interface is no longer available. To continue, please power on the device manually.',
        '知道了': 'Got it',
        '用户名': 'Username',
        '密码': 'Password',
        '登录': 'Sign In',
        '登录中...': 'Signing in...',
        '请输入账号和密码': 'Enter your username and password',
        '用户名或密码错误': 'Incorrect username or password',
        '尝试次数过多，请稍后重试': 'Too many attempts. Please try again later.',
        '服务认证配置异常': 'Authentication configuration error',
        '尝试次数过多，请 ': 'Too many attempts. Please retry in ',
        '秒后重试': ' seconds.',
        '秒后可重试': ' seconds',
        '蜂窝设置': 'Cellular Settings',
        '锁频配置档': 'Band Lock Profiles',
        '重置失败:': 'Reset failed:',
        '正在查询当前已锁定频段...': 'Querying locked bands...',
        '锁定模式': 'Lock Mode',
        '这将把频段锁定恢复为全部频段。继续？': 'This will restore band locking to all bands. Continue?',
        '这将解除 LTE 小区锁定。继续？': 'This will unlock LTE cells. Continue?',
        '这将解除 NR5G-SA 小区锁定。继续？': 'This will unlock NR5G-SA cells. Continue?',
        '删除配置档': 'Delete Profile',
        '频段锁定已提交，正在刷新状态': 'Band lock submitted, refreshing status',
        '操作成功，正在刷新状态': 'Operation succeeded, refreshing status',
        'TTL 值必须是 0-255 的整数': 'TTL must be an integer between 0 and 255',
        '确定删除该规则？': 'Delete this rule?',
        '确定清空全部规则？': 'Clear all rules?',
        '删除短信': 'Delete SMS',
        '确定删除选中的短信？': 'Delete the selected messages?',
        '清空短信': 'Clear SMS',
        '确定删除全部短信？': 'Delete all messages?',
        '短信转发配置加载失败': 'Failed to load SMS forwarding settings',
        '静态地址绑定': 'Static Address Binding',
        'MAC 地址': 'MAC Address',
        'IP 地址': 'IP Address',
        'MAC 地址，例如 AA:BB:CC:DD:EE:FF': 'MAC address, e.g. AA:BB:CC:DD:EE:FF',
        'IP 地址，例如 192.168.5.20': 'IP address, e.g. 192.168.5.20',
        '未配置静态绑定': 'No static bindings configured',
        '为设备固定分配 IP（DHCP 静态租约），保存后立即生效；固件持久化，设备重启后保留；最多 10 条。': 'Assigns a fixed IP to a device (DHCP static lease). Takes effect immediately after saving. Persisted in firmware and retained after reboot. Up to 10 entries.',
        '上游 DNS': 'Upstream DNS',
        '自定义': 'Custom',
        '跟随运营商': 'Carrier Provided',
        '每行一个，例如 223.5.5.5': 'One per line, e.g. 223.5.5.5',
        '启用自定义': 'Enable Custom',
        '恢复运营商': 'Restore Carrier',
        'DNS V6 代理当前未开启，自定义上游 DNS 可能不生效。': 'The DNS V6 proxy is currently disabled; custom upstream DNS may not take effect.',
        '保存时将短暂重启本机 DNS 服务（约 1 秒）；支持 IPv4/IPv6，最多 4 个。': 'Saving briefly restarts the local DNS service (about 1 second). Supports IPv4/IPv6, up to 4 servers.',
        '已添加静态绑定': 'Static binding added',
        '已删除静态绑定': 'Static binding deleted',
        '删除静态绑定': 'Delete Static Binding',
        '删除后该设备将恢复动态分配 IP。': 'The device will revert to a dynamically assigned IP after deletion.',
        'MAC 地址格式无效': 'Invalid MAC address format',
        'IP 地址格式无效': 'Invalid IP address format',
        '该 MAC 地址已绑定': 'This MAC address is already bound',
        '该 IP 地址已绑定': 'This IP address is already bound',
        '最多添加 10 条静态绑定': 'You can add up to 10 static bindings',
        '请至少填写一个 DNS 服务器': 'Please enter at least one DNS server',
        '最多配置 4 个上游 DNS 服务器': 'You can configure up to 4 upstream DNS servers',
        '自定义上游 DNS': 'Custom Upstream DNS',
        '保存时将短暂重启本机 DNS 服务（约 1 秒），确定保存？': 'Saving will briefly restart the local DNS service (about 1 second). Save?',
        '自定义上游 DNS 已保存': 'Custom upstream DNS saved',
        '恢复运营商 DNS': 'Restore Carrier DNS',
        '恢复后上游 DNS 将跟随运营商下发，确定恢复？': 'Upstream DNS will follow the carrier after restoring. Restore?',
        '已恢复运营商 DNS': 'Carrier DNS restored',
        '设置读取失败': 'Failed to load settings',
        '频段信息读取失败': 'Failed to load band information',
        '短信读取失败': 'Failed to read SMS',
        '网络设置未就绪，请稍后重试': 'Network settings are not ready yet, please try again later',
        '放行规则对所有接口生效；阻止规则在 bridge0、eth0、tailscale0 接口上放行，其余接口的对应入站连接将被阻止。': 'Allow rules apply to all interfaces; block rules allow traffic on the bridge0, eth0 and tailscale0 interfaces, and matching inbound connections on all other interfaces are blocked.',
        '继续？': 'Continue?',
        '无本机号码': 'No phone number available',
        '上游 DNS 服务器': 'Upstream DNS servers',
        '规则类型': 'Rule type',
        '新的登录密码': 'New login password',
        '确认新的登录密码': 'Confirm new login password',
        'Webhook 地址': 'Webhook URL',
        '锁定当前服务小区': 'Lock Serving Cell',
        '频点锁定': 'EARFCN Lock',
        'NR5G 频点（逗号分隔，最多 32 个）': 'NR5G EARFCNs (comma separated, max 32)',
        'LTE 频点（逗号分隔，最多 2 个）': 'LTE EARFCNs (comma separated, max 2)',
        '锁定频点': 'Lock EARFCNs',
        '解锁频点': 'Unlock EARFCNs',
        'NR5G 小区锁需要探测 SCS，期间网络可能短暂中断约 25 秒。继续？': 'NR5G cell lock probes SCS; the network may drop briefly (up to ~25s). Continue?',
        '将锁定当前驻留小区，期间可能短暂断网。继续？': 'Lock the current serving cell; the network may drop briefly. Continue?',
        'SCS 探测失败，锁定已自动解除，请手动指定 SCS 重试': 'SCS probe failed, lock auto-released. Pick SCS manually and retry',
        '该固件可能不支持此频点锁': 'This firmware may not support this EARFCN lock',
        'SCS：自动': 'SCS: Auto',
        'SCS：15': 'SCS: 15',
        'SCS：30': 'SCS: 30',
        'SCS：60': 'SCS: 60',
        'SCS：120': 'SCS: 120',
        'SCS：240': 'SCS: 240',
        '锁定小区': 'Lock Cell',
        '请输入频点': 'Please enter EARFCNs',
        '频点数量超出限制': 'EARFCN count exceeds the limit',
        '这将解除 NR5G 频点锁定。继续？': 'This will remove the NR5G EARFCN lock. Continue?',
        '这将解除 LTE 频点锁定。继续？': 'This will remove the LTE EARFCN lock. Continue?'
      }
    };

    function normalizeLanguage(language) {
      const value = String(language || '').trim().toLowerCase();
      if (value === 'en' || value === 'en-us' || value === 'english') return 'en';
      if (value === 'zh' || value === 'zh-cn' || value === 'cn' || value === 'chinese') return 'zh-CN';
      return '';
    }

    function normalizeText(value) {
      return String(value || '').replace(/\s+/g, ' ').trim();
    }

    function translateForLanguage(key, language) {
      const source = String(key || '');
      const lang = normalizeLanguage(language) || defaultLanguage;
      if (lang === 'zh-CN') return source;

      const table = translations[lang] || {};
      if (Object.prototype.hasOwnProperty.call(table, source)) return table[source];

      if (source.startsWith('当前：') || source.startsWith('当前:')) {
        const marker = source.startsWith('当前：') ? '当前：' : '当前:';
        const value = source.slice(marker.length).trim();
        return 'Current: ' + (value ? translateForLanguage(value, lang) : '');
      }

      if (source.startsWith('短信发送失败：')) {
        const value = source.slice('短信发送失败：'.length).trim();
        return 'SMS sending failed: ' + (value ? translateForLanguage(value, lang) : '');
      }

      const gettingMatch = source.match(/^获取(.+)中\.\.\.$/);
      if (gettingMatch) return 'Getting ' + translateForLanguage(gettingMatch[1], lang) + '...';

      const dnsLineMatch = source.match(/^第 (\d+) 行 DNS 服务器格式无效$/);
      if (dnsLineMatch) return 'DNS server on line ' + dnsLineMatch[1] + ' has an invalid format';

      const earfcnGroupMatch = source.match(/^请完整填写 (\d+) 组 EARFCN 和 PCI$/);
      if (earfcnGroupMatch) return 'Please fill in all ' + earfcnGroupMatch[1] + ' EARFCN and PCI groups';

      const simActivationMatch = source.match(/^(已|未)激活卡(\d+)$/);
      if (simActivationMatch) {
        return (simActivationMatch[1] === '已' ? 'Active SIM ' : 'Inactive SIM ') + simActivationMatch[2];
      }

      const smsSelectionMatch = source.match(/^选择短信 (\d+)$/);
      if (smsSelectionMatch) return 'Select message ' + smsSelectionMatch[1];

      return source;
    }

    function translate(key) {
      return translateForLanguage(key, currentLanguage);
    }

    function isRenderedFromKey(key, renderedText) {
      const rendered = normalizeText(renderedText);
      if (!key || !rendered) return false;
      if (rendered === normalizeText(key)) return true;
      return supportedLanguages.some((language) => rendered === normalizeText(translateForLanguage(key, language)));
    }

    function resolveI18nKey(currentValue, storedKey) {
      const current = normalizeText(currentValue);
      if (!current) return '';
      if (storedKey && isRenderedFromKey(storedKey, current)) return storedKey;
      return current;
    }

    function withOriginalWhitespace(value, replacement) {
      const text = String(value || '');
      const prefix = (text.match(/^\s*/) || [''])[0];
      const suffix = (text.match(/\s*$/) || [''])[0];
      return prefix + replacement + suffix;
    }

    function skipTextElement(element) {
      if (!element || element.nodeType !== 1) return false;
      const tag = element.tagName.toLowerCase();
      return ['script', 'style', 'svg', 'path', 'code', 'pre', 'textarea'].includes(tag)
        || Boolean(element.closest('[data-no-i18n]'));
    }

    function skipAttributeElement(element) {
      if (!element || element.nodeType !== 1) return false;
      const tag = element.tagName.toLowerCase();
      return ['script', 'style', 'svg', 'path', 'code', 'pre'].includes(tag)
        || Boolean(element.closest('[data-no-i18n]'));
    }

    let applying = false;
    let observer = null;
    let applyTimer = null;
    let autoLoadStarted = false;

    function translateTextNodes(rootNode) {
      if (!rootNode || typeof document === 'undefined') return;
      const start = rootNode.nodeType === 9 ? rootNode.body : rootNode;
      if (!start) return;
      const walker = document.createTreeWalker(start, NodeFilter.SHOW_TEXT, {
        acceptNode(node) {
          const parent = node.parentElement;
          if (!parent || skipTextElement(parent) || !normalizeText(node.nodeValue)) {
            return NodeFilter.FILTER_REJECT;
          }
          return NodeFilter.FILTER_ACCEPT;
        }
      });
      const nodes = [];
      while (walker.nextNode()) nodes.push(walker.currentNode);
      nodes.forEach((node) => {
        const parentKey = node.parentElement && node.parentElement.dataset
          ? node.parentElement.dataset.simpleadminI18nKey
          : '';
        const key = parentKey || resolveI18nKey(node.nodeValue, node.__simpleadminI18nKey);
        if (!key) return;
        node.__simpleadminI18nKey = key;
        const translated = translate(key);
        const nextValue = withOriginalWhitespace(node.nodeValue, translated);
        if (node.nodeValue !== nextValue) node.nodeValue = nextValue;
      });
    }

    function translateAttributes(rootNode) {
      const start = rootNode && rootNode.nodeType === 1 ? rootNode : document;
      if (!start || typeof start.querySelectorAll !== 'function') return;
      const attributes = ['aria-label', 'placeholder', 'title'];
      const elements = [];
      if (start.nodeType === 1) elements.push(start);
      start.querySelectorAll('*').forEach((element) => elements.push(element));
      elements.forEach((element) => {
        if (skipAttributeElement(element)) return;
        attributes.forEach((attr) => {
          if (!element.hasAttribute(attr)) return;
          if (attr === 'aria-label' && element.hasAttribute('data-simpleadmin-i18n-aria')) return;
          const storeName = 'simpleadminI18n' + attr.replace(/[^a-z0-9]/gi, '');
          const key = resolveI18nKey(element.getAttribute(attr), element.dataset[storeName]);
          if (!key) return;
          element.dataset[storeName] = key;
          const translated = translate(key);
          if (element.getAttribute(attr) !== translated) element.setAttribute(attr, translated);
        });
      });
    }

    function translateAriaKeys(rootNode) {
      const start = rootNode && rootNode.nodeType === 1 ? rootNode : document;
      if (!start || typeof start.querySelectorAll !== 'function') return;
      const elements = [];
      if (start.nodeType === 1 && typeof start.hasAttribute === 'function' && start.hasAttribute('data-simpleadmin-i18n-aria')) {
        elements.push(start);
      }
      start.querySelectorAll('[data-simpleadmin-i18n-aria]').forEach((element) => elements.push(element));
      elements.forEach((element) => {
        if (skipAttributeElement(element)) return;
        const key = element.getAttribute('data-simpleadmin-i18n-aria');
        if (!key) return;
        const translated = translate(key);
        if (element.getAttribute('aria-label') !== translated) element.setAttribute('aria-label', translated);
      });
    }

    function apply(rootNode) {
      if (typeof document === 'undefined') return currentLanguage;
      applying = true;
      try {
        document.documentElement.setAttribute('lang', currentLanguage);
        translateTextNodes(rootNode || document);
        translateAttributes(rootNode || document);
        translateAriaKeys(rootNode || document);
      } finally {
        applying = false;
      }
      return currentLanguage;
    }

    function scheduleApply(rootNode) {
      if (typeof document === 'undefined') return;
      if (applyTimer) clearTimeout(applyTimer);
      applyTimer = setTimeout(() => {
        applyTimer = null;
        apply(rootNode || document);
      }, 0);
    }

    function startObserver() {
      if (typeof document === 'undefined' || typeof MutationObserver === 'undefined' || !document.body) return;
      if (observer) observer.disconnect();
      observer = new MutationObserver(() => {
        if (!applying) scheduleApply(document);
      });
      observer.observe(document.body, {
        childList: true,
        subtree: true,
        characterData: true,
        attributes: true,
        attributeFilter: ['aria-label', 'placeholder', 'title', 'data-simpleadmin-i18n-aria']
      });
    }

    function setCurrentLanguage(language) {
      const normalized = normalizeLanguage(language) || defaultLanguage;
      currentLanguage = supportedLanguages.includes(normalized) ? normalized : defaultLanguage;
      try { localStorage.setItem(storageKey, currentLanguage); } catch (_) { /* ignore storage errors */ }
      if (typeof document !== 'undefined') apply(document);
      if (typeof window !== 'undefined' && typeof window.dispatchEvent === 'function') {
        window.dispatchEvent(new CustomEvent('simpleadmin:language-changed', { detail: { language: currentLanguage } }));
      }
      return currentLanguage;
    }

    function readLanguageFromResponse(response) {
      if (!response || !response.ok) throw new Error('language response failed');
      return response.json().then((data) => normalizeLanguage(data.language));
    }

    function load() {
      try {
        const stored = normalizeLanguage(localStorage.getItem(storageKey));
        if (stored) currentLanguage = stored;
      } catch (_) { /* ignore storage errors */ }

      const fromApi = root.Api && typeof root.Api.getLanguage === 'function'
        ? root.Api.getLanguage().then(readLanguageFromResponse)
        : Promise.reject(new Error('language api unavailable'));

      return fromApi
        .then((language) => {
          if (language) setCurrentLanguage(language);
          else apply(document);
          return currentLanguage;
        })
        .catch(() => {
          apply(document);
          return currentLanguage;
        });
    }

    function save(language) {
      const normalized = setCurrentLanguage(language);
      return root.Api.setLanguage(normalized)
        .then((response) => {
          if (!response.ok) throw new Error('language save failed');
          return { language: normalized, localOnly: false };
        })
        .catch((error) => {
          console.warn('Language saved locally but server save failed:', error);
          return { language: normalized, localOnly: true };
        });
    }

    function translateMessage(message) {
      return typeof message === 'string' ? translate(message) : message;
    }

    function wrapNativeDialogs() {
      if (typeof window === 'undefined' || window.__simpleadminI18nDialogsBound) return;
      window.__simpleadminI18nDialogsBound = true;

      const nativeAlert = window.alert;
      if (typeof nativeAlert === 'function') {
        window.alert = function (message) {
          return nativeAlert.call(window, translateMessage(message));
        };
      }

      const nativeConfirm = window.confirm;
      if (typeof nativeConfirm === 'function') {
        window.confirm = function (message) {
          return nativeConfirm.call(window, translateMessage(message));
        };
      }
    }

    function startAutoLoad() {
      if (autoLoadStarted || typeof window === 'undefined' || typeof document === 'undefined') return;
      autoLoadStarted = true;
      wrapNativeDialogs();

      if (typeof window.addEventListener === 'function') {
        window.addEventListener('simpleadmin:vue-mounted', () => scheduleApply(document));
      }

      const run = () => {
        load()
          .catch(() => currentLanguage)
          .then(() => {
            startObserver();
            scheduleApply(document);
          });
      };

      if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', run, { once: true });
      } else {
        run();
      }
    }

    startAutoLoad();

    return {
      supportedLanguages,
      normalizeLanguage,
      getCurrentLanguage() { return currentLanguage; },
      t: translate,
      apply,
      load,
      setLanguage(language, options) {
        if (options && options.save) return save(language);
        // 未走服务端保存即仅本地生效,如实报告 localOnly。
        return Promise.resolve({ language: setCurrentLanguage(language), localOnly: true });
      }
    };
  })();

  global.SimpleAdmin = root;
})(window);
