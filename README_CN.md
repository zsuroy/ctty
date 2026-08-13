<p align="center">
    <img src="images/logo.svg" alt="ctty Logo" width="120" />
</p>

# 🚀 ctty - 连接管理器

[English](README.md) | [中文](README_CN.md)

[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go)](https://golang.org/)
[![Release](https://img.shields.io/github/v/release/zsuroy/ctty?style=for-the-badge)](https://github.com/zsuroy/ctty/releases)
[![License](https://img.shields.io/github/license/zsuroy/ctty?style=for-the-badge)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey?style=for-the-badge)](https://github.com/zsuroy/ctty/releases)

> **一个轻量级的一体化终端连接管理器 —— SSH、串口、SFTP 尽在一个 TUI** 🔥

ctty 是一个快速、原生的终端工具，用于管理你的所有连接 —— SSH 主机、串口设备、SFTP 文件传输 —— 无需 Electron 的开销。使用 Go 编写，拥有直观的 TUI 界面，将 Tabby 等图形化连接管理器的便利性带到终端中，零臃肿。

**为什么选择 ctty？**
- **嫌 Tabby 太重？** ctty 是单个约 5MB 的二进制文件，没有 Electron，没有浏览器引擎 —— 纯 Go
- **需要一个工具同时搞定串口 + SSH + SFTP？** 大多数终端模拟器只做 SSH；ctty 三者全覆盖
- **想留在终端里？** 不用在多个应用间切换 —— 一切都由键盘驱动

<p align="center">
    <a href="images/ctty.gif" target="_blank">
        <img src="images/ctty.gif" alt="Demo ctty Terminal" width="800" />
    </a>
    <br>
    <em>🖱️ 点击图片查看完整大小</em>
</p>

## ✨ 功能特性

### 🚀 **核心能力**
- **🎨 精美 TUI 界面** - 用优雅的交互式终端 UI 浏览 SSH 主机
- **⚡ 快速连接** - 通过 TUI 或命令行 `ctty <host>` 即时连接
- **🔄 端口转发** - 轻松设置本地、远程和动态（SOCKS）转发，带历史记录
- **📝 轻松管理** - 无缝添加、编辑、移动和管理 SSH 配置
- **🏷️ 标签支持** - 用自定义标签组织主机；`hidden` 标签可隐藏主机但保持可连接
- **🔍 智能搜索** - 内置过滤和搜索快速找到主机
- **📝 实时状态** - 异步 ping 检查和颜色编码的 SSH 连接状态指示
- **🔔 智能更新** - 自动版本检查和更新通知
- **🔌 串口连接** - 管理和连接串口设备（控制台、交换机、路由器），可配置波特率、数据位、校验、停止位；自动检测端口即时出现在列表中
- **📁 SFTP 支持** - 不离开终端即可在远程主机间传输文件

### 🛠️ **技术特性**
- **🔒 安全** - 直接使用现有的 `~/.ssh/config` 文件
- **📁 自定义配置** - 通过 `-c` 标志使用任意 SSH 配置文件
- **📂 SSH Include 支持** - 完整支持 SSH Include 指令，跨多文件组织配置
- **⚙️ SSH 选项** - 通过直观的表单添加任意 SSH 配置选项
- **🔄 自动转换** - 命令行和配置格式之间无缝转换
- **🔄 自动备份** - 修改前自动备份配置
- **✅ 验证** - 内置验证防止配置错误
- **🔗 ProxyJump/ProxyCommand** - 通过跳板机安全连接隧道
- **⌨️ 键盘快捷键** - Vim 风格的快捷键导航
- **🌐 跨平台** - 支持 Linux、macOS（Intel 和 Apple Silicon）和 Windows
- **⚡ 轻量** - 单二进制，无依赖，零配置

## 🚀 快速开始

### 安装

**Homebrew（macOS 推荐）：**
```bash
brew install zsuroy/ctty/ctty
```

**Unix/Linux/macOS（一行安装）：**
```bash
curl -sSL https://raw.githubusercontent.com/zsuroy/ctty/master/install/unix.sh | bash
```

**Windows（PowerShell）：**
```powershell
irm https://raw.githubusercontent.com/zsuroy/ctty/master/install/windows.ps1 | iex
```

**其他方式：**

*Linux/macOS:*
```bash
# 下载指定版本
wget https://github.com/zsuroy/ctty/releases/latest/download/ctty-linux-amd64.tar.gz

# 解压并安装
tar -xzf ctty-linux-amd64.tar.gz
sudo mv ctty-linux-amd64 /usr/local/bin/ctty
```

*Windows:*
```powershell
# 下载并解压
Invoke-WebRequest -Uri "https://github.com/zsuroy/ctty/releases/latest/download/ctty-windows-amd64.zip" -OutFile "ctty-windows-amd64.zip"
Expand-Archive ctty-windows-amd64.zip -DestinationPath C:\tools\
# 将 C:\tools 添加到你的 PATH 环境变量
```

**从源码构建（需要 Go 1.23+）：**
```bash
git clone https://github.com/zsuroy/ctty.git
cd ctty
go build -o ctty .
sudo mv ctty /usr/local/bin/
```

## 📖 用法

### 交互模式

运行 ctty（不带参数）进入 TUI 界面：

```bash
ctty
```

**导航：**
- `↑/↓` 或 `j/k` - 浏览主机
- `Enter` - 连接到选中的主机
- `a` - 添加主机
- `e` - 编辑选中的主机
- `d` - 删除选中的主机
- `m` - 移动主机到其他配置文件（需要 SSH Include 指令）
- `f` - 端口转发设置
- `t` - 打开串口设备管理器
- `H` - 切换隐藏主机可见性
- `q` - 退出
- `/` - 搜索/过滤主机

**实时状态指示：**
- 🟢 **在线** - 主机可通过 SSH 连接
- 🟡 **连接中** - 正在检查主机连通性
- 🔴 **离线** - 主机不可达或 SSH 连接失败
- ⚫ **未知** - 连通性状态尚未确定

**排序与过滤：**
- `s` - 切换排序模式（名称 ↔ 最后登录）
- `n` - 按名称排序（字母顺序）
- `r` - 按最近连接排序（最后登录时间）
- `Tab` - 在过滤模式间切换
- 按名称过滤（默认）- 搜索主机名
- 按最后登录过滤 - 按最近使用的连接排序和过滤

交互式表单将引导你完成配置：
- **主机名/IP** - 服务器地址
- **用户名** - SSH 用户
- **端口** - SSH 端口（默认：22）
- **密钥文件** - 私钥路径
- **ProxyJump** - 跳板机连接隧道
- **ProxyCommand** - 跳板命令连接隧道
- **SSH 选项** - 额外的 SSH 选项，命令行格式（如 `-o Compression=yes -o ServerAliveInterval=60`）
- **标签** - 逗号分隔的标签用于组织

### 串口连接

按 `t` 从主 TUI 进入串口设备管理器。可用串口端口自动检测并立即列出 —— 无需手动设置即可快速访问。

**串口设备列表：**
- 检测到的端口自动出现，使用默认设置（115200 8N1）
- 保存的设备（带自定义名称和设置）排在顶部
- `Enter` - 连接到选中的串口设备
- `i` - 查看设备信息（名称、端口、波特率等）
- `a` - 添加新的串口设备
- `d` - 删除已保存的串口设备
- `/` - 搜索/过滤设备（按名称或端口路径）
- `Esc`/`q` - 返回 SSH 主机列表

**设备信息页：**
- `e` 或 `Enter` - 编辑参数后连接（波特率/数据位/校验/停止位）
- `Esc`/`i` - 返回设备列表

**添加串口设备：**
- **名称** - 友好别名（如 `Switch-Console`）
- **设备** - 端口路径（如 `/dev/cu.usbserial-1420`）；用 `←/→` 从检测到的端口中选择
- **波特率** - 默认：115200
- **数据位** - 5、6、7 或 8（默认：8）
- **校验** - `none`、`even` 或 `odd`（默认：`none`）
- **停止位** - 1 或 2（默认：1）

**连接前编辑参数：**
- 波特率可直接输入，或用 `←/→` 在常用值间切换（9600/19200/38400/57600/115200/230400/460800/921600）
- 按 `Enter` 用修改后的参数连接

**连接：** TUI 暂停并将终端直接桥接到串口。按 `Ctrl+]` 或 `Ctrl+C` 断开并返回 TUI。

也可以直接启动串口管理器：
```bash
ctty serial    # 跳过 SSH 主机列表，直接进入串口设备管理
```

串口设备配置存储在 `~/.config/ctty/serial.json`，与 `~/.ssh/config` 分离。

### 端口转发

ctty 提供直观的界面来设置 SSH 端口转发。在选中主机时按 `f` 打开端口转发设置：

**转发类型：**
- **本地（-L）** - 通过 SSH 连接将本地端口转发到远程主机/端口
  - 示例：通过本地端口 `15432` 访问远程数据库 `localhost:5432`
  - 用法：`ssh -L 15432:localhost:5432 server` → 数据库可在 `localhost:15432` 访问

- **远程（-R）** - 将远程端口转发回本地主机/端口
  - 示例：在远程主机的 `8080` 端口暴露本地 Web 服务器
  - 用法：`ssh -R 8080:localhost:3000 server` → 本地应用可从远程主机的 8080 端口访问
  - ⚠️ **外部访问要求：**
    - **SSH 服务器配置**：在 `/etc/ssh/sshd_config` 中添加 `GatewayPorts yes` 并重启 SSH 服务
    - **防火墙**：在服务器防火墙中开放远程端口（`ufw allow 8080` 或等效命令）
    - **端口可用性**：确保远程端口未被占用
    - **绑定地址**：外部访问用 `0.0.0.0`，仅本地用 `127.0.0.1`

- **动态（-D）** - 创建 SOCKS 代理用于安全浏览
  - 示例：通过 SSH 连接路由 Web 流量
  - 用法：`ssh -D 1080 server` → 配置浏览器使用 `localhost:1080` 作为 SOCKS 代理
  - ⚠️ **配置要求：**
    - **浏览器设置**：在浏览器设置中配置 SOCKS v5 代理
    - **DNS**：启用 "Proxy DNS when using SOCKS v5" 以获得完整隐私
    - **应用程序**：只有支持 SOCKS 的应用程序会使用代理
    - **绑定地址**：出于安全考虑使用 `127.0.0.1`（仅本地访问）

**端口转发界面：**
- 用 ←/→ 箭头键选择转发类型
- 通过引导式表单配置端口和地址
- 可选绑定地址配置（默认 127.0.0.1）
- 实时验证端口号和地址
- **端口转发历史** - 保存常用配置以便快速复用
- 使用配置好的转发选项自动连接

**端口转发故障排除：**

*远程转发问题：*
```bash
# 错误："remote port forwarding failed for listen port X"
# 解决方案：
1. 检查端口是否已占用：ssh server "netstat -tln | grep :X"
2. 使用其他可用端口
3. 在 SSH 配置中启用 GatewayPorts 以允许外部访问
```

*远程转发的 SSH 服务器配置：*
```bash
# 编辑服务器上的 SSH 守护进程配置：
sudo nano /etc/ssh/sshd_config

# 添加或取消注释：
GatewayPorts yes

# 重启 SSH 服务：
sudo systemctl restart sshd  # Ubuntu/Debian/CentOS 7+
# 或
sudo service ssh restart     # 旧系统
```

*防火墙配置：*
```bash
# Ubuntu/Debian (UFW):
sudo ufw allow [端口号]

# CentOS/RHEL/Rocky (firewalld):
sudo firewall-cmd --add-port=[端口号]/tcp --permanent
sudo firewall-cmd --reload

# 检查端口是否可访问：
telnet [服务器IP] [端口号]
```

*动态转发（SOCKS）浏览器设置：*
```
Firefox: about:preferences → 网络设置
- 手动代理配置
- SOCKS 主机: localhost, 端口: [你的端口]
- SOCKS v5: ✓
- 使用 SOCKS v5 时代理 DNS: ✓

Chrome: 启动时指定代理
chrome --proxy-server="socks5://localhost:[你的端口]"
```

### 命令行用法

ctty 同时提供命令行操作和交互式 TUI 界面：

```bash
# 启动交互式 TUI 模式浏览和连接主机
ctty

# 直接连接到指定主机（带历史记录）
ctty my-server

# 在远程主机上执行命令
ctty my-server uptime

# 执行带参数的命令
ctty my-server ls -la /var/log

# 强制 TTY 分配（用于交互式命令）
ctty -t my-server sudo systemctl restart nginx

# 使用自定义 SSH 配置文件启动 TUI
ctty -c /path/to/custom/ssh_config

# 使用自定义 SSH 配置文件直接连接
ctty my-server -c /path/to/custom/ssh_config

# 使用交互式表单添加主机
ctty add

# 添加主机并预填主机名
ctty add hostname

# 使用自定义 SSH 配置文件添加主机
ctty add hostname -c /path/to/custom/ssh_config

# 编辑现有主机配置
ctty edit my-server

# 使用自定义 SSH 配置文件编辑主机
ctty edit my-server -c /path/to/custom/ssh_config

# 移动主机到其他 SSH 配置文件（需要 Include 指令）
ctty move my-server

# 使用自定义 SSH 配置文件移动主机（需要 Include 指令）
ctty move my-server -c /path/to/custom/ssh_config

# 搜索主机（交互式过滤）
ctty search

# 输出机器可读的信息（JSON）用于脚本
ctty info prod-server
ctty info prod-server --pretty

# 使用自定义 SSH 配置文件
ctty -c /path/to/custom/ssh_config info prod-server

# 管道到 jq
ctty info prod-server | jq -r '.result.target.hostname'
ctty info prod-server | jq -r '.result.target.user'

# 显示版本信息
ctty --version

# 禁用自动更新检查（适用于离线机器）
ctty --no-update-check

# 显示帮助和可用命令
ctty --help
```

### 主机信息（JSON）

`ctty info <hostname>` 输出单个 JSON 对象到 stdout，可以用 `jq` 脚本化处理。

```bash
# 提取字段
ctty info prod-server | jq -r '.result.target.hostname'
ctty info prod-server | jq -r '.result.target.port'

# 检查不存在的主机（退出码 2）
ctty info does-not-exist | jq -r '.error.code'
```

### Shell 补全

ctty 支持主机名的 shell 补全，方便不用输入全名即可连接：

```bash
ctty <TAB>           # 列出所有可用主机
ctty pro<TAB>        # 补全以 "pro" 开头的主机（如 prod-server）
```

**设置说明：**

**Bash:**
```bash
# 当前会话启用
source <(ctty completion bash)

# 永久启用（添加到 ~/.bashrc）
echo 'source <(ctty completion bash)' >> ~/.bashrc
```

**Zsh:**
```bash
# 当前会话启用
source <(ctty completion zsh)

# 永久启用（添加到 ~/.zshrc）
echo 'source <(ctty completion zsh)' >> ~/.zshrc
```

**Fish:**
```bash
# 当前会话启用
ctty completion fish | source

# 永久启用
ctty completion fish > ~/.config/fish/completions/ctty.fish
```

**PowerShell:**
```powershell
# 当前会话启用
ctty completion powershell | Out-String | Invoke-Expression

# 永久启用（添加到你的 PowerShell 配置文件）
Add-Content $PROFILE 'ctty completion powershell | Out-String | Invoke-Expression'
```

### 直接连接主机

ctty 支持通过命令行直接连接主机，方便集成到现有工作流：

```bash
# 直接连接到任意已配置的主机
ctty production-server
ctty db-staging
ctty web-01

# 所有直接连接都会记录在历史中
# 使用 TUI 查看最近连接的主机
```

**直接连接的特点：**
- **即时连接** - 无需 TUI 导航
- **历史记录** - 所有连接都会记录时间戳
- **错误处理** - 主机不存在或配置问题时显示清晰消息
- **配置文件支持** - 使用 `-c` 标志支持自定义配置文件

### 远程命令执行

无需打开交互式 shell 即可在远程主机上执行命令：

```bash
# 执行单个命令
ctty prod-server uptime

# 执行带参数的命令
ctty prod-server ls -la /var/log

# 检查磁盘使用
ctty prod-server df -h

# 查看日志（管道到本地命令）
ctty prod-server 'cat /var/log/nginx/access.log' | grep 404

# 强制 TTY 分配（用于交互式命令如 sudo、vim 等）
ctty -t prod-server sudo systemctl restart nginx
```

**特点：**
- **退出码传递** - 远程命令的退出码会传递回来
- **TTY 支持** - 使用 `-t` 标志用于需要终端交互的命令
- **管道友好** - 输出可管道到本地命令处理
- **历史记录** - 命令执行会记录在连接历史中

### 备份配置

ctty 在修改前自动创建 SSH 配置文件备份，确保配置安全。

**备份位置：**
- **Unix/Linux/macOS**: `~/.config/ctty/backups/`（若设置了 `$XDG_CONFIG_HOME` 则为 `$XDG_CONFIG_HOME/ctty/backups/`）
- **Windows**: `%APPDATA%\ctty\backups\`（回退：`%USERPROFILE%\.config\ctty\backups\`）

**关键特性：**
- 修改前自动备份
- 每个文件一个备份（覆盖之前的备份）
- 单独存储以避免 SSH Include 冲突
- 需要时可手动恢复

**其他存储：**
- **连接历史**：存储在同一配置目录中用于持久跟踪
- **端口转发历史**：保存的配置用于快速复用常用转发设置

**快速恢复：**
```bash
# Unix/Linux/macOS
cp ~/.config/ctty/backups/config.backup ~/.ssh/config

# Windows
copy "%APPDATA%\ctty\backups\config.backup" "%USERPROFILE%\.ssh\config"
```

### 配置文件选项

默认情况下，ctty 使用 `~/.ssh/config` 中的标准 SSH 配置文件。可以使用 `-c` 标志指定其他配置文件：

```bash
# TUI 模式使用自定义配置文件
ctty -c /path/to/custom/ssh_config

# 命令使用自定义配置文件
ctty add hostname -c /path/to/custom/ssh_config
ctty edit hostname -c /path/to/custom/ssh_config
ctty move hostname -c /path/to/custom/ssh_config
```

### 高级功能

#### 主机在配置文件间移动

ctty 提供强大的 `move` 命令在配置文件间迁移 SSH 主机。**此功能需要 SSH 配置中存在 Include 指令。**

```bash
# 移动主机到其他配置文件（需要 Include 指令）
ctty move my-server

# 使用自定义配置文件移动（需要 Include 指令）
ctty move my-server -c /path/to/custom/ssh_config
```

**⚠️ 重要要求：**
- **SSH Include 指令必须存在** 于你的 SSH 配置文件中（`~/.ssh/config` 或用 `-c` 指定的文件）
- 配置文件必须包含引用其他 SSH 配置文件的 `Include` 语句
- 没有 Include 指令时，move 命令会显示错误消息

**特点：**
- **交互式文件选择器** - 从 Include 指令中选择目标配置文件
- **Include 支持** - 与 SSH Include 指令结构无缝协作
- **原子操作** - 自动备份的安全主机移动
- **验证** - 防止冲突并确保配置完整性
- **错误处理** - 需要 Include 文件但未找到时显示清晰消息

**使用场景：**
- 从主配置重组主机到专门的 include 文件
- 将开发主机移到单独的环境特定配置
- 合并配置以更好地组织

**所需示例设置：**
你的主 SSH 配置文件必须包含 Include 指令，如：
```ssh
# ~/.ssh/config
Include ~/.ssh/config.d/*
Include work-servers.conf
Include projects/*.conf

Host personal-server
    HostName personal.example.com
    User myuser
```

#### 实时连通性状态

ctty 具有异步 SSH 连通性检查功能，提供主机可用性的可视化指示：

**状态指示：**
- 🟢 **在线** - SSH 连接成功（显示响应时间）
- 🟡 **连接中** - 正在测试连通性
- 🔴 **离线** - SSH 连接失败或主机不可达
- ⚫ **未知** - 状态尚未确定

**特点：**
- **非阻塞检查** - 状态更新在后台进行
- **响应时间跟踪** - 在线主机显示连接延迟
- **自动刷新** - 状态指示持续更新
- **错误详情** - 连接失败的详细错误信息

#### 自动更新检查

ctty 内置版本检查功能，有可用更新时会通知你：

**特点：**
- **后台检查** - 版本检查异步进行，不阻塞启动
- **发布通知** - 有更新时显示清晰指示
- **预发布检测** - 识别 beta 和开发版本
- **GitHub 集成** - 直接链接到发布页面
- **无干扰** - 更新不会中断工作流
- **可配置** - 可在离线或断网环境中禁用

**更新通知出现在：**
- 主 TUI 界面中作为微妙的通知
- 仅在有更新的稳定版本可用时

**禁用更新检查：**

通过 CLI 标志（一次性）：
```bash
ctty --no-update-check
```

通过 `~/.config/ctty/config.json`（持久）：
```json
{
  "check_for_updates": false
}
```

#### 端口转发历史

ctty 记住你的端口转发配置以便快速复用：

**特点：**
- **自动保存** - 成功的转发设置自动保存
- **快速复用** - 之前使用的配置作为建议出现
- **按主机历史** - 转发历史按 SSH 主机跟踪
- **所有转发类型** - 支持本地（-L）、远程（-R）和动态（-D）转发历史
- **持久存储** - 历史在应用重启后保留

### 平台特定说明

**Windows:**
- ctty 与内置 OpenSSH 客户端兼容（Windows 10/11）
- 配置文件位置：`%USERPROFILE%\.ssh\config`
- 兼容 WSL SSH 配置
- 支持 Unix 系统相同的 SSH 选项

**Unix/Linux/macOS:**
- 标准 SSH 配置文件：`~/.ssh/config`
- 完全兼容 OpenSSH 功能
- 自动保持文件权限

## 🏗️ 配置

ctty 直接使用你的标准 SSH 配置文件（`~/.ssh/config`）。它添加特殊注释标签以增强功能，同时保持与标准 SSH 工具的完全兼容。

### SSH Include 支持

ctty 完全支持 SSH Include 指令，允许跨多个文件组织 SSH 配置。这对于管理大量主机或按环境、项目、团队组织配置特别有用。

**Include 示例：**
```ssh
# 主 ~/.ssh/config 文件
Host personal-server
    HostName personal.example.com
    User myuser

# 包含工作相关配置
Include work-servers.conf

# 包含目录中的所有配置
Include projects/*

# 包含相对路径
Include ~/.ssh/configs/production.conf
```

**组织示例：**

*work-servers.conf:*
```ssh
# Tags: work, production
Host prod-web-01
    HostName 10.0.1.10
    User deploy
    ProxyJump bastion.company.com

# Tags: work, staging
Host staging-api
    HostName staging-api.company.com
    User developer
```

*projects/client-alpha.conf:*
```ssh
# Tags: client, development
Host client-alpha-dev
    HostName dev.client-alpha.com
    User admin
    Port 2222
```

**示例配置：**
Include ~/.ssh/conf.d/*

```ssh
# Tags: production, web, frontend
Host web-prod-01
    HostName 192.168.1.10
    User deploy
    Port 22
    IdentityFile ~/.ssh/production_key
    Compression yes
    ServerAliveInterval 60

# Tags: development, database
Host db-dev
    HostName dev-db.company.com
    User admin
    Port 2222
    IdentityFile ~/.ssh/dev_key
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null

# Tags: production, backend
Host backend-prod
    HostName 10.0.1.50
    User app
    Port 22
    ProxyJump bastion.company.com
    ProxyCommand ssh -W %h:%p Jumphost
    IdentityFile ~/.ssh/production_key
    Compression yes
    ServerAliveInterval 300
    BatchMode yes
```

### 支持的 SSH 选项

ctty 支持所有标准 SSH 配置选项：

**内置字段：**
- `HostName` - 服务器主机名或 IP 地址
- `User` - SSH 连接用户名
- `Port` - SSH 端口号
- `IdentityFile` - 私钥文件路径
- `ProxyJump` - 跳板机连接隧道（如 `user@jumphost:port`）
- `ProxyCommand` - 跳板命令连接隧道（如 `ssh -W %h:%p Jumphost`）
- `Tags` - 自定义标签（ctty 扩展）；特殊标签 `hidden` 从 TUI 和 `ctty search` 中隐藏主机，但保持可通过 `ctty <host>` 连接

**额外 SSH 选项：**
你可以在交互式表单的 "SSH Options" 字段中添加任何有效的 SSH 选项。以命令行格式输入（如 `-o Compression=yes -o ServerAliveInterval=60`），ctty 会自动转换为正确的 SSH 配置格式。

**常用 SSH 选项：**
- `Compression` - 启用/禁用压缩（`yes`/`no`）
- `ServerAliveInterval` - 保活消息间隔（秒）
- `ServerAliveCountMax` - 保活消息最大数量
- `StrictHostKeyChecking` - 主机密钥验证（`yes`/`no`/`ask`）
- `UserKnownHostsFile` - known_hosts 文件路径
- `BatchMode` - 禁用交互式提示（`yes`/`no`）
- `ConnectTimeout` - 连接超时（秒）
- `ControlMaster` - 连接多路复用（`yes`/`no`/`auto`）
- `ControlPath` - 控制套接字路径
- `ControlPersist` - 保持连接存活时长
- `ForwardAgent` - 转发 SSH agent（`yes`/`no`）
- `LocalForward` - 本地端口转发（如 `8080:localhost:80`）
- `RemoteForward` - 远程端口转发
- `DynamicForward` - SOCKS 代理端口转发

**表单中的示例用法：**
```
SSH Options: -o Compression=yes -o ServerAliveInterval=60 -o StrictHostKeyChecking=no
```

会自动转换为：
```ssh
    Compression yes
    ServerAliveInterval 60
    StrictHostKeyChecking no
```

### 应用配置

ctty 支持配置文件来自定义行为，包括快捷键和更新检查。

**配置文件位置：**
- **Linux/macOS**: `~/.config/ctty/config.json`
- **Windows**: `%APPDATA%\ctty\config.json`

**示例配置：**
```json
{
  "check_for_updates": false,
  "key_bindings": {
    "quit_keys": ["q", "ctrl+c"],
    "disable_esc_quit": true
  }
}
```

**可用选项：**
- **check_for_updates**: 布尔值，启用或禁用启动时的自动更新检查。默认：`true`。在离线机器上设为 `false` 可避免连接延迟。
- **quit_keys**: 退出应用的按键数组。默认：`["q", "ctrl+c"]`
- **disable_esc_quit**: 布尔值，禁用 ESC 键退出应用。默认：`false`

**Vim 用户：**
如果你经常误按 ESC 导致应用退出，将 `disable_esc_quit` 设为 `true`。这会禁用 ESC 作为退出键，同时保留其他所有功能。

**离线机器：**
如果 ctty 因连接 GitHub 时的 DNS 超时而启动缓慢，将 `check_for_updates` 设为 `false`。也可以使用 `--no-update-check` CLI 标志进行一次性覆盖，无需编辑配置文件。

**默认配置：**
如果没有配置文件，ctty 会自动创建一个保持向后兼容的默认配置。

## 🛠️ 开发

### 前置要求

- Go 1.23+
- Git

### 从源码构建

```bash
# 克隆仓库
git clone https://github.com/zsuroy/ctty.git
cd ctty

# 构建二进制
go build -o ctty .

# 运行
./ctty
```

### 项目结构

```
ctty/
├── main.go             # 应用入口
├── cmd/                # CLI 命令 (Cobra)
│   ├── root.go         # 根命令和交互模式
│   ├── add.go          # 添加主机命令
│   ├── edit.go         # 编辑主机命令
│   ├── move.go         # 移动主机命令
│   ├── serial.go       # 串口管理子命令
│   └── search.go       # 搜索命令
├── internal/
│   ├── config/         # SSH 配置管理
│   │   └── ssh.go      # 配置解析和操作
│   ├── connectivity/   # SSH 连通性检查
│   │   └── ping.go     # 异步 SSH ping 功能
│   ├── history/        # 连接历史跟踪
│   │   ├── history.go  # 历史管理和最后登录跟踪
│   │   └── port_forward_test.go # 端口转发历史测试
│   ├── serialconfig/   # 串口设备配置和连接
│   │   ├── serial.go   # 设备配置存储 (~/.config/ctty/serial.json)
│   │   ├── ports.go    # 端口枚举和辅助函数
│   │   ├── connect.go  # 串口连接桥接 (ExecCommand)
│   │   ├── raw_unix.go # POSIX raw 终端模式
│   │   └── raw_windows.go # Windows stub
│   ├── version/        # 版本检查和更新
│   │   ├── version.go  # GitHub 发布检查和版本比较
│   │   └── version_test.go # 版本解析和比较测试
│   ├── ui/             # 终端 UI 组件 (Bubble Tea)
│   │   ├── tui.go      # 主 TUI 界面和程序设置
│   │   ├── model.go    # 核心 TUI 模型和状态
│   │   ├── update.go   # 消息处理和状态更新
│   │   ├── view.go     # UI 渲染和布局
│   │   ├── table.go    # 主机列表表格组件和状态指示
│   │   ├── add_form.go # 添加主机表单界面
│   │   ├── edit_form.go# 编辑主机表单界面
│   │   ├── move_form.go# 移动主机表单界面
│   │   ├── port_forward_form.go # 端口转发设置和历史
│   │   ├── styles.go   # Lip Gloss 样式定义
│   │   ├── sort.go     # 排序和过滤逻辑
│   │   ├── serial_form.go     # 串口设备列表和连接 UI
│   │   ├── serial_add_form.go# 添加串口设备表单
│   │   └── serial_connect_form.go # 连接前参数编辑表单
│   └── validation/     # 输入验证
│       └── ssh.go      # SSH 配置验证
├── images/             # 文档资源
│   ├── logo.svg        # 项目 logo
│   └── ctty.gif        # 演示动画
├── install/            # 安装脚本
│   ├── unix.sh         # Unix/Linux/macOS 安装器
│   └── README.md       # 安装指南
├── .github/            # GitHub 配置
│   ├── copilot-instructions.md # 开发指南
│   └── workflows/      # CI/CD 流水线
│       └── release.yml # GoReleaser 发布
├── go.mod              # Go 模块定义
├── go.sum              # Go 模块校验
├── .goreleaser.yaml    # GoReleaser 配置
├── LICENSE             # MIT 许可证
├── CHANGELOG.md        # 变更记录
├── README.md           # 项目文档（英文）
└── README_CN.md        # 项目文档（中文）
```

### 依赖

- [Cobra](https://github.com/spf13/cobra) - CLI 框架
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI 框架
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI 组件
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - 样式
- [Go Crypto SSH](https://golang.org/x/crypto/ssh) - SSH 连通性检查
- [go.bug.st/serial](https://github.com/bugst/go-serial) - 跨平台串口通信

## 📦 发布

自动为多平台构建发布版本：

| 平台 | 架构 | 下载 |
|------|------|------|
| Linux | AMD64 | [ctty-linux-amd64.tar.gz](https://github.com/zsuroy/ctty/releases/latest/download/ctty-linux-amd64.tar.gz) |
| Linux | ARM64 | [ctty-linux-arm64.tar.gz](https://github.com/zsuroy/ctty/releases/latest/download/ctty-linux-arm64.tar.gz) |
| macOS | Intel | [ctty-darwin-amd64.tar.gz](https://github.com/zsuroy/ctty/releases/latest/download/ctty-darwin-amd64.tar.gz) |
| macOS | Apple Silicon | [ctty-darwin-arm64.tar.gz](https://github.com/zsuroy/ctty/releases/latest/download/ctty-darwin-arm64.tar.gz) |
| Windows | AMD64 | [ctty-windows-amd64.zip](https://github.com/zsuroy/ctty/releases/latest/download/ctty-windows-amd64.zip) |
| Windows | ARM64 | [ctty-windows-arm64.zip](https://github.com/zsuroy/ctty/releases/latest/download/ctty-windows-arm64.zip) |

## 🤝 贡献

欢迎贡献！请随时提交 Pull Request。对于重大更改，请先开 issue 讨论你想更改的内容。

### 开发流程

1. **Fork** 仓库
2. **创建** 功能分支（`git checkout -b feature/amazing-feature`）
3. **提交** 你的更改（`git commit -m 'Add amazing feature'`）
4. **推送** 到分支（`git push origin feature/amazing-feature`）
5. **提交** Pull Request

## 📝 许可证

本项目基于 MIT 许可证 —— 详见 [LICENSE](LICENSE) 文件。

## 🙏 致谢

本项目是 [SSHM](https://github.com/Gu1llaum-3/sshm) 的 Fork，由 [@Gu1llaum-3](https://github.com/Gu1llaum-3) 创建。我们感谢使 ctty 成为可能的原始工作。

- [Charm](https://charm.sh/) 提供优秀的 TUI 库
- [Cobra](https://cobra.dev/) 提供出色的 CLI 框架
- [@Gu1llaum-3](https://github.com/Gu1llaum-3) 创建了 SSHM，ctty 的基础
- [@yimeng](https://github.com/yimeng) 贡献 SSH Include 指令支持
- [@ldreux](https://github.com/ldreux) 贡献多词搜索功能
- [@qingfengzxr](https://github.com/qingfengzxr) 贡献自定义快捷键支持
- Go 社区构建了这些出色的工具

---

<div align="center">

**由 [zsuroy](https://github.com/zsuroy) 用 ❤️ 制作**

⭐ **如果对你有用，请给个 Star！** ⭐

</div>
