# dragfm-gui

Wails + React 双端点文件管理器：两栏分别选择本机或保存的 FlySSH 多跳 SSH 路由，每栏有持久 PTY；第三栏显示 Running、Pending、事件输出和脱敏 History。原 `dragfm` TUI 不替换，旧 Fyne 原型保留但不是当前交付入口。

## 下载与运行

历史候选包在本仓库 Releases；当前 Issue #3 新版以 PR #4 的具体候选 SHA 和验收证据为准，不把旧版 release 当作本轮交付。按用户要求，本轮先构建、验证 macOS arm64，用户确认后再启动其他平台/架构；工作流中的六平台发布入口保留供之后明确授权使用，本轮不运行。平台包解压后运行 `dragfm-gui` 或 `dragfm-gui.exe`。最终发布仍需对应平台的真实窗口及解压成品验收，不能只凭交叉编译通过。

首次运行创建主密码并填写非空 hint。测试候选前备份已有 vault。程序旁已有保险库优先，否则检查系统应用数据目录已有保险库；新建先尝试程序旁，再回退系统数据目录。主密码每次启动输入；手动锁定和退出会取消任务、保存脱敏历史并释放凭据。Pending 不跨重启恢复。

**系统依赖与签名：** Linux 包基于 Ubuntu 24.04，需要桌面环境、GTK 3 和 WebKitGTK 4.1；Windows 需要 WebView2，原生测试使用 Windows Server 2025 x64 / Windows 11 ARM64；macOS 原生测试使用 macOS 15，编译部署目标为 11.0。单文件应用不代表没有操作系统图形依赖。macOS 是 ad-hoc 签名，不是 Developer ID / notarization；Windows 未配置 Authenticode 发布者证书。应用不会关闭 Gatekeeper 或 SmartScreen。详细边界见 [发布说明](docs/RELEASE_NOTES.md)。

## 日常操作

路径框可编辑和部分选中，回车导航。点击文件选择，双击目录进入。文件区可滚动浏览名称、大小、修改时间和权限；大目录使用虚拟列表，不创建全部 DOM 行。本批改为与 [ls -allt 的时间排序](https://www.gnu.org/s/coreutils/manual/html_node/Sorting-the-output.html)一致的修改时间从新到旧，包含隐藏项、不另将目录提前；时间相同时按原始名称字节顺序稳定排列，不跟随各远端的 locale。共享规则覆盖本机、SFTP 和 Linux POSIX 列表回退，尚待 hosted 回归与成品界面验收。现有 Linux 回退依赖 GNU find 的 NUL 分隔输出，本批补回数值 UID/GID，并按[可变长度的十进制秒字段](https://www.gnu.org/software/findutils/manual/html_node/find_html/Time-Components.html)读取修改时间，避免经过浮点数损失纳秒；这不增加 SFTP v3 自身只有整秒的时间精度。

把文件拖到另一栏：目录行高亮时放入该目录，空白区域放入当前目录。松开后确认复制或移动；重名时明确确认覆盖，目录覆盖使用合并语义。复制保留源；跨机移动校验源和目标后才删除源。

文件区 `Backspace` 返回上一级，无需选中；`d` 打开删除确认；`h` 计算 SHA-256。路径框、配置编辑器和终端保留原有文字输入键。`Ctrl+Shift+T` 切换默认/亮/暗主题，`Ctrl+Shift+L` 锁定保险库。

每栏持续运行本地或 SSH PTY，Shell 的 cd 与文件栏双向同步。运行程序、编辑未提交输入或尚未出现首次提示符时，程序拒绝向 PTY 注入 cd；回到提示符后再导航。导航保留为正常的 cd 命令记录，不人工擦行；界面错误显示在终端旁，不改动 Shell 的光标和历史输出。Windows 使用 PowerShell `-LiteralPath`，不发送 POSIX 命令。

Bash 在同一个交互进程中按 `/etc/profile` → 首个可读的 `.bash_profile` / `.bash_login` / `.profile` 顺序加载登录环境，保留别名、函数和 Shell 选项；是否读取 `.bashrc` 由这些用户配置决定，不额外重复读取。由于 Bash 的登录模式不读取 `--rcfile`，这里采用初始化文件集成：`$-` 包含 `i`，但 `shopt login_shell` 不启用，也不自动运行 `.bash_logout`。Zsh 使用 `-l -i`，跟随用户启动文件改变的 `ZDOTDIR`，启动完成后不保留临时 ZDOTDIR。上述新回归尚待本批 GitHub-hosted 验证，不能用旧原生冒烟替代。

cwd 同步消息使用每会话随机标记和 Base64 路径；Unix Shell 需要系统的 `base64` 命令。普通文件输出中的旧标记或其他会话标记不能解锁 GUI 导航。随机标记不是针对同一用户恶意程序的权限隔离。目录名的空白/控制字符不会被路径输入和同步协议悄悄删改。

第三栏命令与两栏交互终端分工不同：全局命令按队列执行，完整输出行在运行中持续显示，未换行的尾部在退出或取消时显示；需要交互输入的程序请使用对应文件栏的 PTY。Pending 显示脱敏命令预览，History 可点击查看完整说明、最终方法、状态及输出，取消不再显示成普通失败。实时输出保留最后 64 KiB，重启历史每条保留最后 16 KiB；事件丢失时由任务快照恢复正文，不重复追加整段输出。stdout/stderr 分别处理跨块密码、跨行口令和私钥后再合并；单行达到 64 KiB 时明确提示并停止记录该输出流，继续排空输出以免阻塞命令。此限制不影响文件传输，也不限制交互终端。

SOCKS、SSH 跳板及 Hans 的池选择和实际方法分开上报；最终记录保留真正使用的方向、方法和路由。只有实际文件字节统计才显示百分比，未知阶段显示活动条；通道 I/O 不换算成文件完成百分比，校验阶段单独显示。上述本批第三栏改动及真实子进程回归尚未执行 hosted 红绿或原生 GUI 验收。

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

可选字段包括 `###sudo密码`、`###root用户`、`###root密码`、`###root私钥`、`###默认socks`、`###禁用`、`###允许跳板`；加密私钥使用 `###口令` 和 `###私钥`。删掉字段即清除该设置，不继承隐藏旧值。SOCKS 使用 `user:pass@ip:port`，没有认证时可写 `ip:port`；简写中的密码按字面处理，`%`、`#`、`@` 不需要 URL 编码（最后一个 `@` 分隔地址）。IPv6 地址写为 `[::1]:1080`。显式 `socks5://` URL 仍使用百分号编码；v1 保险库迁移时给旧代理串补上该前缀，保留原有密码字节。用户的原始格式和安全决策保存在 [USER_REQUIREMENTS.md](docs/USER_REQUIREMENTS.md)。

`###root私钥` 下面每个物理行是一个 `#私钥` 中的名称，不是磁盘路径，也不是 `--keys` 的逐跳逗号串。它只用于最后一跳的高权账户；省略 `###root用户` 时默认为 root，可不配置 root 密码。选择显式 root 私钥后只尝试这些私钥，不附带普通账户的私钥或控制机 agent；多跳路由前面的账户和指纹不变。未显式选择时，同一账户仍可复用原凭据，换账户则不能借用原账户凭据。采用现有 [FlySSH Credentials.PrivateKeys](https://github.com/lovitus/flyssh/blob/9f299339930bbb7589ad0596f699d0d5ba7a5bc9/pkg/connector/connector.go)，不另写认证协议。保存时校验名称和密钥/口令，缺失或重复引用拒绝保存；删除字段清除关联。Pending 任务持有入队时的密钥快照，编辑影响新任务，不中途换掉旧任务身份。此链路新增真实 OpenSSH/队列验收场景尚未运行，不能据此视为已验收。

远端 helper 发起 SSH 时，普通路由允许先用该进程实际用户的 `~/.ssh/id_ed25519`、`id_ecdsa`、`id_rsa`，再结合该跳显式 vault 私钥，随后才是密码/keyboard-interactive。主目录取实际 UID 的账户记录，不信任 sudo 继承的 `HOME`；私钥要求当前 UID 所有、无组/其他用户访问权限、普通文件且不跟随符号链接，`.ssh` 不可被组/其他用户写入。默认加密密钥没有独立口令时跳过，不猜测其他 vault key 的口令。agent socket 也必须属于实际 UID，sudo 不沿用原用户 agent，非交互 agent 等待限五秒。逐跳 `allow_local_identity` 开关在加密协议中保留；显式 root 选钥关闭它，旧消息未带开关时也不自动读本地密钥。Python 池后备遵守相同开关和 UID 规则。不会把发现的私钥传回控制机或写入日志；这不是完整的 OpenSSH `ssh_config` 解释器，自定义 IdentityFile、证书和硬件密钥请使用显式 vault key 或受支持 agent。[OpenSSH 身份配置参考](https://man.openbsd.org/ssh_config#IdentityFile)。真实队列正向/反向 rsync/SCP 免密用例已准备，包含两端均拒绝 agent 的系统工具路径，尚未运行。

原生 rsync/SCP 的高权轮次按 root→对方 root 执行，两端分别确认。对端未配置显式 root 密钥/密码时，仅允许实际已提权至 UID 0 的远端进程使用它自己的免密材料；普通控制机不能解码该策略后借用自己的密钥登录 root。显式 root 选钥仍优先，选中的密钥缺失时不会暗中改用默认密钥。该协议开关不授予 sudo 权限，也不改变前置跳板凭据。双端确认、root 独立密钥与普通账户隔离的队列用例已补，尚未运行。

“连接、池与缓存”页只读取已保存状态，打开/刷新不会探测网络。逐行测试加入同一任务队列，只连接点选的完整 SSH 会话；测试 SOCKS 时需选择一个已保存 SSH 目标，使用该代理替代目标的默认代理，不访问公共检测网站、不自动换跳板。它验证的是**控制机经所选路由登录**，不是远端互通、传输成功或实际发起端的 TCP RTT；SOCKS 测试不改变传输延迟排名，也不让另一个未验证的默认路由自动取得跳板资格。测试可取消，结果进入任务历史。

同一主机列表可停用会话或取消“允许作跳板”（Markdown 为 `###允许跳板` 下一行 `false`）。仅取消跳板资格不会关闭现有浏览/终端；该主机仍可作为文件端点。可以按名称、成功记录及 SOCKS 历史延迟排列，查看并忘记主机对的跳板缓存。缓存可能来自成功连接或传输，只供优先尝试，不证明当前可达。已入队任务沿用冻结配置，新任务才使用策略变更；已有主机引用的默认 SOCKS 不能直接停用，需要先修改该引用。

有未保存文本时，不允许池页覆盖配置；异步操作返回时若已开始编辑，也保留草稿。显式重新载入需要确认丢弃；载入期间的新输入不会被旧响应覆盖。编辑器版本还覆盖其他连接保存密码的变化，冲突时拒绝保存，需自行保留修改后载入合并，不能静默恢复旧密码。上述配置 UI、真实 SSH/SOCKS 队列场景和原生 WebView 操作脚本均已补入，但还未执行 hosted 红绿；DOM 操作脚本不替代系统级键鼠验收。

交互认证成功后，勾选保存的密码写回所属会话的 FlySSH 路由行，在编辑器中可查看和删除；临时拼接跳板不会改变密码归属。未勾选只在本次解锁期间缓存，认证失败不保存。配置保存采用先落盘再生效的事务；保存失败保留原配置，成功修改端点后清除相关连接缓存并重新连接。

v1 隐藏的逐跳密码覆盖值可能已经归属错误。v2 迁移将它们保留为 `###待核对旧密码` 下的 JSON 字符串数组，默认遮罩、可查看和删除，**不会用于认证**。核对所属主机后，将需要的密码填回正确的 FlySSH 路由行，再删掉待核对字段。原路由中明确填写的密码保持不变。试用前请备份，旧版程序不支持 v2。

私钥解锁后可查看/复制，密码默认遮罩；切换遮罩不修改草稿。每个 SSH 跳点独立确认指纹，已保存指纹变化会阻断重连。只明确选择保存的凭据进入加密保险库，会话密码只在内存缓存。vault 使用 Argon2id + XChaCha20-Poly1305，hint 明文；正文原子写入，限制当前用户访问，对未认证的 KDF 参数和密文体积设置上限。

## 传输、安全与清理

Linux SSH 两端按 direct → SOCKS → 已知/缓存 SSH relay → 官方 Hans → 控制机有界内存中转的顺序尝试。每层重放普通推送、普通拉取、源提权推送、目标提权拉取，以及 rsync、内置 SCP、认证加密流、ncat 载体。SOCKS/relay/Hans 的 ncat 运行在安全载体内，不把代理口令放进进程参数。

direct/SOCKS/SSH 池的 rsync/SCP 新增系统工具后备（源码已接入，未验收）：helper 明确失败且允许重试时，复用实际发起端已安装的 Python 3/Paramiko（SOCKS 另需 PySocks）连接已固定指纹的完整对端路由，分别运行两端 `/usr/bin/rsync` 或 `/usr/bin/scp`，不上传可执行文件、不安装软件。SCP 使用系统 `-f/-t` 模式；rsync 使用原生客户端，rsh 子进程只得到匿名 socketpair 描述符与静态脚本，凭据留在父进程的 stdin 内存配置中，没有监听端口或磁盘 token。两条 rsync 路径都把唯一远端操作数固定为任务的精确绝对路径，再做一次 SSH Shell 引用；不使用仍会展开通配符的 `-s`，也不使用关闭部分参数检查的 `--old-args`。`-e` 的引号按 rsync 自身规则处理，与 SSH 引用分开。SCP 遇到符号链接或换行文件名跳过，不默默跟随链接。设计依据为 [rsync 原生 rsh 调度](https://github.com/RsyncProject/rsync/blob/v3.4.1/main.c) 和 [Paramiko 通道/退出状态契约](https://docs.paramiko.org/en/stable/api/channel.html)。核对时上游 rsync 最新为 3.5.1（2026-09-21）；程序不会自行升级远端工具。特殊名称与通配符诱饵相邻文件的真实队列用例尚未运行。

该系统路径仍只原子提交不存在的根目标，已有目录转由安全合并方法处理。实际进程持有已登记工作区的目录锁，传输结束后等待双方真实退出，再核对清单、恢复权限/时间与获准所有权、sync/rename；跨机移动保持逐文件 SHA-256 和原源快照门禁。状态显示实测协议字节，不当作文件百分比。缺失退出状态、被信号结束或清理不确定时停止重试并保留范围受限的暂存资源；控制机不保存传输内容。系统 SCP/rsync 的断线/取消、高权和多跳完整验收仍未完成，不能以普通传输用例代替这些证据。

本批新增的 direct `tar+ncat` 不再依赖临时 agent：两端使用系统 Bash/tar/ncat/sha256sum，源端对实际发送的 gzip 字节流计算哈希，经既有 SSH 返回；目标端先保存有大小上限的归档，哈希一致后才解包到同目录私有工作区、核对清单并提交。控制机不保存归档；目标需要同时容纳压缩归档和解包内容。归档上限同时受清单大小和可用空间限制，预留解包空间，解包前再次检查；磁盘并发使用仍可能造成失败，不保证磁盘预留。它目前只处理新目标，已有目标继续走安全合并方法。只在两端数据接口地址均确认私有时自动允许明文；其他情况另行确认。来源 IP 白名单可能不适用于 NAT 改写地址，这时失败后继续原策略，不放开监听。

本轮评审修正（尚未验收）：系统 tar+ncat 在 rename 前同步接收文件系统，rename 后同步目标父目录，再执行移动删源门禁；同步失败保留源，提交后失败停止换方法。SCP 保留文件名末尾空白，含 CR/LF 的名字转用其他无损方法。同机原生 cp/mv 除 machine-id 与认证指纹外，还要求同一实际 SSH 账户和已核对的根目录 device/inode；缺少证据按跨机处理，避免 [sshd 按账户 chroot](https://man.openbsd.org/sshd_config#ChrootDirectory) 时把目标路径写到源账户的目录视图。SFTP 初次握手受连接超时和取消约束，取消新连接或任务 Fork 不关闭原浏览连接。

系统 tar+ncat 的新工作区使用 v2 所有权标记和目录 `flock`：任务的独立 SSH 会话跨命令持有共享锁，tar/ncat/解包进程继承自己的目录锁；取消先等待进程，再释放会话锁。清理必须取得独占锁，并重新核对目录/父目录 inode、执行 UID、私有权限和标记内容，不仅依据 PID 或目录年龄。目录锁沿用 [systemd 的临时目录防老化建议](https://github.com/systemd/systemd/blob/main/docs/TEMPORARY_DIRECTORIES.md#automatic-clean-up)，不依赖安装 systemd；需要 Linux 的系统 `flock`，不可用则该方法失败并保留原策略后备。旧 v1 即使 helper PID 已退出，也不能排除孤儿子进程；这类遗留目录保留，不按年龄自动删除。已准备持锁→保留→退出→清理的真实内核锁/SSH 回归，**尚未运行**。

新 helper 安装同样使用 v2 目录锁，但由 Go 调用内核，不要求安装 `flock` 命令。控制模式、独立 SFTP 模式和 rsync transport 在读输入前加锁；启动 Hans、ncat、rsync 时继承目录描述符，不占用 Hans 口令的 fd 3。停机停止接收新操作，等待正在执行的请求、监听及子进程退出；再关闭自身共享描述符，并用另一个打开的目录描述符申请独占锁，防止错误地把子进程的同一把共享锁一起升级。独立 SFTP 先 EOF/Wait；未确认退出、清理失败、安装目录 inode 改变均保留并报告，控制端不再用直接递归删除兜底，也不继续启动下一种传输方法。超时的监听/进程记录不会提前丢弃。设计依据为 [Go 子进程/ExtraFiles 契约](https://pkg.go.dev/os/exec) 及 [Linux 父线程退出信号限制](https://man7.org/linux/man-pages/man2/PR_SET_PDEATHSIG.2const.html)，不把父进程死亡信号当作整棵进程树退出证据。官方 Hans 的真实 SSH 断线、代理被杀且 Hans/SFTP 暂停时保留安装目录，以及已登记 partial 的重连回收场景已准备，尚未执行。

远端发起 SCP/rsync 推送时，对端系统写入进程也通过接收 helper 的受控 server 模式持有目录锁；不额外 sudo，不改变数据协议。Hans 推送使用目标 server helper 登记 partial，拉取使用实际目标 client helper，提交后销账。失败不直接删除仍可能被写入的 partial，留给取得独占锁的 helper 停机清理；SSH 退出状态缺失通过结构化错误传回并停止重试，不按错误文案猜测。[SSH Wait 文档](https://pkg.go.dev/golang.org/x/crypto/ssh#Session.Wait) 明确区分退出失败与缺少退出确认。控制机侧 SCP/rsync 使用独立连接，不因取消中断文件浏览/PTY；未确认停止则保留暂存路径和原始错误。已准备真实写入进程暂停→其 SSH 会话断开→保留 partial/源、不发布目标的 hosted SCP/rsync 回归，尚未运行；不代表遗留 partial 的持久恢复已经完成。FlySSH 的可选受控命令前缀和退出错误补丁记录在 `docs/FLYSSH_PATCHES.md`。

GUI 在创建系统 ncat 工作区、helper 安装或系统 SFTP 租约目录前，将精确路径、父目录身份、所有权标记摘要和实际 SSH 指纹/machine-id 登记到加密 vault；创建后补记 inode，再允许上传/执行。登记保存失败会停止任务，不继续换一条未登记路径；确认回收后才销账，销账失败仍保留记录供下次核对。所有权标记通过 SSH stdin 写入已固定 cwd 的私有目录，不向远端文件写入凭据或传输配置。新建浏览连接时，只为该主机已登记且超过 24 小时的遗留目录排入清理任务，结果进入 Running/Output/History；浏览和 PTY 不复用清理 transport，不恢复旧 Pending。GUI helper 启动不另行扫描其他安装目录。高权记录必须再次确认，弹窗包括相关 partial 路径，跳过/取消保留记录；普通用户上传、sudo 子进程使用的目录仍核对原上传者 UID，只有明确登记并批准的 root 清理可以跨 UID，不能据此扩大扫描。主机、父目录、标记或所有权不符则失败并保留。清理队列满等入队错误显示在文件栏，不阻断浏览。安装前失败的受控清理和重连回收需要系统 `flock`，不可用则保留并报告，不改为无锁删除；正常 helper 运行及退出仍使用自身内核锁。相关真实 SSH/vault/队列回归已准备、未运行，不能当作整个异常恢复已经验收。

helper 和系统 SFTP 的 partial 使用同一加密记录：先保存创建意图，SFTP 排他创建成功后再保存实际 inode，才向调用方返回可写流；提交或确认删除后移除记录。一个已登记的暂存目录覆盖其中的子 partial，合并到已有目录的文件分别登记。重连清理全程持对应租约目录的独占锁，先核对所有独立 partial 的原父目录和 inode，再精确删除；不根据随机名字推断所有权，不通过路径 `chmod` 绕过权限。已存在但没有持久 inode 的 partial、父目录/文件被替换、租约目录丢失而 partial 仍存在均保留并报错。SCP/rsync 或流接收在首次 inode 回报前断线可能只留下创建意图，这种情况不承诺自动删除。普通身份无法删除的只读遗留目录也会保留，不偷偷追加 sudo。未登记的本机/普通 SSH 后备 partial、创建标记不完整的资源及控制机进程直接崩溃的完整矩阵仍未验收。

普通 ncat、加密流及池内 ncat 的共用数据路径新增五个不同高端口的连接重试：连接失败也会换端口，不只是绑定失败；重试前停止并等待旧监听，清除本任务登记的 partial，再生成新的监听和令牌。端口/连接事件单独显示，不伪装成文件完成百分比。实现参考 [Ncat 官方选项及单连接语义](https://nmap.org/book/ncat-man.html)。高权 direct 现接入独立系统文件通道和经批准的非交互命令通道，在私有暂存区恢复数值所有权、模式和 mtime 后才提交；代理池独立命令回退源码也已接入，见下文。禁止 helper 执行、TCP 拒绝后换端口、五端口耗尽、取消清理和双端高权反向队列回归均尚未运行，不是完成或兼容性保证。

候选探测从实际发起端执行三次 TCP 探测后排序。SOCKS/SSH 池的两端探测独立；拒绝 agent 时可使用已安装 Python 3 的 socket 探测，时间在远端测量，不把 SSH 控制链路耗时当 TCP RTT。SSH 自动候选限于本次解锁已经成功登录的主机和该端点对曾成功使用的 relay；有效缓存先复用，失效后移除再探测，不遍历登录整个配置表。官方 Hans v1.7.0 使用固定资源校验和、随机网段/强口令、固定服务端指纹和强制 v5，一端 scoped sudo server，另一端 userspace SOCKS client，角色可反转；启动前明确确认其提权/ICMP 影响。

两端都拒绝 agent 时，池内的 ncat 后备可复用远端**已安装**的 Python 3 + [PySocks](https://github.com/Anorov/PySocks)（SOCKS）或 [Paramiko](https://docs.paramiko.org/en/stable/api/transport.html)（SSH 多跳），不自动安装依赖。优先级仍为 rsync、SCP、加密流、ncat；系统原生方法与 ncat 都按真实胜出方法显示，不把控制机中转伪报为成功直传。静态适配脚本由程序嵌入，v1 JSON 任务配置与凭据只经加密 SSH stdin/fd6 进入内存，不放 argv、环境或远端文件；归档在实际远端之间传输，控制端只收诊断及哈希，后续移动校验沿用原门禁。控制机登录保留完整目标会话链；远端直传只使用对端最后一跳的账户/凭据/指纹，选中跳板池时才在前面加该池的完整链，不重复控制机的接入跳板。SSH 每跳先固定指纹再认证，指纹变化停止重试；普通免密材料限实际执行 UID，sudo 不借用原账户 agent。监听只允许已确认的来源地址；SOCKS 出口 NAT 与代理地址不同时，此受限后备可能失败，但不会改为允许任意连接。最终内存中转仍可继续。池内正向/反向真实阻断、两端拒绝 agent、失效缓存重探测与成功缓存复用的 hosted 场景已补，尚未完成验证。

目标使用随机同目录 partial 和原子提交，目录合并保留无关目标文件。跨机移动逐文件校验 SHA-256 与清单、源 inode/文件标识、大小/mtime/链接目标；源变化时保留源并报告已复制但未移动。父目录物理别名不能隐藏递归自复制；归档不能通过 symlink 写出暂存范围。

R8–R10 安全修复已接入本批候选，尚未验收/交付。最终身份查询失败不能跳过删源条件；普通文件预览/复制仍可使用 SFTP-only 账户，不将未知身份当作已知身份匹配。目录任务确认后用独占未发布 partial 检查两端可见性，预览不写入、不新增连接或权限；探针清理要确认文件/父目录身份与内容，缺能力或并发变动时停止保源。保留初次目录身份及物理路径，完成阶段只读复核，不在已恢复为只读的目录中再次写探针。仅用作载体的既有父目录不恢复旧 mtime，避免要求共享目录所有权或掩盖别人的修改；实际被复制目录只在未观察到并发变化时恢复。这不是未来挂载变化、最后验证与删除之间任意外部竞争或非一致缓存的隔离保证；namespace/ctime 副作用不能撤销。没有文件标识能力的 SFTP-only 目录任务暂不支持，不能以路径拼写不同代替安全判断。

统一移动门禁在最终内容/所有权核验后同步数据、元数据、复制目录以及新建父链和首个既有发布父目录，不打开未修改的更高祖先。范围在复制前记录，失败换方法也不缩小；再次核对原源清单、目录绑定及已校验目标的文件身份后才删除。普通传输和加速完成均使用该门禁。优先 SFTP fsync，缺能力时仅用同一已授权文件视图的 helper 或系统工具；系统等价实现使用带明确路径的 GNU `sync`（逐项 fsync），不用无参数 sync，也不把老内核的 syncfs 当成完整错误确认。依据 [GNU 实现](https://github.com/coreutils/coreutils/blob/master/src/sync.c)及 [Linux syncfs 的错误报告边界](https://man7.org/linux/man-pages/man2/syncfs.2.html)。链接通过其目录发布，不同步链接指向的内容。缺能力、同步失败或退出未确认均保源，不通过换方法掩盖错误；不承诺硬件断电一致性。当前候选请勿移动重要文件。

后续未验修正将强快照的同端身份记录细化到条目，并在该项读取前后核对；最后复查本次复制的全部目标条目，包括复制中新建的子目录，避免只核对根却漏掉子树替换。元数据查询会增加远端移动的校验开销，不用于普通文件预览/复制。旧无 GUI 任务上下文的 helper 方法入口也保留实际获准的源文件视图到最终完成，不为校验另行获取权限。上述修正尚待 hosted 回归及集中复审，不代表所有外部并发修改都有命名空间锁保护。

控制机中转只使用有界缓冲区，不把传输内容写入本地中转文件。受保护本机目录仅在用户确认后使用本任务范围的 sudo；取消/拒绝不能绕过移动的源删除门禁。SSH 命令取消先 TERM 并等待，再按有界期限 KILL/关闭通道；关闭通道不被当作远端进程已退出的证明。[sudo 无法转发 SIGKILL](https://raw.githubusercontent.com/sudo-project/sudo/main/docs/sudo.man.in)，所以系统 tar+ncat 和解包用 Bash 管理独立子进程组，通过 [异步 wait 的信号处理](https://www.gnu.org/s/bash/manual/bash.html) 停止整条管道。连接丢失或强制关闭后没有退出证据时，停止该传输的重试、保留暂存区并显示清理未确认；取消详情也保留在历史，不只显示“已取消”。无响应 SFTP 的进行中请求取消会关闭失效 transport，后续使用重新连接。正常空闲 reader 的取消不会关闭其他会话。这批取消/池方向改动尚待 hosted 验证。

本批正在验证的权限流程：源端与目标端分别批准，包含两端都受保护的预检与最终中转；helper 使用任务自己的已认证 SSH 路由，不占用浏览/PTY 的 transport。风险弹窗的“跳过此类方法”只跳过该风险层级，“取消任务”才停止整个任务。所有传输重试共享原始源清单；已复制后的提权删除也必须重新通过原清单的源/目标校验。仅对获准的高权目标恢复 UID/GID，修改符号链接自身而非其指向对象。上述新增队列/真实 sudo/监听权限回归尚未运行，不代表已验收。

无法执行上传的 agent 或无法识别其架构时，已批准的高权文件操作会尝试远端系统 [OpenSSH sftp-server](https://man.openbsd.org/sftp-server.8) 的 stdin/stdout 通道，不安装软件、不依赖远端 CPU 架构。仅探测固定系统位置，不从用户 PATH 提升任意程序；沿用 [pkg/sftp v1.13.11](https://github.com/pkg/sftp/releases/tag/v1.13.11)。每个任务独占连接，浏览/终端连接保持独立。密码通过 SSH stdin 的随机帧与后续协议分开；一次 sudo 完成认证和执行，不依赖 sudo 缓存。只有实际密码提示和成功握手才保存用户勾选的密码；NOPASSWD 成功不保存未经验证的输入。系统通道在提权后获取独立的目录共享锁，再 exec 系统 SFTP；依据 [OpenSSH 的实际服务端入口](https://github.com/openssh/openssh-portable/blob/master/sftp-server-main.c) 及 [服务端循环](https://github.com/openssh/openssh-portable/blob/master/sftp-server.c)，不假设 sudo 会保留外部 fd。普通退出先确认 SFTP 服务进程结束，释放控制端锁，再在独占锁下清理并销账；仅关闭连接不证明远端已停止。此通道需要系统 Bash/`flock`，缺少时报告原始失败，不改为无锁写入。新真实 SFTP 提交/放弃→销账、暂停写进程→关闭连接→重新解锁连接保留→确认退出后回收，以及实际密码/错误密码场景均已准备、未运行。

系统 sftp-server 确认不存在时，复用 Linux POSIX 目录/元数据接口，通过同一已批准的 executor 运行文件流命令。此后备使用系统 Bash、GNU find/stat/coreutils 和 flock，不上传另一套程序；参考 [Midnight Commander 的 Shell 文件传输](https://github.com/MidnightCommander/mc/tree/master/src/vfs/shell/helpers) 的系统工具方案，没有复制其覆盖目标协议。文件描述符先打开，再发送随机就绪帧；临时文件真实 inode 登记成功后才向 stdin 写入文件数据，密码不会进入数据体。提交先 EOF/等待实际退出、sync，再 rename；正常放弃删除并销账，未确认退出则保留。受控视图关闭会取消并等待自己的命令，不关闭浏览/终端。上传 helper 或系统 SFTP 明确拒绝启动时也先取得真实退出状态与独占锁，再清理空安装；网络断开不冒充拒绝。真实无 SFTP 子系统且无系统二进制、双端受保护目录、二进制内容、正确/错误密码、NOPASSWD、合并/安全移动和提交/放弃的 hosted 用例已扩展，尚未运行，不能算验收通过。

清理只针对本程序拥有的 session/listener/partial/helper/Hans 资源。网络完全不可达时，不能保证马上删除远端文件；不会扩大路径删除范围来掩盖失败。原 TUI、旧 Fyne 离线麒麟 libGL 问题和未授权的服务器 Web UI 不属于这一版变更。

禁用 SFTP 时，普通 POSIX 原子写入使用的 `cat` 必须先收到 EOF 并确认 SSH 退出，才能删除 partial；缺少退出确认则保留精确路径并停止重试。同一错误会穿过取消、单文件 Abort 和外层目录暂存清理，不被“已取消”或另一次删除覆盖。同机 cp/mv 的退出不确定也不继续换方法。正常失败的清理错误与原始错误一起返回，包含本机 rename 后无法删除 partial 的情况。已补真实禁用 SFTP 的 OpenSSH 正常提交/放弃，以及 pidfd 暂停实际 cat→放弃文件/取消目录移动→保留暂存与源的 hosted 场景，尚未运行。没有合法标记/写入租约的普通本机或 SSH partial 跨重启自动回收列入 [延期增强项](postponed-tasks.md)，不是用随机名字扫描删除。

## 可复现构建与验收

构建依赖 Go 1.26、Node.js 24 和对应原生平台工具链；Go 依赖已 vendor，前端使用 lockfile。构建脚本先重新生成四个 Linux agent，再编译 frontend 并嵌入当前控制端。macOS 本机构建：

```sh
npm --prefix frontend ci
DRAGFM_MAC_ARCH=arm64 ./build/build-wails-macos.sh dist
# Intel Mac 使用 DRAGFM_MAC_ARCH=amd64
```

Linux/Windows 具体原生命令保存在 [platforms.yml](.github/workflows/platforms.yml)。`cmd/dragfm-wails` 是新版入口；`cmd/dragfm-gui` 与 `build/build-all.sh` 是保留的 Fyne 原型。

测试/build/release 全部由 GitHub-hosted workflows 执行。当前批次优先 macOS arm64，用户验收后再扩展平台。`build.yml` 提供 Go/race/vet/frontend 与 macOS 窗口验证；`ssh-integration.yml` 提供实际 OpenSSH/权限/代理/Hans 夹具；保留的 `platforms.yml`、`release.yml` 用于另行批准的多平台验证及发布。候选提交授权不包含合并或发布；现有 workflow 与旧证据也不代表新批次已经通过。

本批 macOS 成品脚本已接入 [PyAutoGUI 0.9.54](https://pypi.org/project/PyAutoGUI/) 的系统鼠标/键盘事件，覆盖目录/空白拖放、无选中 Backspace、路径局部编辑、d/h、终端退格/回车、弹窗 Tab 和主题；还检查真实 zprofile/zshrc 变量与函数及最终 xterm 提示符。凭据配置的夹具准备仍用 DOM，不能当作系统输入证据。脚本限定 GitHub 临时 runner、实际应用 PID 和完整可见窗口，核对 trusted 事件及真实文件/PTY结果；无事件权限、坐标不明或窗口被裁切时明确失败，不更改 TCC、不回退模拟事件。三主题、Running/Pending 与最终 History 截图只在配置编辑器关闭后采集。应用只在显式隔离 smoke 模式和 hosted 环境中启用继承管道，不开网络测试服务；正常运行不依赖 Python。弹窗按 [WAI-ARIA 模态焦点约定](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/)新增初始焦点、Tab 循环与关闭后恢复。上述实现与新验收入口均尚未运行，不能算原生交互已通过。

每个 release 提供六个平台包、对应源码、`PROVENANCE.json`、两份测试证据 ZIP 和 `SHA256SUMS`。验收场景与复审修复映射见 [ACCEPTANCE.md](docs/ACCEPTANCE.md)，外部证书和环境边界见 [UNFINISHED.md](docs/UNFINISHED.md)。源码存在不代表当次 CI 成功，以对应 revision 的 workflow 和已发布证据为准。

## 许可证

GPL-3.0-only；FlySSH 复用部分为 MIT，官方 Hans 及其他依赖遵循各自许可证。成品包含 `LICENSE`、`THIRD_PARTY_NOTICES.md`、汇总依赖许可证与对应源码；详见 vendor、`go.mod`、`frontend/package-lock.json`。
