# dragfm-gui

Wails + React 双端点文件管理器：两栏分别选择本机或保存的 FlySSH 多跳 SSH 路由，每栏有持久 PTY；第三栏显示 Running、Pending、事件输出和脱敏 History。原 `dragfm` TUI 不替换，旧 Fyne 原型保留但不是当前交付入口。

## 下载与运行

公开候选包在本仓库 Releases。选择 `darwin/linux/windows` 与 `amd64/arm64` 对应包，解压后运行 `dragfm-gui` 或 `dragfm-gui.exe`。六个平台都需要真实原生窗口验收，不能只交叉编译后上传；发布时解压后的成品还会在另一组原生 runner 上再次运行。

首次运行创建主密码并填写非空 hint。测试候选前备份已有 vault。程序旁已有保险库优先，否则检查系统应用数据目录已有保险库；新建先尝试程序旁，再回退系统数据目录。主密码每次启动输入；手动锁定和退出会取消任务、保存脱敏历史并释放凭据。Pending 不跨重启恢复。

**系统依赖与签名：** Linux 包基于 Ubuntu 24.04，需要桌面环境、GTK 3 和 WebKitGTK 4.1；Windows 需要 WebView2，原生测试使用 Windows Server 2025 x64 / Windows 11 ARM64；macOS 原生测试使用 macOS 15，编译部署目标为 11.0。单文件应用不代表没有操作系统图形依赖。macOS 是 ad-hoc 签名，不是 Developer ID / notarization；Windows 未配置 Authenticode 发布者证书。应用不会关闭 Gatekeeper 或 SmartScreen。详细边界见 [发布说明](docs/RELEASE_NOTES.md)。

## 日常操作

路径框可编辑和部分选中，回车导航。点击文件选择，双击目录进入。文件区可滚动浏览名称、大小、修改时间和权限；大目录使用虚拟列表，不创建全部 DOM 行。

把文件拖到另一栏：目录行高亮时放入该目录，空白区域放入当前目录。松开后确认复制或移动；重名时明确确认覆盖，目录覆盖使用合并语义。复制保留源；跨机移动校验源和目标后才删除源。

文件区 `Backspace` 返回上一级，无需选中；`d` 打开删除确认；`h` 计算 SHA-256。路径框、配置编辑器和终端保留原有文字输入键。`Ctrl+Shift+T` 切换默认/亮/暗主题，`Ctrl+Shift+L` 锁定保险库。

每栏持续运行本地或 SSH PTY，加载登录环境，Shell 的 cd 与文件栏双向同步。运行程序或编辑未提交输入时，程序拒绝向 PTY 注入 cd；回到提示符后再导航。原生测试覆盖空格、单引号、方括号和 `$` 路径；Windows 使用 PowerShell `-LiteralPath`，不发送 POSIX 命令。

第三栏命令可选当前栏、左栏、右栏或控制机。任务单并发，排队项可取消，运行项也可取消。实际字节进度可用时显示进度；仅有 wire I/O 或阶段信息时显示活性，不用动画/协议字节伪造文件完成百分比。

## 加密 Markdown 配置

单个无空行 Markdown，物理行号、不软换行，保存时验证并去掉空行：

```md
#主机
##单跳主机
user:"EXAMPLE_PASSWORD"@192.0.2.10:22,/bin/bash
##多跳主机
jump@192.0.2.20:22 user@192.0.2.30:22 --keys ",work-key" ,/bin/zsh
#私钥
##work-key
-----BEGIN OPENSSH PRIVATE KEY-----
...替换为自己的有效私钥...
-----END OPENSSH PRIVATE KEY-----
#socks池
##proxy1
user:EXAMPLE_PROXY_PASSWORD@192.0.2.40:1080
```

`--keys` 的逗号位置对应每一跳，名称引用同一保险库的私钥，不是磁盘路径；上例第一跳没有配置私钥，第二跳使用 `work-key`。不指定 Shell 时使用远端登录默认 Shell。私钥示例是格式说明，必须替换为有效密钥才能保存。

可选字段包括 `###sudo密码`、`###root用户`、`###root密码`、`###默认socks`、`###禁用`；加密私钥使用 `###口令` 和 `###私钥`。SOCKS 使用 `user:pass@ip:port`，没有认证时可写 `ip:port`；URL 特殊字符使用百分号编码。用户的原始格式和安全决策保存在 [USER_REQUIREMENTS.md](docs/USER_REQUIREMENTS.md)。

私钥解锁后可查看/复制，密码默认遮罩；切换遮罩不修改草稿。每个 SSH 跳点独立确认指纹，已保存指纹变化会阻断重连。只明确选择保存的凭据进入加密保险库，会话密码只在内存缓存。vault 使用 Argon2id + XChaCha20-Poly1305，hint 明文；正文原子写入，限制当前用户访问，对未认证的 KDF 参数和密文体积设置上限。

## 传输、安全与清理

Linux SSH 两端按 direct → SOCKS → 已知/缓存 SSH relay → 官方 Hans → 控制机有界内存中转的顺序尝试。每层重放普通推送、普通拉取、源提权推送、目标提权拉取，以及 rsync、内置 SCP、认证加密流、ncat 载体。SOCKS/relay/Hans 的 ncat 运行在安全载体内，不把代理口令放进进程参数。

候选探测从实际发起端执行三次 TCP 探测后排序。SSH 自动候选限于本次解锁已经成功登录的主机和该端点对曾成功使用的 relay；有效缓存先复用，失效后移除再探测，不遍历登录整个配置表。官方 Hans v1.7.0 使用固定资源校验和、随机网段/强口令、固定服务端指纹和强制 v5，一端 scoped sudo server，另一端 userspace SOCKS client，角色可反转；启动前明确确认其提权/ICMP 影响。

目标使用随机同目录 partial 和原子提交，目录合并保留无关目标文件。跨机移动逐文件校验 SHA-256 与清单、源 inode/文件标识、大小/mtime/链接目标；源变化时保留源并报告已复制但未移动。父目录物理别名不能隐藏递归自复制；归档不能通过 symlink 写出暂存范围。

控制机中转只使用有界缓冲区，不把传输内容写入本地中转文件。受保护本机目录仅在用户确认后使用本任务范围的 sudo；取消/拒绝不能导致源被删除。命令取消会终止 owned 进程组/SSH command；无响应 SFTP 的进行中请求取消会关闭失效 transport，后续使用重新连接。正常空闲 reader 的取消不会关闭其他会话。

清理只针对本程序拥有的 session/listener/partial/helper/Hans 资源。网络完全不可达时，不能保证马上删除远端文件；不会扩大路径删除范围来掩盖失败。原 TUI、旧 Fyne 离线麒麟 libGL 问题和未授权的服务器 Web UI 不属于这一版变更。

## 可复现构建与验收

构建依赖 Go 1.26、Node.js 24 和对应原生平台工具链；Go 依赖已 vendor，前端使用 lockfile。构建脚本先重新生成四个 Linux agent，再编译 frontend 并嵌入当前控制端。macOS 本机构建：

```sh
npm --prefix frontend ci
DRAGFM_MAC_ARCH=arm64 ./build/build-wails-macos.sh dist
# Intel Mac 使用 DRAGFM_MAC_ARCH=amd64
```

Linux/Windows 具体原生命令保存在 [platforms.yml](.github/workflows/platforms.yml)。`cmd/dragfm-wails` 是新版入口；`cmd/dragfm-gui` 与 `build/build-all.sh` 是保留的 Fyne 原型。

测试/build/release 全部由 GitHub-hosted workflows 执行。`build.yml` 验证 Go/race/vet/frontend 和完整 macOS SSH 窗口；`ssh-integration.yml` 验证实际 OpenSSH/权限/代理/Hans/失败矩阵；`platforms.yml` 验证六平台真实窗口；`release.yml` 校对源码、二进制与证据 SHA，打包后再次在六个平台解压运行，上传后下载全部 assets 校验，最后发布非 draft 的候选版本。

每个 release 提供六个平台包、对应源码、`PROVENANCE.json`、两份测试证据 ZIP 和 `SHA256SUMS`。验收场景与复审修复映射见 [ACCEPTANCE.md](docs/ACCEPTANCE.md)，外部证书和环境边界见 [UNFINISHED.md](docs/UNFINISHED.md)。源码存在不代表当次 CI 成功，以对应 revision 的 workflow 和已发布证据为准。

## 许可证

GPL-3.0-only；FlySSH 复用部分为 MIT，官方 Hans 及其他依赖遵循各自许可证。成品包含 `LICENSE`、`THIRD_PARTY_NOTICES.md`、汇总依赖许可证与对应源码；详见 vendor、`go.mod`、`frontend/package-lock.json`。
