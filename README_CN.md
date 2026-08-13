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
- **嫌 Tabby 太重？** ctty 是单个约 15MB 的二进制文件，没有 Electron，没有浏览器引擎 —— 纯 Go
- **需要一个工具同时搞定串口 + SSH + SFTP？** 大多数终端模拟器只做 SSH；ctty 三者全覆盖
- **想留在终端里？** 不用在多个应用间切换 —— 一切都由键盘驱动

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
- **📈 连接历史** - 记录 SSH 连接和最后登录时间
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

### 串口连接

按 `t` 进入串口设备管理器。可用串口端口自动检测并立即列出 —— 无需手动设置即可快速访问。

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

**编辑参数：**
- **波特率** - 可直接输入，或用 `←/→` 在常用值间切换（9600/19200/38400/57600/115200/230400/460800/921600）
- **数据位** - 5、6、7 或 8（默认：8）
- **校验** - `none`、`even` 或 `odd`（默认：`none`）
- **停止位** - 1 或 2（默认：1）

**连接：** TUI 暂停并将终端直接桥接到串口。按 `Ctrl+]` 或 `Ctrl+C` 断开并返回 TUI。

也可以直接启动串口管理器：
```bash
ctty serial    # 跳过 SSH 主机列表，直接进入串口设备管理
```

串口设备配置存储在 `~/.config/ctty/serial.json`，与 `~/.ssh/config` 分离。

### 命令行用法

```bash
# 启动交互式 TUI
ctty

# 直接连接到指定主机
ctty my-server

# 在远程主机上执行命令
ctty my-server uptime

# 强制 TTY 分配
ctty -t my-server sudo systemctl restart nginx

# 使用自定义 SSH 配置文件
ctty -c /path/to/custom/ssh_config

# 添加主机
ctty add

# 编辑主机
ctty edit my-server

# 搜索主机
ctty search

# 查看版本
ctty --version
```

## 🛠️ 开发

### 前置要求

- Go 1.23+
- Git

### 从源码构建

```bash
git clone https://github.com/zsuroy/ctty.git
cd ctty
go build -o ctty .
./ctty
```

### 依赖

- [Cobra](https://github.com/spf13/cobra) - CLI 框架
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI 框架
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI 组件
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - 样式
- [Go Crypto SSH](https://golang.org/x/crypto/ssh) - SSH 连接检测
- [go.bug.st/serial](https://github.com/bugst/go-serial) - 跨平台串口通信

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
