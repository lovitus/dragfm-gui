# dragfm-gui

dragfm-gui 是一个面向 Windows、macOS 和 Linux 控制端的双栏文件管理器。主界面使用 Wails + React，文件表格、路径框、拖放命中和终端均按桌面文件管理器重做；每一栏都可以是本机或一条保存在加密保险库中的 FlySSH 多跳路由。第三栏显示当前任务、输出、等待队列和脱敏历史。

本项目不会替换 `dragfm` TUI。两者可以并存。

## 主要操作

- 在路径框中输入路径并回车；路径框和栏内 PTY 终端的当前目录双向同步。
- 文件栏处于当前栏时，`Backspace` 返回上一级，不要求先选中文件；路径框、终端和配置编辑器中的 `Backspace` 始终保留原生删除行为。
- 单击选择文件，双击进入目录；把文件横向拖向另一栏。悬停在目标栏的目录行上时该目录会高亮，松开后文件放入该目录；在空白处松开则放入目标栏当前打开的目录。
- 目标存在时对话框变为“复制并覆盖 / 移动并覆盖 / 取消”；目录覆盖采用合并语义。
- `SHA-256` 计算选中项或目录清单；删除操作需要确认。
- 当前文件栏选中项目后可按 `d` 打开删除确认，按 `h` 将 SHA-256 任务加入队列；输入框、终端和配置编辑器保留正常的字母输入。
- 第三栏命令框可选择左栏、右栏或控制机。所有任务全局单并发，其余任务进入 Pending。
- `Ctrl+Shift+L` 手动锁定；`Ctrl+Shift+T` 在默认颜色、亮色和暗色之间切换。

## Markdown 配置

连接设置是一个带物理行号、不软换行的 Markdown 编辑器。保存后会去掉空行。`##` 后是名称，紧接的内容属于该名称：

```md
#主机
##主机名1
uhome:"EXAMPLE_PASSWORD_1"@10.1.1.100:41122,/bin/bash
##主机名2
uhome:"EXAMPLE_PASSWORD_2"@1.2.3.4:41122  uhome:"EXAMPLE_PASSWORD_3"@5.6.7.8:41122   uhome@1.1.2.2:41122 --keys ",,keyname1" ,/bin/zsh
##主机名3我不写shell你用登录默认的
1:1@1.1.1.1:22
#私钥
##keyname1
abc...
#socks池
##socksname1
user1:pass1@1.1.1.1:1080
```

一个“主机”可以是一跳，也可以是一整条 FlySSH 多跳路由。主机行末尾的 `,/bin/bash` 或 `,/bin/zsh` 指定交互 Shell；不写则使用登录默认 Shell。`--keys` 的逗号位置和 SSH 跳数严格对齐，名称引用 `#私钥` 分区里的同名私钥，不是磁盘文件。没有认证的 SOCKS 可直接写 `ip:port`；密码含 `@`、`:` 等 URL 特殊字符时使用百分号编码。

条目还可使用 `###sudo密码`、`###root用户`、`###root密码`、`###默认socks` 和 `###禁用`。带口令私钥使用 `###口令` 与 `###私钥`；未使用这些字段的原始短格式保持不变。

每个 SSH 跳点都独立执行 TOFU 指纹确认。保存后的任一跳指纹发生变化都会阻断连接。

所有已经配置的 SSH 主机都具备跳板能力，但程序不会为了探测而遍历登录全部主机。自动候选只包括本次解锁期间已成功连接过的 SSH 会话，以及这对端点以前实际成功使用过的跳板。成功关系仅以主机 ID 加时间戳保存在加密保险库中；缓存跳板失效时删除该关系，再从已知会话中选择，减少无意义的 SSH 暴露。

本地终端启动用户默认 Shell 的登录式交互会话：macOS 的 zsh 会依次加载用户的 `.zshenv`、`.zprofile`、`.zshrc` 和 `.zlogin`，bash 会加载登录 profile 后进入带 `.bashrc` 的交互会话。cwd 同步通过临时启动文件安装，不向终端输入可见命令，退出时清理。SSH 终端读取远端 `$SHELL` 后使用 login + interactive 模式。

## 传输与移动安全

- 同一机器且无目录合并冲突时，移动使用原生 rename/mv 语义，所有路径均为端点解析后的绝对路径。
- 本机与 SSH 端点之间优先尝试 FlySSH 内置 SCP；数据先到随机 `.dragfm-partial-*`，成功后再改名。
- 其他组合使用控制机有界内存流，不在控制机创建中转文件。
- 传入控制机本地受保护目录前会先做可写探测；普通 SCP/内存流不会重复撞同一个权限错误。用户确认后可仅为本任务使用 sudo，密码走独立 stdin，文件数据走另一匿名管道，并继续采用同目录 partial + 原子改名。
- 跨机移动逐文件计算 SHA-256，并比较相对路径、类型、大小、符号链接目标和内容哈希。源快照变化或目标清单不一致时保留源，并报告“已复制但未移动”。
- 取消会关闭任务上下文、SSH session 和 partial writer，并做范围受限的清理。
- 两个 Linux SSH 端点会按以下层级执行：同机原生操作；正向/反向及普通/root SSH/scoped sudo 的 rsync、内置 SCP、TLS 1.3 加密 agent 流与直接链路 tar+ncat；认证 SOCKS 池；本次解锁期间已成功登录或按主机对缓存的 SSH 会话池；Hans v1.7.0；最后是控制机有界内存中转。SOCKS、SSH 跳板和 Hans 层只重放 rsync、SCP 与认证加密流；不会为了调用系统 ncat 而把代理凭据写进进程参数。
- SOCKS 与 SSH 会话候选由实际发起端各做三次 TCP 探测，至少两次成功后取中位延迟。成功的 SOCKS 记录延迟和时间；成功的 SSH 跳板只按两端主机 ID 记录，不会为了探测而登录整个主机表。
- 四种 Linux 架构的临时 agent 由本项目构建；Hans 使用官方 v1.7.0 release 的静态 musl 二进制，构建脚本固定官方 URL 与 SHA-256。凭据通过已加密的 helper 协议传入，不进入参数、环境变量或远端文件；Hans 口令通过匿名继承文件描述符交给官方二进制。
- Hans 只在用户确认 ICMP/提权影响后进入：选择一端 root 或 scoped sudo 作为 server，另一端使用无需 TUN 的 userspace SOCKS client，随机网段与强口令、独立身份、服务端指纹固定并强制 v5；角色失败时反转。

## 保险库

保险库正文使用 Argon2id 派生密钥并由 XChaCha20-Poly1305 整体加密。hint 位于明文头部。查找顺序：程序旁已有保险库、系统应用数据目录已有保险库、程序旁新建、最后回退系统应用数据目录。文件以当前用户权限原子保存。

每次启动都要求主密码；运行中只在退出或手动锁定时重新锁定。私钥在解锁后允许查看和复制，密码与含密码的路由默认遮罩。Pending 不跨重启恢复，History 仅持久化脱敏摘要。

## 构建

当前优先交付的是 Wails 界面。需要 Go 1.26、Node.js、npm 和 Xcode Command Line Tools；源码包含 Go vendor 目录，因此构建不依赖 FlySSH 工作区路径：

```sh
./build/build-wails-macos.sh
```

脚本先编译 `frontend/`，再用 `production` 标签生成 `dist/dragfm-gui-wails-darwin-arm64` 单文件，并嵌入 `NSHighResolutionCapable` Info.plist。现阶段不会自动构建或启动其他平台版本；按当前约定先交付 macOS arm64，界面验证通过后再恢复六平台发布矩阵。

旧 Fyne 原型仍保留在 `cmd/dragfm-gui`，原有 `build/build-all.sh` 也没有删除；它不再是当前视觉验收入口。

## 许可证

dragfm-gui 使用 GNU GPL v3.0。FlySSH 的复用代码使用 MIT 许可证。Wails、React、xterm.js、CodeMirror、Fyne 原型及其他依赖的声明见 `THIRD_PARTY_NOTICES.md`、`go.mod`、`vendor/modules.txt` 和 `frontend/package-lock.json`。
