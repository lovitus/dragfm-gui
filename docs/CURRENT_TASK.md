# 当前交付游标

当前批次：Issue #3 原始需求复核与完整修复。唯一验收账本为 https://github.com/lovitus/dragfm-gui/issues/3 ，原始沟通需求保留在 `USER_REQUIREMENTS.md`。旧 `ACCEPTANCE.md` 和 RC2 的 PASS 只代表当时覆盖的场景，不证明当前全部需求完成。

维护入口：`cmd/dragfm-wails`。旧 `cmd/dragfm-gui` 是保留的 Fyne 原型，不应作为新版验收入口。

## 当前源码与验证状态

- **749 已完成完整 hosted 验证；当前只准备其集中复审指出的 owned-release 收尾，尚未冻结/验证。** HEAD/远端/下次 lease=`7491699e0227b1ec5c42fde3c5437278eb7ed2d1`，tree=`a8adbd41e7b9bd6a7a85679e9253375f64a1865f`，parent 仍为基线，基线上一个完整提交。当前工作树仅原生验收输入清理和准确结果文档，产品 `cmd/internal/frontend/vendor` 未改变。
- 唯一 [749 主 run `36698319439`](https://github.com/lovitus/dragfm-gui/actions/runs/36698319439) 已 **completed/success**，实际工件已下载并核对。首次600秒观察 timeout 后，收齐原角色集中复审里程碑，再对同一ID一次有界读取取得终态；观察 blocker 已解除，没有重触发或循环查询。所有等待/下载句柄关闭；不再查询该终态run或已失败7ed。
- **A 完整当前绿已经取得，O 当前兼容正例保留。** 主 native 和真正解包后的 native 均 exercise23/restore4/changed-key4/文件系统8/普通无参数库启动7项成功；左右栏真实滚轮/深处命中/元数据/单选/可信双击/目录导航/cwd/空白Backspace及滚动复位通过。7ed 的指定 A 旧侧及两项 O 旧红→新绿不可变记录保留，不再执行；不是外部CLI或四CPU认证，不把 DOM 路径步骤说成 OS 输入。
- [原角色749集中复审5908769071](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5908769071)认可 click-state 方向，指出条件性清理P2：down后fail-safe拒绝up，公共mouseUp又被同一fail-safe拒绝，且可能阻断错误记录。没有运行复现，不是7ed dblclick=0的原因。准备修正仅在恢复原post之后、当前指针位置发送一次 owned left-up；不关闭FAILSAFE、移动、激活、补down或重打动作。原动作/释放错误分别记录，释放失败不得成为PASS。
- **当前收尾的行为仍未验。** 已先列清理失败方式并核对上游 `position()` 是不触发fail-safe的只读getter；没有新增假故障或mock测试。冻结后复用全部既有真实native与解包门禁，corner-specific故障注入仍明确未验，普通流程绿色不能外推该边界。
- 749实际成品 SHA=`49edec095d04a2529d6bccf763ea7128b8b33a73180aed675683193dedbf86a6`；原始事件/七份producer/最终收据、成品/native/SHA三方及五项bundle校验均已核对。尚未复制该新成品到用户dist；旧4f/f72候选与保险库保留。无本地测试、构建、GUI或新部署，无合并、发布、扩平台。
- **唯一下一步：把最小 owned-release/错误保留修正与上述事实合批冻结amend，以lease749推送同一完整候选，沿唯一hosted主链验证并回原固定角色集中复审。** 不重做已成立一次性对照、不放宽断言或期限。D/F4人类呈现选择仍待答复，不重复催促；重要移动警示保留，整体目标不标完成。

## 历史：749 完整 A/O 与 macOS 成品证据

- 749的实际 event=`f669a91c99d4c5ff28870c1f1ffa8304ab88a7fe`，通过GitHub git commit API核对 parents=基线+749、tree=a8adbd41与候选一致；七份producer和最终收据同 event/head/tree/run/attempt。实际 Go/race/重复sudo JSONL 无fail；三SSH37根pass/无fail。固定f614上游/default-driver27项pass/无fail，当前vendor来源与测试文件SHA受收据绑定。
- 两栏各7次真实wheel、scrollTop1880、初始0/深处区域/真实渲染成立；深处文件 `0640`、`8.0 KiB`、`2023-11-14 21:10`正确，entry0063单选、entry0064双击进入、空白Backspace返回与scroll0/cwd成立。可信dblclick=2、原生double-click动作2、click-state-event8，总wheel15。完整主native及真正解包native都通过包含这项的23项流程；后者保留的是脱敏摘要，不假称已读它的逐栏原始字段。
- 实际product/native/最终收据 SHA=`49edec095d04a2529d6bccf763ea7128b8b33a73180aed675683193dedbf86a6`，archive=`c9ed01b3f40b333bffee74c8c568fcc5a05d3c69a6ee2ac49c49d8c2aaa9eb43`，SOURCE=`b19d7142130d8997bc5d4f6ddcb6f3829f4ef88e62c6233bf1ea588c735d4745`，fixed manifest=`11ff41f53031030185ea7d3be2c61ccf9c8b4c429a8b8d1961cb91cb31db0686`。下载bundle五项校验均OK；没有本地重编译、重打包、重签名或GUI执行。
- [749集中复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5908769071)是独立只读源码，不是独立读取CI/私有工件；它接收的7ed范围事实已正式记录。749正常链成功不消除其条件性cleanup P2或D/F4人类判据；必要收尾见当前游标，不重新扩A–P/CPU矩阵或输入框架。

## 历史：7ed 冻结与限定 A/O 证据

- **2026-09-30 A/O 完整候选已冻结并推送 `7edcf26797df3c02272a72e760656ce8d481ff5f`，尚无新运行结论。** 当前 HEAD/远端/下次 lease 均为该 SHA，tree=`53b039d9ad06875d2a67c5d3ef349a15f963fb6b`，parent 仍为基线 `6c339a19ecc621ff1cdee0ba9f5775b0ac37342e`，基线上只有一个完整交付提交。原生大目录动作、精确上游默认 SCP 契约、指定行为撤回及同批需求文档已合批；产品仅修 vendor SCP 无 `-p` 时越过 umask 的文件 chmod 分支，`cmd/internal/frontend` 未改变。push 已正常结束；工作树仅冻结后事实文档，不单推或另跑。无本地测试、构建或 GUI，无部署、合并或发布。
- 唯一 hosted 主 run 为 **[`36694314681`](https://github.com/lovitus/dragfm-gui/actions/runs/36694314681)**。指定脚本首次观察 600 秒返回 timeout/124，只有 run ID、没有终态，不能当作代码/测试失败或 PASS；原输出仅私有保存。这一观察 blocker 只记录一次，不重触发 workflow 或无限重复查询。无活动工具等待；先收取已经派发的原角色集中实施评审里程碑，再对同一 ID 一次有界读取。
- 集中实施复审里程碑后，对同一ID的一次指定脚本读取已取得 **completed/failure**，headSha 精确为7ed；观察超时解除，不重查终态。失败作业为 build-macos-arm64，package/verify-packaged 均 skipped，因此没有本候选已验收成品。状态只定位作业，尚不能判断是编译、原生前提或产品错误；下一步按唯一失败日志入口与真实工件定位，不重跑冲绿或改成功条件。
- **独立设计复审已收齐。** 原固定角色的 [A–P 结论 `5906481045`](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5906481045)限定认可当前 Mac 的 P 技术链及 B/C/E/F/G/H/I/J/K/L/M/N 所列原契约证据，不是任意环境认证；[A/O 设计复核 `5907117332`](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5907117332)补充无 `-p` 的 SCP 新目录 umask/已有目录权限分支。角色审的是冻结 4f、公开契约及提供的未冻结设计，不冒称已读新实现或新 CI；角色 idle，不新增角色或重复派发逐行评审。
- **7ed 集中实施复审已完成：[正式评论 `5908173160`](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5908173160)。** 单一定时器静默30秒后的原角色读取为 completed/idle；实际读冻结7ed/4f、tree/parent、原生流程/两权限对照与producer条件，限定增量未发现新增P1/P2或明显门禁绕过。该角色只读静态，未查本轮CI/日志/私有工件，结论是实现接线复核、行为待证，不能销账新umask问题或批准A/O/整PR。existing driver只证明已有目录中的新叶文件，不冒称覆盖已有叶文件权限；路径表单仍DOM、mtime只核对深处文件，负对照另有隔离构建，不混入正式成品来源。GUI子树未变不等于整个vendor/成品与4f原字节相同。
- 冻结时 PR 正文已成功同步7ed；追加集中复审/观察事实的单次正文更新遇到 GitHub GraphQL EOF，结果未知、没有立即重试，不算产品/测试失败。根本地事实文档与原角色正式评论保留；待真实主链结果收齐后一次统一同步，不另推结果文档提交或重触发workflow。
- A 沿原原生入口创建独立 192 项真实目录，先证明深处条目初始未渲染，再以原 OS 滚轮进入深处，分别验达到目标区、真实条目已渲染、坐标可命中；之后核对名称/大小/分钟时间/权限，实际点击文件、双击目录和空白 Backspace 返回并验 cwd/滚动复位。最多 24 次原生滚轮，不写 scrollTop、不放宽原 260 秒期限。临时对照只撤回已知独立虚拟行 offset 修复；只有滚动/渲染前提成立后出现指定深处 hit-test 失败才算红，编译、启动、权限、输入或到不了目标区均不算。
- O 复用固定 FlySSH `f61415b692c4f27ea774059f1404b806073c489d` 的三份上游原字节测试/支持文件并核对 SHA，直接测试当前 vendor 实现，不覆盖产品实现或改 FlySSH 工作树。现有真实 OpenSSH 队列的 nil-prefix SCP 已有文件名/内容证据；新增范围只补无 `-p` 下载：新只读目录遵守 child-only umask，已有目录保留原模式。driver 走公开 FromOptions/Run、真实 loopback Go SSH 与系统 scp，明确不是外部 CLI 可执行文件或 OpenSSH sshd。指定撤回只去掉 mkdir 临时 owner-write；新目录必须到达实际权限失败且已有目录控制通过才算红。
- 整批走读另发现真正产品问题：receiveFile 无条件 Chmod(source mode)，会把实际 umask 创建的 `0640` 改成 `0644`。当前 FlySSH 远端 HEAD `9f299339930bbb7589ad0596f699d0d5ba7a5bc9` 已读，同样尚无该修复；依 [OpenSSH 官方 sink 分支](https://github.com/openssh/openssh-portable/blob/master/scp.c)最小只让显式 `-p` 执行恢复 chmod，不改全局 umask/已有文件模式或 GUI 的 `-p`。新增同 driver 的指定撤回只恢复真实旧无条件 chmod；实际内容和目录模式正确后，两类新叶文件仍被放宽为 `0644` 才算红。保留 `0640` 期望，上游 `-p` 原用例另验证显式保留权限；当前没有运行结论。
- 统一 workflow 保留前后端、全量 Go/race/vet、三 SSH 与原/真正解包 Mac 门禁；本批 A/O 临时对照并入同一候选，只有当前分支的对照成功才允许打包，其它入口明确跳过此临时项但原 producer 仍须全成功。已成功的 4f recorder 固定旧基线调用在本批撤下，永久输入拒绝与提取回归保留，不重做已成立 P 对照。没有测试-only/碎片提交或新增多平台矩阵。
- 本地检查仅 `git diff --check`、三份 rollback 的 `git apply --check` 和 Go 文件的 gofmt；它们不算测试通过。预先失败方式及实际执行契约见 `ACCEPTANCE.md`；新运行失败保留其故障层级，不能用源码一致/编译成功代替红绿。
- **唯一下一步：按指定失败日志入口只取一次 `36694314681` 的失败记录，并下载实际工件定位 macOS 作业失败。** 不重查已终态run、重触发workflow、逐fixture建评审、重建旧4f或覆盖候选/保险库；不调整成功条件或超时来冲绿。D/F4 人类呈现选择仍待答复，不重复催促或擦除用户历史；重要移动警示保留，未验收前不合并、发布或标目标完成。

## 历史：4f 已测基点与 A–P 集中评审

- **2026-09-30 接续审计：上一完整目标轮为 progress。** 4f 的最终收据 P2 已取得实际旧红→新绿与原字节成品核验；本轮接同一工作树并收取 A–P 集中评审，不重做已经成立的对照，不把 D 的人类呈现选择冒称整个项目没有其他验收工作。
- **当前完整候选/远端/下次 lease 为 `4f92c268f5cda994ad52ff70807be6dc315b6a4c`。** parent 仍基线 `6c339a19ecc621ff1cdee0ba9f5775b0ac37342e`，tree=`37937cd6d2ff818c1eae3293d6bebc71d598034f`。唯一 hosted 主链已终态 success，实际工件核验完成；仍为完整一个交付提交。工作树仅冻结后结果/需求文档，不单推或重跑；无本地测试、构建或 GUI，无部署、合并或发布，f72 用户副本保留。下列 0d 是历史基点，不冒充新证据。
- 本候选唯一主 run **[`36679311064`](https://github.com/lovitus/dragfm-gui/actions/runs/36679311064) 已 success**。首次指定脚本 600 秒观察 timeout 后，完成独立实施复审与 A–P 审计里程碑；随后对同一 ID 的一次有界观察取得终态，没有重触发/无限重试，观察 blocker 已解除。工件已下载私有目录并逐项核对；所有等待/下载句柄完成，无活动工具等待。主/source/三 SSH/两次 Mac native 均是同一 run，不查已终止 run。
- **4f recorder 四格旧红→新绿已经实际成立。** 固定 0d 旧 script SHA=`6a0de0752dc7d972f5fb198630c3e27bfe9c396c844043429b2c1fbb63d96328` 与真实 git blob 哈希相同；删 SOURCE 校验行、连同 SOURCE 删除、自洽重写、增加未列资产，旧侧均实际写出成功收据，新侧均指定拒绝且无成功收据。新 intact 正例及缺固定输出控制、原单 Mac/提取/许可/来源/默认六平台门禁通过；正例未执行旧侧，不称“双绿”。只是 recorder 输入安全边界，不假称 Linux 上重新执行 Mac GUI；该四格一次性对照成立，不重跑，只在下次完整冻结移除临时固定基线。
- 4f 实际 event=`fb4318edf7fbdd649eaf69fb4a2e4a6ff3cdcf32`，GitHub git commit 的 parents=基线+4f、tree=37937cd6 与候选一致；七份 producer 收据和最终收据同 event/head/tree/run/attempt。SSH37根 pass/无 fail，Go/race/重复sudo无 fail、audit0；原始与真正解包 Mac native 均22/4/4/8/7项成功。成品/native/校验三方 SHA=`462ffb1418bac0e5da90f8f687e9e77b794a88e037cba62b3e4872f98af6d08f`，归档=`20ea7acb3d072de3decb4d62dd1a44b5053e64648eea5be72e167bfca9f88b28`，SOURCE=`0c7dafdd58b4c888e885d5546821c3fc567909424f0e7b0a4cb2e61855960d5f`。真实五项分发校验均OK，最终 fixed manifest=`b74fb5a8c3202aed57bf8dfeccc898d2bf1ff82cedc86050506d7578ae7af114` 与下载清单原始字节相同；ZIP仅VALIDATION.json。该具体P2已有行为闭合证据，整个P/A–P仍待集中验收，不自动勾选。
- [原角色 4f 集中实现复审 `5905687463`](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5905687463)已收取，角色 idle。固定清单输出→workflow→最终 recorder 接线成立；限定增量未发现新增 P1/P2。结论仅静态，实现已复核、行为待证；没有独立读新 CI/私有工件。四格旧侧的红是实际错误接受且写出成功收据，正常 recorder 正例只执行新侧，不宣传双绿。P/A–P 未批准。此独立复审里程碑已完成，不逐行追加评审；现在只读接原始需求账本，之后对同一 run 收取终态/工件。
- 已逐项重新读取原始需求/Issue #3，并核对实际 0d native/三 SSH/Go 工件、两个真实窗口截图和关键源码；A–P 的证据层次/剩余边界索引加入 `ACCEPTANCE.md`，不自动勾选 Issue。旧 `REQUIREMENTS.md` 六平台句子已按用户后续 Mac 优先授权修正，属于结果文档、无产品改动/另跑。新源收据行为仍未宣称通过；需求审计里程碑已完成，下一次只读取同一 run，不重启动。
- **唯一主 workflow [`36671191205`](https://github.com/lovitus/dragfm-gui/actions/runs/36671191205) 已终态 success，实际工件已私有下载并核对。** 首次指定脚本 600 秒观察 timeout 后，完成独立复审里程碑，再用同一 ID 一次有界读取取得终态；未重触发、无限重试，观察 blocker 已解除。没有活动工具等待或 workflow。主 producer、SSH 三组、Mac 打包及真正解包后的 Mac 原生验收均来自该同一 run。
- 0d 实际工件 event=`e1a4609de5fd87f16b4d7081847b30343e2e3bd9`，parents=基线+0d、tree=15b99aa，与候选匹配。SSH 37 根全部 pass/无 fail；Go/race/重复 sudo 无 fail、audit0；原 native 与独立解包 native 均 exercise22/restore4/changed-key4/独立文件系统8/普通无参数库启动7项成功。Mac 成品/native/SHA256SUMS 三方哈希=`462ffb1418bac0e5da90f8f687e9e77b794a88e037cba62b3e4872f98af6d08f`，Mac 归档=`8602a6b35658b8013ed55adfbc2dde7d3c04904c75abc4a690f39eba79e5ad03`，SOURCE=`cef570c7bda12edd62ff85125a9ed4a104aa86c248a2cbf8b2a01bea6ea8fe9d`。实际五项分发校验全部 OK，证据 ZIP 仅 VALIDATION.json。没有据此批准整个 P/A–P 或用户的终端呈现。
- 0d 的两个指定旧红→新绿均已成立：旧 f72 强要未选择的平台、新侧验证真实单 Mac 归档和提取字节；旧提取器接受未在校验清单中的实际源码、新侧拒绝。八个输入/默认六平台控制通过，不冒称八个旧 bug。该一次性对照本轮撤下，永久新行为 E2E 保留；不重新运行这两个对照。
- 0d 时的[原固定角色复审 `5904450123`](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5904450123)曾指出条件性 P2：旧最终 recorder 只重读当前清单，SOURCE 校验行消失或清单/源码一起改写可能仍签成功收据；不是实际损坏。当时行为未证，现在严格提取器固定输出、最终核对及同事件四格已取得上述4f事实，不继续沿用旧“未验”的口径；不因此批准整个P/A–P。
- 用户候选已复制到 **`dist/candidates/4f92c26/`**：原字节单文件、完整原 release-bundle、源码/许可/校验、两份脱敏收据和 README。单文件与五项原分发校验全部OK；执行权限与真实归档相同，无本地编译/重签名/运行，没有覆盖旧f72/保险库。Mac产品字节与f72相同，本轮只修验收收据，不宣称外观/终端有新变化。
- PR body 已同步 4f 实测事实，A–P 未结案证据索引公开记录在[评论 `5906202514`](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5906202514)，没有自动改Issue复选框。已派发同一固定角色一次集中原需求验收走读与限定P2行为记录；不额外查CI/启动workflow或新角色，其结论尚未收取。关注真实缺口而非扩矩阵/泛化疑虑，原始需求不缩减；所有新结果文档尚未另推提交。
- **一次评审回读约束已记录，不能算评审完成或代码/CI失败。** 首次存留快照为 active/只有userMessage，未取得正式结论；修正等待脚本的active识别后，在同一句柄的有界等待内 read_thread 返回“无法加载会话”，随即停止、不重派。一次延迟的正式PR记录读取仍只有既有实施评论与本线程索引，没有新A–P结论；不能推断角色已停止/会话被删除。原始错误和正式记录快照只在私有目录；无活动工具等待。4f技术结果保持，完整验收不能跳过独立记录。
- 根继续只读核对 M/O：实际 transferAccess.relay→transfer.Run→copyItem 是256KiB Source.Open→Destination.CreateAtomic；本机partial只在最终目标目录，不是控制机中间缓存。双保护队列实际使用该方法；RouteFailureRecovery 实际winner是SOCKS，已纠正索引，不能算所有网络失败→内存的证据。FlySSH只读查看已保存项目/冻结f614 CLI与现有hosted CI定义，CLI仍调用共享pkg/transfer；未改其工作树、测试/构建或触发workflow，源码/CI定义不冒称CLI运行通过。
- **原角色 A–P 集中结论已经收取：[正式评论 `5906481045`](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5906481045)。** 本轮单一定时器首次静默30秒后读回 completed/idle 与正式结论；没有重派任务或查询已终态CI，旧会话回读限制已解除。该评审独立核对冻结源码、正式索引和上游契约，运行事实仍来自本线程实际工件核验，不能称角色独立访问私有工件。限定 P2 行为闭合、当前 Mac 的 P 技术交付链可认可；B/C/E/F/G/H/I/J/K/L/M/N 的所列原契约证据足够，非任意环境认证。A 仅欠真实大目录 OS 滚动/深处条目动作（隐藏项已有 Local 与真实 SFTP/POSIX 行为证据）；O 仅欠受本地 vendor 补丁影响的默认 CLI 调用契约映射，不代表已有 CLI 故障。A–P未自动勾选、整PR未批准。
- **剩余原契约收口方案，尚未执行或冻结：** A 沿现有原生入口/OS 输入器增加一次真实多屏目录的滚轮、深处条目名称/元数据、选择或进入再返回，不另造浏览器/性能/平台框架；O 优先把现有真实无prefix SCP/零AgentTimeout认证与固定上游默认入口逐段绑定，只有确有未覆盖的受影响契约才复用其既有用例。新增行为检查的红必须来自已复现行定位修复的真实撤回，不将编译/启动失败或任意故障注入算红；若无法取得指定失败，不记已验证交付。预先失败方式：滚轮落在终端/第三栏、事件不可信、仅scrollTop变化而条目仍不可见/被遮挡、虚拟深处索引或元数据错位、输入导航/返回未同步PTY；CLI路径则防止GUI prefix/timeout代替默认调用、只看源码相等冒称实际CLI运行。产品尚无新实现缺陷证据，FlySSH工作树只读。
- **唯一下一步：完成 A 的原生深处条目动作与 O 的默认调用证据收口这一完整批次。** 收口前不另推结果文档或测试碎片，不重建已交付4f或重跑已成立P对照；若形成下一完整候选才移除已成功的4f临时基线。D/F4仍待人类呈现选择，不重复催促、不擦用户历史或改变预期；无新授权不合并/发布/扩平台，重要移动警示保留，目标不标完成。

## 已测基点 f72b122（历史，不是当前游标）

- **f72b1224f67bfb5c5bad8e7c766b8ae9c0e19a60 是已测基点，冻结前 HEAD/远端/下次 lease 相同。** parent 仍为原始基线，tree=`94cecaf8497ac172247b4a45b1a98ad3d5e6ae46`。主 `36662871843`、SSH `36662871946`、新取消红绿 `36662871910` 均终态 success，实际工件已下载、核对；这些 run 不重查。工作树另有上述完整 P 增量、回归与文档未提交/未验，没有本地测试/构建/GUI或合并/发布。
- 研究与失败方式已集中核对：[标准 argparse](https://docs.python.org/3/library/argparse.html)、[同提交复用 workflow](https://docs.github.com/en/actions/sharing-automations/reusing-workflows)、[官方 download-artifact](https://github.com/actions/download-artifact)。上游当前示例为 v8，新下载步骤采用该现成实现，旧上传/既有调用不做无关升级。风险清单包括错 merge/head/tree/run/attempt、生产者未完成、缺 SSH shard/普通启动、混字节/清单漏资产/丢执行位、漏上游许可和原日志外带、PR/main/release 重复 SSH、在解包端偷偷修复或重签名。相应处理已接入同一链；不是已测结论。
- 新取消红绿 `36662871910` 已终态 **success**，实际工件已下载私有目录并核对：旧 a247 两方向 routed 78 都在真实取消+精确退出前提后出现指定过滤失败，新 f72 两格通过；77/普通23/未routed78六个控制双通过，无额外测试失败。0.03–0.05秒子例不是 KILL/超时冲过；临时对照已成立，不重跑。原固定角色正式评论 `5903317508` 静态确认本轮限定增量无新增 P1/P2，未读取运行工件，不外推为 H/整PR批准。
- 主 `36662871843` 首次按指定脚本等待 **600秒超时**，只记录一次待确认 blocker；在不同 SSH 门禁及复审收齐后，对同一 run 的一次有界读取已得到 **success**，没有重触发/无限重试，该等待 blocker 已解除。Go/race/重复 sudo 无 fail、audit0。普通无参数启动7项通过、5次 launch 都 argc1/不同cwd/初始锁定，库头 hint/private/passwordAbsent 全通过；原 native exercise22/restore4/changed-key4/独立文件系统8项通过。一次 NS/AX 对照均匹配，仅证明本轮观察值，不倒写历史 None 的内部根因。真实 OS 输入计数包括6次拖放，原主机选择等 DOM 步骤继续明确只记 DOM 证据。
- SSH `36662871946` **success**，实际三组 37 根全 pass/无 fail；新取消八格也在正式门禁通过。主/SSH/source 工件 merge=`9841bbd79f2f131fe2bd6f6ebcdda6371ec25896`，parents=基线+f72、tree=94ce，与候选相同。Mac binary/native/SHA256SUMS 三方哈希=`462ffb1418bac0e5da90f8f687e9e77b794a88e037cba62b3e4872f98af6d08f`；实际 Mach-O arm64。源码哈希=`b88d899c5b506f1413ed2b3713de79300126985c2ae75833874db4f326a30d2c`，归档实际含 GPL/FlySSH MIT/Hans 1.7 源码及许可证。
- P 已测 f72 的静态边界：单文件 Mac 和源码分别由现有 build workflow 产出，官方 Hans 源码/许可证已在 tracked third_party；该基点打包器仍要求六个平台且未做当前提取后验收，不能把源码文件存在当成实际打包 PASS。上面的新 Mac 打包路径是本轮工作树实现，尚无新运行结论，不覆盖或改写 f72 原始候选/证据。
- 用户验证候选字节已从 hosted 工件复制到 `dist/candidates/f72b122/`，校验值相同，附源码、SHA256SUMS 与边界说明；未覆盖旧 dist/保险库、未启动或部署，未本地重新打包/编译/签名。原生截图已目视核对；Shell GUI 导航仍按既有设计保留正常 cd 记录，不擦行，不能将完整提示符检查冒称导航命令隐藏。
- 原固定角色已将本轮事实正式记入评论 `5903682137`：routed78 具体 P2 限定行为闭合；f72 普通无参数启动已有对应入口证据，不再沿用“缺这项证据”的旧结论，但不批准整个 H/PR。其未独立读取本轮 CI/私有工件，来源区别明确。
- **D/F4 的自动导航呈现口径仍待用户确认。** 保留完整正常 cd 记录，还是隐藏程序发起的导航命令而保留用户命令/历史？原角色未找到最终认可或“全部必须隐藏”的明确记录；README/注释不冒充用户决定。当前终端源码不变，不能用最后一行完整替代全部异常残片检查，不清屏/擦图/改预期来结案。已提出一个具体偏好问题，并按 notifyuser 使用稳定 key `dragfm-gui:issue3:d-f4-navigation-transcript` 提醒一次；不重复催促、重新查已终止 CI 或为未选方案写代码。选择保留时明示验收边界；选择隐藏时先调研现成 Shell/终端集成与集中设计，再进下一完整批次，不盲调。
- 本轮 PR body 同步成功；上述通知脚本返回 `sent`（仅表示 Telegram 接受，不表示用户已读），不再次发送。两个短命令句柄均已完成。
- 当前是本批边界恢复与可验证候选，不是全需求批准；P 完整发行打包/提取后验收仍未做，D 也不表示 A–P 只剩这一项。新临时红绿已成立，后续实际冻结时移除入口而保留永久回归；不为删除入口或文档单独推送/重跑。重要移动警示、未合并/发布保持。没有活动工具等待或需要接续的 workflow。

## 已测基点 a247a67 与本批收尾

- **a247a674a3792e89376f13644fb3804807d2ab8a 三个既有 run 均终态。** 中断丢失旧等待句柄后，仅读取同一 run，未重启验证。原始工件仅 `/Users/fanli/.codex/private/dragfm-issue3-ci-a247a67/`。
- SSH `36654234706` **success**，实际三组 36 根全部通过。原反向禁 helper ncat 2.24 秒、反向普通 system rsync 10.11 秒、反向 SOCKS ncat 25.81 秒，以及 STOP/退出丢失四格通过；保留其限定证据，不把一次绿色倒写为所有历史超时的根因。主/SSH 实际工件 COMMIT=`97153b61d8dd680c9b4ed2c906db10bcb27f6596`，parents=基线+a247，tree=`0348ee5fba71ac4d80b68123272df58ae92ac051` 与候选匹配。
- 一次临时红绿 `36654234701` **success**：14 条真实旧行为失败/新通过，分别为 Bash 6 格、SSH pair 4 格、Paramiko peer 2 格、真实取消 23/77 两格。Bash 5.2.21-2ubuntu4、libc6 2.39-0ubuntu8.9；旧侧已完成 child 输出后仍实际 No-record/期限失败，新监护完成。127 分类不混作额外 UAF 复现；pair/peer/取消是所列错误边界，不冒充完整移动 E2E；产品 trap 的 127 和独立解包注入仍未独立验。该对照已成立，不重跑。
- 主 `36654234699` **failure**，Go/race/重复 sudo 无 fail、audit0。原生普通创建已由 AX 观察到 workspace，随后第一次 workspace capture 前，`NativeInput.perform` 的 NSRunningApplication 查询返回 None，而先前 Popen.poll 仍存活。只有 create/workspace 两次观察、8 次焦点检查；库头、手动锁定及后续普通/旧 native 流程未执行，H 不通过。失败日志仅按专用脚本读取一次；不声明产品退出、缓存根因或新成品已验收。
- 固定角色评论 `5902200944` 已静态确认监护与 77 跨层处理，但新增 P2：普通失败取消内部 life 后，带 context.Canceled 的 routed 78 虽有显式 PreserveSource 标记，仍会被 pair 过滤，可能继续后备。未运行该反例，不表示指纹认证被绕过。工作树已用原 Retryable() 标记保留明确停止原因，普通同伴取消仍过滤；没有改变通用 Retryable 或 78 含义。errors.As 的首匹配规则只按当前归一化错误链使用，不宣传为任意错误树算法。
- 原固定角色已集中认可两处最小设计，要求回归保留实际各端 Exec 结果。现有真实 OpenSSH fixture 加入两方向 78/77/23 与无 route 78 表：先 peer ready，再 listener 23 触发内部取消，TERM ack 后才释放 peer；观察适配器原样转发真实 Exec，并验证原 listener 23、取消+精确 peer 状态、父 context 存活及最终错误链。不是模拟 pair、真实指纹变化或完整归档 E2E，不用 sleep 抢调度。临时入口替换已完成的旧对照，只对比 a247 的两条新 P2 旧红/新绿及六条双绿控制，编译/前置失败不算红。
- **唯一下一步：冻结同一完整候选并统一 hosted 验证，再交原固定角色集中实现复审。** 本地增量只做 gofmt/diff 检查，未测试/构建/GUI。输入器已先看 Popen/实时 AX，已前台时不查询 NS；需要原一次激活却无 NS 对象仍失败。原权限、单窗口/几何、最终 exact PID、fail-safe 与期限保留；沿原普通启动全链验，不假造 NS 内部根因。新增回归与上述最小修正、结果文档同批，既有红绿不重跑；H/P/A–P 与重要移动警示不关闭，未合并/发布。

## 已测基点183279b及本批准备

- **本轮progress，已测完整候选仍 `183279bf4dea03846ddf62c4395645a8be8cd7ec`，HEAD/远端/下次lease均相同。** 本地准备同一批Bash监护及退出未知传播修正、必要回归与文档，尚未提交/测试；仅gofmt/diff检查。没有本地构建/GUI、部署/合并/发布或活动工具等待句柄。
- 主 `36650078687` 首次600秒仅观察 timeout；本轮遵照接续指示，用指定脚本继续读取同一 run，已终止 **success**，未重新触发。实际工件均已下载私有目录并核验：Go/race/重复sudo无fail、audit0；普通无参数启动7项通过，涵盖创建、错误密码保持锁定、手动锁定、重启以及程序旁/系统回退/优先级；5次真实启动均argc1、不同cwd、初始锁定，库头hint/privateFile/passwordAbsent全通过。原native exercise22/restore4/changed-key4和独立文件系统8项通过；主机选择部分仍为原明确标注的DOM动作，不冒充OS输入。源码merge/parents/tree与SSH相同；成品SHA-256=`191fee625cc35ba90abdfb1a4ea14ab02e49cbbda4270f24f5b90be88a411ba6`，只下载未部署。一次焦点对照NS/AX均匹配，不能倒推旧失败的缓存根因。以上是所列流程证据，不是整个H/P/A–P批准。
- SSH `36650078700` 已终止 failure，失败日志只按专用脚本读取一次、所有实际工件已下载 `/Users/fanli/.codex/private/dragfm-issue3-ci-183279b/`。34 根中 33 pass/1 fail：`TestHostedNcatWithoutExecutableHelperRetriesBlockedPorts/target-pull` 90.07 秒。新增实际错误明确进入父期限分支前的pair耗时87.658秒（不包含随后取消收尾）、已消费源结果=true/目标=false，目标取消错误之后附共享stderr中Bash正常 `wait -f` 行重复 `wait_for: No record of process`；仅保留两端共享8KiB尾部，未知报错侧/起始时刻/端口尝试次序，不把全部耗时归因该循环，也不把失败后未执行的保源/清理断言算通过。原a1/a3用例本次过，历史原因仍未知。三组COMMIT=`8dac57c96b1697efaf227bf4f53bcf2e6bc0ea08`，parents=基线+183279b，tree=`374f8862d59d4349c4d8457759135c674044c846`与候选匹配。
- 固定角色已完成当前焦点限定实现复审，评论 `5901667570`：无新增 P1/P2，设计/静态复审不代替 H 实测；两处 P3 证据保存/显示恢复错误可能遮蔽首因或漏存计数，已记 `postponed-tasks.md`，不为之另推/重跑。新 Bash 错误与源码行号、原90秒场景已交原角色继续只读审计；不新开角色，不猜改 wait/期限或放宽测试。
- 固定角色已集中认可设计方向并指出取消、二级native桥接和解包三个传播缺口。工作树现在将两处wait改为`-f %1`，spawn到捕获状态/退出放在预先解析的同一brace group；保持唯一后台作业、原精确PGID、信号、启动窗口和fd9。127无法区分工具退出/无job，保守转既有77，trap也保留77。所有pair Exec出口与独立解包归一化77，78只在原route协议解释；SSH取消保留原结构化退出错误而不全局解释77，Python native对端77保留ExitUnconfirmed。没有升级用户Bash、回退普通wait、按stderr控制重试或放宽期限。
- 待验失败清单：快速完成job丢失、旧PID等待释放后读取、STOP被误判、取消吞77、native对端77降为1、解包漏未知分类；保留原完整传输/移动/STOP验收。新增仅真实监护/SSH错误边界表及实际Paramiko/native/取消回归；同一临时hosted红绿入口用183旧产品和相同新回归验证，不以编译/配置失败当红。真实Bash在与SSH相同Dockerfile的映像运行，glibc扰动仅监护回归子进程，记录Bash/libc包版本；不保证扰动必然复现，必须取得实际No-record行为失败，否则红未建立。127分类变化单独说明，不冒充又一次UAF复现。
- **唯一下一步：冻结上述完整跨层修正，amend同一候选并交既有主/SSH与一次临时红绿入口统一验证，再由原固定角色集中复审。** 两个183 run均终态，不再等待/重跑。当前新代码全部未验，H/P/A–P未批准，重要移动警示和历史超时保留。

## 历史：183279b 准备与 eba99e6 结果

- **eba99e6 两个 run 已终止，实际工件已核对；本轮 progress。** HEAD/远端/下次 lease=`eba99e60324524a07219f35a2b908a0149be630e`。主 `36647102905` failure，SSH `36647102944` success。主失败日志仅读一次；全部下载原始证据仅 `/Users/fanli/.codex/private/dragfm-issue3-ci-eba99e6/`。无活动 workflow/下载/等待句柄，无本地测试/编译/GUI、部署、合并或发布，工作树仅结果文档。
- SSH 实际 34 根 pass/0 fail，a1 反向免密 9.24 秒、a3 强制反向 SOCKS ncat case3 21.96 秒通过，没有触发新增超时证据；两个旧超时原因仍未知，不能把绿色当修复。三个 COMMIT 为 `a9797e0b890dc4fce45d8cd0c41b248656ad0381`，parents=基线+eba99e6，tree `ecc7b45f0933c719067913550293726faed33370` 匹配。
- 主 test/source 成功，Go/race/重复 sudo 无 fail、audit0。H 真正完成普通 portable 创建、精确库头、手动锁定、退出；锁定观察 `staleSnapshots=1` 后取到完整 unlock/1字段/正确 hint，支持原 AX 失效快照处理。第二次无参数启动也读到正确 unlock/1字段/hint，首次鼠标动作前却因 `native input target did not gain foreground focus` 失败；未输入错误密码、未成功解锁重启库、未执行 fallback/优先级和旧 native 流程，不宣称 H 完整通过或有当前可交付成品。
- 本轮源码契约复核：普通启动 perform 在主线程，后面 exercise 等在 reader 线程，均无 Cocoa 主事件循环。Apple 明确易变状态依赖主 runloop；PyObjC 已提供 AppHelper，但整体改调度会扩大验收脚本生命周期。选择现成 ApplicationServices 桥接直接读取 `AXFocusedApplication`/PID，依赖固定到官方当前 12.2.2；不自行封装 C ABI、不另建事件/轮询框架。尚无同刻 live 焦点对照，不断言 eba99e6 必为缓存假阴性或 a1 旧 nil 已确诊。
- 已准备但未运行的最小增量：初始化明确设置 Python 进程的 AX 两秒消息期限；动作前检查一次、原窗口几何核对后发键前再查 exact PID。原应用存在、Popen 存活、单窗口/几何、Quartz 权限与 fail-safe 继续保留。缺 AX 权限/无值/错误/错误类型/PID 不符均停止，绝不回退缓存；仍至多原一次 activate/0.15 秒，无新重试、慢发键或权限修改。仅第二次普通启动首个动作记录一次非原子 NS/AX 布尔对照、线程、存活/激活状态与读取耗时；不含 PID、窗口内容或输入值。失败也保存既有 OS_INPUT_ACTIONS。两个原有调用入口依赖同步，release 仅改声明而不触发。
- 原固定角色本轮已完成独立设计复核，明确推荐 B 的 Python 同步 AX 读取；支持保持原 nil/权限失败即停止、最终读数放在几何核对后、仅一次现场对照。已按这些边界收尾；角色只读 eba99e6 源码和提供的证据，不冒充新补丁已验或整 PR 批准。**唯一下一步：冻结并 amend 同一完整候选，统一 GitHub-hosted 验证。** 本地只有上述未验改动和文档，只做 diff 检查，无测试/编译/GUI/CI 重跑。H/P/A–P、重要移动警示与旧超时仍保留。

## 历史：eba99e6 准备与提交

- **已推 eba99e6，等待原 hosted 门禁。** HEAD/远端/下次 lease=`eba99e60324524a07219f35a2b908a0149be630e`，主 `36647102905`、SSH `36647102944`。仍为同一完整候选 amend，PR body 本次成功同步。原生观察/原错误出口取证及文档已冻结，无新增测试/CI入口、无多平台/部署/合并/发布。唯一下一步按指定脚本等待这两个既有 run，不重启或另触发验证。
- **本轮接续并准备统一验证，上一轮为 progress。** 已核对 a3a13a0 HEAD/四个既有改动和角色完成回复，无活动 workflow。固定角色赞同只作废 AX invalidUIElement 的整次快照、消耗原采样次数，不重做点击/输入；未确认反向 ncat 的必现死锁，明确本用例不是 GUI 后台队列或保险库 journal 链。角色只读源码，未查 CI/私有日志，不将其结论扩成验收。
- 新增取证最小范围：原策略用例失败时输出已保存的 Started/Elapsed（包含 attempt defer）、准备/策略耗时与期限；不改五分钟 ctx、网络、方向或期望。ncat 原取消错误出口保留已有 8 KiB stderr、两端原始错误及 errors.Is 链，标注 pair 耗时与主循环是否已消费结果，不把这些布尔值冒充远端进程存活。没有新事件协议、定时监控、远端诊断命令/参数/凭据；没有新增测试。取消/TERM-KILL/wait/租约/清理/重试分支完全保留，仍未宣称修复旧超时。
- 集中内部复核：失效快照不能部分成功；未知退出标记必须穿过新增 error 包装，stderr 只沿已有脱敏队列或用例 redactor 出口；不能据收到拒绝文本提前取消。H 观察修正与本轮错误层级取证作为同一完整候选统一 hosted，不重复旧红绿、不扩平台。本地仅 gofmt/diff 检查，未测试/构建。**唯一下一步：冻结并推候选，读取原两条 hosted 门禁实际结果。**

## 已测基点：a3a13a0

- **a3a13a0 两个 run 均已终止，当前无活动 workflow/下载/等待句柄。** HEAD/远端/下次 lease=`a3a13a0a5f5f940c4c5a9cd95f80471d4c1b00da`。主 `36644593753` failure，SSH `36644593872` failure；日志按指定脚本各取一次，实际工件全部下载私有目录 `/Users/fanli/.codex/private/dragfm-issue3-ci-a3a13a0/` 并核对。没有本地测试/编译/GUI、部署、合并或发布。本轮取得字段真实旧失败/新越过证据，分类 progress。
- 主 test/source 通过，Go/race/重复 sudo 无 fail、audit0。新提示截图保留小写，实际库头 `hintMatched=true/privateFile=true/passwordAbsent=true`；与 49a2262 原场景构成有限字段修正行为对照，不推断三属性各自必要或完整 H 通过。随后手动锁定后的 `observe('unlock')` 被 AX `-25202 invalidUIElement` 截断，H 后续和旧 native exercise/restore/changed-key 没有运行，没有新的已验收成品。
- SSH 实际 34 根中 33 pass/1 fail；原反向普通 system rsync 7.95 秒通过，但 a1dfa42 超时仍原因未知。新失败为 `TestHostedReviewedRouteFailureRecovery/fixture-case=3`，300.33 秒期限：双端拒 helper/关闭 rsync-SCP/双向直接 TCP REJECT，要求目标经 SOCKS ncat 拉取；source-push ncat 五端口均 Connection refused，target-pull ncat 在 deadline 时返回三项 context deadline，尚未进入 SOCKS。既有策略事件只在失败 defer 打印，不能从打印时间推断实际哪段耗时；当前未确认进程、取消或租约死锁，不回退 wait -f/放宽期限/重跑销账。三工件 merge `fb2a8f3c25a2077b1b74023a5231af1d0fcf106a` 的 parents=基线+a3a13a0，tree `2c8f146174ce696eb636badfb628ed5fb47e9b89` 已与 HEAD 核验。
- 工作树已准备但未验的 H 最小观察修正：仅将 AX invalidUIElement 作为整次快照作废，沿原 0/30/60 有限观察从应用窗口重新取树；失效计数写回既有报告。不能跳过失效节点拼出成功，也不改状态/提示断言、期限、输入器、TCC 或系统设置。Apple 官方码值支持失效对象的故障层级，不等于产品已正确锁定。只做 diff 检查，未本地编译 Swift。
- **唯一下一步：根据固定角色对反向 ncat 原链的集中走读确定最少取证或修正。** 已向原角色发送上述实际结果、ncat 调用链与 AX 边界；一个 210 秒有限等待超时，权威状态仍 active，不冒充其认可，也不新开会话/重复等待。本地走读尚无必现死锁证据；原错误分支省略 ncat stderr、原事件日志省略已有 Started/Elapsed，是目前明确的证据缺口。不要猜改超时或制造新框架。H 观察修正与确定的主传输处理形成同一完整候选后再统一 hosted，当前仅未提交增量；A–P 不勾选、安全警示保留。
- 本轮 PR body 同步调用遇 GitHub GraphQL EOF，未确认远端更新成功；本地 `ISSUE3_CANDIDATE.md` 已保存完整结果，下个有意义里程碑再同步，不循环重试、不把外部接口错误算成产品失败。四个工作树改动均为本轮观察修正/结果文档，未提交。

## 历史：a3a13a0 准备与提交

- **已推 a3a13a0，等待原门禁。** HEAD/远端/下次 lease=`a3a13a0a5f5f940c4c5a9cd95f80471d4c1b00da`，主 run `36644593753`、SSH run `36644593872`；PR body 已成功更新。只有提示字段三属性与结果文档进入本次 amend，仍为同一个完整交付提交；未新增测试/CI入口、不触发多平台或发布。下方修正准备已完成，唯一下一步读取这两个 run 的终态和真实工件，不重复启动。
- **本轮从 49a2262 接续，上一轮为 progress。** 已核对 HEAD/工作树/游标和固定角色的已完成意见，当前无活动 workflow/等待。新产品增量只在创建保险库的提示字段设置 `autoCorrect="off" autoCapitalize="none" spellCheck={false}`；发送器、密码字段、onChange/API/TrimSpace/保险库保存均不变。仅候选未验，不强行转小写/归一化/改写旧提示、不改系统文字服务。固定角色确认当前证据足以支持这一有限字段修正，但不是精确系统分支归因或 H 验收。
- 失败方式与验证：若属性未被实际 WebKit 遵守，原逐字库头断言仍失败；若再遇应用不可识别，只算前置未建立，不能作为文字修正红/绿。沿用 49a2262 原 native 流程、随机小写提示、无密码截图、所有期限与比较，不新增源码属性匹配测试，不读取密码字段或重新打字。49a2262 保存前大写截图/库头失败为旧行为证据；本轮新结果尚未取得。参考 WHATWG autocorrection/autocapitalization 语义；后者通常不影响实体键盘，不声称三项各自都已证明必要。集中内部复核已完成，仅 diff 检查后整批 amend/push，交给既有 hosted 门禁，不新增 workflow 或扩平台。
- **唯一下一步：冻结上述字段修正并验证原普通启动全链。** 下方 49a2262 状态是上轮已测基点；本轮修正未部署/合并/发布。反向超时仍未解，不因上轮 SSH 绿色销账，原诊断保留。

## 已测基点：49a2262

- **49a2262 已取得终态；无活动 workflow 或等待句柄。** HEAD/远端/下次 lease 为 `49a22629005da6297da38c9d3b92866cb7b2b0f6`。主 `36642350653` failure，SSH `36642350676` success。全部现有工件已下载并检查；主失败日志按专用脚本仅读一次。原始证据仅 `/Users/fanli/.codex/private/dragfm-issue3-ci-49a2262/`，没有本地测试/构建/GUI、部署、合并或发布。用户再次确认完整候选先推、hosted 后验证的长期授权，不再询问；并不授予碎片提交或验收前合并/发布。
- SSH 三组实际 34 根全部 pass；原反向普通 system rsync 子例 11.23 秒通过，未触发新增超时诊断。生产代码未改，故 a1dfa42 的超时仍原因未知，不能因本次绿色销账。三份 COMMIT 都为 `4f72f7a392a141bf9d93801abc60ac0e1037cd9b`，API parents=原基线+49a2262，tree=`f1fbaac8470bdc3ef45c1f8cdb030c4dbd4ef0a4` 与候选一致。
- 主 test/source 通过；实际 Go/race/重复 sudo 工件无 fail，npm audit0。H 普通创建已进入 workspace，但加密头 hint 精确比较失败：两者长度均 27，privateFile/passwordAbsent 为 true。新截图在输入任何主密码之前已显示小写 `plain-…` 变成 `Plain-…`，有文本转换下划线；差异已出现在输入界面，不能直接归为选错保险库/明文泄漏/持久化损坏。Unlock 的普通 text 提示字段未关闭文本更正；后端只 TrimSpace，没有改大小写。截图不包含主密码；原生后续未运行，没有当前可交付成品。
- 固定角色的队列诊断回复已读取：无已确认必现死锁，建议保留最后策略转换/终态投递边界、两套期限与有界快照；控制机栈不能替代远端栈。本次未复现，不为取证工具单独再推/重跑。角色已收到 49a2262 结果和 H 新截图的脱敏事实，正核对提示字段最小处理边界，不另开会话或全 PR 复审。
- **唯一下一步：依据 H 的保存前大小写变化核对最小输入修正。** 输入驱动继续冻结，不重打/加时/改精确期望或系统设置；先结合固定角色与 WebKit 官方资料判断提示字段是否应明确关闭 autocorrect/autocapitalize/spellcheck，再决定改动。反向超时旧失败保留未解，A–P 不勾选，不移动重要文件、不扩多平台。当前工作树只含结果文档。

## 历史：49a2262 取证准备与 a1dfa42 结果

- **已推 49a2262，等一次 hosted 取证。** 当前 HEAD/远端/下次 lease 为 `49a22629005da6297da38c9d3b92866cb7b2b0f6`。主 `36642350653`、SSH `36642350676`；PR body 更新已确认。新候选仅原失败分支的诊断和本批结果文档，不改变生产行为/期限/期待，不移除任何门禁。已有用户主流程完整候选仍为同一个 amend 提交，无新临时红绿、六平台、部署、合并或发布。读取脚本的唯一活动句柄会在工具执行时返回；不要重启这些 run。
- **本轮继续真实反向免密终态定位，尚未推新候选。** 已核对当前 a1dfa42 源码/工作树/失败工件；上一目标轮实际改源码、取得监护旧失败/新通过和新失败层级，分类为 progress。固定角色正式评论 https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5900492963 已读回：限定监护机制的行为证据成立，整体不批准。新源码走读尚未确认反向队列卡住或事件丢失；不能凭 180 秒 test ctx 推定独立 queue ctx 已取消。
- 已准备原用例的一次性超时证据：原脱敏 last job:update/attemptFailures、同 ID 权威队列快照与 Revision/阶段/计数；先用标准 runtime.GoroutineProfile/CallersFrames 保存相关函数名/行号，不读取参数值、地址或原始栈/路径。不改产品、超时、成功期望、PID/网络规则，不新增测试。失败方式：若事件丢失要与任务仍 Running 区分；若 native 已返回要区分后置 SHA/sync/清理；若诊断容量不足明确标注而不伪称完整。锁外复制已收集事件，快照只用已有队列接口，不再发远端命令或先取消制造故障。
- H 输入适配保持冻结未验，本轮不盲调/重打/延长时限。固定角色正对这一队列链做集中只读诊断，不另开会话/agent；当前无活动 workflow/运行句柄。完成内部走读后只冻结同一完整候选，由已有 hosted 门禁取得一次证据；不重跑已完成红绿或扩多平台。
- 固定角色等待器首次返回外部读取错误 `Can't load conversation`，已停止该等待，未反复查询/新建替代会话，也不把接口故障当产品失败。新评审结论尚未读取；主会话的集中内部走读已完成。诊断仍仅原失败分支，增加实际 test deadline/观测时刻/任务开始结束时刻，避免把整个三分钟夹具预算误当传输本身运行三分钟，或把取证期间才到达的终态称作事件丢失。该独立只读评审的暂时不可读不阻止既有授权范围内的安全 hosted 取证。
- **当前已测 HEAD/远端/下一次 lease 为 `a1dfa42713a7413c26b5f78878f1bcb22174294d`，本轮 progress，整体仍未验收。** 主 `36639594738`、SSH `36639594722` 均已终止 failure；失败日志分别仅读一次，所有现有测试/native/SSH 工件已下载 `/Users/fanli/.codex/private/dragfm-issue3-ci-a1dfa42/`。无活动 workflow、下载或等待句柄，没有本地测试/构建/GUI、部署、合并或发布。工作树只留当前结果文档，产品监护修正已在已测候选中。
- 主 test/source 成功，Go/race 无 fail、audit0。普通 native 本轮首次 OS 点击前失败：Swift AX 已看到同 PID 的 create/3fields，随后 Python NativeInput 的 NSRunningApplication 返回 nil，报 `native acceptance application is unavailable`。没有输入 hint、截图或写库，本轮不能评价此前真实库头 hint 不匹配是否仍出现；H 后续与旧 exercise/restore/changed-key 全未运行，原生成品未交付。停止盲调/重复输入/加超时，H 保留未验；既有正确功能补验的证据口径问题仍未收到用户答复。
- SSH core/protected 成功，transports 失败；34 根中 33 pass/1 fail。原 peer-writer 四格全 pass。system/rsync 的真实 G/R group-stop 后 Bash 保持存活、父关系不变、exit_status0，随后才出现 planned_ssh_sigkill；原 partial/源哈希/不发布目标/独立浏览/收尾断言全过。与 96a1b0e 的相同原场景 147 提前退出形成行为对照，支持监护 wait 修正；旧 exit20 的具体信号来源仍不倒写确诊。三个工件 merge `286d97613c1b4156227832a7c1a79105d29c5122` 的 API parents=原基线+a1dfa42，tree `6041ac8de758384114b17a0ba6630dbcf10ac3bb` 已核对。
- **唯一下一步：诊断反向免密队列未终止。** 唯一新失败 `TestHostedInitiatingUserPasswordlessQueuedTransfer/scp=false/pull=true/system=true/root=false` 在 `initiator_identity_integration_test.go:325` 等 terminal 事件到 180 秒期限，只有 context deadline exceeded，未给出下层错误。先读该用例与实际 queue/strategy/native 完成链，利用已有 attempt/队列状态确定故障层，不猜作网络问题、不加时限、不重跑冲绿。修复主传输优先于继续整改 H 输入工具。H/P/A–P 与重要文件移动警示保持；不扩多平台矩阵。已把本轮实际结果送固定角色，仅请求记录有限监护结论而非再审整个 PR，其回复尚待下一有意义里程碑读取。

## 历史：a1dfa42 准备与 96a1b0e 定位

- **已推 a1dfa42，等 hosted 终态。** HEAD/远端/下次 lease 为 `a1dfa42713a7413c26b5f78878f1bcb22174294d`；主 `36639594738`、SSH `36639594722`。监护修正与 H 最小取证同一完整候选提交，原角色已收到冻结范围，不重复逐行评审。没有本地测试/构建/GUI、部署、合并或发布。一次 PR body 更新遇 GitHub GraphQL EOF，未确认成功；这是外部接口问题，不是产品/测试失败，下一次结果汇总时再带新内容更新，不循环重试。
- **96a1b0e 两个 run 已终止，已取得真实监护故障证据，整体未通过。** 主 `36637208797`：test/source 成功，Go/race 无 fail、audit0。H 普通创建后磁盘头部 `hintMatched=false/privateFile=true/passwordAbsent=true`，更早的真实文件校验截住；不只是 AX 读取提示失败，但还不能区分实际系统输入与持久化之间的差异。未改产品保险库/前端，不再猜改输入或断言；下一次只在输入随机公开 hint、尚未输入任何主密码时留窗口证据，附头部长度，禁止截图已填写密码的表单（输入误定向也可能泄露）。H 后续和旧原生 exercise 均未执行，不能算本轮通过。
- SSH `36637208835` 仍 34 根中 33 pass/1 fail；新诊断字段冲突已修，helper/scp、helper/rsync、system/scp 三格通过。system/rsync 实际 Bash5.2.21/rsync3.2.7：初始 G→Bash→SSH；G 的 observer SIGSTOP→CONT→group-stop/LISTEN 后，处理 R 停止时 Bash 的 proc.stat exit_status=37632（147<<8），随即 Bash 消失、活 G.ppid=1，SSH仍活。原断言因 `no owning SSH session process` 失败，未执行 planned_ssh_sigkill，之后才 observer_teardown。这证明真实监护在活子进程暂停后退出；不证明旧 exit20 的 HUP/TERM 来源，本轮未观察到它们。
- 96a1b0e 三 SSH 工件 merge `6a51fdd3e33438098241dac67768675dbcd4b083`，API parents=原基线+96a1b0e，tree `8e46f3249a22ada4225fb48f3419fcb5f72fa0a8` 匹配。两份失败日志分别仅读一次，原始工件 `/Users/fanli/.codex/private/dragfm-issue3-ci-96a1b0e/`。无活动 workflow、下载或等待句柄。真实消息已送原角色作针对性根因/修正边界复审，尚未收到结论；不是重新做全 PR 审计。
- 当前未提交修正：真实 remote Linux 系统监护两处 wait 改为 wait -f，必须等终止而非 STOP；新监护 shell 尚无子进程时先空 wait -f 探测，不支持就明确失败且不启动 writer。原 trap、进程组 kill、租约和清理范围保留；原 peer 回归所有断言、PID/ptrace、数据量、超时不变。已有 96a1b0e 真失败是旧行为证据，修正尚未跑，不追加源码匹配或“一行包装”测试。等待集中根因复核后与 H 最小证据一起形成下一完整候选；不重做已完成的安全红绿或多平台发布。
- 固定角色已完成这次真实故障的集中复核（未查 CI/私有工件、未新增评论）：96a1b0e 的 STOP→147→监护消失/子进程仍活结合固定源码，足以定位产品监护缺陷；不必再加产品 wait 日志。确认实际新监护 shell 启动前空 `builtin wait -f` 可作能力检查，两处等待都明确 builtin；保留 set -m、stopping/trap、组内 TERM/KILL、原始退出码、退出未知/租约门禁，不能把直接子进程 wait 当整个组退出证明。已按该边界统一准备，下个候选沿既有完整 peer 主流程取绿，不增加新测试或修改期望。

## 历史：本轮两个候选的准备和先前证据

- **当前已推 96a1b0e，正在等一次 hosted 取证。** HEAD/远端/下次 lease：`96a1b0ed715fb51d5aa13153ab546ec0c2158933`。主 `36637208797`，SSH `36637208835`；同一交付提交的完整 amend，无额外红绿/平台 workflow。下方 ceb5c7f 原始失败保留；其 SSH merge `21b0798221c5c96ee5599782c326756cc94f4e1f` 的 API parents 为原基线+ceb，tree `3f268d70767d7f129712f6291639505fde3fe5c1` 已核对。原角色收到实际结果与修正摘要，本次小修不反复启动静态复审，等实际有限诊断再集中判断。
- **ceb5c7f 两个 run 已终止且证据已读，本轮有进展但未通过。** 主 `36635355610`：test/source 成功，Go/race 工件无 fail、audit total=0；macOS 成品已编译并通过普通无参数创建进入工作区，截图已核对，随后 `Manual lock did not return to the selected encrypted vault` 断言失败。Swift/AX 和真实键鼠已经运行，不是权限 blocker；现有报告未区分字段数量和 hint 匹配，因此尚不能归为产品或 AX 文本读取问题。之后的五场景闭环、旧 exercise/restore/changed-key 都未运行，不借 f153603 通过代替。未取得本候选可交付工件。
- SSH `36635355707`：protected/transports 成功，core 失败；34 根中 33 pass/1 fail，peer-writer 四个子例均在新 `event(name, **identity)` 的 name 字段重名处 TypeError，位于 ptrace 前。不是旧 rsync exit20 再次复现；未取得信号原因。固定评审已独立静态指出相同 P2，并正式评论 https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5899711187 ，其未看本轮 CI。原始日志仅各读一次，工件已下载到 `/Users/fanli/.codex/private/dragfm-issue3-ci-ceb5c7f/`；无活动 workflow/等待/下载句柄。
- 下一候选准备：明确重名错误只将事件种类参数改为 kind；CONT 成功后才记录，Go 结果标签澄清为 observer 清理后/Go cancel 前，不把清理当首因。H 只补有限观测字段（数量、hint 匹配来源布尔值）和创建后的真实加密头校验，以及失败锁定页截图；不读密码值、不猜改产品、不放宽断言。再次失败时依实际证据决定，不靠反复调等待冲绿。
- **已推候选 ceb5c7f，hosted 已终止，结果见上。** HEAD/远端及下次 lease 为 `ceb5c7f072e53b4aeb66b79797a05b4ca99ef0dc`。主 run `36635355610`，SSH run `36635355707`；临时红绿不再触发。原固定角色已完成这一 SHA 的完整增量复审，实现会话独占 CI 读取。仅一次整批 amend/push，没有部署、合并、发布或本地测试/构建。
- **本轮接续：H 普通成品启动补验与 peer-writer 信号取证已准备，尚未运行。** 保留下方 f153603 证据；工作树增量是既有 native harness 内的普通无参数启动阶段，以及原 peer-writer 用例的有界内核事件诊断。没有修改产品的监护 wait/信号行为，没有修复声明。既有完整候选先推送再 hosted 验证的长期授权继续有效，不再就提交先后反复询问；既有正确功能补验能否计入验收、不伪造旧 bug 红的口径已异步询问，尚未收到答复。
- H：复制同一 macOS arm64 成品到一次性目录，真实 argc=1、不同 cwd、未替换 HOME、无 vault 参数/DOM 注入/stdin RPC；用系统 AX 仅观察状态和几何、Quartz 实际键鼠创建/解锁/锁定/退出。验证新安装程序旁私有加密保险库、错误密码仍锁定、正常重启、真实 0555 目录回退标准系统目录、恢复可写后继续复用系统保险库、两者存在时程序旁优先。只使用已有辅助功能授权，不弹授权提示/改 TCC；已有系统配置则停止，不覆盖。仅清理本次正向识别的新保险库。
- 本轮失败方式及集中内部走读：AX/显示/几何不可用属 hosted 环境或 harness 未验，不能回退产品测试钩子冒充普通启动；错库/明文/非私有文件/重启免密/错误密码放行均失败。密码不进入参数/日志/截图，helper 只输出已知静态状态、坐标和布尔值。五个无参数进程均需真实退出；未知配置不删除。本地仅阅读、gofmt、diff 检查，未编译 Swift 或运行任何测试。
- 固定评审新的只读定位指出：`ncatOwnedCommand` 的 job control + 普通 wait 可能在 STOP 后提前返回（GNU Bash 文档支持）；rsync exit 20 可为 INT/TERM/HUP 或错误传播，当前无法证明发送者。旧说法“故障注入前”应精确为“计划 SSH SIGKILL 前，已执行 ptrace/SIGSTOP 后”；不能把它归为纯夹具或已经确诊产品。原用例现在仅记录选中 PID 顺序/父组关系、实际包版本、内核 wait/siginfo、实际继续信号及计划 kill/teardown 时序，不记 argv/路径；已消费终态记账避免 finally 二次 wait 遮盖首因。保留所有原断言、8 秒暂停期限和 32 MiB 数据；没有第三次猜改暂停策略。若本次不复现只算正常轨迹，旧失败不销账。
- f153603 一次性红绿 workflow 已从当前自动入口移除，四条真实红绿的源码/运行/工件仍保留在 f153603；永久回归和 main/SSH 门禁不变，不重复取证或重跑冲绿。
- **上轮已测接续点：f153603 已取得全部终态与集中复审，整体仍未验收。** HEAD/远端尚为 `f153603c1851a51218d369b3ac75965bfed7eae4`，这是下次 amend/push 的 lease 值。上轮完成整体候选 push、真实红绿与原生工件核对、原角色集中复审，属于 progress。当前无活动 workflow/下载/等待句柄；本轮未测工作树增量见本节首部，不重复运行已完成的一次性红绿。无本地测试/构建/GUI、部署、合并或发布。
- f153603：红绿 `36631196144` success，目标新子目录/文件被替换及 root-only 源 rsync/SCP move 的 4 条旧红/新绿成立，unchanged 正例双通过；结果及实际事件已读。主 `36631196256` success，完整 Go/race/vet/前端及 macOS arm64 原生通过；取消复制回归在 unit/race 都 pass。原生成品 exercise 22 项、restore 4 项、changed-key 4 项及独立文件系统 8 项均 success；Running/Pending 截图已目视核对。二进制本地 SHA、SHA256SUMS.wails、native BINARY_SHA256 三方一致为 `6a590e4933c139f65ebe03a8cf66a383054018b04e3127308bbfb99e11667eb0`，只下载至私有目录，没有部署。
- f153603 SSH `36631195996` 整体 failure：protected/transports success，core 失败；实际 34 根中 33 pass、1 fail。唯一失败仍为 peer-writer system/rsync 注入前 `wait_status=5120 exit_code=20`，随后 finally 再 wait 出 ECHILD；没有建立活 writer group-stop 前提，不是产品保源失败/通过的证据。该夹具已多轮失败，停止猜改/重跑冲绿，原失败保留；建议下一步取真实工具退出/信号来源证据后才设计修正，不放宽断言或删除门禁。三 SSH 工件 merge `38bdaa80641faaf64f652ad2f367bb1ded5c50bc` API 父提交为原基线+f153603，树 `5508caec65216d5a800b909ddf71e2062b95b25c` 匹配。日志仅读一次、原始证据 `/Users/fanli/.codex/private/dragfm-issue3-ci-f153603/`。
- 原固定角色已完成 f153603 复审并直接发布 https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5899040876 ：指定目标子项 P1/旧高权源视图 P2 的源码修正接入实际链，限定增量无新增 P1/P2；其未读取 CI，不能把静态结论扩成全 PR 批准。上述 hosted 结果随后同步给原角色。H 普通无参数 vault.Locate 成品入口、P 完整打包与整个 A–P 验收仍缺；重要文件移动警示保留。
- 本轮实现：Manifest 仅在控制机保留初始目标物理路径、源/目标目录身份、首次发布边界；换方法不缩小原边界。最终完成只读复核这些证据，不再次写目录探针。最终源哈希之后再次核对已校验目标的同端 dev/inode，父路径重定向即保源。仅载体的共享父目录不强行恢复旧 mtime；实际复制目录仍保留并发检查。同步只覆盖复制文件/目录、新建父链和首个既有发布父目录，不要求读取更高的 0711 祖先。不能抵御最后验证与删除间所有外部命名空间竞争，不承诺 namespace/ctime 无副作用。
- 2be964d 证据：红绿 `36628593248` success，7 条真实旧红/新绿（父链接绑定、1777/0555/0711、三个最终同步失败）及 2 正例双通过；所有 result.json 和实际权限事件已核对。SSH `36628593175` success，三个分组 33 根 pass/0 fail；COMMIT `d5172efed5e8d5e497e4c5892280c50690446040` 的父提交经 API 核对为原基线+2be964d，树 `3207938588c26ad00e5317617178921ac101f508` 一致。主 `36628593394` test failure、macOS skipped：取消复制测试的 cancellingEndpoint 隐藏 FileVersion，在 snapshot 阶段报 unsupported，尚未到取消阶段；audit total=0，无新 race/native 成品证据。原始工件 `/Users/fanli/.codex/private/dragfm-issue3-ci-2be964d/`，主失败日志仅读一次。
- 上批夹具改动（SFTP 相对链接、reviewEndpoint 能力、chroot /dev/null、POSIX payload 观察）已越过对应失败；旧 peer-writer system/rsync 提前退出问题仍未解决，不能因这轮成功销账。原固定评审已完成 2be964d 静态复审，但该会话无评论接口，实施会话代录于 https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5898774116 ：新建目标子项未纳入最终绑定 P1；旧无 transferAccess 的高权源视图在目标完成分支丢失 P2。均为静态反例，未报告实际数据损失。
- f153603 实现范围（证据见本节上方，不等于全部兼容性）：强快照逐项保留同端身份，并在该项 hash/list 前后核对；最终源哈希后复查所有本次复制的目标条目，而非只查根，源快照比较同样不接受已知子项身份丢失。不会把 target inode 与 source inode 跨机比较，不额外写 0555 目录。`snapshotForAgentAttempt` 将实际获准源文件视图留在调用方 operation，贯穿 direct/Hans/stream 后续完成；不新开高权账户。取消测试只透传底层 FileVersion，不暴露 NativeCopier 绕过取消。新增真实子目录/文件替换及 unchanged 正例、真实 OpenSSH/helper root-only 源 rsync/SCP move，临时对照旧侧均 2be964d；先前红绿不重复取证。
- **唯一下一步：** 冻结上述 H 普通入口与原 peer-writer 取证增量，以一个完整候选 amend/push，再一次 hosted main/SSH 验证并回原角色集中复审。已通过安全红绿不重做；若 peer 未复现只保留正常轨迹，不销账，若 AX 不可用如实记环境/入口未验。H 不用强制 fixture 路径替代。P 按用户 Mac-first 边界，不自行发六平台构建或 release。A–P 保持完整范围。

## 历史：63b4790 验证与后续设计起点

- **本批安全候选已推送，验证尚未通过。** 当前 HEAD/远端及下次 force-with-lease 值 `63b479032d4b0ef900aeb3dbe13d15085a1469e2`，上一候选 `c077881f77214291068a5d9f264ca72c91828319`，分支 `fix/issue-3-acceptance`，草稿 PR #4。以下“历史”段不再驱动操作。本轮整批 amend/push、取得实际红绿和 SSH 结果、读回集中评审并准备未提交修正，属于 progress。无微提交。
- R8：确认任务后，以已有 AtomicWriter 的独占未发布 partial 写随机 nonce，检查目标各复制目录/最近存在父目录在源树与有关祖先中的可见性。预览不写、探针不 Commit、不额外连接/提权；清理前核对父目录、文件标识及内容，未确认或并发变动则停止保源。探针不能证明任意未来挂载变化或不一致缓存的隔离；不宣称无 namespace/ctime 副作用，mtime 仅在未观察到并发变化时恢复。SFTP-only 缺文件标识能力的目录任务会安全拒绝，不伪称已兼容。
- R9：强快照的 FileVersion 原始错误和零 inode 不再被吞掉；已知源标识不能降级为未知后删源。普通文件的非破坏性预览/复制不强制该能力。
- R10：目标完整内容/所有权核验后，同一授权文件视图同步数据、最终元数据、目录及发布父链；随后再核对原始源清单并删除。普通 SFTP、helper、SystemFiles、本机与已批准 sudo 均接入。无扩展时只接受有确认退出的 GNU 逐路径 fsync 等价命令；错误不靠换方法洗成成功。已将旧加速完成分支的独立删源代码接回 FinishMove，高权初始/最终快照使用一致的 SFTP 精度。
- 待执行行为证据：真实 Local/SSH 同目录及目标位于源内的队列反例；真实 SSH stat 身份丢失；SFTP 无同步能力/数据同步失败/发布父目录失败/正常移动；加速完成同步失败保源；真实两台独立 SSH 文件系统相同绝对路径移动正例。临时 `issue-3-red-green.yml` 在 c077881 和候选覆盖同一协议/文件系统夹具，要求 11 条行为旧红新绿及 2 条原本正常的正例双通过；符号缺失、编译或夹具失败不算红。
- P 的三个 SSH 工件分组打包输入修正及 undici 锁文件修复随同候选。H 普通无参数 vault.Locate 成品证据、发布打包完整证据仍未完成；不以测试保险库重启或局部门禁代替，不为它们推迟当前安全修复的 hosted 验证。六平台发布不启动。
- 三个 run 已按指定脚本确认终态，均整体 failure；失败日志分别仅读一次，无活动 workflow/下载/等待句柄。红绿 `36623800510`：8 条确切旧红/新绿，身份正常正例双通过；另外四条 SFTP 目录同步场景被夹具相对链接改写提前阻断，不计红绿。主 `36623800411`：前端 14 文件/40 用例通过、npm audit total=0；Go 失败，race/native 未跑，macOS 构建跳过。SSH `36623800488`：protected/transports 成功，core 失败，总 29 根 pass/3 根 fail；独立两 SSH 同绝对路径移动通过。失败为 chroot stat 无身份、peer-writer system/rsync 的 ProcessLookupError、POSIX stopped-directory 观察未建立。旧进程观察不稳定未修，不重跑冲绿。
- 工件身份：三 SSH 分组均记录 merge `b21cd1b92627bdfc472c47fd0e440896b22bc11c`，API 父提交确认为原基线+63b4790，源码树 `b1c07f2bb6b97251a98008df07bce2e9e2442054`。原始日志和工件均在 `/Users/fanli/.codex/private/dragfm-issue3-ci-63b4790/`。无本地测试/构建/GUI、部署、合并或发布。
- 固定角色已完成 63b4790 集中复审并评论 https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5897930808 ：R9 放行缺口静态修正；仍有校验后的目标绑定 P1，探针额外 owner/write 权限和同步全部未修改祖先的 P2。工作树（均本批所有）已补 FinishMove 在最后源校验后、删除前核对 afterTarget 的同端 dev/inode；新增真实文件系统在 SyncPaths 边界原子切换目标父链接的正/反例 `target_binding_test.go`，待以 63b4790 为旧行为基线 hosted 对照，未运行。仅载体的既有父目录清理不再恢复旧 mtime，也不隐藏其并发项变化；实际被复制目录仍保留检查。最终只读空目录问题及必要发布父链跟踪尚未处理，不能宣称完成。
- 已准备两项夹具修正未验证：带真实 exec 的 loopback SFTP 不设置会改写链接 target 的 WithServerWorkingDirectory，与生产 helper 相同；默认凭据夹具保留原 home。reviewEndpoint 仅透传真实 FileVersion/SyncPaths，修复测试包装器隐藏能力，原断言不改。红绿夹具这是首次修正。chroot 脚本只建立 bin/etc/data，缺 /dev/null 会使现有 stat 格式探测的重定向失败；这是静态诊断，尚未补夹具/复验。POSIX observer 对每个 partial 事件先 lstat，可能追到新的短命探针，尚无 stderr 证据，不能确诊。peer-writer 既有多轮不稳定，保持停止猜改并报告，不能用本次其他 PASS 抹掉。
- **唯一下一步：** 将最终目标绑定、初次重叠证明的只读最终复核、仅实际修改目录的持久化范围统一完成，补共享 1777/只读空目录/0711 祖先的真实行为正例；连同上述真实失败的最小修正组成一个后续完整候选后再 amend/push。不得只推夹具小修。P1 新回归以 63b4790 为旧侧，原已取得 8 条红绿不重复冒充新证据。H 普通无参数保险库成品与完整 P 分发仍缺，A–P 未验收；暂不用候选移动重要文件。

## 历史：R8–R10 实施中的记录（非当前游标）

- **当前安全阻断（优先于下列历史 PASS）：** 原固定评审已完成 c077881 集中复核，正式意见 https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5896808812 。R8（Local/SSH 同数据别名）、R9（最终 inode 查询丢失）、R10（SFTP 最终数据/元数据/发布目录未确认持久化）为三条待行为复现的 P1 移动删源缺口，不是已发生数据损失的报告。已请用户暂不用此候选移动重要文件，并发送一次授权的重要问题通知；没有合并、发布或部署。旧 native/SSH 通过只证明各自原场景，不能抵消这三条路径。
- 当前工作树包含 P 的打包输入与 undici 修正、R9 的未验证实现/安全表驱动，以及 R10 的端到端同步能力源码。强校验 Snapshot 保留 FileVersion 原始错误，已知 inode 不得退化为零后继续删除；非破坏性预览/复制仍可将无法取得的身份记为未知，不以此阻断 SFTP-only 复制。固定评审的 R8/R10 设计咨询已完成：分开同机快路径资格与重叠判断，nonce 只能证明限定可见性，不能升级为任意共享子树互不重叠的证明；同步必须保留有效文件权限视图和原失败。没有新建角色，尚未推送。
- R10 未验证实现：FinishMove 在目标哈希/所有权核验之后、最后一次源核验及删除之前，调用目标 SyncPaths，文件后目录自下而上并覆盖发布父链。Local/已批准 sudo 走固定 fsync 文件操作；普通 SFTP 优先原扩展，缺失时可用原账户 GNU sync 的逐路径 fsync（不使用 -f/syncfs）；helper 经现有加密控制协议 filesystem-sync；SystemFiles 沿用原批准 executor。高权 SFTP 的普通 Exec 不充当高权能力；已有 writer 的明确 fsync 错误新增不可重试标记。目标链接不追踪，物理父路径先解析；缺能力/错误/未知退出保源。此源码尚未编译或执行，不是已修声明。
- 新增待运行行为场景 `internal/webgui/move_gate_regression_test.go`：真实 loopback SSH 认证、pkg/sftp 文件服务和实际 shell/文件系统，隔离 PATH 中的 stat/sync 故障工具；覆盖复制后实际换 inode 再令 stat 失败、无 fsync 扩展且系统能力缺失、文件同步失败、发布父目录同步失败和等价同步正常移动。没有替换 Endpoint 或 transfer 被测对象，不是源码匹配测试，也未冒充完整 OpenSSH/高权同步证据。原凭据路由夹具仅增加显式可选 exec，默认仍拒绝。安全表驱动及这些场景均未运行/未取得旧红新绿，尚缺 R8、真实高权回调及 H 的普通无参数 vault.Locate 成品证据。
- **唯一下一步：** 完成统一移动安全边界与真实 R8–R10 反例/正例，连同 P 修正、H 的真实启动证据和文档组成完整候选，再一次 amend/push 到同一 PR，GitHub-hosted 验证后交原角色集中复审。当前无活跃 workflow/下载/等待句柄；不启动多平台、发布或本地测试/构建/GUI。后面的“唯一下一步”等段落均为历史，不再驱动任务。

本轮为 progress：PR 正文已补移动安全警示；落实 R10 跨 endpoint/helper/SystemFiles/统一删源门禁及 R9/R10 真实协议回归准备，仅 gofmt、diff 检查。HEAD 和已测产物仍为 c077881；19 个工作树改动均属本批，没有提交、推送、部署或新 workflow。R8 尚未实施：不要匆忙用跨主机 dev/inode 相同或不同当作同物/隔离证明，也不要把 CreateAtomic.Commit 当作独占发布探针。设计咨询在原会话最新完成轮次；后续只接当前游标，不重复旧 UI 调试或重新推导已测链路。

- 基线：`6c339a19ecc621ff1cdee0ba9f5775b0ac37342e`；分支 `fix/issue-3-acceptance`，草稿 PR https://github.com/lovitus/dragfm-gui/pull/4 。HEAD/远端及最近已测完整候选为 `c077881f77214291068a5d9f264ca72c91828319`。主36613262559、SSH36613262490均success，已按指定脚本确认终态，无活跃等待。macOS原生成品exercise/restore/changed-key和独立文件系统核验通过；22项主流程检查含实际Pending滚动取消、移动/合并、d/h、双向PTY和受保护本机下载。SSH工件31根pass，未抹掉旧peer-writer提前退出故障。成品SHA `88c5d89fefc697a0382804840484651f567d15df134f12331cb20f3a46a46210`已与下载文件及native记录三方核对；仅私有目录下载，未运行/部署/发布。A–P仍须逐项集中复核，不能用两项绿色workflow替代全范围验收。工作树仅更新本轮证据文档，保留已测产品SHA。下方旧段落是历史，唯一下一步见下。
- 原评审会话 `for dragfm-gui` 已完成对 `5a0e7ea` 的集中评审，正式评论 https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5892311568 。R1–R3 原反例证据范围认可，非整批验收；新增 P1 R6 启动不可重试错误被后备吞掉、R7 高权/控制通道初始化取消不能保证本地读取退出。已给同一会话纠正 Home EOF 误报并同步 999e455 三组红绿；不要求逐小修改重审，不运行其快照编辑脚本。
- F1 已实现未验：预检/复制/移动统一保守同机判定；SSH 身份取实际认证最后一跳；新增克隆 machine-id 的真实双 SSH 回归。
- F3 已有未验实现：配置稳定 ID、字段删除生效、先落盘再发布内存、相关缓存失效、连接 ID 驱动终端重连；拼接路由的逐跳凭据回写原始会话，仅认证成功后缓存/保存；保存密码直接写入可编辑路由。
- v2 迁移保留旧 SOCKS URL 转义字节；旧隐藏密码因可能归属错误，转为默认遮罩的 `###待核对旧密码`，不自动用于认证。不是静默覆盖现有路由密码。
- F3 队列快照已实现未验：入队冻结配置、内存凭据和池资格；策略不再按同名主机读取编辑后的路由；旧任务不得回填新配置缓存；队列保存前脱敏，取消时释放持有快照的闭包。新增真实双 SSH 改名路由回归和删除配置后的日志/失败/panic 脱敏回归。
- F4 已有未验实现：Bash 在同一交互进程加载登录配置，保留函数/别名/选项和 PROMPT_COMMAND 数组；Zsh 跟随动态 ZDOTDIR 并在启动后恢复；Shell 配置绑定实际连接；初次提示符前不允许 GUI cd。导航用正常 cd 记录，取消擦行，界面错误改在终端外显示。Bash 集成不设置 readonly login_shell，也不模拟 .bash_logout，边界写在 README，不能声称完全相同的登录模式。
- 新增真实本地/SSH Bash/Zsh 启动回归、profile 读取 stdin 时不注入 cd 的回归，以及导航拒绝的界面提示回归。旧命令字面值断言移除了擦行后缀，原因是该后缀擦错物理行，不是运行失败后修改预期。
- F4 协议已实现未验：每个 PTY 使用随机 nonce + v1 Base64 cwd 帧；两个 GUI 入口共用有界解码器，只接受本会话绝对路径，忽略伪造/旧标记，保留提示符尾部即时输出。控制字符目录通过解析后的 printf 解码导航，路径框/列表不再 trim 合法空白。新增实际 Bash/Zsh 的伪造标记→运行中 read 防注入、特殊 cwd→真实写文件→文件栏刷新回归；两种场景分别执行，均待 hosted 红绿。
- F2 凭据部分已实现未验：跨账户 root 路由不继承普通用户私钥/Agent，缺少显式高权凭据则回退获准的 sudo；同一账户保留原凭据。sudo 保存意愿绑定任务，只在实际密码 sudo 握手成功后事务保存，root SSH/已是 root 不算密码验证。动态挑战密码加入任务脱敏 matcher，覆盖与旧口令重叠的情况。新增真实 SSH 连接+实际队列+保险库的未验证密码不保存/不泄漏回归。
- F2 双端权限已接入队列、尚未验证：PrepareDrop 不再因源 Stat permission 拒绝预览；任务分别确认源/目标权限，在必要时创建 `Session.OpenFiles` 高权文件视图。完整源清单固定到任务，预检、direct/池/Hans 快照与最终 relay 共享；方法切换不重新采纳变化后的源。已复制后的校验/删除遇到权限错误，只重做原基准的校验和删除，不重新复制。源/目标的 typed AccessError 保留错误层级，不按文本猜需要提权的一端。
- helper 使用标准 SFTP stdin/stdout 子模式；复用 pkg/sftp，不监听网络、不在控制机暂存文件；高权 inode/物理父路径/所有权和 partial 登记通过原 control channel。注意 pkg/sftp REALPATH 仅做 lexical Abs，已为高权视图增加真实父目录别名解析，避免同机保护退化。Exec 仍是原账户能力探测，不能当成高权 Shell。
- helper 改走独立 Fork 的已认证有效路由（含拼接跳板和逐跳确认 pin），取消时只关闭任务自己的 transport，不关闭浏览/PTY；结束先关 SFTP 子 channel 再清理所属 partial 和安装目录。新的 UID/GID 信息进入源清单；仅高权目标按能力保留所有权，使用不跟随符号链接的 Lchown；移动额外核对目标所有权。
- F5 已实现未验：每个任务共享多风险批准/跳过缓存，sudo 不能替代监听批准；池内与 Hans 内的方法同样检查权限。新增 SkipChallenge RPC 和“跳过此类方法”按钮，与“取消任务”分开；跳过监听/Hans 后仍允许最终中转，拒绝必需的源读取权限则明确失败。源/目标风险按端点而非当前方向绑定。
- sudo stdin 已改为一次认证/执行的随机帧，尚未验证：旧分开 `sudo -v` 再 `sudo -n` 依赖时间戳缓存，不能保证 timestamp_timeout=0 成功。现在密码 sudo 消耗第一行密码，非交互子命令读取随机帧后才进入二进制协议；NOPASSWD 则丢弃未用密码再读帧。关闭 xtrace/自动 export，不把口令放入参数/环境/文件；实际随机提示符+完成边界+成功握手才保存密码。沿用既有 API 的 hosted 真 sudo 回归，未以缺符号编译失败作为红证据。
- O 高权无 agent 回退已有源码未验：`remoteagent.SystemFiles` 优先固定系统路径 OpenSSH sftp-server，确实不存在时复用 Linux POSIX 文件接口；不安装/上传额外可执行文件，不依赖 CPU。任务专用 Fork，root SSH 或分别获准的 sudo；真实物理路径、数值 inode、Lchown、随机 partial 登记；预检/最终内存 relay 使用同一高权视图，不替换浏览/交互 Shell。关闭先确认系统服务/文件命令退出，再按原父目录 device/inode 和精确随机 basename 清理；未知退出保留，清理失败进入队列结果。删除/rename 清除已登记的子 partial；完整主流程和异常断线仍需 hosted 行为证据，未宣布 O 完成。
- F2/O 真实密码验收准备未运行：扩展原双保护目录队列主流程，加入拒绝 agent 的 NOPASSWD 与密码账户；密码由 hosted fixture 动态生成且只经 stdin 设置，sudo timestamp_timeout=0、passwd_tries=1。正确密码必须双端完成合并/移动并保存至各自主机；错误密码必须失败、源 SHA 不变、目标无新内容、vault/队列无泄漏。NOPASSWD 输入未用密码仍不得保存。均未编译、未运行，不称已验证。
- sudo 密码判断增加 stderr 完成边界的事件等待，避免 stdout 握手先到而漏掉真实密码提示。新增 hosted 双非 root SSH 队列复制/移动场景：源 Stat 不可访问、目标不可访问、分别批准、跳过监听/Hans、最终 relay 合并、真实 SHA/源保留删除、非 root 文件和符号链接 UID/GID、浏览连接存活、NOPASSWD 不保存未验证密码。新增仅高权流可用时独立监听批准/拒绝的纯状态机表驱动回归。都尚未运行；使用基线 API 的覆盖，不以缺符号编译失败作为红证据。
- F6 已有跨层实现未验：QueueCommand 显示脱敏命令预览，stdout/stderr 各自做跨块秘密匹配及跨行凭据/私钥脱敏，完整行在运行中合并为最多 64 KiB 的 replaceable Output 快照，每 100ms 合并更新。退出/取消刷新最后的未换行内容，失败状态不覆盖正文。单行达到 64 KiB 后明确提示该流后续不记录、继续排空，不以截断的原始凭据作为输出；README 说明此边界。队列 coalesce 连续正文快照，前端时间线不保留每次正文副本，丢事件通过 JobSnapshot 恢复。
- F6 历史及 UI 已实现未验：History 保存最终 State/Method 和最多 16 KiB 脱敏正文，取消单独展示；可点历史行查看完整任务说明与输出。输出/事件切换不隐藏 Pending/History；读旧输出时不强制滚到底。命令不再标成“传输中”。SOCKS/SSH/Hans 外层是候选组，内部实际方向/方法/路由通过同一 strategy observer 上报；组完成不覆盖实际获胜方法。原生 cp/mv 可细化显示；通道 I/O 仅活性消息，不覆盖实测百分比或校验阶段；重试重置计数，文件数统一普通文件口径。
- F6 未运行回归：真实 Local.Exec 子进程通过事件驱动 socket gate 保持运行，验证实时输出→第二条 Pending→失败保留 stderr 尾部→取消保留正文→重新解锁保留历史但无 Pending；跨块/跨行秘密、引用凭据中出现 PEM 标记、Markdown 密码等安全表驱动同样走真实队列。测试只用基线已有 API，扩展字段通过 JSON 读取，旧版预期因缺实时输出失败而非缺符号编译红。真实 TCP-block→SOCKS 旧集成用例增加实际候选/方向/方法事件断言；前端新增正文快照不重复及失败历史可打开回归。都未执行，不能算红绿证据。
- F7 跨层实现未验：Markdown 仍为主入口，增加“连接、池与缓存”页，打开/刷新纯读不联网；显示 SSH/SOCKS 状态、排序、停用、精确主机对缓存和忘记按钮。同一 SSH 会话增加 `###允许跳板 false`，仅退出自动跳板资格，不中断现有浏览/终端；按原有事务保存并删除相关缓存。新任务采用变更，已入队任务保留冻结配置。缓存来源可能是成功连接或传输，界面明确不作为两远端当前互通的证据。
- F7 测试入口已实现未验：QueueConnectionTest 加入同一队列，SSH 只连接所选完整会话；SOCKS 需指定已保存 SSH 目标，只替换该次代理，失败不遍历其他会话/池，不访问公共检测网站。复用 flyssh 连接/挑战/密码保存逻辑；仅报告控制机登录和含人工确认的总耗时，不更新实际发起端 RTT/传输成功记录，也不让一次代理登录认证另一条默认路由。旧配置测试排队后版本变化则联网前拒绝。
- F7 编辑保护已实现未验：GetConfigTexts 返回会话代数+配置版本+可编辑正文摘要，含并发连接保存的凭据；SaveConfigTextsAtRevision 和状态操作拒绝旧版本。界面有未保存文本时禁用策略操作；异步策略返回或重新载入时如新增输入，保留草稿并说明合并方式，不覆盖。清除缓存后同步编辑版本；保存时编辑器只读。连接测试事件和快照按 revision 合并，旧快照/旧任务不能回滚当前测试结果。配置主内容可收缩滚动，保留顶部和保存按钮；示例仍为用户的无空行结构，使用文档保留地址和假口令。
- F7 验收准备未运行：实际 loopback SSH/SFTP+未授权目标 guard 的公开 RPC 流验证只测所选路由、失败不换跳板、缓存忘记/退出跳板不关闭浏览连接和旧草稿拒绝；hosted 已有真实认证 SOCKS/OpenSSH 夹具增加点选登录→不污染传输排名/默认路由资格→停用闭环。前端异步草稿和乱序快照回归、成品 WebView 的池页操作→真实 SSH 登录→退出跳板→Markdown 再保存与弹窗边界检查已补入；这些是待执行的行为验收，不是 PASS，WebView DOM 操作不证明系统级键鼠。
- F9 已实现未验：共享列表按 mtime 降序（不目录优先），相同时原始名称字节升序稳定排序；保留隐藏文件，列头标明顺序。修正旧“目录必须第一”的测试预期，因为该预期与 ls-allt 原要求冲突，不是为通过失败用例降低要求。增加固定文件系统 mtime/隐藏项/同时间大小写的排序回归；已有真实 SSH 的 SFTP/禁用 SFTP 流扩展为文件/目录/隐藏项的同一顺序；成品脚本核对本机及 SSH RPC 与渲染顺序。
- POSIX 元数据缺口已有未验修复：GNU find NUL 协议新增数值 UID/GID，List/Stat 保持 OwnerKnown；mtime 分开读取十进制整数/小数，避免 float64 损失纳秒；非法数值明确报元数据错误。真实禁用 SFTP 回归对照实际本地文件/链接 UID/GID 和纳秒时间，不 mock 被测端点。SFTP v3 的整秒能力不伪装为纳秒。
- F8 direct 后备已有未验实现：`ncat_transfer.go` 使用两端系统 Bash/tar/ncat/sha256sum，不执行 agent；源 FIFO 对实际归档字节 SHA-256，目标限量归档先落在随机 0700 同目录工作区，SSH 哈希一致后解包、比对清单/恢复元数据/rename。控制机不保存归档，移动仍走源基准和逐文件 SHA 门禁；现有目标继续其他安全合并方法。来源接口白名单、非私网风险确认、五个随机端口覆盖连接失败，前一对 SSH 命令退出后清理再重试。高权 direct 已接入独立 SystemFiles 的文件+受控命令 adapter；权限按源/目标分别确认，任务通道不关闭共享预检/浏览通道，提交前恢复 UID/GID（链接自身）、mode/mtime。没有系统 server 但 agent 可用时仍保留原载体后备，已经提交/安全门禁失败不能重复制。代理池独立 ncat 与目标目录异常退出工作区发现仍未完成。
- F8 空间检查已有未验实现：归档计数做 int64 溢出检查，接收限额取清单上界与可用空间扣除解包内容/余量的较小值；达到限额停止该方法而不重复耗端口，解包前再次核对剩余空间，不保证并发占盘下的预留。目标 Stat 权限/其他原始错误不再误报“已存在”。空间限制及真实网络篡改拒绝尚缺 hosted 行为证据。
- F8 高权队列回归已准备未运行：原双保护队列增加 system-only 密码账户正向复制、真实源→目标高端口防火墙 REJECT 后反向移动；必须在最终 Method 看到实际 ncat/方向而非 relay，并检查 SHA、源保留/删除、文件/链接 UID/GID、无 root 工作区/监听、浏览连接存活及 sudo 密码保存。追加 64 MiB 真实随机源，收到机器阶段 ncat-connected 后直接 CancelJob，必须及时取消、源 SHA 不变、不提交目标、无 live root ncat/监听/暂存区。没有 mock Endpoint，不改 SSH 控制路径；真实断线与无法确认退出的远端遗留发现仍缺覆盖。
- F8/O 取消实现新增未验：源码与 OpenSSH/sudo 官方资料确认旧首发 SIGKILL 无法经 sudo 转发高权子进程。Remote.Exec 现在分开 Start/Wait，已启动但无退出状态/信号的错误标记 ErrCommandExitUnconfirmed；取消先 TERM 等退出，才 KILL/关闭通道，关闭不能伪装已退出。独立 tar+ncat/解包由 Bash monitor 管理一个专用子进程组，异步 wait 捕获 TERM/HUP 后终止该组；内层关闭 job-control，内部命令不加载 BASH_ENV，凭据不进入脚本。runNcatPair 保留退出不确定性，内部取消同伴不冒充用户取消；退出未确认时停止重试、保留源和暂存区，不边写边删。
- F6 取消详情补漏未验：Queue 仍用 cancelled 状态，但包含清理/退出错误时在 Message 保留脱敏原因，使已有 UI/历史无需额外字段即可显示；原始 Error 同时保留。新增纯队列状态机表驱动回归，对照普通取消与清理未确认。既有 hosted SSH 取消延迟用例改为 stdout PID 就绪事件，去掉 20ms 远端查询轮询，增加真正 root 单子进程验证 sudo 信号转发，退出后单次 PID 检查；均待旧红新绿，没有本地执行。
- K 池单端失败修复已有源码未验：SOCKS/SSH 池不再因源端或目标端 agent 启动失败就退出整个池；两端独立启动/探测，仅跳过缺少真实发起能力的方向，保留失败层级。扩展原 TCP-block 全策略→认证 SOCKS 验收为源端真实拒绝 helper 的 target-pull 场景；失效 SSH 缓存重新探测用例同样增加单端拒绝，核对真实内容/缓存/拒绝标记。两端均无 agent 的独立池通道仍未完成，不借控制机 RTT 冒充远端探测，也不使用会泄露口令的 ncat argv/env 选项。
- F8 共用加密载体已有未验实现：controller 一次生成五个不同端口，helper `ListenPort` 使用指定高端口，不再只做 bind 重试；每次地址尝试新建监听/令牌，失败后 `listener-stop` 取消并等待已接入连接退出，再用 `discard-partial` 通过原 `os.Root` 清理已登记 partial；没有登记的路径拒绝删除。helper Close 先停/join再清理，避免接收线程与清理竞争。普通 ncat 完成通道固定不变，仅 select 本地变量置 nil；修正跨 goroutine 通道变量竞争。运行栏独立显示尝试端口/连接建立，不把 0 通道 bytes 伪装为进度。
- F8 回归已写未运行：hosted SSH 增加一次性 systemtester 登录 Shell 策略，真实拒绝上传 helper（非 mock Endpoint，非禁止 SSH signal 的 ForceCommand）；双方向目录复制/移动含特殊文件名、模式/mtime/链接，真实 iptables 首 SYN REJECT 后换新端口，五端口全部拒绝后源 SHA 不变，真实 ncat TCP 建立后取消并检查无 live ncat/监听/partial、原浏览连接可用。加密流与 agent ncat 也各测两个方向的首次拒绝后新端口成功。均调用基线已有传输 API；旧版预期因实际 helper 执行被拒/连接失败而红，尚未取证。移除已不用的 `ncatTarConsumer` 及源码字符串匹配测试，替代为真实传输闭环。走读顺手移除先前引入的重复 `forget-partial` switch case，未用编译缺符号当红证据。
- 新行为回归尚未运行。只执行过 gofmt 和 diff 格式检查；没有本批编译、测试、候选提交、workflow、成品或部署。UI 原生鼠标/键盘、全部传输路径和 A–P 完整需求仍未验收。
- O/F8 目录租约新增未验：`remoteagent.Workspace` 为系统 ncat 创建 v2 标记，独立 SSH 会话跨命令持有目录共享 flock，tar/ncat/解包通过 fd 9 继承共享锁。控制机掉线不等于写入结束；正常收尾释放控制会话后，只有取得独占锁、确认目录及父目录 device/inode/UID/权限、标记哈希不变才删除精确随机 basename。缺失/不支持 flock 不绕过，随机标识生成失败也不降级到时间戳。新工作区增加 Linux 系统 flock 依赖，不影响其余方法。取消未确认依旧保留源/暂存区，不声明已回收。
- 现有 agent 的过期清理接入同一 v2 目录锁，root helper 识别当前执行 UID 的 root 工作区；v1 无有效 agent PID 不再按年龄删除，/proc 读取权限错误不当作退出。重查 inode 后才删除；实际删除/锁错误不再吞掉。控制侧旧 `CleanupStale` 的不安全直接 Remove 改为经标记/执行身份/锁复核的系统命令入口；GUI 重连使用下述登记的 `RecoverWorkspace` 精确目标，而非扩大扫描。v1 agent 的子进程孤儿及 SystemFiles partial 异常回收仍需后续审计，不据此宣布 O 完成。
- 新安全回归准备未运行：`TestCleanupStaleTempsHonorsLiveDirectoryLease` 使用真实内核 flock，覆盖 v1 未知工作区保留、v2 持锁保留及解锁后删除；已有所有权表改用 v2 闲置契约，额外保留 v1 无 PID，原因是原“无 PID 即删除”预期不安全。`TestHostedStaleCleanupPreservesLiveAndLegacyWorkspaces` 通过基线已有 Remote/Exec/CleanupStale 与真实 SSH 账户持锁，不用新 CreateDirectory/Fork API，检查旧目录/活跃归档保留、退出后清理、浏览连接存活。旧红预期是误删 v1 或不能回收解锁 v2，尚未执行，不拿编译缺符号作证据。现有普通/高权 ncat 主流程回归自动覆盖新的租约收尾，但真实连接中断下的孤儿进程继承锁尚缺证据。
- O/F8 重连恢复跨层新增未验：config.WorkspaceRecord 只在加密 vault 正文中保存 host ID、实际 SSH 指纹/machine-id、提权标志、精确路径、UID、父目录/目录 identity、标记 SHA 和创建时间。NewWorkspace 在 mkdir 前同步保存意图，创建后补记 inode；保存失败标为 NonRetryable，停止该任务。标记不再经可能变化的绝对路径 SFTP 写入，而是在独占新目录后固定 cwd，校验新目录 inode/UID，以 noclobber 从 SSH stdin 写入；后续再次与实际文件视图核对。正常关闭删除后事务销账；销账失败明确报错且记录留存，下次可确认已经不存在。现有未发布 v2 config 继续使用 v2（基线是 v1，不为同一未发布批次另增迁移）。
- 新 webgui/workspace_recovery.go 接在真实 ensureEndpoint 浏览连接发布后，仅选择当前 host ID 的过期记录加入现有队列，不在显式连接测试或单次 List 上偷偷清理。按当前 vault generation/host 去重活动清理，队列有取消/运行输出/脱敏历史；队列满/锁定等入队失败通过 DirectoryListing.Warning 展示，浏览仍可用。清理 Fork 实际已认证路由，核对原 SSH 身份及 machine-id；逐项限时，目录/父目录/标记变化不删除。root 记录独立询问列出的精确路径，获准后用 SystemFiles 受控执行器；跳过保留，取消中止，不自动 sudo。清理期间配置变化停止，旧任务只登记原身份且不覆盖新路由/密码，换 vault generation 不准回写。没有恢复旧 Pending，也不扫描其他主机。
- 新 hosted E2E 准备未运行：TestHostedReconnectRecoversOnlyJournaledWorkspaces 使用真实 /var/tmp 子目录、OpenSSH、内核元数据、sudo、vault 重开和公开 ChangeEndpoint→任务→History。普通、写前意图、批准 root 应回收；跳过 root、不同指纹、替换标记及真实更换父目录应保留，且旁边用户文件不变、浏览继续可用、再次解锁没有旧 Pending。新字段经 JSON 注入/读取，因此旧版能编译但不会产生恢复任务；不能以缺 WorkspaceRecord API 为旧红。所有用例尚未跑；写前日志真实崩溃时序、活跃遗留从 GUI 重连再重试、保存失败/锁定并发尚缺 E2E 证据。仅 gofmt / diff --check，无构建或发布。

## 唯一下一步

上一轮仅确认长期授权，为 no progress；本轮重新核对工作树和已终态证据，完成 SSH 工件核验并推进原生输入诊断，为 progress。c695d21 两个 run 已终态，无旧等待脚本，不重启或重复查询。

当前 c1def98 正式证据：

- `36594017619` success：已下载并核验两份 result.json 及对应实际事件。rsync 三个原反向目录场景均旧错误目录/错误后备失败、新精确方法成功；系统池六个旧反例（SOCKS case1–5、双无 agent 跳板 cache case2）旧失败、新通过，三个原本正常的正例两侧都通过。对照撤回对应产品行为，非旧整仓复测。此前十八组保留原 SHA，不扩大为 A–P 验收。
- `36594017566` failure，仅 ssh-core 失败；protected、transports 两组全部通过，含 12 个实际用户免密、48 格方向/权限方法、Hans、六个 SOCKS 强制后备及三个 cache 重探。core 其余 23 个根测试通过；peer-writer 本次 helper/rsync 和 system/scp 通过，helper/scp 与 system/rsync 的 Python 断言为 `writer stop not observed`。明确是 SIGSTOP 后立即读 /proc 的竞争，不是“没找到 rsync”或产品已被证明删除活 partial。没有放宽结果断言。
- `36594017643` failure，仅 build-macos-arm64 的 native 失败；test/source 通过。native 工件确认 hosted 从 1024×768 切为支持的 1280×960，恢复 status=0；实际进入 OS 点击/按键，路径后缀 source 选中断言通过，输入 target 后实际值为 targ，停在 partial path replacement。尚无法仅凭该值判定是发键丢失还是页面处理问题，不称产品输入已修好。其后的拖放/主题/终端/重启全链未跑到。
- 原评审正式评论 https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5893753215 ：限定 b630ad2，R6/R7 已报告机制与 SCP 权限反例“已修复、已复核”，没有新范围内阻断。不是整 PR 批准；R7 部分 Session 由真实协议夹具构造，不能称五个完整 helper/OpenSSH E2E。

原始证据只在 `/Users/fanli/.codex/private/dragfm-issue3-ci-c1def98/`，红绿、SSH、native 工件均已下载；两个失败日志按指定脚本各取一次。主 test 工件未下载，不声称已核验该包。没有本地测试/构建/GUI、用户主机/凭据操作或部署/合并/发布。

当前 b630ad2 正式证据：

- `36589938706` success：下载并核对三份 result.json；R7 五个真实 SSH 断流子场景旧读阻塞/候选通过，R6 持锁安装失败不应被后备掩盖的旧行为失败/候选通过且明确拒绝安全后备两侧都成功，SCP 原反向目录模式旧失败/候选通过。是同一 harness 撤回对应产品行为，不是原始旧整仓对照。此前十五组证据保留各自 SHA，不因此关闭 A–P。
- `36589938889` failure：test/source job 成功；完整前端、Go、race、重复 sudo、vet 门禁通过，macOS arm64 编译完成但 native exercise 首次 OS 输入前被窗口裁切门禁拒绝。下载原生工件确认 runner 显示区域 1024×768，产品窗口 1080×680（x=20、y=65），WebView 1080×652；不是 Retina 倍率误判。没有系统键鼠/截图通过证据，不缩小产品最低尺寸或跳过裁切来伪造通过。test-results 整体下载遇外部 blob EOF，日志读取/原生定向下载成功，外部下载失败单独记录，不误判代码失败。
- `36589938732` failure，三组均终态且没有整包超时。protected 全通过：双保护 14/14、显式 root key 队列。transports 的 48 格方法矩阵（含修正后的 12 个显式 root 认证场景）及 Hans 双方向方法通过；免密原生 12 场景剩三个 rsync pull 失败，SCP 已通过。core 的真实队列/History、PTY、helper loss/installation、POSIX、独立 ncat、重连恢复及系统 partial journal 都通过；唯一失败为 peer-writer 的两个 rsync 夹具未能建立“真实进程已暂停”的前提，不算安全通过。
- 仍失败：SOCKS fallback case1 的实际胜出方法/方向断言，case2–5 两端禁 helper 后误落控制机 relay，双无 agent 跳板缓存失效重探；未改预期或删测。peer-writer 缺明确 Python traceback，后续应在 observerDone join 后保留已收集 diagnostic，不能在仍写 Buffer 时读日志造成 race。

新证据只在 `/Users/fanli/.codex/private/dragfm-issue3-ci-b630ad2/`；红绿、SSH、native 工件已下载。两个失败日志按专用脚本各读一次。没有本地测试/构建/GUI、用户机器/凭据操作、部署/合并/发布。

上一候选 999e455 正式证据（历史，不重新执行）：

- `36582593356` success：POSIX reader.Close、同 sshd 不同 chroot、peer direct 不重放控制机跳板三组均旧行为失败/候选通过。已核对下载的两份 result.json。保留同一 harness 撤回对应产品行为，不声称未修改旧整仓的对照。此前十二组证据仍属各自 SHA。
- `36582593473` failure：前端通过；Go 全量中 TestServeReportsRemoteActivityAndCleansOnControlEOF 第 67 行收到 io.ErrClosedPipe，macOS 构建跳过，本次未取得新窗口坐标或原生输入结果。该测试主动关闭 net.Pipe 后，取消与 Handle 结果同时就绪可使 Serve 发送最后响应，返回原始关闭错误；需要核对这是错误预期还是实现应补取消标记，不直接扩大超时或吞所有错误。main 工件下载遇外部 blob EOF，原始失败日志已取得，下载失败不算产品失败。
- `36582593152` failure：双保护目录 14/14 通过（含原反向 ncat、POSIX 密码场景）；helper control loss 三场景全部通过（含独立 SFTP），故该用例第二次修正成功，不再改它。Running/Pending/恢复 History、真实 PTY 循环、官方 Hans 双方向等已有通过记录，不能扩大为整个项目完成。
- 全 SSH 新暴露：实际发起用户反向 rsync 缺 `.empty`，反向 SCP 空目录/mode 不符，system native 两个拉取场景未获得指定方法；peer-writer 两个 rsync 场景未建立观察器前提；矩阵十二个高权 SCP/rsync 报缺认证方法，需核对 fixture 显式 root key 与产品账户契约，禁止恢复跨账户借密钥；SOCKS fallback/事件、双无 agent 跳板和资源回收仍失败。最终 20 分钟**总测试预算**耗尽于 system-journal/abort 刚开始（该子用例运行不到一秒），不能误诊为该用例卡住；之后用例未执行。先修主流程，不以调长上限代替修复。

原始材料仅在 `/Users/fanli/.codex/private/dragfm-issue3-ci-999e455/`；两个失败日志均只按专用脚本读取一次，红绿及 SSH 工件已下载。SSH 状态曾 600 秒观察超时和网络读取失败，随后对同一 handle 确认终态 failure，未重跑。无本地测试/构建/GUI、部署、合并或发布，A–P 不关闭。

本轮完整修正（已进入 b630ad2，证据范围以上为准）：

- R6：Retryable 检查覆盖 helper 启动和 OpenFiles 的共同失败出口；exec 请求无应答标为 ExitUnconfirmed，安装销账失败保留不可重试标记，ctx 取消不丢原错。新真实队列→权限预检→文件视图→流式复制回归使用 disposable SSH 登录策略：明确拒绝且清理完成须后备成功；持锁子进程使安装清理真实失败时须停止后备、保留源/安装/加密 journal，不发布目标。未用 mock Endpoint，不声称这是完整拖放 UI 验收。测试清理须先确认停止子进程，失败就保留现场。
- R7：握手 watchdog 从 NewSession/credential 写入开始覆盖，只关闭任务独有 transport；Stop 会 join 已启动的取消回调，不能成功返回后再异步关闭。OpenFiles 监听 Close 的 done，在持 filesMu 初始化时也能退出；记住未确认文件启动错误，禁止 Close 把控制通道正常当成文件子进程已退出。系统 SFTP/控制调用的关闭不再只发 CHANNEL_CLOSE；即使写成功也关闭独有 transport 解阻，原始缺退出状态仍不可重试且不允许删除。POSIX 启动同样在返回前停止 watcher 并检查取消。
- R7 回归使用真实 loopback SSH/SFTP 与独立浏览 Fork；对端消费实际 agent hello/SFTP INIT 后阻断出站（含 close 应答），覆盖 control-start、file-start-cancel、file-start-close、control-call、system-start。要求有界返回、原浏览可用、安装/journal 保留；临时 workflow 撤回四个产品文件对照，必须五个旧读阻塞反例都成立，编译/夹具失败不算红。
- SCP：已有反向目录用例暴露接收目录只 MkdirAll、不恢复 -p mode；核对 FlySSH upstream master 尚同样实现与 OpenSSH sink 的接收后 chmod。目录创建时保留 owner 写/遍历能力，结束后恢复原请求权限及 mtime；无 -p 保持实际 umask/既有目录权限。沿用真实原用例撤回 vendor 改动核对红绿，不另造协议或关闭校验。rsync 目录缺失根因未确认，只给原失败用例增加实际目标树诊断。
- 测试合同：主动 front.Close 的生命周期测试允许 io.ErrClosedPipe，因为 Handle 最终响应与取消存在合法竞争；原 partial 清理断言不变，不计产品红绿。12 个高权方法矩阵原 fixture 未提供已隔离的 root key，现显式引用专用 root vault key；不改产品认证规则/成功预期，不计产品修复证据。
- 全 SSH 用例上次累计 20 分钟（双保护约 447 秒、密码免密约 197 秒、路由约 157 秒、矩阵约 107 秒）。已有 workflow 分为 protected/transports/core 三个独立 hosted job，core 是补集以覆盖以后新增 Hosted 用例；各自仍 20 分钟/相同断言，fail-fast=false。不用扩大超时掩盖主流程失败，不因失败删测。不是新测试框架。

本次候选准备（尚无新运行证据）：

- rsync 原目标树证明 source/source/.empty 多一层；按官方 trailing-slash 约定，agent/direct/Hans/控制机和 system native 对原始清单确认的目录拷贝内容。普通文件/符号链接不加尾斜杠，不放宽路径匹配、SHA 或删除门禁。
- 系统池根因走读：空 Go Route 的 Hops 编为 JSON null，Python 在实际 TCP connect 前 enumerate(None)，连错误脱敏分支也再次抛同样异常。现在明确按无 SSH hop 处理，保留指纹/账户校验；三次探测与中位资格不变。探测 stderr 有界收集，探测失败链不再丢成笼统“不可达”。原未禁 helper 的方法和排序不改。
- 原生验收依据 Apple CGDisplayCopyAllDisplayModes/CGDisplaySetDisplayMode，只在一次性 hosted runner 选择受支持的桌面模式，读回逻辑尺寸并在 finally 恢复；无合适模式就是环境 blocker。产品最小窗口、裁切/前台检查、系统输入与 fail-safe 均保留，不触碰用户桌面/TCC。
- 失败方式：目录不能多嵌套或展开通配符；null 探测不能误报网络、不借控制机 RTT；错误处理不能再遮盖原错误；不支持显示模式不能冒充原生通过。已有免密/代理/跳板用例原断言保持，新增失败诊断均脱敏；peer writer 先 join observer 再读 stderr，仍未猜修其未确认的暂停失败。
- 临时红绿复用原三个 rsync pull 和真实 SOCKS/跳板恢复用例，分别撤回对应行为。前十八组不重复临时取证；常驻全量门禁不变。中位函数单测仅随内部返回 error 改签名，不放宽两次成功/三次采样合同。仅 gofmt/diff 检查，没有本地测试/构建。

历史 c1def98 的三个 run 终态见本节上方。只有本批的一个交付提交；后续 lease 以当前 HEAD 为准，不再使用此历史 SHA。已下载证据，不重复临时红绿。

下一批工作树已准备、尚未运行：peer-writer 沿用已验证的 helper observer 模式，以 PTRACE_SEIZE + 阻塞 waitpid 确认实际 group-stop，再 PTRACE_LISTEN 保持停止；不再信号后立即读 /proc，不放宽活 partial/源保留断言。此为该暂停竞争的首次功能修正（c1def98 只补诊断），不是产品安全通过证据。native 记录仅路径框的 trusted key/input 事件，监听 input 唤醒；按 PyAutoGUI 官方 macOS catch-up 设置使用人类速度输入，长文本分成有界 32 字符动作，不改变原 15s 应答/20s 断言超时，不重发丢失字符、不用 DOM 填值冒充 OS 输入。尚待证据区分发键/页面层。

本轮集中走读确认：暂停观察仅针对实际精确工具 PID，使用已配置的 disposable SYS_PTRACE 能力，保留 pidfd 身份和最后退出确认；不是生产新提权。native 的 32 字符动作保持原应答期限，字符不自动补发；最终内容/焦点/任务与源保护断言不变。已完成的临时红绿 workflow 从自动 PR 入口移除，完整源码和撤回方法保留在已验证的 c1def98 提交及其证据工件；常驻 build/SSH 门禁不变，没有为了变绿删用例。

已 amend/push `c695d21fd1290c4043ae9db3aee4d7d01daf2404`；当前 HEAD/未来 lease 为该 SHA。SSH `36597179404` success；三组工件已下载并逐事件核对，core 24、transports 5、protected 2 个根测试全部 pass，无 fail。peer-writer helper/system × scp/rsync 四格全部 pass，首次功能修正有效。工件 COMMIT 为 GitHub PR merge `11095349de8f7d37e04fe9bb91ca74e768a29c84`，已通过 Git object API 核对父提交正是本批基线与 c695d21，没有把 merge SHA 误认为来源不符。

主门禁 `36597179468` failure，仅 macOS native 失败，test/source 成功。已取一次失败日志及 native 工件；输入 target 仍成为 targ。路径 trace 确认 t/a/r/g 均有 keydown→beforeinput→input→keyup，e/t 的 down/up 到达同一路径框但没有文本插入；ctrl/meta/shift=false。放慢发键首次修正无效，不再加等待。源码未发现普通 e/t 的取消分支；现在不能区分默认取消、系统合成和发送源问题，未宣称根因或产品修复。后续拖放/PTY/主题/重启仍未跑到。

原始证据在 `/Users/fanli/.codex/private/dragfm-issue3-ci-c695d21/`。本轮再次核对 PyAutoGUI 官方实现和 WebKit Input Events/IME 资料，只作诊断依据。准备的增量仅在 disposable 路径框记录 keypress/composition、alt/code/keyCode/repeat/时间戳、派发后 defaultPrevented/焦点/长度；失败截图仅限已关闭配置的局部路径替换阶段。保留 64 项上限、不记录配置/私钥、不补字、不改断言或原超时。未修改产品输入行为。已给原稳定评审会话发送同批证据，请其只读独立定位，不要求另跑工作流。

诊断增量已集中走读并 amend/push `90f561c7a80ac3620f20630befb7afdc4e48c293`，仍是同一个完整交付提交；当前 HEAD/未来 lease 为此 SHA。主门禁 `36599837027`、SSH `36599836703` 均已用指定脚本取得 failure 终态，失败日志各取一次，native/SSH 工件已下载至 `/Users/fanli/.codex/private/dragfm-issue3-ci-90f561c/`。无存活等待句柄，不重复查询。主 test/source 成功；SSH protected/transports 成功、core 失败。

90f561c native 仍在 partial path replacement：e 的 down69/keypress101/up69 都未取消，t 的 down229/up84；code 分别 KeyE/KeyT，四修饰键 false、repeat=false、isComposing=false、composition 计数全0，捕获和下一 task 的 defaultPrevented 均 false，焦点/节点连接状态均 true。前四字符正常插入，尾部无 beforeinput/input，实际仍 targ；这次事件顺序没有交错。限定阶段截图已观察，无可见候选窗，正常路径框保留焦点。根据 WebKit EventHandler.cpp，229 是输入法处理标记，即使 DOM composing=false；不能直接据此认定具体 IME、预测服务或发送源缺陷。

90f561c SSH 实际事件核验：30 个根测试 pass、1 fail；peer-writer helper/scp、helper/rsync、system/scp pass，system/rsync 在故障注入前 `writer exited before group-stop`，finally 的第二次 wait 又报 ECHILD。这不是产品删除活 partial 的证据，也不是通过；不能靠重跑概率消除。未改该用例或放宽断言，下一批仍需在真实退出前提上定位夹具生命周期。

原稳定评审已完成对 c695d21 的只读输入审查：同意不足以定根因，强调派发后取消/节点身份/发送序列，不用补字或增等待；公开结论明确未读本机私有工件，只依据传达的摘录。已把 90f561c 的229/未取消证据与精确 SSH 失败交给同一会话，请其继续只读评估。wait_threads 不支持该 ChatGPT ID，只按需 read_thread；最后一次读回为 active，不把它当完成。

当前工作树仅产品 FilePane 路径框增加 autoCorrect=off、writingsuggestions=false、autoComplete=off、autoCapitalize=none；原 spellCheck=false、受控 onChange、选区及正常 IME 保留，不干预 composition、不设 ASCII 限制、不改用户系统/TCC。依据 WebKit WWDC24 的逐字段预测关闭属性和标准 autocorrect 行为，路径是逐字标识符，不能由自然语言辅助改写。已发生的229使该方向值得验证，但不宣称根因确认。失败方式：不能吞中文/Unicode、不能补字绕过原断言、旧 WebView 不支持属性时不能声称有效；不新增源码匹配测试，只沿用原系统键鼠 E2E。同一输入测试第一次节奏修正失败，此次为待验证的第二个功能候选；若仍同处失败停止修改并报告，不继续猜修。

路径框候选已集中走读并 amend/push `7768c25b5934502ceafa75b5bb15e507b49d98bf`；当前 HEAD/未来 lease 为该 SHA。只有产品路径框自然语言辅助属性变化，没有改发送器/断言/超时/SSH。主 `36602175840` failure（仅 macOS native），SSH `36602175887` success，均已用指定脚本取得终态；失败日志一次，native 与 SSH 全部工件下载并核验，原始证据 `/Users/fanli/.codex/private/dragfm-issue3-ci-7768c25/`。无活跃等待句柄，不重跑。

7768c25 原生仍同一断言失败：target→targt、缺 e。e 有真实 down69→keypress101→up69，四修饰键/组合状态 false、派发后未取消且仍有焦点，无 beforeinput/input；末尾 t 则正常 keydown84/keypress116→beforeinput/input，DOM keyup 在延迟约812ms的 keydown前被观察。属性组合没有恢复流程，不将少一个缺字或229消失记为成功；不能归因到单一属性，也不能定因 Apple/PyAutoGUI/React。配置入口和有限 DOM 检查不替代后续 OS 拖放/终端/重启，后者未跑到。没有可交付的验收成品。

7768c25 SSH 的三个工件实际事件为31根测试全部 pass、无 fail。90f561c system/rsync 在 group-stop前自然/异常退出的夹具竞争未修，仍保留失败；此轮偶然满足前提不能称其已恢复稳定。不得以清除老日志或删测消除问题。

原稳定会话对90f561c的只读反馈已读回：支持有界字段策略实验，不强制先加底层日志，但建议单因素及撤回对照；其回复到达前组合属性已运行。现组合仍失败，没有可归因红绿，不需继续堆属性或做消融来包装成成功。已向其发送准确7768c25结果并请停止额外评审/查询。用户已在本轮获知两次修正仍失败与当前证据。本次为 progress（真实新证据），不标记整个目标完成或第三次阻塞。

本轮接续：上一轮仅确认长期候选授权，为 no progress；重新核对 HEAD 和游标后继续实际取证。输入实现仍停改，不做第三次策略猜测。核对 Apple 的应用内 NSEvent monitor 文档、PyAutoGUI 实际 Quartz post 实现和 Wails v2.14 发布说明；没有对应 macOS 输入修复证据，不升级依赖。失败方式：观察不能吞改事件、捕获其他应用或配置秘密、覆盖原断言错误、因 renderer 卡住而长期监听。

本轮仅扩展现有 hosted 原生验收的观察：同一个局部替换 `target` 动作关联 request ID，发送端原样调用原 CGEventPost 并记录 type/keycode/flags/timestamp；目标应用用公开 AppKit local monitor 记录对应收到的事件和 first responder，原事件原样返回。只允许一次已知六字符动作，最多64条，结束移除且25秒自动到期；未预期字符仅记 `[other]`。普通启动不注册或启用该观察，不加全局监听/TCC，不注入字符、不补字、不改原节奏/断言/20秒期限。DOM 增加同一 action/node 身份和 readOnly/disabled，成功或失败都保留该动作快照；观察收尾失败不能覆盖已有输入失败。

观察变更已合入并推送完整候选 cd3042a。主36605428342 failure，仅macOS native失败；SSH36605428350 failure，仅core失败。指定状态脚本取得终态，失败日志各读一次、所有native及SSH工件下载到私有证据目录，无活动等待句柄。SSH工件31根中30pass、1fail；merge `8512f4639cf90634ed3786592fd40f3ab97a33ec` 的父提交确认为原基线和cd3042a。system/rsync仍`writer exited before group-stop`后清理ECHILD，未建立故障条件；旧失败保留，不重跑碰绿。

本次路径仍targt、缺e；action19发送记录准确包含六个字符的12次down/up，所有CGEvent flags=`0x20a00000`（投递前），timestamp为0。AppKit收到characters正常、firstResponder始终WailsWebView/keyWindow=true/nonrepeat，flags始终`0x00a00000`，即公开SDK的SecondaryFn `0x00800000`与NumericPad `0x00200000`。e的同一eventUptime在AppKit monitor出现两次（相隔约1.6ms），DOM仅一次down/keypress/up且无beforeinput/input；末t的AppKit down/up顺序正常，DOM down延迟约920ms并晚于keyup。DOM节点身份/焦点/readOnly/disabled稳定。说明普通字母的发送flags已经不符合该动作，四DOM修饰键false不能证明没有Fn；不能再说e没有到达应用。Apple公开快捷键Fn-E能打开字符查看器，与现象一致，但没有该操作调用证据，不把它写成已证实根因，也不擅自解释未文档化的`0x20000000`。

原稳定会话对cd3042a的集中静态审查已读：原事件转发/普通启动隔离成立，观察仍有开销；旧DOM trace范围不止这次动作，AppKit未知字符遮罩不等于键码匿名化，main queue阻塞会让同步stop和25秒回调延后，finish/start有终态窗口，Python证据写入失败可能遮蔽原发送错误。本次隔离运行已正常stop（expired/capped=false），未出现这些故障，不能据此把代码称作长期完备诊断功能。临时观察已得到所需边界证据；下批应移除，保留历史SHA和私有工件，不继续扩大工具整改。已把flags事实和来源发给原会话仅做分析；最近读回仍active，尚无该轮结论，不冒充批准。

原会话随后完成flags复核：确认投递前事件不符合普通文本意图，支持只修hosted文字发送器、保留原键码/顺序/节奏/断言；不能把默认状态来源、Fn-E字符查看器调用或整个产品故障当作已证实。支持移除临时AppKit观察，不升级诊断框架。

本轮工作树完成最小发送器候选，尚未运行：仅现有`text`动作的Quartz按键事件清除SecondaryFn/NumericPad两位并在post前读回确认；Shift/Cmd/Ctrl/Alt及其他位保留，`keys`快捷键/箭头动作完全不改。没有替换事件源/timestamp/post位置，不改20ms字间节奏/50ms系统间隔，不特判target/e、不粘贴/塞Unicode/DOM代填/补键。动作finally恢复原post函数，只保留修正数量，不记录原始按键。删除临时AppKit三个文件和Go控制分支、JS字符trace，cd3042a及私有工件可恢复该次取证，原OS操作与断言保留。SSH夹具只补内核wait状态/退出码到原失败消息，没有改故障注入、安全断言或用更大文件/睡眠碰绿。

事前失败方式：清除全部flags会破坏Shift/组合键，清除箭头分类会改变选区；更换输入机制会规避真实用户路径；异常时不恢复wrapper会污染后续动作；删取证不能删失败证据。当前实现只收窄到普通text动作，未改产品输入源码。仅gofmt/diff检查，无本地测试/构建或GUI操作。既有cd3042a原生失败是撤销修正的旧证据，新候选仍须原路径选择→输入→Backspace→Enter→实际导航及后续完整链通过；没有新增测试或声称同run对照已完成。

发送器候选已推并运行`85985131a064e4fe93b274f3069a086f64aa3fe1`。主36608315400 failure（仅macOS native），SSH36608315370 success；均已指定脚本读终态，主失败日志一次、两run工件下载，无活动等待。SSH实际31根pass，merge `f964a0a9176767859676a3c28a32d66ffc437744`父提交核验为原基线与本候选，提前退出夹具这轮没有触发，不宣称已修稳定性。

8598513原生越过路径选择/target输入/Backspace/Enter实际导航及本地profile函数/完整提示符断言，三主题截图均已生成，亮色图实际可见PROFILE=profile-loaded和完整NATIVE>。随后OS拖动适配器抛`button argument not in ('left', 'middle', 'right')`。旧parent在读取已生成renderer报告之前先抛bridge错误，exercise.json未保存；因此本轮只按顺序入口和实际截图说明前置已到达，不称完整GUI PASS。上游dragTo默认PRIMARY直接传macOS拖动后端，后者只接受left/middle/right，与本例不传button吻合。

当前工作树仅修正新暴露的拖动发送参数并保留原stage报告：down/drag/up和异常释放均明确同一个left按钮；仍mouseDownUp=false，不额外按下/抬起、不改坐标/悬浮/断言。parent在bridge失败前先保存已经存在的renderer report，原bridge错误仍失败。没有改产品拖放，不新增测试、不本地运行。先前临时观察文件已从候选移除，可从cd3042a与私有证据恢复。

明确按钮候选已推`2aa3af486a1888a091f001988eca5012df5a6ce4`。主36610213420 failure（仅macOS native），日志一次、native工件已读；SSH36610213422初次600秒状态观察超时不是测试终态，未重启/重跑，后来同run指定脚本确认success、31根pass，工件merge `85c6d20c1c01eaf8eb12012d52158fdec74fb353`父提交核验匹配原基线和2aa3af4。无活动等待或下载句柄。

2aa3af4的exercise.json已保留10项实际检查：原路径局部选择/替换、输入Backspace及Enter导航、真实profile函数与完整NATIVE>、三主题、SSH及mtime文件列表、真实目录拖放与Running+Pending同时可见。running-and-pending截图已目视核验。随后`job cancelled`失败，期望取消的第二条Pending命令`sleep 30; printf pending-should-not-run`最终succeeded，取消和后续移动/合并/重启未验，不能扩称整个GUI通过。

走读发现旧nativeClick只验证窗口内rect和trusted pointerdown坐标，不验证被overflow裁切后的实际命中。CSS的Pending下限72px（含28px标题），旧截图只有第一行；新增命令第二行中心可能落在History区。TaskPane传正确job.id、API调用CancelJob、Queue有pending和running取消分支，但旧轮没有记录实际命中/调用，不能将上述假设写成旧运行的确诊。

当前完整候选准备最小原生操作修正：在发click前用elementFromPoint拒绝被挡控件，并用trusted pointerdown/up/click的派发时composedPath确认真正命中预期控件（保留成功关闭模态后卸载按钮的身份）。Pending取消按钮被裁切时先对该列表发一次真实OS滚轮（3格），等待该按钮可命中，重新定位并读取同一job仍为pending再点击/验证cancelled；不使用DOM scrollTop/代点、取消RPC代调用、额外重试或加长等待。报告仅补充夹具job ID、前后滚动几何/命中、点击前状态和可信click标志。适配器仅允许1–8格且无拖动按下的滚轮动作。原会话已完成只读集中分析：确认旧测试缺少实际命中证明，未确认误点History或后台取消故障；这轮按上述意见收敛，不改产品取消API。

候选c077881的新证据：主36613262559和SSH36613262490均success。下载native工件首次TLS handshake timeout（下载层，不是代码/测试失败），同一artifact ID经API一次重试成功；没有重新运行workflow。原生22项检查、restore/changed-key、独立文件系统8项都通过。实际Pending列表clientHeight=45、scrollHeight=70；滚动前scrollTop=0且按钮hit=false，真实OS滚轮后scrollTop=25且hit=true；同job点击前仍pending、可信pointerdown/up/click命中，终态cancelled。没有改产品取消引擎。原失败是缺少用户可达性验证的证据，现在补齐，不能倒推出旧点击究竟落在哪个元素。

同候选SSH三个分组共31根pass/0fail；工件merge `b2bdb2200b206b3539f8fd9a83fbfc09d30d0b3e`父提交已核对为基线+c077881。frontend 14文件40用例通过；Go全量/race/重复sudo报告无fail。native History/终端截图目视确认，报告区分配置DOM输入与真实OS输入，不冒充配置键盘验收。macOS单文件与SHA已下载至私有证据目录，哈希与原生被测二进制一致。原始材料只在 `/Users/fanli/.codex/private/dragfm-issue3-ci-c077881/`；无本地测试/构建/GUI，未部署、合并、发布。

交付自查两项已准备源码修正、尚未运行：`package-release.py`按实际workflow读取core/protected/transports三个SSH目录，分别检查同revision、没有fail、包正常完成及所属关键根用例通过，证据归档保留三个原目录，不合并伪装旧目录。缺组、混SHA、仅有子项pass但包未完成均拒绝。npm锁文件仅把jsdom间接dev依赖undici 7.29.0更新7.29.1及对应官方integrity，保留零告警门禁；官方GHSA-3wwx-pv8p-q78v和npm元数据已核对，相同Node最低版本、兼容`^7.20.0`。未本地npm安装/测试，不能宣布告警已清零；更不把此Node测试依赖问题宣称为Wails产品被利用。README/UNFINISHED澄清旧六平台描述是后续发布契约，本轮macOS优先不变。

唯一下一步：接回原评审会话对冻结c077881和Issue #3 A–P的集中意见，把确切主流程/安全缺口与上述交付输入和依赖修复合为完整候选，统一hosted验证。当前六个dirty文件均为本轮结果文档/打包输入/依赖锁修正，尚未提交；无活跃workflow/下载句柄、无本地测试/构建/GUI，未部署、合并或发布。未为小修单独推候选，旧peer-writer提前退出证据保留。A–P尚未验收。上一轮为progress（完整native/SSH证据），本轮已落实两项交付收尾实现、等待原角色集中复核，不改动已通过交互来制造进度。

## 历史：5a0e7ea 的证据和后续候选准备（已由上述游标推进）

5a0e7ea 三个 workflow 均已终态，不再等待/重读旧 run。指定脚本确认：红绿 `36577338798` 整体失败，但 SFTP 初始化取消、普通 transport 关闭、POSIX 多级创建、SCP 精确文件名、ncat 同步故障这五组都有实际旧红/新绿；R4 chroot 前置条件失败，不算红；R5 system pull 通过，agent push 被 rsync 非正常退出分类阻断。主门禁 `36577338699` 的前端/Go/race/vet 全部通过，macOS 二进制编译完成，但原生输入因窗口/viewport 与屏幕坐标判定失败，不算 OS 输入通过，成品未交付。真实 SSH `36577338696` 失败并在 20 分钟超时；双保护目录 14 场景中 12 个通过，反向 ncat 夹具规则失败、POSIX password 场景任务完成后的 Home EOF。Hans 普通断线/暂停后杀 helper 两场景通过；独立 SFTP 的测试 defer Close 仍挂起，后续用例未执行。官方 Hans agent 子验证通过不替代完整路径。

状态读取曾出现一次网络读取失败、一次 600 秒观察超时，均未视为测试终态/重启依据；后续同一 run 正式返回 failure。现无活动等待脚本/工作流句柄。失败日志每 run 仅读取一次，原始材料只在 `/Users/fanli/.codex/private/dragfm-issue3-ci-5a0e7ea/`，公共工件仅脱敏结论。此前 ec4004d/16bb76b 的七组红绿仍是各自原证据，不扩大为全项目通过。

当时下一步（现已完成）：将下列修正统一 amend 为 999e455 并读取 hosted 结果。临时红绿覆盖 POSIX reader.Close、chroot、peer-route；永久门禁不变。该候选结果与新评审见上，不从以下历史段落恢复旧动作。

本轮证据更正与产品修正：上一轮是 progress（完整候选、真实 hosted 结果及集中复审发出）；本轮重看精确行号发现 326 是 t.Helper 标注的 assertRemoteFile 调用，不是 322–324 的 Home 错误。此前“Home/浏览断连”结论不成立。sessionReader.Close 将已正常结束的 cat channel.Close EOF 与真实 Wait 成功一起返回，短文件关闭时会误报失败。现在只在 Wait 确认成功时归一化预期 transport 关闭，真正缺退出/非零退出仍保留，重复 Close 保留首次结果。新回归使用真实 cat、SSH exit-status/close 双向应答事件，保证实际通道已关闭后才 Close 文件，不用睡眠制造时序；缺失文件的真实非零退出也必须在重复 Close 后保留。尚待 hosted 红绿，未声称已解决整个队列场景。

本次完整候选修正（尚未取得新结果）：① rsync bridge 远端明确退出后先给本端 EOF 退出机会，最多 3 秒再 kill-and-wait，真正退出不明仍不可重试；旧日志证明立即 kill 制造 signal: killed 阻断 SCP。② chroot 夹具 machine-id 和 busybox 复制受 umask 077，显式 install 为 0644/0755，并补布尔身份诊断。③ iptables 的 tcp-reset 规则补 -p tcp，不归咎网络或锁。④ SFTP 夹具先关独有 owned transport 再 client.Close；这是该子场景第二次清理修正，若仍失败停止继续改该测试、报告证据并请用户决定。⑤ macOS native 仅补 frame/viewport/screen 数字诊断，不削弱坐标门禁、不猜测改布局。⑥ 上述 sessionReader.Close 修正与确定性真实 SSH 回归。

候选设计（通过范围仅以上述证据为准）：R1 vendored SCP 只去协议 LF，保留末尾空格/tab，Wails SCP 拒绝 CR/LF/链接后无损后备。R2 SFTP 初始化取消只关独有链。R3 ncat rename 前后同步。R4 同机还要求认证用户和 root device/inode。R5 peerTransferRoute 只用最终端点+明确池前缀，控制机登录保留完整会话。R1–R3 相应行为红绿已取得，不代表独立评审已批准；R4–R5 整组仍未通过。

5a0e7ea 的临时红绿保留完整 harness：端点组恢复 ec4004d 的 ssh.go，SSH 组恢复相应产品源码并撤掉 SameMachine 新 guard，但保留 Identity 字段。实际结果见上。下批只保留未完成的两组，不重跑已有证据；永久 SSH 默认全量，不以缺符号/夹具失败作红。Markdown/stalled request 也已通过本次主门禁。

最近仅确认长期授权的答复为 no progress，本轮为 progress，无外部 blocker。完整错误链证明 helper 并非单纯认证失败：正常关闭自有 SSH/SFTP 的 EOF/net.ErrClosed 被加入清理错误，触发不可重试并阻断后备。现在仅归一化普通 transport 的这两种关闭结果，真实 command/files 退出错误不丢；新增真实 Fork→关闭→浏览仍可用回归。POSIX Stat 找最近存在的祖先后区分可搜索/拒绝/非目录，不再把缺多级父目录当权限不足；新增真实无 SFTP 创建/写入和拒绝访问回归。R4 journal 关联只允许已批准的账户切换，仍核对机器/key/root；历史恢复记录仍按主机绑定和精确 marker/父目录 inode 校验，不把旧记录当 cp/mv 证据。

故障观察器只在 disposable container 增加 SYS_PTRACE，用 Linux ptrace(2) 的 seize→SIGSTOP→waitpid group-stop 事件确认暂停，仍以 pidfd 定位；正式程序不增加权限。5a0e7ea 已有 Hans 两种故障通过，独立 SFTP 清理仍失败，不混用证据。iptables 的新 stderr 已定位规则缺 TCP，未推修正见上。没有本地测试/构建、部署或本机 GUI，A–P 全部未结案。

已把新 SHA、三个正式终态、五组红绿和剩余问题同步给原 `for dragfm-gui` 会话，请其对已推源码集中复审并保持 PR #4 正式结论；未把本地未推修正交给它批准，没有新建角色/会话。当前尚未收到这轮评审结论。

平台节奏遵守用户后续要求：platforms.yml 保留六平台矩阵，但不再随 PR/push 自动触发，仅供用户验证 macOS arm64 后明确启动；build.yml 的自动 macOS arm64 构建/系统输入门禁与完整 Go/前端门禁均未削弱。

系统输入源码已接入，详见本轮段。核对官方 PyAutoGUI 0.9.54（PyPI 2023-05-24）、Apple CGPreflightPostEventAccess/CGEventPost、W3C 模态焦点约定；原 GitHub v0.9.54 tag 路径返回 404，使用官方 PyPI 版本与此前已读取的 upstream macOS 实现，不借资料失败弱化验收。依赖只在 hosted venv 安装，尚未安装/运行。原生旧红必须给旧产品保留新的 pipe 测试 harness；不能把缺 __dragfm_native_input__ hook 的超时当产品红。模态焦点等可在候选撤销产品修改、保留新 harness 后验证红，再验证当前实现绿。

## 本轮 macOS 系统输入与模态焦点（仅源码，未运行）

- 上轮为 progress，本轮也为 progress：核对唯一游标后修改既有 native-smoke 入口及实际 Modal。无活动 run/job、无外部 blocker，未启动用户设备 GUI/私有主机或本地测试，A–P 不改范围。
- 事前失败方式：DOM dispatch 不能证明 OS 菜单/捕获/焦点；坐标缩放或窗口裁切不能误点其他应用；缺输入权限不能伪装成产品通过；取消不能误记为失败；配置截图不能泄漏 fixture 私钥；测试初始化 HOME 不等于登录配置执行。
- 新 `build/ci/native_input.py` 复用 PyAutoGUI，预检 Quartz event-posting 权限、真实 PID/前台、单窗口完整可见和逻辑点坐标；保留 fail-safe。动作仅 click/down/drag/up/受限按键/有界 ASCII/capture，无任意命令入口。既有 smoke-only Go hook 通过继承 stdin/stdout 请求/应答，不开放 socket、不影响正常启动，结束报告也发事件。
- Python 等待由报告文件 100ms 轮询改为 stdout 事件+一次有界 process wait；sshd 准备也读真实 listening 事件；renderer wait 改由 DOM/runtime/input/resize 事件驱动。GHA build 与既有 arm64 packaged-release 验收入口加入同一 pinned 依赖，不触发任何 workflow 或扩大平台构建。
- 真实 OS 拖到目录/空白→确认→两任务 Running/Pending→移动/合并、空白 focus 后 Backspace、h/d、路径 Shift 选中局部编辑与退格、SSH 终端实际退格回车、三主题、最小窗口第三栏控件/历史点击已接入原用例。pointer/keyboard 检查 trusted 事件，点击还比对真正 client 坐标；三主题/Running-Pending/最终 History 截图不在配置 editor 打开时采集。凭据/Markdown 准备依旧 DOM，明确不冒充 OS 文本录入证明。
- 本地 zsh 的 HOME/ZDOTDIR 限 app 子进程自己的私有 fixture，写真实 .zprofile 变量与 .zshrc 函数/短提示符；从系统键盘执行函数，检查输出与最终 xterm 渲染行；不修改 runner 登录文件。已有 Linux Bash/Zsh 与真正远端启动回归仍保留。
- 走读发现 Modal 只有 aria-modal、没有焦点管理，现增加安全初始焦点、最上层 Tab/Shift-Tab 循环、关闭后恢复；保留 password autofocus，不把危险确认当默认。原生每次传输对话框都实际按五次 Tab 检查包围。挑战匹配只增加新的“目标端提权 · 本机”且仍限定 protected 路径；用户点击“取消任务”应得到 cancelled，旧 failed 预期按当前明确语义修正，源保留要求不变（未运行，不是跑失败后调预期）。
- 仅 gofmt / git diff --check；没有编译、测试、提交、推送、workflow、成品、部署或本机 GUI。真实 runner 权限/坐标/xterm DOM 仍需运行证据，失败不能降级成 DOM PASS。不要将本轮测试工具准备或焦点源码当作整个主流程完成。

## 本轮 root 免密与精确文件名（仅源码，未运行）

- 上一轮仅确认长期授权，是 no progress；本轮核对工作树/Issue 后实际编辑，是 progress。无 workflow/job 句柄，无外部 blocker。官方 rsync 手册下载一次超时，仅为资料读取失败；用已读取的 upstream main.c/options.c/io.c/util1.c 证实调用规则，不把资料错误当产品失败。
- 先前已写但未记入游标的 root 修改已核对：agentroute `root_identity_only` 仅最终跳、实际 UID 0、无借用密码/私钥且明确启用本地身份时可解码；普通控制机 Decode 拒绝。helper 取 os.Geteuid、系统 Python 在认证前核对；显式 root key 仍使用原配置，失效不暗降级。native 高权轮次两端分别批准，direct/池/Hans 和最终 Method 标注都表达 root→root；控制机 root Dial 保持显式凭据限制。
- 根因走读：rsync `-s` 仍调用 glob_expand，单引号 Shell 引用不能避免 `[a]` 匹配其他名称。没有重写 glob 或关闭 rsync 检查；Go/Python 都保留标准 argv/协议，在受控 rsh 中核对 `--server … . operand` 后将唯一 operand 换为原任务精确绝对路径、只引用一次。去掉 Python -s；Go -e 使用 rsync 双写引号规则，隔离继承 RSYNC_*，增加 --。旧 helper server validator无需放宽，继续要求绝对操作数。
- 实际队列免密用例扩为 12 个 helper/system、rsync/SCP、push/pull 和 root 组合；root 默认私钥只在各自隔离容器生成、控制机不读回，两端独立确认。所有场景使用空格/单引号/中文/方括号/星号/问号/反斜杠的真实文件名，旁边放会被 glob 误选的不同内容文件；完成后检查精确目标、源移动/保留、诱饵未动、浏览可用及无暂存。pure UID 策略和双端风险表未运行，旧红需撤销行为但保留 API，不接受缺符号编译红。
- 仅 gofmt / git diff --check。没有本批测试、编译、提交、推送、workflow、成品、部署或本机 GUI。正常/取消/复杂多跳证据与 A–P 全量验收仍缺，不标完成。本轮不为单个文件名修复单独提交。

## 本轮 无 agent 原生 SCP/rsync 增量（仅源码，未运行）

- 上一轮仅确认长期候选授权，是 no progress；本轮重新核对工作树/唯一游标后实际编辑源码与验收用例，是 progress。无 run/job/等待句柄，无外部 blocker；没有缩小 A–P 范围。核对 rsync 官方最新版 3.5.1（2026-09-21）、rsync rsh 引号/-s/pipe.c、OpenSSH scp.c 的 -f/-t、Paramiko channel 退出状态文档；未安装或升级远端依赖，不复刻协议、不复制/构建 Hans。
- 事前失败方式：不能用最终 relay 冒充原生成功；凭据不能进入 argv/env/file；路径不能经 Shell 二次解释；双向管道不能在 EOF/等待子进程时死锁；SSH 退出缺失或仍有写进程时不能清理后重试；移动只能在原清单/SHA 和源未变后删除。
- `runNativeMethod` 接入 direct/SOCKS/SSH 池，在 helper 明确且可重试的失败后尝试系统原生方法。复用 Paramiko/PySocks、任务冻结路由、逐跳指纹/UID 身份策略；仅实际发起端需要 Python 库。系统 SCP 两端使用原 -f/-t，rsync 原生客户端使用 -s，静态 Python rsh 通过继承匿名 socketpair 连接父进程；无监听端口、token 文件或自写 SCP/rsync 协议。rsync 的 -e 按其专属“双写引号”语法，不能套 POSIX 反斜杠规则；远端 argv 单次 shell quote 后以 "$@" 执行。
- 两端私有工作区复用已有写前 journal/目录锁，native 本地/对端工具及其子进程继承 fd9；共用 Bash 管理进程组现支持转发 argv。目标只写同父目录工作区 payload；两边退出确认后检查清单、恢复 metadata/获准 owner、syncfs/rename，再完成现有移动门禁。sync 指向拥有的目录，不跟随复制的 symlink。SCP 链接/换行名称拒绝该方法；已有目标继续其他安全合并方法。系统工具缺失或 rsync 不支持 -s 时不安装、不伪装通过。
- Python 独立 stdout 仅回报节流累计双向协议字节，Go 只作为活性展示；验证阶段走原 Progress API。不把 wire bytes 当完成百分比。77 保留为退出未确认，78 为 host key changed；错误保留方法/阶段。共享系统后备清理函数修正为任何清理不确定都 NonRetryable，不只“提交后”才停止换方法。未知状态保留范围受限资源，未宣称取消矩阵已验。
- 扩展 `TestHostedInitiatingUserPasswordlessQueuedTransfer` 为 helper/system × rsync/SCP × push/pull（8 场景），系统 push 文件、pull 目录/空目录/rsync 链接，含空格/单引号/中文/$ 字面名称；实际控制机 key 仍限 controller 来源，各远端默认 key 不读回。走 ChangeEndpoint→PrepareDrop→QueueTransfer→真实目标内容/移动源删除→浏览和 workspace 销账，要求正确 method/direction、确认 helper 真拒绝，不能 relay 冒充。旧版/撤销通路应因实际方法失败取红，不以缺符号编译红取证。
- 原 SOCKS ncat 两个用例显式取消 disposable 两端 rsync/scp 执行权限并恢复，保留对 ncat 本身的验证；另添双拒绝 helper 的原生 rsync 正向/反向池场景，精确要求真实 method/direction。不是降低失败预期；当前尚无任何一次运行。已有真实 SSH 池缓存用例继续覆盖新通路，但复杂多跳/高权/取消需集中取证。
- 仅 gofmt / git diff --check；没有编译、测试、候选提交、推送、workflow、新成品、部署或本机 GUI。全部为未验证源码。下一步仍是上方唯一游标，不为这一子路径单独提交；完整候选授权已再次明确，无需再问。

## 本轮 实际发起端身份增量（仅源码，未运行）

- 上轮为 progress，本轮也是 progress：重新读取工作树/唯一游标后修改源码。没有真实 run/job 句柄，无 blocker，没有缩小 A–P 范围。复核官方 FlySSH connector、OpenSSH IdentityFile/IdentitiesOnly、x/crypto/ssh/agent 和 Go os.Root 源码；没有发现可直接替代应用 UID 策略的上游实现，不升级依赖、不复刻 SSH。Go 1.26 Root.OpenFile 会解析 NOFOLLOW 的 symlink 错误，因此最终使用固定 basename + 目录 fd 的 Openat，而非误称 Root.OpenFile 不跟链接。
- 失败方式：协议丢失 consent 会让免密不可用，反过来默认 true 会突破显式 root key 限制；sudo HOME/agent 可能属于原用户；不安全/损坏/加密无口令默认 key 不应让显式 key/password 失效；失联 agent 不能无界挂起；测试不能把 controller key 或最终 relay 当作远端免密。
- agentroute 新 optional allow_local_identity，来自已有每跳 UseAgent，缺字段 false。helper 的 SCP/rsync/经跳板流统一 decodeInitiatorRoute，按 OS 实际 UID 读取自有 .ssh 中三个默认私钥，限制目录/文件 owner+mode、无 symlink、非 FIFO、有界 64KiB、先确认可解析；不能用另一把 vault key 的口令。只在内存补入既有 FlySSH 公钥认证列表，不回传/写盘，指纹仍先于认证。显式 root key 的 false 在协议中保留。
- 系统 Python 载体也按此开关执行，并从 pwd.getpwuid 取 home，使用目录 fd+NOFOLLOW/NONBLOCK、有界读及 owner/mode；不再依赖 expanduser 的 HOME。两路均不使用 sudo 原 UID 的 SSH_AUTH_SOCK。FlySSH 增加可选 AgentTimeout，远端非交互 helper 五秒，0 保持 CLI/controller 原策略；Python 仅在独立载体创建标准 Agent 时设 socket 缺省五秒并立即恢复。签名/SSH 失败仍返回真实错误，不声称成功，增量 vendor 文档已记录。
- 新 TestHostedInitiatingUserPasswordlessQueuedTransfer 走真实 ChangeEndpoint→PrepareDrop→QueueTransfer→浏览：仅在临时容器生成各自不同默认 key，authorized_keys 对控制机 key 加真实控制源 IP 限制、对方只接受自己的新 key。四场景为 rsync/SCP × push/pull，强制 SCP 时临时取消系统 rsync 执行权限并恢复，强制 pull 时仅阻断源容器到目标的22端口并恢复；监听/Hans 跳过。核对实际 Method、正文、移动源删除、目标无 partial 和公开 List，不能凭最后 relay 成功过关。私钥留在 fixture，不被读回；使用既有产品 API，旧红应为无法直传/错误 method，不是缺符号。未运行。
- 逐跳 consent/显式 root false/旧消息缺字段的纯安全表已补；待在 hosted 旧版/撤销行为上取红绿。实际 agent socket 故障、sudo 默认 home/UID、多跳和系统载体新策略仍需随整批验证。仅 gofmt / git diff --check；没有本批测试、编译、提交、推送、workflow、成品、部署或本机 GUI。

## 本轮 F2/root 私钥关联增量（仅源码，未运行）

- 上一轮仅答复长期授权，为 no progress；本轮已重新读取工作树和唯一游标并推进源码，为 progress。无 workflow/job 等待句柄，无外部 blocker；未改 A–P 范围。已核对 FlySSH 官方 HEAD `9f299339930bbb7589ad0596f699d0d5ba7a5bc9`（tag 列表已有 v2.0.16）及 Go 模块代理 x/crypto 最新 v0.57.0；上游早已提供内存 PrivateKeys/Passphrase，本缺口在应用关联，不升级/重写认证库。参考 OpenSSH IdentitiesOnly 的显式身份边界。
- 事先确定的失败方式：不能把普通密钥/agent 借给不同 root 账户；未知/删除私钥不准发布配置；Pending 必须保留原关联与密钥正文/口令；root 只替换最后一跳，保留前置跳板凭据和 pin；秘密不能进入日志/历史；删除字段不得复活隐藏配置。
- 保留无空行三分区 Markdown，新增可选 `###root私钥`，每物理行一个 vault 名，支持名字中空格/逗号、不另造逐跳语法。全文读取后只在本次私钥集解析为稳定 RootKeyIDs，缺失/重复拒绝；省略 root用户 时仅有显式 root 私钥才默认 root。Host.Clone 深复制关联，旧 v2 文档没有字段时行为不变；未自动把普通私钥迁移成 root 私钥。旧单行导入器保留已有关联。
- helper/system files/direct SCP-rsync/Hans 统一从任务冻结 document 读取显式 PEM/Passphrase。跨账户先清普通凭据，同账户显式选钥也关闭 agent 并替换 keylist；同账户未选钥保留既有凭据。所有 vault 密钥本来就在任务脱敏 matcher 中，不另复制秘密到日志。配置侧说明与 README 同步。
- 新 TestHostedExplicitRootVaultKeyQueuedTransfer：GitHub 临时 OpenSSH 配置 root 只接受独立生成的 fixture 公钥，普通账户为必须密码 sudo 的 passwordtester，先实际验证普通 key 不能登录 root、sudo -n 失败。真实 SaveConfigTexts→PrepareDrop→Pending→换成未授权 key→旧任务执行：helper 复制和确实拒绝 helper 后 system files 移动两场景；旧任务应使用入队加密私钥，后续新任务必须认证失败且保留源/不发布目标。检查正文、copy/move、浏览存活、未验证 sudo 不保存及队列/历史脱敏。仅在 disposable 容器解锁 root 公钥登录，不操作用户设备。该测试使用基线 API，旧红应为合法配置拒绝/实际认证或传输失败，不是缺符号编译失败。
- 增补 Markdown 名称/顺序/删除/缺失/重复引用表，扩大既有 root route 账户隔离表（key-only、key+password、same-user explicit key）。纯 route 表的旧红须在候选撤销实际选择逻辑、保留扩展函数签名；不能把基线缺 variadic 参数当红。所有新增用例均未运行，尚无红绿证据。仅 gofmt/git diff --check；无本批编译、提交、推送、workflow、构建、部署或本机 GUI。

## 本轮 无 agent 池后备增量（仅源码，未运行）

- 上轮为 progress，本轮也是 progress；重新读取工作树/唯一游标，尚无真实 workflow/job/等待句柄，也无外部 blocker。没有把新系统通路或少量脚本当作完整 A–P 验收。
- 研究 Ncat 官方 proxy 文档确认其口令入口只有 argv/环境，不能用于这里的凭据约束；研究 PySocks 与 Paramiko 官方连接、direct-tcpip、固定 host key 和认证 API。GitHub Paramiko latest-release API 404 是发布元数据入口问题，不是产品测试失败；改查官方 PyPI，当前 Paramiko 5.0.0、PySocks 1.7.1。本批不安装远端软件、不新增 Go/vendor 依赖；只使用已经安装的系统 Python 库，缺依赖保留原始层级并继续后备。
- 失败方式设计：不能用控制机可达替代发起端可达；代理密码/密钥不能进入 argv/env/file；每跳指纹先于认证；源推送/目标拉取都必须由正确远端发起；监听不能为不明代理出口放开来源；取消必须沿用拥有的进程组/目录锁；失败保留源并不把预检或缓存记录当实际传输成功。
- 新 openPoolProber 在 agent 不可用但退出明确时，用任务专用 SSH Fork + 远端 socket 计时，三次中位逻辑不变。无明确退出不降级继续。SOCKS/SSH 池的 ncat 方法在 agent 路径不可用后，复用已有系统 tar+ncat 归档/校验/提交流程，以嵌入静态 Python 适配 PySocks/Paramiko；源数据不通过控制机中转。完整已保存 SSH 会话链附在跳板链后，最后一跳从实际端点打开监听连接。
- v1 JSON 经 SSH stdin/fd6，与 tar fd0/fd1 分离；关闭 core dump、禁 bytecode 写入；不复制上游协议实现、不上传带秘密脚本。每跳按 SHA-256 pin 验证再认证；系统默认私钥必须当前 UID 所有且不可组/其他用户读取，sudo 不使用原账户 agent；显式 vault key 仅用于所属 hop，密码/keyboard-interactive 随后尝试。错误按阶段/类型返回并遮罩秘密；host key mismatch 返回保留的退出码 78→NonRetryable，等待所有进程退出后停止换端口/路线。
- 来源限制保留：SOCKS 从实际发起端解析代理地址作为允许来源，SSH 链最终端点自身地址作为允许来源；不能确认的 NAT 出口不放开监听，后续内存中转仍在。公网/不明路径沿用明文风险确认。目录合并仍不走系统整根 ncat，而由后续支持合并的方法处理；未把此边界藏成成功。
- 扩展现有 hosted 两端 TCP 互斥防火墙→认证 SOCKS 真实传输场景，加入两端均拒绝 helper、以及只允许目标访问代理的反向拉取；成功应显示实际 pool/candidate/direction/ncat，文件与原浏览连接不变且无残留。原“源无 agent 必须 target-pull”预期改为允许真实系统 source-push/ncat，因为本轮确实新增该能力，并非失败后放宽测试。SSH 失效缓存用例加入双拒绝 helper，成功后第二次实际复制必须复用缓存、没有新 probe 阶段。Docker 仅在 disposable hosted 镜像安装系统 python3-socks/python3-paramiko，不操作用户设备。
- 新用例只用基线已有的完整策略/runJumpPool API；旧红应是池路径不可用或落到错误 tier，不可用缺新符号编译红。尚无任何红绿结果：仅 gofmt 与 git diff --check。没有编译/测试/候选提交/推送/workflow/成品/部署，没有启动本机 GUI 或私人服务器。系统 carrier 的 sudo/失败密码/指纹变化/多跳/取消与秘密不落盘仍须随整批验证，不能拿普通复制代替安全边界证据。

## 本轮 O 无系统 SFTP 后备增量（仅源码，未运行）

- 上轮仅确认长期授权，属于 no progress；本轮重新核对工作树、游标与 USER_REQUIREMENTS 后推进源码，属于 progress。没有真实 run/job 等待句柄，也没有外部 blocker。仍以 A–P 完整用户流程为目标，没有发布或验收任何子集。
- 研究 MC 官方 Shell VFS send/get，沿用系统工具而不复制其覆盖协议；官方 release API 再确认 pkg/sftp 最新 v1.13.11（2026-07-12），无需升级，也不新增依赖。实施前列出失败方式：缺二进制不能混同网络失败、sudo 密码不能进入数据体、打开/登记失败不能暴露可写流、取消须确认退出、移动继续原 SHA 门禁。
- SystemFiles 只在实际探测退出码 69 且无退出不明标记时选择 POSIX。BorrowCommandFilesystem 复用 Remote 的 Linux 元数据/路径/链接接口，通过批准的 sudo executor 读取/写入；每个实际命令继承原共享 fd9 租约。文件先打开 FD8→随机就绪帧→登记实际 inode→才允许数据，io.Pipe 不落盘；mkdir/链接/rename/remove 同样登记/销账。提交 EOF+实际退出+sync+rename，放弃正常退出后清理；未知退出保留并停止重试。视图关闭停止 admission、取消并等待命令，不关浏览链。
- 走读实际 vendor ssh.Session.Wait 确认 stdout/stdin copier 错误会遮蔽成功退出；受控流在主动取消时仅把本地管道切到丢弃输出/输入 EOF，仍等待真实 SSH 退出，避免预检 Open 后 Close 把正常取消误判为未知退出。没有把 SSH 缺退出状态改成成功。
- 上传 helper/系统 SFTP 的握手或密码帧失败新增有界 EOF/Wait：明确退出后才使用原 identity+独占锁清理空安装并销账；缺退出状态/超时保留。POSIX 初始工具/权限探测失败同理。实际 sudo 提示+完成边界+成功命令才标记密码验证，NOPASSWD 不保存未用输入。
- 扩展现有 TestHostedDualProtectedQueuedTransfer，增加真正移除系统 sftp-server 的两个 disposable OpenSSH 端点（同时拒绝 SFTP subsystem 和上传 helper），二进制内容大于 SSH window，NOPASSWD copy/move、正确密码 move、错误密码保留源且不保存。成功必须完成真实合并/SHA移动/文件及链接 UID/GID、无残留 partial/恢复记录、浏览连接存活。API 使用基线已有入口，旧红应为受保护传输拒绝而非缺符号编译。
- TestHostedSystemPartialJournalLifecycle 扩展 POSIX commit/abort，目标包含引号和换行，核对写前真实 inode 持久登记、提交/放弃结果和关闭销账。此组旧红需撤销 POSIX fallback（保留本批 SystemFiles API），不可拿基线缺新 API 算证据。以上全部未编译未运行，只有 gofmt/git diff --check；没有候选提交/推送/workflow/成品/部署，也没有启动本机 GUI 或访问私人主机。

## 本轮 M/N 普通流写入退出增量（仅源码，未运行）

- 上轮为 progress，本轮也为 progress。重新读取当前工作树、USER_REQUIREMENTS 和线上 Issue #3；A–P 未勾选，没有把生命周期测试当主流程验收。原始取消/失败清理要求仍在；额外跨重启扫描普通无标记 partial 的全平台框架明确延期，不挤占功能实现。无 workflow/process 等待句柄，无外部 blocker。
- 走读发现无 SFTP 的 sshAtomicWriter.Abort 仅关闭 channel 就 rm，transfer.Run/copyItem/native cp 又丢弃清理错误；外层暂存树可能在未知写进程仍存活时删除。同机 mv 的不明结果也会继续 fallback。依据官方 x/crypto/ssh Start/Wait/ExitMissing 契约，修复应用生命周期而非重写 SSH 库；当前 vendor v0.54.0，已核对官方 v0.57.0 文档，上游 Wait 仍需要调用方区分退出与关闭，本轮不升级依赖。
- POSIX writer 在 Start 成功后立即安排一次 Wait；Commit 使用原任务 ctx，Abort 先 EOF 再有界等待/复用现有 TERM→Wait→KILL，退出未知保留 partial。fsync/rename 的执行结果未知同样不并行 rm。ErrCommandExitUnconfirmed 现在带不可重试语义；SFTP CLOSE 未确认也携带此标记，Remote.Exec Start 响应丢失不再冒充明确拒绝。context_io 保留取消和原始错误链，不以 ctx.Err 丢掉退出信息。
- 单文件 Abort 错误进入 transfer.Run 最终结果；外层 root 暂存、符号链接暂存和同机 cp 清理均有界并保留错误，未知退出不删除外层目录。已确认 cp 失败且清理成功仍可按原权限回退；未知 cp/mv 或清理失败停止后续方法。Local AtomicWriter 缓存文件 Close 结果，rename/fsync 失败保留 Abort/unlink 错误，不把重复 Close 的错误误作新故障。
- 新 TestHostedPOSIXWriterExitProtocol 使用实际 OpenSSH 拒绝 subsystem、系统 cat、真实文件/目录和 source SHA：正常 Commit/Abort 无遗留；inotify 写入事件→pidfd 暂停精确 fd1 对应的 cat→文件 Abort/目录 Move Cancel 应在 8 秒内失败、不可重试、保留活跃 partial/整个暂存树、目标未发布、源未变。退出由 pidfd 观察后才清理测试目录。基线已有 API 可编译，旧红须为错误清理/错误可重试而非缺符号；暂停状态不满足只能记 fixture 失败。
- hosted Docker 夹具新增一个仅拒绝 SFTP 的实例，不启用 ForceCommand，保留真实 exec/signal 语义，不动用户机器。启动 readiness 从 250ms 重复 SSH 改为同一 30 秒定时器约束的 sshd 日志事件阻塞读取，之后一次真实登录；非工作流状态查询。新增本机真实目录 0500→rename 与 unlink 同时失败回归，要求保留 LinkError/PathError 中的原始操作和原目标；没有 mock writer，root 运行不伪装成权限门禁通过。
- 新增测试均未运行；仅 gofmt/git diff --check。无本批编译、提交、推送、workflow、成品或部署；没有启动本机 GUI、调用私人主机或使用本地/私有 runner。整个项目仍未完成，接上面的唯一下一步。

## 本轮 O/partial 持久恢复增量（仅源码，未运行）

- 上一轮仅确认长期授权，属于 no progress；本轮核对工作树/游标后推进了源码。无真实 run/job 等待句柄，无外部 blocker。本轮前已有未落游标的 helper partial 改动也一并走读和记录；原范围仍为 Issue #3 A–P，不把恢复子项当成全项目完成。
- WorkspaceRecord 增加独立 partial 的 Path/ParentID/FileID，Document.Clone 深复制；helper 经原加密 control channel 回报完整快照，不写远端任务日志。接收操作先登记 absent intent；实际 SFTP 创建后再调用登记以 pin inode，保存失败不返回可写流。已登记 root 覆盖暂存树内子项以减少 vault 重写；不同合并目标分别保存。forget 同时清除后代；已 pin 的 inode 被替换不采纳新身份。encrypted-stream 提交失败不再直接删 partial，成功明确销账，非重试错误不继续换端口。
- RecoverWorkspace 在安装目录独占锁内先核对所有独立 partial 原父目录和 inode，再删除 partial、最后删除租约目录；目标已存在但未 pin、父目录/子项替换、失去原锁目录均保留且报告。没有 shell pathname chmod；只读普通 orphan 不能安全删除就保留，只有已批准 elevated record 才用 root。创建后首个快照前崩溃的 peer SCP/rsync/stream 可能只有 intent，这种不确认归属的资源不自动删除。
- SystemFiles 现在在启动前登记并创建普通上传者所有的租约目录，系统 SFTP 在 sudo 后获得自己的 fd9 共享锁，文件操作命令也加锁。正常 Close 先 EOF/Wait SFTP，再释放控制端 lease，以已批准 executor 取得独占锁清理；连接断开/未确认退出保留记录。写前 absent intent→创建后父 inode/文件 inode 核对→vault 保存→可写流；提交/放弃后销账。root SSH 系统路径的安全失败不继续 sudo；凭据保存失败保留清理错误。高权恢复时使用新的独立已登记系统通道，正常结束销其自己的空记录，不扫描其他路径。
- SFTP AtomicWriter 的 Abort 原来没有 forget 回调，现仅在 CLOSE 和 REMOVE 均确认后销账；CLOSE 失败不继续删除。Commit 保留 Abort 的清理/登记错误，Close 结果缓存用于正常 Commit/Abort 生命周期，不把第二次 CLOSE 的错误误判为退出失败。普通未配置 lifecycle callback 的 SSH 仍存在持久恢复缺口，不据此宣称全面完成。
- 已确认 pkg/sftp 最新 release 仍为 v1.13.11（官方 API 2026-07-12），项目已有，无依赖升级。参考 OpenSSH 官方 sftp-server-main.c/sftp-server.c 和 flock 契约确认 after-sudo FD 继承方案；这是设计依据，不能替代真实进程持锁证据。没有下载/重写 Hans，没有本地或私人 runner 测试。
- helper lifecycle 新 partial-crash 用例真实创建/写入独立 SFTP partial，再 pidfd 暂停写进程并杀 control helper；age 仅修改 disposable marker+对应 vault 记录，不替换产品时钟；实际 Lock/Unlock/ChangeEndpoint→Running/History 先保留，观察写进程退出后再回收。重连安全表补 pinned 文件/链接、获准 root readonly 树、替换 inode 和 unpinned 实例，并核对旁边用户文件。均未运行。
- 新 TestHostedSystemPartialJournalLifecycle 走真实 queue→OpenSSH sftp-server/sudo→加密 vault：commit/abort 必须实际提交或删除并销账；connection-loss 通过精确安装目录 fd9 inode 识别系统写进程，pidfd 暂停后关闭连接，真实解锁/重连清理只能保留，观察退出后第二次连接才能回收。该组旧红需在候选撤销 SystemFiles journal wiring（保留本批原有系统后备 API）取行为失败，不能把基线缺 SystemFiles 符号算红。现有双端受保护目录用户传输 E2E仍需验证，生命周期测试不替代主流程。SIGSTOP 后单次状态观察不满足属于 fixture 失败，不算产品旧红。
- 仅 gofmt 和 git diff --check 通过；无测试、编译、提交、推送、workflow、新产物或部署。当前所有新增用例都是待运行，不是红绿证据。没有启动用户 GUI 或连接私人主机。

## 本轮 O/helper 安装登记增量（仅源码，未运行）

- 上一轮仅答复长期候选授权，属于 no progress；本轮已重新核对工作树和游标并推进源码，无真实 run/job 等待句柄，无 blocker。复核 systemd 临时目录/官方 flock 文档，沿用现有目录租约，不新增清理框架或依赖。开始前确认失败方式：写前保存失败不能 mkdir、inode pin 失败不能上传/执行、断线或清理不确定不能销账、提权恢复不能把普通上传者 UID 当成 root UID。
- 从 NewWorkspace 提取共用 createWorkspaceDirectory，保持私有随机目录、固定 cwd、marker stdin 写入和父/目录身份校验；helper Start/StartElevated 可选同步 WorkspaceJournal，GUI 的全部普通/root SSH/sudo helper 路径绑定冻结 host ID 与两条实际认证通道身份。先保存意图，再建目录/补记 inode，再上传二进制及 SHA 校验。正常 Session.Close 收到 helper 清理确认后才销账；保存失败留账并报告。安装前失败用原目录身份+独占锁清理，不再 blind Remove，失败 NonRetryable；root SSH 分支遇到此类安全失败不继续 sudo。
- GUI 注册了 journal 的 helper 不再调用 cleanup-stale-temps 扫描其他安装；未注册的低层旧 API 仍保留原入口。正常 helper 安装/运行不增加系统 flock 依赖；安装前失败清理/重连清理仍要求系统 flock，不可用则保留，不静默绕锁。Lock 后旧 generation 不能回写销账，新解锁可确认已不存在；没有恢复 Pending。
- RecoverWorkspace 只对 saved.Elevated 且实际执行 UID=0 的精确记录允许 root 检查原上传者所有的目录；marker/目录 UID 仍必须匹配原记录，父 inode/目录 inode/marker SHA/创建时间照常逐项核对，通用 stale scan 不得到跨 UID 权限。清理命令比较保存的 UID 而非错误要求 marker 必须 root 所有。提权弹窗返回后再次核对配置代数，避免用过期授权执行删除。
- 新 TestHostedHelperInstallationJournalLifecycle：真实队列→已上传 helper→实际 probe→加密 vault，普通/sudo 正常关闭销账、Lock 保留原记录且不恢复 Pending、真实不可写 vault 文件系统错误拒绝新安装；通过基线已有 API 和 JSON 读新增字段，预期旧红是缺登记或错误继续创建，不是缺符号编译红。现有重连回归新增普通上传目录内有真实 root 私有子目录场景；走读修正原测试把 job:update 误写为 job（尚未跑过，不是调整失败预期）。
- 真实失联用例扩为 TestHostedHelperControlLossPreservesLiveDependents，在官方 Hans 两种场景之外加入实际独立 --sftp SSH channel，标准 pkg/sftp client 先读取真实 ELF 字节，再 pidfd 暂停精确 SFTP 进程、杀 control helper，要求安装与锁保留，明确结束该进程后才清 fixture。没有 mock helper/Endpoint；SIGSTOP 后观测就绪仍须 hosted 实测，fixture 不满足不能算旧红。
- 仅 gofmt / git diff --check，无测试、编译、提交、推送、workflow、产物或部署。helper/SystemFiles 独立 partial 持久恢复仍未完成，其他 A–P 项保持原范围。未对整个 O 或项目宣称完成。

## 本轮 O/SCP-rsync 对端退出增量（仅源码，未运行）

- continuation 为 progress，上一轮亦为 progress，无真实等待句柄/阻塞。走读确认 runAgentMethod/runHansMethod 在传输错误后直接 Remove partial，会越过 helper 停机锁；FlySSH finishSession 的 prior-error 分支还会丢弃 Wait 的缺退出状态。已核对上游 master 的 public transfer 源码和官方 x/crypto/ssh Wait 文档；未发现上游已有受控 server 前缀/完整错误保留。API commit 列表访问失败仅为资料访问失败，不是产品测试失败，未升级依赖；新增 vendor 补丁明确记入 FLYSSH_PATCHES.md。
- 新 --transfer-server 只接受系统 SCP/rsync server 的生成参数、绝对操作数，使用 /usr/bin 或 /bin 固定工具路径，无任意 Shell eval/额外 sudo。main 在进入该模式前持安装目录锁，外部实际写入进程继承锁并独立进程组；native stdin 数据协议不改、不放凭据/路由到 argv/env/file。FlySSH Spec 可选 argv 前缀逐词引用，nil 保持 CLI 原命令；rsyncbridge 以相同方式前缀远端 server。源推送 receiver helper 的安装用于对端持锁，目标拉取本来在目标 helper 内写入，沿用已有锁。
- 删除 direct/Hans SCP/rsync 的失败分支盲删，交由最终 helper Close 取得独占锁后处理。Hans 推送把 partial 登记在真实目标 serverAgent，拉取在 client transferAgent；目标提权判断使用任务真实 target 权限而非仅当前 direction，提交/校验绑定正确 targetHelper。普通 rename 成功后显式 forget-partial，失败销账不可重试/源保留；高权 Commit 原有销账继续使用。原 encrypted-stream 的 scoped cleanup 不在本轮重写。
- FlySSH 新 ExitUnconfirmedError 保留原始错误链并 Retryable=false；finishSession 不因 prior SCP error 丢弃 Wait 错误，RunContext 合并 ctx 与实际传输结果。rsyncbridge 对缺远端退出状态/远端 signal，以及本地 rsync signal/WaitDelay 保留退出未确认。helper 协议 optional error_code=exit_unconfirmed 传递机器状态；Send/Receive/非法 response 同样按请求可能已执行处理，不靠错误文本分类。control-side SCP/rsync Fork 已认证路由，不再让 RunContext 关闭浏览/PTY共享 client。没有 helper 的 local/SSH 方法使用随机 partial，未确认停止则保留路径；已确认错误的清理限时且保留清理失败，不吞掉原错。
- 新 TestHostedPeerExitLossRetainsLivePartial：两 SSH 夹具、实际 runAgentMethod/SCP/rsync/32MiB 源目录/move；inotify 等真实 partial 创建，pidfd 暂停精确工具进程后仅杀所属 sshd session，检查 NonRetryable、源清单/SHA 不变、目标未发布、活跃 partial 仍存在，最后 pidfd 退出后才删 fixture。原生工具/端点不被 mock，基线已有方法可以编译；旧红应是丢失退出错误或提前删 partial。已观测停止状态不符属于 fixture 失败，不能算代码旧红。没有运行、没有红绿结果；已有正常 direct/池/Hans 矩阵需同时验证新 server 参数兼容，不能用新增安全用例替代正常主流程。
- 仅 gofmt / git diff --check。没有本批测试、编译、提交、推送、workflow、成品、部署或本机 GUI。持久恢复及 A–P 其他未完成项保持原范围，未宣称 O 完成。

## 本轮 O/helper 生命周期增量（仅源码，未运行）

- continuation 为 progress：新增 internal/agentlease，在已验证的私有安装目录持共享内核 flock；main 所有入口在握手/文件读写前获取（含 --sftp、rsync child）。Hans 保留口令 fd3、追加目录 fd；ncat/rsync 同样继承。清理不用共享描述符原地升级，而是关闭本进程副本、另开描述符申请独占锁，子进程继承副本仍能阻止清理。锁持续到 scoped partial 和安装目录处理完；重新核对启动时 inode。新安装 marker v2；v1 一律保留，不再因为 PID 消失/复用自动删除。没有添加远端系统 flock 的 helper 依赖，Hans 仍使用官方 release 二进制。
- Service.Close 改为一次性、可返回错误的停机：停止 admission/cancel→等待活动 Handle→停止/join 子进程/监听→独占目录→清理。超时或错误不删除 partial/安装目录。监听 done 从单次 error 消费改为关闭广播并单独保存结果，真正退出才从 map 移除，防止 wait/stop 超时后漏掉仍写入的接收器；现有 listener_cancel_test fixture 随完成契约调整，未改取消预期。Hans stopOnce，退出未确认不提前删进程记录；取消信号针对 Linux 专用进程组，Wait 有界。rsync 同样独立进程组/有界 kill-and-wait。
- 控制端 Session.Close 普通/高权均请求 helper 受控清理，不再强制远端 Remove；Start 请求或握手失败可能已经执行，保留 marker 目录。独立 helper SFTP 复用 systemFileTransport 的 EOF+远端 Wait；BorrowFileChannel transport 明确拥有 SFTP Close，避免二次关闭的 EOF 被误判为清理失败。startTransferAgent cleanup 返回 error，direct/池/Hans/加密流及高权文件视图将其保留到任务最终结果，并以 NonRetryable 停止换策略，不能把“不确定已停止”当作下一次写入的起点。Hans 重试前 StopProcess 错误也停止重试。
- 新 TestHostedHelperControlLossPreservesLiveHans 使用实际上传 helper、官方 Hans server、真实独立 SSH transport、Linux pidfd/select 退出事件：普通断线应退出并回收；kill helper + SIGSTOP Hans 应保留目录和继承锁、Close 不声称清理成功。测试先核对真实 stopped 状态，fixture 失败不能当成产品旧红；没有复制/模拟 Hans。只用基线已有 StartElevated/InstallHans/StartHansServer API，新目录租约不以缺符号编译红取证。已有真实 flock 安全表补了 v1 明确不存在 PID 的孤儿场景。上述全部未编译、未运行，尚无红绿证据。
- 官方资料已核对 Go os/exec ExtraFiles/WaitDelay、Linux PDEATHSIG（父线程、fork 后清除），以及 rsync upstream pipe.c 的 fork/exec 描述符处理；这些只是设计依据，不是 runtime 证据。仅 gofmt 和 git diff --check，无本批候选、workflow、构建、GUI 或部署。SystemFiles/独立 partial 持久恢复、对端子进程退出确认、A–P 其他项仍未完成。

## 交付约束

- 用户已长期授权：完整实现、测试和文档形成一批后，可以先推候选，再在 GitHub-hosted workflow 取得旧版红/新版绿证据；最终 PR 一个完整交付提交。该授权不包含合并或发布。
- 当前优先 macOS arm64；用户验证后才扩展其他平台/架构构建，不沿用旧“立即六平台发布”游标。
- 不运行用户 GUI、私人服务器、旧保险库或本地/私有 runner 测试。凭据夹具仅用于 hosted runner；原始私人日志不进 Git。
- 无实际 run ID 时不轮询。workflow 状态/失败日志必须使用用户指定的专用脚本和等待约束。不得用旧徽章或模拟输入冒充新候选原生验收。
