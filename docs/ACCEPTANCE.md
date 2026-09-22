# 可复现验收与深度复审

以实际 release commit 为边界。`build.yml`、`ssh-integration.yml`、`platforms.yml` 可独立运行；`release.yml` 组合它们，并在独立 runner 解压后再验一次所有平台的成品。全部测试/build/release 使用 GitHub-hosted runners。测试生成的主机、账号密钥和文件是一次性隔离夹具，不使用用户机器、真实保险库或用户私有服务器。

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
