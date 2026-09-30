# 可复现验收与深度复审

以实际 release commit 为边界。`build.yml`、`ssh-integration.yml`、`platforms.yml` 可独立运行；`release.yml` 组合它们，并在独立 runner 解压后再验一次所有平台的成品。全部测试/build/release 使用 GitHub-hosted runners。测试生成的主机、账号密钥和文件是一次性隔离夹具，不使用用户机器、真实保险库或用户私有服务器。

当前优先 Mac arm64 的候选路径不调用六平台发布器：`build.yml` 汇合 source/test/原 Mac native/一次 SSH 三组，package-only 生成显式 Mac 包，独立 Mac runner 校验完整清单并运行真正解包成品。0d 的两条打包/提取红绿以及 4f 的最终 recorder 四格旧红新绿与真正解包主链均已有实际 hosted 工件，确切源码/产物见当前任务游标。recorder 边界表只验证真实工件输入，不当作 Linux 上的新 Mac 产品执行。原六平台门禁保留用于后续授权交付，以下历史映射不自动批准当前 A–P。

## 验收映射

| 要求 | 当前可执行证据 |
|---|---|
| 实际 Wails/RPC，不允许模拟成功 | `api.test.ts`；`native-smoke.js`；`portable-smoke.js` |
| 双栏、目录/空白拖放、复制/移动/覆盖/合并、无选中 Backspace、d/h | Workspace 组件回归；macOS SSH native suite；六平台 local native suite；独立文件系统 SHA-256 核对 |
| 可见行不能重叠或被遮挡，权限/大小/时间不能丢失，大目录不创建全部 DOM | `FilePaneVirtualization.test.tsx`；native `elementFromPoint` 命中和真实矩形范围检查 |
| 单 worker、Pending/Running、取消、快照修复、History、重启 | `internal/jobs` 并发/race 测试；`TestHostedSSHQueueAndHistory`；native 两次/三次进程启动 |
| 登录 PTY、cwd 双向同步、编辑中不注入 cd、陈旧事件不逆转新导航 | endpoint PTY tests；CWD gate/序号/事件排序组件测试；native xterm paste/Enter、目录往返 |
| 严格 Markdown、物理行号、密码遮罩切换不能清空草稿、vault 私钥 | config/routespec tests；`ConfigEditorComponent.test.tsx`；macOS native 编辑/保存/重连 |
| 真实非 root sudo 和受保护本机下载 | `TestHostedNonRootSudoTransfers`；native protected-local copy / declined-sudo move |
| direct/SOCKS/SSH relay 四方向/权限、四方法 | `TestHostedReviewedTransportMatrix`：3 层 × 2 权限 × 2 方向 × 4 方法 = 48 |
| 官方 Hans、反转角色与每种方法 | `TestHostedReviewedHansRolesAndMethods`：2 角色 × 4 方法 = 8；官方二进制实际 TCP 数据平面 |
| 真实失败回退、缓存不遍历所有主机 | `TestHostedReviewedRouteFailureRecovery`（隔离容器实际 iptables 阻断）；`TestHostedReviewedFailedRelayCacheReprobes`（禁用缓存失效并重新探测可用会话） |
| 源变更不删除、目标原子发布、目录合并、控制机不落盘中转 | transfer manifests/no-replace/source mutation tests；真实方法矩阵文件哈希与元数据核验；有界流实现 |
| 取消必须及时返回而非等待 sleep 自然结束 | `TestHostedSSHCommandCancellationLatency`；local child-process cancellation；native 五秒取消上限 |
| 无响应 SFTP 不阻塞 Lock | `TestStalledSFTPRequestIsCancelled`，实际 SSH 连接经暂停转发器；idle reader cancellation 不关闭无关会话 |
| 归档不能通过符号链接写出暂存目录 | `archive_receive_test.go` 的绝对/相对链接、根链接、重复根、空归档、损坏/截断 gzip、只读目录用例 |
| vault 恶意 KDF 参数、巨型密文、原子写入、脱敏 | `internal/vault` 与 `internal/webgui` 回归；bare master-password 与所有已知配置秘密脱敏 |
| Windows 不能把替换文件当原 inode、PTY 可退出 | `TestWindowsFileIdentitySurvivesRenameAndDetectsReplacement`；原生 ConPTY；quoted PowerShell LiteralPath |
| 六平台不是编译即成功 | 每个 runner 的 GOHOSTOS/GOHOSTARCH 断言、实际 Wails 窗口、文件与 PTY 操作、退出并重启 |
| 发布不是上传即成功 | 源码/二进制/证据 SHA 一致；六平台解压后二次验收；上传后下载所有 assets 并验证校验和；最后才解除 draft |

## 本轮复审修复的关键问题

1. 老的事件管道可阻塞 worker；取消与 Pending/Running 切换存在缝隙。现在原子状态变更、单 dispatcher、可恢复快照与递增 revision 保证 UI 丢事件不破坏队列最终状态。
2. 之前重写 FilePane 后未与 CSS 同步，绝对定位行没有 offset，截图中全部重叠。恢复虚拟列表并加入真实坐标命中检查；不能用合成 click 成功冒充可用布局。
3. 自动 refresh、旧 cwd prompt 和已保存 SSH 标签可能分别撤销新导航、清除新选择或过早启动本地 PTY。以 endpoint-ready、导航 gate、事件序号和保留最新有效 selection 修复。
4. SSH CancelJob 曾只 Close channel 后无界等待，Local.Exec 曾留下持有管道的子进程。现在取消 owned process group/SSH command，有限等待后关闭失效 transport，并实测延迟。
5. SFTP 没有请求取消接口；当前只为进行中的 I/O 注册 transport cancellation，不让空闲已取消 reader 影响其他会话。
6. 词法路径检查挡不住目录别名隐藏的自复制，归档词法检查挡不住先放 symlink 后写文件。加入物理父目录解析和 `os.Root` 限域写入，保留最终 symlink 本身的语义。
7. Windows 本机版本标识、PowerShell 导航和 PTY 等待生命周期需真正平台实现；不以“交叉编译成功”替代运行验证。
8. remote machine-id、能力与文件版本探测有时间/大小上限；缺少 Linux machine-id 时按跨机安全路径处理，不伪造机器身份。
9. 凭据不进入 argv/环境/日志；master password 也纳入已知秘密脱敏。代理 ncat 使用加密载体，不通过把口令塞进 CLI 来勾完成项。
10. 发布包含确切对应源码与许可证，检查发布分支没有前进，杜绝把旧 runner 的成品作为新提交发布。

## 解释证据

Go JSON 中父测试与子测试分别计数，不能把累计 pass 数当独立场景数。任务 wire-byte 活性可能含协议开销，不能除以文件大小伪造进度。原生自动化是实际 desktop webview 加受控输入，不是人工长期试用；签名/公证边界见 `UNFINISHED.md`。

## A/O 契约收口（749完整Mac通过，输入清理收尾仍待验）

本批沿既有原生与真实 SSH 入口补证据，不新增产品方向、平台或外部 CLI 框架。没有本地测试、构建或 GUI；以下保留执行设计，真实7ed边界与未验范围见本节末尾，不将设计当PASS。原角色的 [A/O 集中设计复核](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5907117332)来自冻结4f与提供的设计；其后7ed实现复审只读新冻结源码，未读本批CI/私有工件，证据层次分开。

A 的真实 fixture 单独放在 `browse-many`：192 项、明确文件大小/模式、准备内容之后才写入唯一 mtime。原 OS 输入器在左右 Local/SSH 栏逐项证明：深处文件与目录最初未渲染 → 滚轮在该栏且可信 → 到达深处 viewport 区域 → 条目真实渲染 → 独立矩形/elementFromPoint 命中 → 元数据正确 → 原生选中文件/双击进入目录 → 空白 Backspace 返回、PTY cwd 同步和滚动复位。没有设置 scrollTop、scrollIntoView 或合成滚动；主机/路径导航仍按既有 DOM 口径，不把它们伪称 OS 输入。最大 24 次滚轮与原主入口期限固定，不能为冲绿追加动作或延长等待。

`file-layout-contrast.py` 在独占同事件 worktree 只撤回已知虚拟行 offset 修复，并使用同一构建和原生入口。只有前述滚动及深处渲染前提成立后，在 `left scrolled deep entries are hit-testable` 指定边界实际失败才算红；其它失败不能算。临时控制构建的真实哈希、补丁哈希和有限行为证据单独记录；不称未修改旧版 4f 有此 bug，也不把 Linux 的输入对照当 Mac GUI 执行。当前成品的原 native 与真正解包 native 都必须通过同一新增动作。

O 的已有真实 nil-prefix 证据是 `TestHostedQueuedSCPPreservesDistinctWhitespaceNames/newline=false`：`runFlySSHSCP` 的公开 RunContext/Run 不设置 RemoteCommandPrefix，实际 OpenSSH 队列必须选择 `direct · target-pull · scp` 并核对名称和内容。GUI 使用 `-p`，因此不能用它证明无 `-p` 新目录权限契约。

`flyssh-compatibility.py` 取得固定上游 f614 的 `spec_test.go`、`scp_integration_test.go` 和 `internal/testkit/netkit.go`，逐个核对原字节 SHA；只补测试支持元数据，不替换当前 vendored SDK 的非测试实现。上游原用例复用其真实 loopback Go SSH 和系统 shell/scp，不说成 OpenSSH sshd 或外部 FlySSH CLI 的全功能运行；既有上游正例不编造旧缺陷红。新增 default-directory driver 通过公开 FromOptions/Run、无 prefix/无 `-p`，核对真实内容、新目录 `0550`、已有目录保持 `0770` 与文件 `0640`；umask `0027` 只在一次子进程设置并恢复，root 不跳过而失败关闭。

无 `-p` 的新增检查通过同一次临时对照只撤回目录临时 owner-write：源实际可读、新目标已建 `0550` 且叶文件不存在、错误确属权限失败，同时已有目录控制仍能传入内容且模式不变，才接受指定红；编译、fixture、认证或其它失败不算。整批走读另发现 receiveFile 无条件 Chmod 会绕过实际文件 umask，不能把正确 `0640` 期望改为 `0644` 冲绿。当前上游 HEAD `9f299339930bbb7589ad0596f699d0d5ba7a5bc9` 的该分支也未修；依据 [OpenSSH 官方 sink](https://github.com/openssh/openssh-portable/blob/master/scp.c)，产品最小修正为只在显式 `-p` 时恢复源模式。第二指定撤回只恢复旧无条件 chmod，实际内容与目录模式均正确后，新/已有目录内的两个新叶文件都被放宽为 `0644` 才算红；其它失败不算。

每次对照结束还原当前 SDK 原字节、校验非测试源码 SHA 未变化，最终运行未改动的上游兼容正例与相同 driver 取得新绿；原 `-p` 用例也必须继续通过。这里是待执行设计，不是红绿已经成立。AgentTimeout 零值默认分支的静态映射不冒称真实 agent 延迟测试；不扩 CPU/平台或无凭据的硬件矩阵。

本批仅在当前完整候选分支挂临时 A/O 对照，成功后下次真实完整冻结撤下，不为撤入口另推微提交。统一主门禁仍要求原 source/test/Mac/SSH 成功及当前分支对照成功；已完成 4f recorder 固定旧侧不重跑，永久拒绝/提取门禁保留。产品只修默认 SCP 的文件 umask，不声称 GUI 外观或终端残留有新变化；D/F4 仍是独立的人类判据。

冻结 7ed 的[集中实施复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5908173160)限定确认接线与未发现新增P1/P2，不替代运行证据；角色未读本轮CI/私有工件。driver的existing是已有目录内新建叶文件，不写成已经覆盖既有叶文件权限；A显式mtime核对的是深处文件，不外推全部192项。隔离撤回版另行构建，其二进制不进入正式收据/候选bundle；不能宣传整个run绝无第二次构建。当前成品含vendor权限改动，不沿用4f原字节或PASS。

### 历史：7ed 实测边界与原生双击修正

[主run36694314681](https://github.com/lovitus/dragfm-gui/actions/runs/36694314681)实际失败，根已下载核对工件；其GitHub event/parents/tree与7ed绑定。O两项指定撤回已旧红→当前绿，固定上游兼容用例及原`-p`通过。A撤回侧在真实wheel7次/top1880、深处区域与条目已渲染后按指定hit-test失败；当前同一边界与元数据/单选已通过，但随后真实double-click=1只得到普通click，可信dblclick=0并在其原20秒阶段超时。右栏大目录动作、后续传输/恢复与真正解包没有运行，不假称完整A或Mac通过。

原PyAutoGUI0.9.54的Mac发送器将双击发成两次单击，与[上游尚未合并PR949](https://github.com/asweigart/pyautogui/pull/949)及[Apple click-state定义](https://developer.apple.com/documentation/coregraphics/cgeventfield/mouseeventclickstate?language=objc)相符。准备的有限修正只在同一个double-click动作内给原四次物理down/up标记1,1,2,2；原guards/实机目标/失败关闭/owned-button清理保留，临时post包装finally恢复。不改DOM断言、导航/期望、输入次数或期限，不添加新的wrapper测试框架；修正后的同一完整native流程仍待hosted。

一次性A指定旧失败及两项O红绿已经留在7ed不可变记录；下一完整候选撤其临时调用，避免重新计算旧红，永久大目录native动作、上游/current正例与全部主门禁继续执行。A完整当前绿和同事件真正解包Mac必须取得后才谈本批技术收口，不以局部PASS包装主入口。

### 749完整当前绿与最小清理收尾

[749 run36698319439](https://github.com/lovitus/dragfm-gui/actions/runs/36698319439)已成功且实际工件核验完成。左右栏各7次真实wheel/top1880，深处渲染/独立hit-test/文件0640、8KiB、分钟mtime/单选成立；可信dblclick2、原输入动作2、click-state-event8，目录进入/cwd/空白Backspace返回和scroll0均通过。主native和真正解包native均exercise23/restore4/changed-key4/文件系统8/普通无参数库启动7项成功。后者是受绑定脱敏摘要，不假称已读逐栏原始字段；DOM路径动作和单个深处文件mtime范围不扩大。固定f614上游/current默认driver27项pass，三SSH37根pass、Go/race/重复sudo无fail。event=`f669a91c99d4c5ff28870c1f1ffa8304ab88a7fe`与749head/tree及全部producer/最终收据绑定，实际成品/native/收据SHA=`49edec095d04a2529d6bccf763ea7128b8b33a73180aed675683193dedbf86a6`，五项bundle校验OK。

[749集中源码复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5908769071)接受有限click-state方向，并提出条件性owned-release P2，未运行复现、未独立读本轮CI。设计失败方式：down后fail-safe挡up、公共mouseUp再次被挡；释放自身失败掩盖原动作；phase/final清理抛错阻断原证据。最小修正只在原post恢复后、当前坐标发送一次owned left-up；不关FAILSAFE、移动、重新激活、补down或重打动作，成功post不冒称OS回执。原动作和cleanup错误分开记录，phase保留renderer/input错误，final释放失败必须失败而不是PASS。上游position只读getter不调用fail-safe；新增mock/源码匹配测试不适用，没有添加。修正仍须冻结后完整原生门禁；corner-specific故障注入明确未验，不以普通流程绿色销账该边界。D/F4和重要移动警示保留，A–P不自动勾选。

## Issue #3 A–P 只读审计快照（2026-09-30，未结案）

需求逐条来自原始沟通与 Issue #3，不重新缩成当前已完成的小范围。本次实际读取了 0d 的 native/普通启动/三组 SSH/Go 工件、真实截图及对应源码，随后也收取核对 4f 同一主 run 的实际工件；4f 的 `cmd/internal/frontend/vendor` 与 0d 无差异，旧证据不冒充新证据。新四格 recorder 对照和真正解包 Mac 主链已有当前事实。当前唯一源码/运行游标仍见 `CURRENT_TASK.md`，本表不是第二个任务游标。

下表是已核对证据与缺口索引，不是通过声明，不自动修改 GitHub 复选框。只有完整契约被其对应证据覆盖、交叉复审并核对未验边界后才能结案。

| 项 | 当前可定位的实际证据 | 尚不能据此宣称的范围 |
|---|---|---|
| A 文件浏览 | native 的本机/SSH mtime 排序、真实 OS 路径局部替换/Enter；截图可见权限/大小/时间；`TestLocalListMatchesModificationOrder` 与真实 SFTP/POSIX 回退检查隐藏项；10,000 行虚拟化组件行为 | 仅大目录滚动/深处条目操作仍欠真实 OS 动作；端点隐藏项已具备行为证据，不笼统挂为未验；不扩大到任意文件名/性能矩阵 |
| B 拖放 | native 的 OS 目录拖放、空白受保护目标、覆盖移动与目录合并；拖放时真实 document selection 为空；独立文件系统核对 | native 通过只覆盖所列交互/文件，不外推所有冲突类型或用户私有目录 |
| C 按键/布局 | 无选中 Backspace、输入框/终端 Backspace、h/d、三主题均有 OS 动作；最小窗口三栏控件/History 点击与截图 | 主机选择等 DOM 操作仍明确记 DOM，不冒充 OS；不新增横屏/其它设备矩阵 |
| D PTY/cwd | zprofile 变量和 zshrc 函数经 OS xterm 输入实际执行；20 轮真实 SSH PTY 导航/命令/resize、编辑/运行拒绝；native 双向 cwd | 只验最后一行完整不能证明所有历史随机碎片均无；截图保留正常 cd 转录，呈现选择仍待人类，不清空历史或修改预期结案 |
| E 队列 | native 一笔 Running/一笔 Pending 同时可见、OS 排队取消与运行取消、实际字节/History、重启不恢复 Pending；命令流式与凭据边界回归 | wire 活性与文件字节必须继续区分；不能把方法标签/动画当成传输推进 |
| F Markdown | native CodeMirror 保存/物理行号/遮罩；真实配置解析无空行、多跳与 vault key 位置、SOCKS 字节回归 | DOM 配置动作与真实 RPC 是对应口径，不宣传每个配置控件都经 OS 输入 |
| G 管理/低暴露 | 配置身份/字段删除与缓存失效回归；真实选中 SOCKS 登录/禁用、双端授权/root key；成功跳板缓存复用与失效重探 E2E | 自动候选限定已成功登录会话/已批准缓存，不扫描所有保存主机；每种编辑字段与旧凭据绑定须逐项覆盖，不只看测试总量 |
| H vault | 普通无参数启动 7 项/5 次真实进程，错误密码/手动锁定/重启与程序旁/系统回退优先；真实库头私有访问/正文无明文密码；KDF/迁移回归 | Argon2id/XChaCha20 与显式保存语义仍需源码/具体凭据回归共同证明，不以“看到解锁页”代表全部 H |
| I 预检/同机 | 真实克隆 machine-id 与同 sshd 不同 chroot 保源；预检快照/相对路径/空间能力；实际 cp/mv 行为回归 | 不以 machine-id 单独判同机；不能把不可得的 inode/空间伪造为已知 |
| J 直接/权限 | 48 格真实方法矩阵含 direct；双端受保护队列、普通发起用户免密、root vault key、端口重选与 sudo 拒绝保源 | 48 格本身不是双端同时保护证据；“免密→会话→保存”须按独立凭据用例和实际路由归属核对 |
| K 池/缓存 | 源码从实际两端各三次探测取成功样本中位；真实 SOCKS/SSH 方法矩阵、缓存优先无多余探测、失效重新探测 | median 排序表与实际网络 E2E 范围分开；不把控制机登录 RTT 冒充远端 TCP RTT |
| L Hans | 官方 v1.7.0 实际 TCP 数据平面的 2 角色×4 方法；高权 server/无 TUN client、require-v5、指纹/口令 fd 和范围清理源码 | 资源 SHA 不能单独证明隧道可用；测试角色结果不等于用户已批准所有临时网络影响 |
| M 内存中转 | 双端受保护队列在明确跳过网络风险后实际使用 controller-memory-relay 并核对目标内容/所有权；源码从 Source.Open 经256KiB缓冲直接写 Destination.CreateAtomic，partial在目标端；目录合并/取消回归 | 网络阻断 RouteFailureRecovery 的实际 winner 是SOCKS，不算“全部网络失败→内存”证据；没有控制机全系统I/O追踪，不能冒称做过该观测；目标partial不是中间落盘 |
| N 移动/清理 | native 覆盖移动及独立 SHA；真实同数据别名/身份丢失/耐久同步失败保源；未知退出、合法标记、部分文件/进程清理回归 | 不把复制成功当允许删源；失联不能保证即刻删除所有远端资源，重要移动警示尚未解除 |
| O 嵌入/协议 | 四类 Linux agent/Hans 资源构建与格式/哈希检查；真实 SFTP/POSIX 回退、多跳认证/指纹变化阻断；FlySSH vendored connector/transfer 复用 | 不是四种 CPU 实际远端执行；只欠本地补丁影响的默认公开CLI调用契约证据映射，不认定CLI已坏、不扩为外部CLI全功能认证 |
| P 当前 Mac 交付 | 4f 真正单 Mac 归档/源码/许可/五项完整校验、四格 recorder 旧红新绿与最终固定 manifest；独立解包 Mac 原入口已通过；原角色集中结论限定认可具体P2与当前Mac技术交付链 | 技术P不等于整PR/A–P或人类认可；未发布/合并或新增其它平台构建 |

同一固定角色的[集中结论 `5906481045`](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5906481045)已经收取：当前P技术链与限定P2可认可，B/C/E/F/G/H/I/J/K/L/M/N所列原契约证据足够，但不是任意环境认证；剩余具体证据缺口仅A原生大目录动作、O默认CLI契约映射，D/F4是独立人类判据。评审独立走读冻结源码/正式索引，并引用本线程的实际运行事实，没有独立读取CI/私有工件。下一步限定为A/O一个完整收口批次，不逐行建角色/提交，也不自动修改Issue勾选或解除重要移动警示。

审计口径更正：M 的控制机路径证据来自双保护队列与实际流式源码，不来自网络阻断回退用例；后者明确断言 SOCKS 成功。此更正只修本索引的证据归属，不改变产品/测试预期，也不把“风险跳过”冒充“所有方法均实际尝试失败”。
