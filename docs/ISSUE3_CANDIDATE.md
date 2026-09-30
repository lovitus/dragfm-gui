# Issue #3 完整修复候选 · 待验证和独立评审

关联 #3。本 PR 是包含后端、协议、前端、回归和文档的一批候选，不是验收完成或发布声明。基线为 `6c339a19ecc621ff1cdee0ba9f5775b0ac37342e`。所有原始需求继续以 `docs/USER_REQUIREMENTS.md` 和 Issue #3 A–P 为准，不自动关闭 Issue 或勾选需求。

## 当前候选增量与安全警示

**7491699e0227b1ec5c42fde3c5437278eb7ed2d1 的完整 hosted 主链已成功，当前准备其集中复审指出的最小输入清理收尾，尚未冻结/验证该增量。** [run36698319439](https://github.com/lovitus/dragfm-gui/actions/runs/36698319439)的实际工件与event/head/tree均已核对；主native及真正解包native均23/4/4/8/7项成功，包含两栏真实大目录滚动、深处命中、元数据、单选、可信双击、目录/cwd及空白Backspace返回。Go/race/重复sudo无fail、三SSH37根pass，固定上游/default-driver27项pass。产品/native/最终收据SHA为 `49edec095d04a2529d6bccf763ea7128b8b33a73180aed675683193dedbf86a6`，五项bundle校验OK；旧7ed指定A失败、两项O旧红→新绿留在原run，不重复执行。完整基线上仍一个候选提交，无新部署、合并、发布或自动勾Issue。

749保留原PyAutoGUI发送器，只给其四个left-down/up事件设置Apple原生click-state 1,1,2,2；实际可信dblclick2和完整导航已经成立，未放宽原20/260秒或动作上限。[原角色749集中复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5908769071)指出条件性P2：按下后fail-safe拒绝释放，旧close的公共mouseUp也可能被拒绝并掩盖原动作错误。当前最小修正只在原post恢复后、当前位置发送owned left-up，不关FAILSAFE/移动/激活/补down/重打动作；分别保留原错误和释放错误，清理失败不能成为PASS。它未实际故障注入，正常749绿色不外推corner边界；本增量仍须完整hosted及原角色集中复审。产品源码与749不变，所有永久门禁保持；无本地测试、构建或GUI。D/F4人类呈现选择与重要移动警示仍保留。

## 历史：7ed 冻结、集中复审与首次限定对照

**A/O 完整候选 `7edcf26797df3c02272a72e760656ce8d481ff5f` 已冻结并推送，尚无新运行结论。** tree=`53b039d9ad06875d2a67c5d3ef349a15f963fb6b`，原始基线之上仍一个完整提交；真实多屏文件浏览动作、默认公开 SCP 目录/文件权限分支、对应指定行为撤回与需求文档已经合批。产品仅修 vendor 的无 `-p` 文件 chmod，`cmd/internal/frontend` 未改，不宣称本轮改变 GUI/终端外观；无本地测试、构建或 GUI，无部署、合并、发布或扩平台。下述 4f 是历史已测事实，不代替本候选 PASS。

[原角色 A–P 集中结论](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5906481045)限定认可当前 Mac 的 P 技术链及 B/C/E/F/G/H/I/J/K/L/M/N 所列契约证据，非任意环境认证；[A/O 设计复核](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5907117332)确认现有 nil-prefix OpenSSH 队列证据并补充无 `-p` 的目录 umask/已有目录模式分支。新 A 通过原 OS 输入器实际滚动至 192 项目录深处，分别检查渲染/坐标/元数据后，点击文件、双击目录、空白 Backspace 返回并验 PTY cwd/滚动复位。新 O 固定原字节复用上游用例，直接运行当前 vendor，再以 child-only umask 的公开 FromOptions/Run 驱动默认目录分支；不是外部 CLI 可执行文件或四 CPU 运行认证。

整批走读发现真实缺陷：无 `-p` 文件下载无条件 Chmod 会把 caller umask 创建的 `0640` 改为 `0644`；当前 FlySSH 上游 HEAD 也尚未修复。依 [OpenSSH 官方分支](https://github.com/openssh/openssh-portable/blob/master/scp.c)最小只在显式 `-p` 时恢复源模式，保留既有 umask 与 `-p` 行为，不改测试期望。同一 hosted 主链只接受指定前提成立后的实际红→绿：A 临时撤回独立虚拟行 offset，O 分别撤回 mkdir owner-write 与文件 preserve chmod 门禁；编译/启动/认证/其它失败均不算红，随后当前源码、上游原 `-p` 正例与真正解包 Mac 必须通过完整门禁。已成功的 4f recorder 固定旧侧调用在本批撤下，永久输入/提取门禁保留。原始 A–P 不缩减、不自动勾选 Issue；D/F4 人类呈现选择和重要移动警示仍保留。冻结后才请原固定角色集中读新实现，并据同事件真实工件更新本 PR。

[7ed 集中实施复审 `5908173160`](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5908173160)已完成：原角色核对冻结源码/tree/唯一parent和整条接线，限定增量未发现新增P1/P2或producer绕过；仅静态，未读本轮CI/私有工件，仍是行为待证。driver的existing是已有目录内新叶文件，不称已测覆盖已有叶文件；DOM路径步骤、深处文件mtime与隔离负对照构建的证据范围不扩大。本候选唯一 [run `36694314681`](https://github.com/lovitus/dragfm-gui/actions/runs/36694314681) 首次指定脚本600秒仅观察超时，未知终态；不归咎代码或重触发。实施复审里程碑收齐后只对同一ID作一次有界终态读取，取得真实结果后再更新。

## 历史：4f 已测候选与最终收据闭合

当前完整候选为 **`4f92c268f5cda994ad52ff70807be6dc315b6a4c`**，tree=`37937cd6d2ff818c1eae3293d6bebc71d598034f`，原始基线之上仍一个完整提交；唯一[主门禁 `36679311064`](https://github.com/lovitus/dragfm-gui/actions/runs/36679311064)已 success，实际工件已核对，未部署、合并或发布。仅 Mac 交付链增量，GUI/终端/传输产品代码与 0d/f72 相同，不宣称本轮改变了终端呈现；D/F4 选择仍待用户确认。

**本轮最终 recorder 四格指定旧红→新绿成立。** 删 SOURCE 校验行、连同 SOURCE 删除、自洽改写、增加未列资产：固定 0d recorder 都实际写出成功收据，新侧均按固定清单/资产集合拒绝且无成功收据。旧 script 的实际 blob SHA=`6a0de0752dc7d972f5fb198630c3e27bfe9c396c844043429b2c1fbb63d96328`；新完整正例、缺固定输出控制以及原单 Mac/来源/许可/拒绝门禁通过，完整正例仅新侧，不称双绿。真正解包的独立 Mac 主链也已执行原入口，22/4/4/8/7项成功；这与 Linux 上 recorder 输入对照分开记录。SSH37根 pass/无 fail，Go/race/重复sudo无 fail、audit0。

新 event=`fb4318edf7fbdd649eaf69fb4a2e4a6ff3cdcf32` 的 parents=基线+4f、tree=37937cd6；七份 producer 和最终收据同 event/head/tree/run/attempt。Mac 成品/native/校验三方 SHA=`462ffb1418bac0e5da90f8f687e9e77b794a88e037cba62b3e4872f98af6d08f`，实际归档=`20ea7acb3d072de3decb4d62dd1a44b5053e64648eea5be72e167bfca9f88b28`，SOURCE=`0c7dafdd58b4c888e885d5546821c3fc567909424f0e7b0a4cb2e61855960d5f`。五项分发校验全部OK，最终 fixed manifest=`b74fb5a8c3202aed57bf8dfeccc898d2bf1ff82cedc86050506d7578ae7af114` 与实际下载清单相同；证据ZIP仅一个VALIDATION.json，无原始日志外带。[原角色实现复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5905687463)只读静态确认接线与无新增P1/P2，没有独立读取本轮私有工件。该具体P2有运行闭合事实，P/A–P总体仍待逐项核验，不自动批准PR、合并或发布。

**历史 0d：P/macOS arm64 的实际单平台链已经运行；当时未闭合的最终收据边界已由上述 4f 四格证据补齐。** [原固定角色设计复核](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5903928362)确认该工作可独立于 D/F4 推进。显式单平台打包消费同事件 source/test/三 SSH shard/full native 原字节与成功收据，默认六平台要求保留；PR/main 由 build 唯一调用 SSH，release 去掉重复 SSH 与候选旁路。打包/解包不重建或重签名产品；实际许可、对应源码、完整校验和、脱敏摘要，以及真正解包后独立 Mac 原生链已核对。0d 的两条 f72 旧红→新绿与八个输入/默认六平台控制成立；两个一次性对照已撤下，永久新行为回归保留，不重跑。SSH 37 根通过；Go/race/重复 sudo 无 fail、audit0；原包前与独立解包后的 native 均 exercise22/restore4/changed-key4/文件系统8/普通无参数库启动7项成功。这些仅证明所列技术场景，不批准全部 P/A–P。

0d event=`e1a4609de5fd87f16b4d7081847b30343e2e3bd9`、tree=15b99aa 与候选一致。Mac 原生/实际成品/校验文件三方哈希=`462ffb1418bac0e5da90f8f687e9e77b794a88e037cba62b3e4872f98af6d08f`；实际 Mac 归档=`8602a6b35658b8013ed55adfbc2dde7d3c04904c75abc4a690f39eba79e5ad03`；SOURCE=`cef570c7bda12edd62ff85125a9ed4a104aa86c248a2cbf8b2a01bea6ea8fe9d`。实际五项分发校验全部 OK、证据 ZIP 只有 VALIDATION.json；原始输出仅私有保存，没有重新部署/运行用户设备。

[固定角色实施复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5904450123)曾指出条件性 P2：最终 recorder 缺少执行前完整清单固定值，删去 SOURCE 校验条目或同步改写清单/源码仍可能成功。它没有看 CI/私有工件，不代表实际损坏或批准全 PR。本轮严格提取器输出固定原清单哈希，最终 recorder 对比该值、完整资产集合和全部字节，缺输出也失败；同事件真实输入四格与新完整正例已通过，详情见上。旧 f72 两条对照没有重做，原永久门禁不删；本轮四格成功后仅在下次完整冻结撤固定基线，不另推文档/删除入口微提交。GUI/终端逻辑未改，原始 A–P 范围保留。

**完整候选 `f72b1224f67bfb5c5bad8e7c766b8ae9c0e19a60` 已测，本轮三个门禁均 success，仍未宣称全需求验收。** 仍为原始基线之上一个完整交付提交，tree=`94cecaf8497ac172247b4a45b1a98ad3d5e6ae46`；同次 push 的[主门禁](https://github.com/lovitus/dragfm-gui/actions/runs/36662871843)、[SSH](https://github.com/lovitus/dragfm-gui/actions/runs/36662871946)及[新取消边界红绿](https://github.com/lovitus/dragfm-gui/actions/runs/36662871910)均已读取终态和实际工件，没有重触发、部署、合并、发布或本地测试/构建/GUI。下述 a247 为历史基点。

新取消红绿实际成立：旧 a247 的两方向 routed 78 在真实取消+精确 peer 退出前提后出现指定过滤失败，新 f72 两格通过；77/普通 23/未 routed 78 六个控制均双通过，没有额外测试失败。[原固定角色冻结实现复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5903317508)确认本轮限定增量无新增 P1/P2，该角色未读运行工件。SSH 三组实际 37 根全部 pass、无 fail，新八格在正式门禁也通过。主门禁首次 600 秒等待超时，待不同门禁/复审收齐后的一次有界读取取得同 run success，不误报故障或重启构建。Go/race/重复 sudo 无 fail、audit0；普通无参数成品创建、错误密码、手动锁定、重启及便携路径选择共7项通过、5次真实启动，库头/私有权限/无明文密码校验通过。原 native exercise22/restore4/changed-key4/独立文件系统8项通过；原主机选择等 DOM 步骤不冒充 OS 输入证据。一次焦点对照不倒写历史 None 根因。

主/SSH/source 工件 merge 为 `9841bbd79f2f131fe2bd6f6ebcdda6371ec25896`，父提交=基线+f72、源码树相同。原生记录、成品及 SHA256SUMS 三方哈希为 `462ffb1418bac0e5da90f8f687e9e77b794a88e037cba62b3e4872f98af6d08f`；只复制原始 hosted 字节到版本化候选目录供用户验证，未覆盖旧成品/保险库或启动 GUI。源码归档实际含 GPL/FlySSH MIT/Hans 1.7 源码与许可证，未本地重打包。完整发行打包/提取后验收仍未做，不为补 P 触发未经授权的六平台或发布。GUI 导航按既有设计保留正常 cd 转录，不擦除历史，完整提示符不等于导航命令隐藏；D/F4 口径继续明示核对。H/P/A–P 和重要移动警示不自动关闭，下一步由原角色接事实记录与需求账本。

[原角色行为记录](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5903682137)已限定闭合 routed78 的具体 P2，并更新普通无参数启动证据；不批准整 PR。下一唯一验收重点是 D/F4：原要求关注随机残留/重复/截断，现有正常自动 cd 转录是否接受没有最终明确决定；也不能擅自新增“所有自动命令绝对隐藏”要求。现已向用户提出保留正常记录或隐藏程序导航命令的一个呈现选择，选择前不改代码/预期/用户历史。P 与其余未验范围继续保留，不重新打开已成立红绿，不为删临时入口或结果文档另推微提交。

**完整候选 `a247a674a3792e89376f13644fb3804807d2ab8a` 已取得实际结果，整批仍未通过。** [SSH 门禁](https://github.com/lovitus/dragfm-gui/actions/runs/36654234706) success，三组实际 36 根全部通过，含原反向禁 helper ncat、反向 system rsync、反向 SOCKS ncat 和 STOP/退出丢失四格。[一次性行为红绿](https://github.com/lovitus/dragfm-gui/actions/runs/36654234701) success：14 条实际旧行为失败/新通过（Bash 6、真实 SSH pair 4、Paramiko peer 2、取消 23/77 两格）；旧 Bash 完成 child 输出后仍出现 No-record/期限失败，新监护及时返回。Bash 为 5.2.21-2ubuntu4，libc6 为 2.39-0ubuntu8.9；127 的保守分类不冒充另一次 UAF 复现，错误边界不冒充完整移动 E2E。产品 trap 的 127 与独立解包注入没有独立运行证据。既有红绿已成立，不重复运行。

[主门禁](https://github.com/lovitus/dragfm-gui/actions/runs/36654234699) failure：Go/race/重复 sudo 无 fail、audit0。原生无参数创建已由 AX 看到 workspace，但随后第一次 workspace capture 前，输入器的 NSRunningApplication 查询返回 None；此前 Popen.poll 仍存活。仅有 create/workspace 两次观察和 8 次焦点检查，库头、手动锁定和其余 native 流程未执行，H 不通过；不能据此宣布产品退出或已确诊缓存内部根因。主/SSH 工件 merge 的父提交与 a247 源码树已核对。原始证据仅私有保存，未重跑、部署、合并或发布。

[固定角色集中复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5902200944)静态确认监护/77 传播，新增 P2：内部同伴取消时，已分类的 routed 78 可能被 pair 过滤，导致继续后备；这不是已经绕过指纹认证或旧 No-record 原因。同批实现已保留显式不可重试标记、继续过滤普通同伴取消。原生输入器现在只在需要原一次激活时要求 NS 对象；已存活且实时 AX 确认前台的目标，不再被 NS 空对象否决。原权限/几何/最终焦点/停止保护不放宽。原角色已集中认可最小设计，本地实现只做 gofmt/diff 检查，尚未运行验证；A–P、H/P 和重要文件移动警示保留。

新回归使用原真实 OpenSSH fixture/pair：peer ready 后由 listener 的真实 23 触发内部取消，TERM ack 后才释放 peer；旁路观察适配器不改任何实际 Exec 输入/返回，先验父 context 存活、listener 23 和取消+精确 peer 状态，再验最终停止标记及原错误链。两方向的 routed 78 必须旧红/新绿，77、普通 23 与未 routed 78 共六格必须两侧绿；无真实 TERM/精确退出证据或编译/认证失败都不算红。临时入口仅对照 a247 的这个新边界，不重跑已成立的 14 条对照；该表不冒充真实指纹握手、产品 trap 或归档移动 E2E。下一步冻结同一完整候选统一 hosted，再由原角色集中实现复审，未合并/发布。

历史已测基点 **`183279bf4dea03846ddf62c4395645a8be8cd7ec`** 的结果如下，不外推为 a247 的通过证据。[主门禁 36650078687](https://github.com/lovitus/dragfm-gui/actions/runs/36650078687) 首次600秒仅观察超时，随后通过指定脚本继续读取同一run，已取得 **success**，未重跑。

实际工件已核对：Go/race/重复sudo无fail、audit0；普通无参数成品的创建、错误密码保持锁定、手动锁定、重启及程序旁/系统路径回退/优先级共7项通过，5次启动均无参数、不同cwd且初始锁定。原native exercise22/restore4/changed-key4及独立文件系统8项通过，含原生拖放、Running/Pending/History、终端及重连；原流程中主机选择等DOM动作仍只按DOM证据记录。主/SSH工件merge及tree一致；成品SHA-256为`191fee625cc35ba90abdfb1a4ea14ab02e49cbbda4270f24f5b90be88a411ba6`，仅私有下载核对，未部署。一次焦点对照NS/AX均匹配，只能确认当前方案通过，不能倒推旧缓存根因或批准整个H/P/A–P。

[SSH 36650078700](https://github.com/lovitus/dragfm-gui/actions/runs/36650078700) 已失败，实际工件34根中33通过，唯一失败是禁helper后首连接被拒、应换端口完成反向ncat移动的场景（90.07秒）。现在有新边界证据：进入父期限分支前pair耗时87.658秒（不包含随后取消收尾）、源结果已消费而目标结果未消费；目标取消错误后附两端共享stderr中的Bash `wait_for: No record of process` 重复诊断，行号对应监护正常 `wait -f`。尚未确认报错侧、内部根因、文本起始时刻或具体哪次端口尝试；不能回退STOP/退出区分，也不能把失败后未执行的保源/清理断言计为通过。三个工件merge的parents/tree与当前候选已核验；两项旧超时用例本次通过仍不销账。原始证据仅私有保存。

[固定角色限定复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5901667570)未发现本轮焦点增量新增 P1/P2，但指出两处条件性 P3：证据写入可能覆盖 AX 失败摘要，以及显示恢复异常可能阻止动作计数保存；它们已记延期收尾，不为此重跑。当前先定位真实传输失败，不宣布 H/P/A–P 或整 PR 通过；未部署、合并或发布。

### a247 已验证的监护机制与未验边界

已对照[GNU Bash官方5.2.21源码](https://ftp.gnu.org/gnu/bash/bash-5.2.21.tar.gz)、5.3源码及[5.2 patch 30](https://ftp.gnu.org/gnu/bash/bash-5.2-patches/bash52-030)：旧数字PID强制等待保留PROCESS指针，而内部等待可能通知并删除记录；jobspec路径改为重新检查job槽。新候选使用`wait -f %1`，保留[真正终止而非仅停止的语义](https://www.gnu.org/s/bash/manual/html_node/Job-Control-Builtins.html)，并将spawn到结果捕获/退出置于同一预解析brace group，避免读取下一行时提前通知快速完成的job。外层仍只有一个自有后台任务，信号只操作原精确PGID，fd9租约及原STOP保护不变；不要求用户升级Bash。以上是相关机制依据，不是旧日志的具体报错侧/时序已经确诊。

127与缺job无法区分时保守转既有77，真实工具127也会停止重试，这是明确的安全取舍。正常/取消路径都保留77；pair全部Exec出口及独立解包按监护协议分类，普通工具78不冒充指纹变化。SSH取消保留原错误链，专用调用者才解释77；native对端77不再降为普通RuntimeError/1。固定角色已静态确认接线，已运行边界结果见上；独立解包和产品 trap 的 127 不用相邻组件结果冒充故障注入通过。

验证沿用原完整移动、换端口和STOP/SSH退出丢失场景，不改期限/期望。真实Bash快速/延迟0、23、127表、真实SSH监听/发起侧分类，以及实际Paramiko/SCP/rsync对端和取消状态回归已对照183旧代码取得上述行为红绿，编译/配置失败不算红。glibc官方[释放后内存扰动与tcache设置](https://sourceware.org/glibc/manual/2.27/html_node/Memory-Allocation-Tunables.html)仅用于该回归子进程，不改变产品/整台runner，不保证某种内存值或必然失败；实际版本见上。127→77只是保守分类证据，不混作旧等待缺陷复现。新的 routed 78 P2 尚未验证，无本地测试/构建，无合并/发布。

### 历史：183279b 准备与 eba99e6

**最新已测 `eba99e60324524a07219f35a2b908a0149be630e`，仍未整批验收。** [SSH 36647102944](https://github.com/lovitus/dragfm-gui/actions/runs/36647102944) 实际 34 根全部通过，旧反向免密及反向 SOCKS ncat 子例均通过；没有触发超时取证，两项历史超时仍原因未知，不冒充已经修复。实际三个工件 merge 的 parents/tree 已与候选核验。

[主门禁 36647102905](https://github.com/lovitus/dragfm-gui/actions/runs/36647102905) 的 test/source 通过，Go/race/重复 sudo 无 fail、audit0。macOS 普通无参数成品实际完成创建、逐字库头、手动锁定及退出；出现一次失效快照后，按原安排读取到完整正确锁定页，原观察修正有行为证据。重启也显示正确锁定库及 hint，但输入驱动首次动作前未通过前台焦点检查；错误密码/重启解锁/系统路径回退与后续旧 native 流程未执行，H 仍不通过。

本轮仅准备原生输入驱动的焦点读法修正，尚未运行。普通启动在主线程、后续流程在 reader 线程，两者均无 Cocoa 主事件循环；[Apple 文档](https://developer.apple.com/documentation/appkit/nsrunningapplication?language=objc)说明相关易变状态依赖它刷新。比较 AppHelper 主事件循环与直接 AX 读取后，选择范围较小的现成 [PyObjC ApplicationServices](https://pyobjc.readthedocs.io/en/latest/apinotes/ApplicationServices.html)（固定 12.2.2）：同步读取[当前接受键盘输入的应用](https://developer.apple.com/documentation/applicationservices/kaxfocusedapplicationattribute?language=objc)并严格比较目标 PID，不另建调度框架。

失败边界：AX 权限不足、无值、消息错误/错误类型或焦点不符均停止发键；初始化设置 Python 进程的两秒 AX 消息期限，原应用存在/进程存活、单窗口/几何、Quartz 权限与 fail-safe 保留。最终 AX exact PID 读回在几何检查之后、实际发键之前；原一次激活与 0.15 秒间隔不变，不修改 TCC、不重试输入、不用缓存回退放行。只在重启首个动作记录一次非原子 NS/AX 布尔对照、线程、存活/激活状态和读取耗时；失败也保存既有动作计数，不读取密码字段或记录其他应用身份。这不证明 eba99e6 的具体失败一定是缓存误判；旧 nil 与两个传输超时也不自动销账。

固定角色已完成本轮独立设计复核，明确推荐 Python 同步 AX 方案，并要求保留失败关闭输入、最终读数位置和一次对照等边界；本地增量已据此收尾。该角色只读冻结 eba99e6 与提供的证据摘要，不将设计意见冒充新候选运行通过或整 PR 认可。没有新增测试、放宽断言或 workflow 重跑，两个现有原生入口只同步依赖声明，未触发多平台/发布。H/P/A–P 未批准，未部署、合并或发布。

### 历史：eba99e6 准备及 a3a13a0

新准备增量：固定角色已完成 a3a13a0 集中走读，支持 AX 失效时丢弃整次快照、消费原有限采样次数，仍未确认 ncat 死锁原因。原用例失败日志补已有策略 Started/Elapsed 和准备时间；原 ncat 取消出口保留两端错误、有限 stderr 与结果是否已消费的状态，便于区分尚在执行和已经进入收尾，不把 stderr 文本作为取消协议。没有改策略顺序、期限、网络、进程监护、租约、重试或测试成功条件，没有新增测试/监控框架。观察修正与错误层级取证统一随完整候选验证，当前尚未运行，不将取证改动冒充传输修复。

**最新已测 `a3a13a0a5f5f940c4c5a9cd95f80471d4c1b00da`，整批仍失败、未验收。** [主门禁 36644593753](https://github.com/lovitus/dragfm-gui/actions/runs/36644593753) 的 test/source 通过，Go/race/重复 sudo 无失败、audit0。原生创建提示在截图中保持小写、真实加密库头逐字匹配成功，文件权限与无明文密码检查通过；这是相对 49a2262 的有限字段修正证据。之后手动锁定的 AX 读取报 `invalidUIElement (-25202)`，H 及旧原生后续未运行，不能扩大为保险库完整验收。

[SSH 36644593872](https://github.com/lovitus/dragfm-gui/actions/runs/36644593872) 实际 34 根中 33 通过。原反向免密子例通过，但旧超时未销账；新失败是禁 helper/native 工具、直接 TCP 拒绝后要求反向 SOCKS ncat 的完整策略子例，五分钟期限内尚未进入 SOCKS。正向 ncat 五端口按预期拒绝，反向 ncat 返回期限错误；目前没有确定根因，不以网络或旧监护修正代替诊断。三组工件来源 merge/parents/tree 已与候选核对，完整原始证据仅私有保存。

未提交准备仅包含观察器在 AX 对象失效时作废整个快照、按原有限计划重新读取，而非接受残缺页面。依据 [Apple 的 invalidUIElement 定义](https://developer.apple.com/documentation/applicationservices/axerror/invaliduielement)，不是修改权限或放宽验收；尚未执行。固定角色正在集中核对反向 ncat 与该观察边界，等待超时但任务仍 active，未取得新的评审结论。未重跑冲绿、不部署/合并/发布，A–P 不勾选。

### 历史：提示字段修正及 49a2262

本轮字段修正待 hosted 验证：只对创建保险库的主密码提示设置 `autoCorrect="off" autoCapitalize="none" spellCheck={false}`，不改变字面值、onChange/API/后台、密码字段或已有保险库。固定角色已核对输入到保存链，支持这一有限处理；未查本轮 CI 或私有工件，不代表整 PR 批准。依据 [HTML 标准的字段文字辅助控制](https://html.spec.whatwg.org/multipage/interaction.html#autocorrection)，不把实体键盘首字母变化单独归因于 autocapitalize。发送器/系统设置/测试逐字比较及期限都不改；复用原普通无参数创建、锁定和重启验证，不新增属性字符串匹配测试或第二套验收框架。旧行为证据是下方 49a2262 的实际保存前截图与库头失败，新通过尚未取得。

**最新已测 `49a22629005da6297da38c9d3b92866cb7b2b0f6`，整批仍未验收。** [SSH 36642350676](https://github.com/lovitus/dragfm-gui/actions/runs/36642350676) 的三个分组共 34 根全部通过，原反向普通用户 system rsync 子例 11.23 秒通过。此候选仅增加原超时分支诊断，生产代码未改；没有复现 a1dfa42 的超时，也没有取得其根因或修复证据，旧失败仍保留。三组实际工件的 merge 父提交与候选 tree 已核验。

[主门禁 36642350653](https://github.com/lovitus/dragfm-gui/actions/runs/36642350653) 的 test/source 通过，Go/race/重复 sudo 无失败、audit0；macOS 普通无参数创建进入工作区后，真实保险库头的 hint 精确比较失败。文件私有、未检出明文主密码，实际/期望 hint 长度相同。关键新证据是在输入任何主密码前的窗口截图中，计划的小写 `plain-…` 已显示成 `Plain-…`；不将它误报为密码泄漏、选错库或已确诊持久化错误。H 后续原生流程未执行，本候选没有已验收成品。

输入适配器仍冻结；下一步沿提示字段真实输入链核对最小文本转换处理，不重新打字、不改比较期望/时限或系统设置。固定角色已收到本轮脱敏证据，整 PR 不批准、A–P 不勾选，未部署、合并或发布。完整候选先推、GitHub-hosted 后验的长期授权有效，不反复询问；不将授权扩大为碎片提交或验收前发布。

### 历史：a1dfa42 及此前候选

本轮正在定位 a1dfa42 反向免密终态缺失，尚未宣称修复：原超时分支只输出 deadline，未给出已收集的尝试错误，且 Queue.Updates 是 best-effort、Snapshot 才是权威状态。准备在该原分支一次记录已有脱敏事件/尝试、队列状态与 Revision、标准 Go profile 的函数名/行号；不含参数值/原始栈，不改变产品、期限、成功条件或网络规则。先判定任务未结束还是终态未送达，再决定修正。H 输入工具冻结未验。[固定角色记录的限定监护结论](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5900492963)保留，不能外推成 A–P 已验收。

**最新已测 a1dfa42713a7413c26b5f78878f1bcb22174294d，仍不具备合并/发布条件。** [主门禁 36639594738](https://github.com/lovitus/dragfm-gui/actions/runs/36639594738) 的 test/source 通过，Go/race 无失败、audit0；native 普通启动在最初系统点击前被输入驱动 `native acceptance application is unavailable` 阻断。Swift AX 此前已看到 create 页，但 Python AppKit 查不到应用；未输入 hint 或创建库，因此不能借此次结果宣称之前 hint 不匹配已修复。H 和旧 native 后续未执行，没有新可交付原生产物，不改断言/重复输入/放宽时限。

[SSH 36639594722](https://github.com/lovitus/dragfm-gui/actions/runs/36639594722) 的 core/protected 通过、transports 失败，34 根中 33 通过。原 peer-writer 四格通过：系统 rsync 停止后监护持续存在、父关系不变，原计划 SSH kill 随后才执行，partial 保留、源哈希、目标未发布及独立浏览等原断言全部成立。与 96a1b0e 同一原场景的实际失败构成监护修正的行为前后对照；不把它扩大成旧 exit20 信号来源已确诊或所有传输已通过。工件 merge 父提交与 a1dfa42 源码树已核验。

唯一 SSH 新失败为反向普通用户系统 rsync 免密队列：`scp=false/pull=true/system=true/root=false` 等 terminal 事件到 180 秒期限，仅见 context deadline exceeded，尚无下层原因。下一步优先定位真实传输终态缺失，不先扩输入工具整改，不归咎网络、不延长超时或重跑销账。H/P/A–P、安全警示及未合并/发布状态不变，固定角色已收到这份实际证据摘要。

**最新 96a1b0e 仍未通过。** [主门禁 36637208797](https://github.com/lovitus/dragfm-gui/actions/runs/36637208797)的 test/source 成功，Go/race 无失败、audit0；普通创建后加密文件头的 hint 已与计划输入不符，文件权限合格且没有明文主密码，因此不能再解释成单纯 AX 识别问题，也不能称已确诊选错库或产品持久化错误。H 及旧 native 后续未执行，保持未验。

[SSH 36637208835](https://github.com/lovitus/dragfm-gui/actions/runs/36637208835)仍 33 根通过/1 失败。此次真实 trace 显示系统 rsync 被 observer 暂停后，Bash 监护以 147 退出、子进程仍活并被重新托管，而计划的 SSH kill 尚未执行；原断言因无法再沿子进程找到 SSH owner 失败。此为实际监护停止/终止混淆的证据，不是旧 exit20 信号来源已证实，也不把失败归为纯夹具。旧诊断 TypeError 已越过，其他三个 peer 子例通过。证据来源 merge 的父提交与 tree 已核验，完整原始日志仅私有保存。

准备中的最小产品修正：监护和 trap 的 wait 使用 `-f`，且在启动任何 writer 前验证 Bash 支持；不支持时明确失败，不留下新子进程。原停止/清理范围、ptrace/PID选择、期限和断言不变。H 只补输入公开 hint、尚未输入任何密码时的窗口证据，不重打文本或放宽比较。以上尚未验证，未解除安全警示；固定角色正在根据真实诊断核对边界。

固定角色随后完成针对性复核：上述真实事件与固定源码已足以定位停止/终止混淆，不需要再猜改夹具；认可在实际监护 shell 启动前检测、两处用 `builtin wait -f` 的最小方向。`-f` 不等于整组死亡或取消已确认，原期限、trap、退出未知和租约门禁不变。该结论采用实现会话提供的事件摘要，角色没有独立读取本轮工件，也未代为批准 H/P 或发布。

**ceb5c7f 本轮结果仍未通过。** [主门禁 36635355610](https://github.com/lovitus/dragfm-gui/actions/runs/36635355610) 的前后端/Go/race/vet 通过、audit total=0；普通无参数 macOS 成品已实际创建保险库并进入工作区，手动锁定返回表单后的 hint/字段断言失败，现有观测还不能定位到产品或 AX。后续 H 场景和旧原生 exercise/restore/changed-key 未跑，不沿用旧 SHA 的通过结果。

[SSH 36635355707](https://github.com/lovitus/dragfm-gui/actions/runs/36635355707) 的 34 根中 33 通过；peer-writer 四个子例因本次新诊断 event 的 name 参数与进程 name 字段重名而在 ptrace 前报 TypeError。这不是旧 exit20 复现，也没有信号来源证据。[固定角色静态复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5899711187)独立指出这一 P2，未读取本轮 CI。下一修正仅消除确定的字段冲突、澄清诊断时序，H 补已知状态/数量/布尔证据，不放宽断言或修改产品行为。A–P 不销账，没有合并或发布。

ceb5c7f 的实现范围（实际未通过边界见上）：在既有 hosted native 流程内增加普通无参数成品验收，无测试保险库路径、DOM 注入或替换 HOME。系统 AX 仅观察，沿用真实 Quartz 键鼠，计划覆盖创建、错误密码、手动锁定、重启、程序旁/标准系统路径回退与优先级。仅操作一次性 runner；既有配置或缺系统授权即停止，不修改 TCC。此为已有功能补验，不伪造旧版缺陷红，不提前将 H 勾选完成。

同批为原 peer-writer 失败补有限信号/进程关系记录，并避免消费同一终态两次遮盖原错。产品监护脚本 job control + 普通 wait 存在“状态变化不等于终止”的静态疑点，尚未确诊它导致旧 exit 20。诊断不改变原 wait/信号、期限、数据量或断言，不重跑冲绿。这里的“注入前”仅指计划的 SSH SIGKILL 前：此前 ptrace/SIGSTOP 已影响被观察进程，不能断言它自行退出。本次若不复现，旧问题仍未解决。[GNU Bash wait 语义](https://www.gnu.org/s/bash/manual/html_node/Job-Control-Builtins.html)、[Linux ptrace 信号事件](https://man7.org/linux/man-pages/man2/ptrace.2.html)为设计参考，不是运行证据。

已完成的临时红绿入口从新候选中移除；其 f153603 源码与已通过工件保留，永久回归/main/SSH 门禁不变。

最新已测候选为 **f153603c1851a51218d369b3ac75965bfed7eae4**，整批仍未验收。强快照逐项身份与旧高权源视图保留已取得[4 条旧红/新绿和 1 正例双通过](https://github.com/lovitus/dragfm-gui/actions/runs/36631196144)。[主门禁通过](https://github.com/lovitus/dragfm-gui/actions/runs/36631196256)：取消复制回归在 unit/race 恢复，macOS arm64 成品原生 exercise 22 项、restore 4 项、changed-key 4 项及独立文件系统 8 项通过。产物哈希与 native 记录三方匹配为 `6a590e4933c139f65ebe03a8cf66a383054018b04e3127308bbfb99e11667eb0`，仅下载核对，未部署。

[SSH 全量仍失败](https://github.com/lovitus/dragfm-gui/actions/runs/36631195996)，34 根测试中 33 通过，唯一失败是旧 peer-writer system/rsync 在故障注入前退出（exit_code=20），随后重复 wait 的 ECHILD；未建立 group-stop 前提，不算产品保源行为证据。不反复重跑冲绿、不放宽断言。工件 merge `38bdaa80641faaf64f652ad2f367bb1ded5c50bc` 的父提交与 f153603 源码树已核对。原始日志仅私有保存。

[固定角色 f153603 集中复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5899040876)确认指定子项 P1/源视图 P2 修正接入实际链，本轮限定增量无新增 P1/P2；该角色没有读取本轮 hosted 工件，静态审查不冒充整个 PR 批准。H 普通无参数保险库成品入口、P 完整打包证据及 A–P 全范围仍待完成。下一步补 H 的真实入口，而不是用 smoke 的强制 fixture vault 路径当成便携配置证据。安全警示保留，未合并或发布。

2be964d 已取得[红绿 success](https://github.com/lovitus/dragfm-gui/actions/runs/36628593248)：7 条旧红/新绿（目标父链接、1777/0555/0711、三个最终同步失败）及 2 正例双通过；[SSH success](https://github.com/lovitus/dragfm-gui/actions/runs/36628593175) 的实际三组工件共 33 根通过，无失败。工件 merge d5172efe 的父提交与源码树已核对。[主门禁 failure](https://github.com/lovitus/dragfm-gui/actions/runs/36628593394)：取消复制测试包装器隐藏身份能力，尚未进入取消阶段；macOS 构建跳过，没有当前成品验收。工作树仅透传该能力，不改取消与清理断言。npm audit total=0。旧 peer-writer 的提前退出不因一次成功销账。

[原角色集中复审代录](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5898774116)确认原根绑定与必要权限实现有改善，但新增上述目标子项 P1 和旧高权源视图 P2。这是待行为复现的静态发现，不是数据损失报告。首轮与本轮已验证对照保留，新的临时入口不重复计算历史红绿。

以下为既有风险及历史证据；整批 A–P 未验收，当前候选仍勿移动重要文件。H 普通无参数保险库成品证据和 P 完整发布打包证据尚缺，未部署、合并或发布。

[c077881 集中复核](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5896808812)新增 R8–R10 三条 P1 静态删源风险：混合 Local/SSH 入口实际指向同一目录、最终源 inode 查询失败后被跳过，以及 SFTP 复制完成后缺少最终持久化确认。尚未运行对应行为反例，不宣称已经发生数据损失；在完整修复和验证前，请勿使用该候选移动重要文件。已通过的原生/SSH 场景仍有效，但不能代表这些边界安全。

当前完整安全候选包含 R8 的确认后独占未发布探针、R9 的身份丢失保源门禁、R10 跨普通 SFTP/helper/SystemFiles/本机 sudo 的最终同步，以及下述 P 打包/依赖修正。加速传输的旧独立删源分支也统一接回 FinishMove；初始/最终高权快照保持相同文件视图和时间精度。探针不在预览写入、不 Commit、不新增连接/权限；清理先校验目录/文件身份和随机内容，不能确认就停止保源。它不是未来挂载变化或非一致缓存的隔离保证；namespace/ctime 副作用不可撤销，观察到并发变化时不强行恢复 mtime。

真实 SSH/SFTP/文件系统回归覆盖队列自复制与源内目标、真实 stat 失败、同步能力缺失/文件和父目录同步失败/正常移动、加速完成保源，并补独立两台 SSH 相同绝对路径移动正例。临时 hosted 红绿入口对 c077881 和候选运行相同夹具，要求 11 条行为反例旧红新绿和 2 条正常正例双通过，不接受编译/前置失败冒充红。目前均未运行，没有新的 PASS；高权完整 E2E 仍待本候选门禁。H 的普通无参数便携保险库选择和 P 的完整发布打包证据仍缺失，与已有显式夹具保险库及原生交互证据分开。A–P 未验收，未合并、部署或发布。

## 本批实现范围

### 最新候选 63b4790 的实际结果（仍不通过）

- [红绿 36623800510](https://github.com/lovitus/dragfm-gui/actions/runs/36623800510) 整体失败。实际事件证明 8 条旧行为失败/新通过：两条 Local/SSH 队列重叠、真实 SSH 身份失败、加速完成同步失败保源、三条身份丢失状态和已知 inode 不能变零；身份正常正例两侧通过。另四条目录 SFTP 同步场景在旧/新两侧均被夹具 `WithServerWorkingDirectory` 改写相对链接阻断，未到达预期同步边界，不能算红绿。已保留失败，工作树修正夹具与生产 helper 一致的路径语义，原链接断言未改，尚未重跑。
- [主门禁 36623800411](https://github.com/lovitus/dragfm-gui/actions/runs/36623800411) test 失败、macOS 构建跳过。前端 14 文件/40 用例通过，npm audit total=0；Go 另两条旧测试包装器隐藏了真实文件标识能力，工作树仅透传该能力及最终同步，不改原安全预期。未取得本候选 race/native 或成品验证。
- [SSH 36623800488](https://github.com/lovitus/dragfm-gui/actions/runs/36623800488) protected/transports 成功、core 失败，共 29 根 pass、3 根 fail。独立两台 SSH 相同绝对路径移动通过；三个失败为 chroot 身份查询、system/rsync 故障观察进程消失、POSIX stopped-directory 未建立实际暂停。既有进程观察不稳定不能靠重跑冲绿，也不算产品安全通过。三组工件 merge `b21cd1b92627bdfc472c47fd0e440896b22bc11c` 的父提交已核对为原基线与 63b4790，源码树一致。
- [固定角色复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5897930808) 认可已收敛的源码方向，新增目标在校验后重绑定的 P1，以及共享父目录/只读空目录、未修改祖先同步的权限 P2。工作树补最终目标 inode 绑定复查、受控链接切换回归，并停止恢复仅作载体的未复制父目录 mtime，均未运行；最终只读目录与同步范围仍需统一处理。H/P 完整证据仍未验，A–P 不关闭。

上述是冻结 63b4790 的证据；后续工作树不是其已测内容。无部署、合并、发布；原始日志仅存私有证据目录。

- 双栏路径、拖放目录/空白、快捷键、模态焦点、主题和第三栏；每栏持久交互 Shell，加载真实用户 profile、提示符与 cwd 帧同步。
- 单个无空行 Markdown 配置、稳定身份、删除字段生效、版本冲突保护、队列端点/凭据快照、池测试/停用/排序和主机对成功缓存。
- Running/Pending/History、取消和重启历史；运行中的命令输出持续脱敏，真实策略/方向/阶段与未知进度区别显示。
- 保守同机判定、两端分别批准的普通/root/sudo 访问，实际发起用户免密与显式 root 私钥隔离；直接、SOCKS、跳板、官方 Hans、最终有界内存中转。
- helper 不可用时的系统 SCP/rsync/ncat 和高权 SFTP/POSIX 文件访问；精确路径、完整源快照/SHA 门禁、受限 partial、退出不明停止重试、工作区租约与加密保险库恢复登记。
- GitHub-hosted 真实 OpenSSH/防火墙/sudo/代理/Hans 用例；macOS arm64 成品的真实系统键鼠入口。Hans 沿用官方 v1.7.0 二进制，没有复刻。

## 设计失败方式与保护

同机误判、配置串号/旧凭据、正在编辑时注入 cd、菜单截获 Backspace、端点权限混用、路由探测扩大、输出泄密、半成品覆盖、源变化后误删、远端进程尚未退出就删暂存，均属于本批需要验证的失败方式。风险确认按操作和端点保留；跳过一种方法不等于取消整个任务。未知退出、无法证明资源归属或清理失败不伪装成功。

## 验证状态（候选送审时）

本次候选将 R1–R7、普通 SSH 关闭错误分类、POSIX 路径分类、SCP 目录权限和门禁夹具修正并入同一个交付提交。测试/构建只在 GitHub-hosted 执行；历史 SHA 的证据不代表本次候选已通过。最新源码以 PR head SHA 为准。

后续候选只修正已定位的 peer-writer 暂停确认和 hosted 原生发键/事件诊断，保留原产品断言。已完成的临时对照入口移出自动 PR 工作流（源码在 c1def98）；常驻前端/Go/race/SSH/macOS 原生门禁不变。结果以下方具体 SHA 为准，不提前宣布通过。

### 最新已测候选：`c077881f77214291068a5d9f264ca72c91828319`

- [主门禁36613262559](https://github.com/lovitus/dragfm-gui/actions/runs/36613262559)与[SSH36613262490](https://github.com/lovitus/dragfm-gui/actions/runs/36613262490)均正式success。下载工件核验：前端14文件40用例、Go全量/race/重复sudo无fail；真实SSH三个分组31根pass，无fail。SSH源码merge `b2bdb2200b206b3539f8fd9a83fbfc09d30d0b3e`父提交匹配基线+c077881。旧peer-writer提前退出记录继续保留，本次未触发不等于已修稳定性。
- macOS arm64实际Wails成品：exercise的22项主流程、restore、changed-key及独立文件系统8项通过。覆盖路径局部编辑、登录环境/完整提示符、三主题、目录/空白拖放、Running/Pending/History、Pending和Running取消、覆盖移动的SHA门禁、目录合并、无选中Backspace、d/h、SSH PTY编辑/cwd、真实sudo保护目录下载与拒绝提权保留源、新进程恢复历史但不恢复Pending、主机指纹变化阻断。配置编辑部分仍是DOM输入，不扩大为全部OS输入或人工试用。
- 新取消证据：Pending可视45px、内容70px；滚动前按钮hit=false，真实OS滚轮令scrollTop从0到25后hit=true；同job点击前仍pending，可信pointerdown/up/click确实命中，最终cancelled。产品取消逻辑未改，原cancelled断言保留。解决旧验收脚本未检查滚动裁切的缺口，不猜测旧点击的具体落点。
- 原生History/终端截图已目视核验；本机仅下载未执行的单文件，与产物校验清单、native记录一致：`88c5d89fefc697a0382804840484651f567d15df134f12331cb20f3a46a46210`。首次native下载TLS超时，按同artifact ID重试成功，不归责代码且未重跑workflow。
- 当前进入Issue #3 A–P集中复核；尚未整体验收、合并、部署或发布。绿色workflow不能替代未覆盖需求。此后工作树仅更新结果文档，产品源码保持该已测SHA。
- 交付自查修正已准备、尚未提交/验证：`package-release.py`分别读取core/protected/transports三分组，逐组检查revision、fail、包完成及关键根用例，归档保留原目录；缺组/混SHA/缺包终态均拒绝。当前已测npm审计仍有一项jsdom间接dev依赖undici 7.29.0的moderate告警；工作树按[上游安全公告](https://github.com/nodejs/undici/security/advisories/GHSA-3wwx-pv8p-q78v)仅更新锁文件为兼容修复7.29.1，官方integrity已核对，保留发布零告警门禁。未运行新审计，不把开发依赖告警等同于GUI运行时漏洞。仍只优先macOS arm64，未启动多平台或发布。

### 上一已测候选：`2aa3af486a1888a091f001988eca5012df5a6ce4`

- [主门禁36610213420](https://github.com/lovitus/dragfm-gui/actions/runs/36610213420)：test/source成功，native报告明确通过路径局部编辑/Backspace/Enter导航、真实profile函数/完整提示符、三主题、目录拖放与Running/Pending同时显示；截图已目视核验。随后取消第二条排队命令失败，该命令最终succeeded，后续移动、合并、取消和重启链未验，不是整体GUI通过。
- [SSH36610213422](https://github.com/lovitus/dragfm-gui/actions/runs/36610213422)：初次状态观察超时，随后同一run确认success、工件31根pass，源码父提交匹配。没有重启工作流；旧故障夹具提前退出记录保留，不声称稳定性已修。
- 新取消失败尚未确诊。原会话只读复核确认旧脚本缺少滚动裁切/实际点击命中门禁，未确认误点History或后台取消故障。完整候选补充真实滚轮显露按钮、点击前同job仍Pending检查、重新定位与命中测试，以及trusted pointerdown/up/click派发路径确认；报告保留有界的滚动几何/命中与夹具job状态。仍严格要求任务cancelled，不以RPC或DOM代点，不增大窗口/改产品取消逻辑；尚无本修正运行结果。A–P不关闭。

### 上一已测候选：`85985131a064e4fe93b274f3069a086f64aa3fe1`

- [主门禁36608315400](https://github.com/lovitus/dragfm-gui/actions/runs/36608315400)：test/source成功，native越过原路径编辑与导航、profile函数/完整提示符检查，并生成三主题截图；随后首次拖动在发送适配器报`button argument not in ('left', 'middle', 'right')`，不是拖放功能通过。parent提前抛bridge错误，没有保留已生成的renderer报告；本轮以顺序流程和实际截图证明到达点，不称整体验收。
- [SSH36608315370](https://github.com/lovitus/dragfm-gui/actions/runs/36608315370)：工件核验31根pass，源码merge父提交匹配。旧system/rsync故障注入提前退出未修，本次未触发不能抹掉旧失败。
- 工作树针对新错误明确down/drag/up/异常释放使用同一个left按钮，仍由OS执行真实拖动、不额外补事件。上游`dragTo`默认PRIMARY没有在macOS后端被接受；不改产品拖放或原断言。bridge报错前保留已有renderer阶段报告，失败继续失败。尚未运行此后续修正，A–P未验收。

### 8598513的发送器修正范围

原会话已复核flags证据，支持仅修正hosted普通文字动作的事件语义，不继续猜改产品路径框。现在仅在既有`text`动作投递前清除Fn/NumericPad两位并读回检查，真实Shift/Cmd/Ctrl/Alt保留，快捷键/箭头`keys`动作不改；原PyAutoGUI按下/释放序列、节奏和原OS路径编辑/导航断言保持。没有更换事件源/时间戳/投递位置，没有粘贴/DOM代填/补字。修正数量可保留，原始按键不再记录。

已移除完成取证的临时AppKit监听、Go观察控制分支和JS字符trace，历史cd3042a及私有证据保留；不把临时工具完善变成新主任务。SSH仅补故障夹具提前退出的内核状态/退出码，原安全断言不变。没有本地运行、新增测试或新的PASS；既有旧失败和新候选后续原E2E结果按SHA分别记录。

### 最近已测候选：`cd3042a762b2d1ef2993d12e710dd78a1422a81b`（只观察）

输入实现按两次失败规则停改。只给既有隔离原生验收的 `target` 动作增加发送端CGEventPost、AppKit和DOM关联记录；不补字、不改输入策略/成功条件/等待期限，普通启动不启用。[Apple应用内事件观察](https://developer.apple.com/library/archive/documentation/Cocoa/Conceptual/EventOverview/MonitoringEvents/MonitoringEvents.html)原事件原样放行仍有观察开销，不能称实际间隔零影响。

- [主门禁36605428342](https://github.com/lovitus/dragfm-gui/actions/runs/36605428342)：test/source成功，macOS native仍为target→targt。发送端确实post了六个字符全部12次down/up，但普通字母投递前flags均为`0x20a00000`；AppKit收到的flags均为`0x00a00000`，包含Fn与NumericPad，characters正常。e已到达应用，并非发送序列缺少e；DOM四常用修饰键false没有覆盖Fn。e有DOM down/keypress/up，无beforeinput/input。故障优先收窄到验收发键构造，尚未证明修正后的整条GUI流程可用。
- [Apple列出的Fn-E快捷键](https://support.apple.com/en-ie/102650)是字符查看器；此路径与现象相符，但缺少该动作调用证据，仍是推断，不宣称已证实是系统弹窗吞键，不解释未文档化flags。
- [SSH36605428350](https://github.com/lovitus/dragfm-gui/actions/runs/36605428350)：30根测试pass、1fail。仍是system/rsync的writer在group-stop前退出，未建立故障注入条件。SSH merge父提交已核验匹配本候选；不以历史绿掩盖此失败。
- 原会话静态审查指出临时观察的范围/未知键码/异常收尾边界仍有缺口。本次观察正常停止，不等于长期完备；将保留私有证据后移除临时观察，不继续扩大诊断工具。未知输入、原始日志不发布。flags的新事实已同步原会话，尚未收到该项结论。A–P未验收，无部署、合并、发布或验收成品。

### 最近已测候选：`7768c25b5934502ceafa75b5bb15e507b49d98bf`

- [主门禁 36602175840](https://github.com/lovitus/dragfm-gui/actions/runs/36602175840) test/source 成功，macOS native 仍在原路径替换断言失败。正式路径框关闭纠错/预测等辅助后，target 变为 targt，仍缺 e；e 的 trusted down/keypress/up 未被 DOM 取消且焦点保持，没有文本插入。末尾 t 正常插入不代表整条流程通过，不能把结果归因到某个单一属性。未改发送器、成功条件或超时。
- [SSH 36602175887](https://github.com/lovitus/dragfm-gui/actions/runs/36602175887) success，三组工件核验31根测试全部通过。但90f561c的 system/rsync 故障夹具提前退出没有修改，本轮绿不能证明其稳定性问题已修，旧失败保留。
- 第一次输入节奏修正与第二次字段策略均未恢复原路径操作，按用户“两次修正仍失败”规则停止继续修改输入实现。建议下一步只观察发送→目标 AppKit→原生文本插入边界；不继续堆属性、增等待或补字。原稳定会话认可字段策略可做对照，但未批准根因或整体验收，已同步本次失败。A–P 未验收；未部署、合并、发布，无验收成品。

### 上一候选：`90f561c7a80ac3620f20630befb7afdc4e48c293`

- [主门禁 36599837027](https://github.com/lovitus/dragfm-gui/actions/runs/36599837027) test/source 成功，macOS native 仍失败。完整事件记录确认 e/t 未被 DOM 取消、焦点与节点连接保持、四修饰键关闭；t 的 keyCode=229，无 composition 或文本插入。根据 [WebKit 事件处理](https://github.com/WebKit/WebKit/blob/main/Source/WebCore/page/EventHandler.cpp) 可收窄到输入法处理路径，但未确诊具体原因。路径替换仍未验收，后续拖放/终端/重启未跑到。
- [SSH 36599836703](https://github.com/lovitus/dragfm-gui/actions/runs/36599836703) protected/transports 成功，core 失败。工件核验 30 个根测试 pass、1 fail；peer-writer system/rsync 的真实工具在 group-stop 前退出，夹具没有建立故障条件。原产品安全断言未改，不能用上一轮绿或反复重跑把这一失败抹掉。
- 失败日志各取一次，原始工件只留私有目录。工作树准备路径框关闭自动纠错/预测补全等自然语言辅助，保留选区和 IME；依据 [WebKit 逐字段 writing suggestions 文档](https://webkit.org/blog/15443/news-from-wwdc24-webkit-in-safari-18-beta/#html)，不是已证实的修复。尚未运行，不更改发送速度、不补字、不改原断言。原稳定会话继续只读复核。A–P 不关闭。

### 上一候选：`c695d21fd1290c4043ae9db3aee4d7d01daf2404`

- [SSH 36597179404](https://github.com/lovitus/dragfm-gui/actions/runs/36597179404) success。三组工件及实际事件已核验：core 24、transports 5、protected 2 个根测试全部通过，包括 peer-writer helper/system × SCP/rsync 四格。工件 PR merge `11095349de8f7d37e04fe9bb91ca74e768a29c84` 的父提交已核对为原基线与本候选。不是所有原需求的自动验收。
- [主门禁 36597179468](https://github.com/lovitus/dragfm-gui/actions/runs/36597179468) test/source 成功，macOS native 失败。路径 OS 输入仍停在 target→targ；t/a/r/g 有真实文本插入，最后 e/t 只有 trusted 按下/释放，无 beforeinput/input。放慢发键未解决，不能断言是产品、IME 或发送源；后续界面链未跑到。
- 后续工作树只补路径输入的有界取消/合成/焦点诊断，未改产品或验收预期。原稳定会话正在独立只读定位；没有本地测试/用户 GUI 操作。A–P 仍未验收，未部署、合并、发布。

### 上一候选：`c1def980ce567d4b8905cd2fceda041a6b955d43`

- [红绿 36594017619](https://github.com/lovitus/dragfm-gui/actions/runs/36594017619) success，工件及实际事件已核验。三个 rsync 反向目录场景、六个系统池旧反例均旧红/新绿；三个本来可用的正例两侧都通过。不是完整旧整仓复测，不将正常正例计作旧红。
- [SSH 36594017566](https://github.com/lovitus/dragfm-gui/actions/runs/36594017566) 仅 core 失败。protected/transports 全部通过，包括实际用户免密 12 场景、48 格方法矩阵、Hans、六个 SOCKS 后备及三个跳板 cache 恢复。core 其余 23 个根测试通过；peer-writer helper/scp 与 system/rsync 停在 `writer stop not observed`，另外两格通过。新 stderr 证明夹具在异步 SIGSTOP 生效前读状态的竞争，不把它当产品安全通过。
- [主门禁 36594017643](https://github.com/lovitus/dragfm-gui/actions/runs/36594017643) test/source 通过，macOS 原生验收失败。hosted 显示已成功调整为受支持的 1280×960 并恢复；实际系统输入进入路径局部编辑，但 target 最终为 targ。输入丢失层级仍待 trusted 事件证据，后续拖放/终端/主题/重启等未跑到；不能交付为 GUI 已验收。
- [原会话集中复审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5893753215) 固定 b630ad2，R6/R7 具体反例与 SCP 模式修复已复核，无新范围内阻断；不是整个 PR 批准，不涵盖当前 c1def98 的新增行为。
- 下一批仅在工作树准备 peer-writer 的阻塞内核暂停确认，以及 native 发键节奏/路径框事件诊断；没有新的测试/提交，未声称产品输入已修。整个 A–P 仍未验收，未部署、合并、发布。

本候选修正：

- 目录传输按原始源清单给 rsync 目录源添加尾斜杠，避免随机暂存根下多出源 basename；普通文件/链接、精确引用和移动校验保持。
- 系统 TCP 探测将 Go 空路由的 JSON null 正确解析为无 SSH hops，避免在 socket 前抛内部错误后被误判为网络不通；同时保留实际探测的有界 stderr 和错误链。三次采样、中位排序、候选范围和账户隔离不变。
- 原生 hosted fixture 仅选择 CoreGraphics 返回的受支持显示模式，读回逻辑尺寸并恢复；产品最小窗口和裁切拒绝不变，无支持模式明确记环境 blocker，不在用户设备运行。
- 临时红绿切换为原 rsync pull 三场景及真实 SOCKS/跳板恢复，对照仅撤回相应产品行为；前十八组保留各自证据，不重复运行。新增失败诊断不修改原测试预期，peer-writer 只补上安全 join 后的原始错误，尚未确认其故障原因。
- b630ad2 的 R6/R7/SCP 已由原稳定会话集中复审；不把后续增量混入其评审目标。没有本地测试、部署、合并或发布，A–P 未关闭。

### 上一候选：`b630ad203818e6eb07351d98c3c79489daf5e759`

- [补充红绿 36589938706](https://github.com/lovitus/dragfm-gui/actions/runs/36589938706) success，三份结果工件已核验：R7 五个实际 SSH 断流/关闭竞争、R6 启动失败回退边界、SCP 原目录权限回归均旧红/新绿；明确拒绝且清理完成的安全回退在对照两侧通过。不是未改旧整仓对照，不扩大为全部需求验收。
- [主门禁 36589938889](https://github.com/lovitus/dragfm-gui/actions/runs/36589938889) failure。test/source job 成功，前端/Go/race/vet 通过；macOS 编译后原生输入被裁切门禁拒绝：hosted 显示区域 1024×768 装不下产品 1080×680 窗口及边距。没有系统键鼠/截图通过证据，不缩小最低尺寸、不移除门禁；待核对 runner 支持的显示模式。test-results 工件下载外部 EOF 与该失败分开记录；native 工件已取得。
- [真实 SSH 36589938732](https://github.com/lovitus/dragfm-gui/actions/runs/36589938732) failure，但所有分组已跑完、无全局预算耗尽。双保护 14/14、显式 root key、48 格方法矩阵、Hans、真实队列/历史/PTY、系统 partial journal/重连恢复通过。剩余三个 rsync pull、代理 fallback/事件/双无 agent 缓存、两个 rsync peer-writer 夹具前提失败；不算全 SSH 验收通过。

下载结果进一步确认 rsync 是目标中多嵌套一层源目录，并非文件消失；下一批依据目录清单使用 rsync 的内容拷贝参数。该后续工作树修改不属于本候选的已测内容。未部署、未合并、未发布，A–P 保持未勾选。

### 本次修正及验证范围

- helper 启动的不可重试错误不再被 system fallback 吞掉；已有独立子进程持锁导致清理失败时必须保留源/安装/journal，不提交目标。明确拒绝且清理已确认仍可后备。真实队列和 SSH 夹具同时覆盖这两个分支。
- 控制/SFTP 初始化、filesMu 与 Close 竞争、已发 channel-close 但读端无应答，均以关闭任务独有 transport 解阻；不关闭浏览连接，也不将连接关闭当远端退出证明。五种真实 SSH 协议输入后断流的回归已准备。
- SCP 目录结束后恢复 -p 权限/mtime，避免 umask 造成空目录模式错误。原反向目录主流程取得旧红/新绿。rsync 根目录放置错误已定位，后续修正尚未验证。
- 生命周期测试的主动 pipe 关闭允许原始 ErrClosedPipe，保留实际清理断言；高权方法矩阵改用显式 fixture root key，保持账户隔离和原成功要求。这两项是测试合同/夹具修正，不计产品红绿。
- 真实 SSH 全量按已有用例分三组独立 hosted fixture，仍用原超时和全部断言，避免前面用例累计耗尽 20 分钟使后面从未执行。新增用例默认归 core；没有删测或放宽门禁。

临时红绿只证明上述新 R6/R7/SCP 行为，前十五组保留原 SHA 证据。完整 A–P、系统输入和正常全部传输策略仍待验收。

### 上一候选：`999e4556f80a709d2d35f2608fa3b3a78ad6c8b4`

- [补充红绿 36582593356](https://github.com/lovitus/dragfm-gui/actions/runs/36582593356) success。已下载并核对 result.json：POSIX 文件读取关闭、同 sshd 不同 chroot、peer direct 路由三组均实际旧红/新绿；保留当前 harness 撤回对应产品行为，不是未修改旧整仓复测。
- [主门禁 36582593473](https://github.com/lovitus/dragfm-gui/actions/runs/36582593473) failure。前端通过，Go helper 生命周期测试主动断开控制 pipe 后收到关闭错误；macOS 构建跳过，因此没有本次窗口坐标或系统输入通过证据。未直接放宽该测试，仍需判定错误合同。
- [真实 SSH 36582593152](https://github.com/lovitus/dragfm-gui/actions/runs/36582593152) failure。双保护目录 14/14 和 helper 控制失联三个场景通过，独立 SFTP 第二次清理修正成功。后续暴露反向原生目录传输、部分高权认证、代理 fallback/事件/缓存/回收问题。20 分钟整包预算耗尽时 system-journal/abort 才刚开始，不算已证明该用例卡死；后续用例未验。
- [5a0e7ea 集中评审](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5892311568) 认可 R1–R3 原反例的局部证据，新增 R6 启动错误未统一阻断后备、R7 高权/控制通道取消解阻缺口。上文是随后准备的新候选修正，不属于 999e455 的已验证内容；整批 A–P 不能关闭。

三个工作流均已按专用脚本确认终态。未部署、合并或发布。原始日志仅在私有证据目录；main 工件下载的外部 EOF 与产品失败分开记录。下一批先补全取消/错误传播主流程及真实反例，不以关闭保护、放宽认证或无证据加长超时代替修复。

### 前一候选：`5a0e7ea5cecac8ed78d0e1080013d2cc4cb9ab6d`

- [补充红绿 36577338798](https://github.com/lovitus/dragfm-gui/actions/runs/36577338798) 整体失败。五组实际旧红/新绿成立：SFTP 初始化取消、普通 SSH 关闭、多级 POSIX 创建、SCP 文件名空白、ncat sync 失败源保护。chroot 因夹具身份前提未满足不能算红；peer-route 的系统拉取通过，agent 推送被 rsync 立即 kill 造成的退出不明阻断。
- [主门禁 36577338699](https://github.com/lovitus/dragfm-gui/actions/runs/36577338699) 前端/Go/race/vet 通过；macOS arm64 编译完成，但原生 OS 输入门禁在窗口坐标映射处失败，没有交付成品，不用 DOM 通过替代系统键鼠。
- [真实 SSH 36577338696](https://github.com/lovitus/dragfm-gui/actions/runs/36577338696) 失败/20 分钟超时。双保护目录 14 场景中 12 个通过，包括此前 helper 复制/移动、部分 POSIX 复制/移动。剩余：反向 ncat 夹具缺 TCP 协议限定；POSIX 密码场景任务后浏览 Home EOF。Hans 两个控制丢失场景通过；独立 SFTP 测试清理仍阻塞，后续用例未执行。不能据此称完整 SSH 或 A–P 完成。

后续修正不属于 5a0e7ea 的已测结果：rsync 等正常 EOF 退出再有限强停；chroot machine-id/busybox 的实际读/执行权限、防火墙协议、独立 transport 清理；native 数字坐标诊断。独立 SFTP 清理已到第二次修正，若仍失败停止继续改该用例并报告用户，不无限调试。

进一步走读更正：POSIX password 的 326 行是 assertRemoteFile 调用，Home 在此前已经成功，不能称为已证实的浏览断连。实际源码缺陷是短文件 cat 正常退出后，sessionReader.Close 把 channel EOF 当成失败。后续候选仅在真实 Wait 成功时忽略预期关闭，保留非零/缺失退出并缓存 Close 结果；确定性回归等待真实 SSH channel-close 应答，覆盖正常文件和 cat 非零退出。临时红绿缩为这一新回归及未完成的 chroot/peer-route，其余已取证组不重复。原始队列仍须在完整 SSH 门禁重验。

### 前一候选结果：`ec4004d401dc02aba0c5bcc9da9c755e8891b17c`

- [补充红绿 36570052760](https://github.com/lovitus/dragfm-gui/actions/runs/36570052760) 成功：登录 profile、实时命令队列的旧行为失败且修正后通过。连同首次的五组，共七组证据，不代表所有新增回归都完成红绿。
- [主门禁 36570052720](https://github.com/lovitus/dragfm-gui/actions/runs/36570052720) 失败：编译和 profile 已恢复，但在途 SFTP 请求的取消夹具、Markdown 凭据标题与 stderr 交错的实时输出回归失败。主门禁 macOS 构建跳过。
- [真实 SSH 36570052739](https://github.com/lovitus/dragfm-gui/actions/runs/36570052739) 失败并在 20 分钟测试期限超时。完整错误链进一步定位：helper 普通 transport EOF 被误当不明清理，导致认证失败不能继续后备；POSIX 不存在的多级目录被误当无权限。这两项已有源码修正及实际 SSH 回归，尚待新结果。host-key 和 SFTP 测试清理死锁已修，暂停就绪改用内核事件；防火墙 status 4 仍待完整 stderr 证据，不能声称已修复。部分 PASS 不代替全路径；超时后的用例未执行。
- [既有平台工作流 36570052776](https://github.com/lovitus/dragfm-gui/actions/runs/36570052776) 自动触发并成功，仅证明六平台构建和 DOM smoke。没有据此发布、操作用户设备或宣称原生系统键鼠验收。
- [独立评审 R1–R5](https://github.com/lovitus/dragfm-gui/pull/4#issuecomment-5890605357) 针对 `16bb76b`，尚未通过。本次候选已纳入 SCP 文件名空白、SFTP 初始化取消、tar+ncat 落盘与源保护、同机账户/root 视图区分、远端直传不重放控制机跳板，及对应真实 SSH 回归。**尚无这些修正的新 hosted 结果，不标为验收通过。**

原始日志/产物下载只保存于工作区外 private 目录。旧 run 不重复轮询；按 `CURRENT_TASK.md` 取得本次完整候选的红绿和永久门禁证据后，回原评审会话集中复审。

### 首次 hosted 结果：`16bb76b84b57e0661bbbe18ae574ca87dd5b3224`

- [一次性红绿运行 36568395787](https://github.com/lovitus/dragfm-gui/actions/runs/36568395787) 已结束失败。配置插入 ID、删除元数据、root 私钥引用、保守同机身份、本机清理错误五组在旧版实际断言失败、候选通过。登录 profile 的候选未通过，命令队列候选因编译错误未运行；不计为绿。
- [主门禁 36568395710](https://github.com/lovitus/dragfm-gui/actions/runs/36568395710) 已结束失败。前端 14 文件/40 用例通过，但 Go 的未使用 import 和 PTY 测试失败使整批未通过；macOS 构建跳过，没有本批成品。
- [真实 SSH 36568395631](https://github.com/lovitus/dragfm-gui/actions/runs/36568395631) 已结束失败；同一组未使用 import 阻断测试包编译，不能据此评价传输可用性。
- 具体阻断：`transfer_attempts.go` 未使用 `path`、`transfer_attempts_test.go` 未使用 `strings`；Bash 测试错误假设 bracketed-paste 控制序列后总有 CRLF，虽然实际输出包含预期变量；zsh 环境遇到全局 compinit 的不安全目录询问。未通过项不作完成声明。原始证据保存在 private 目录，不复制私人日志/凭据到 PR。

下表为送审时入口清单，不替代上述真实结果；其余新增测试旧红、完整 SSH、原生输入及 A–P 主流程仍需证据。

首次失败后的候选修正保留同一个完整交付提交：删除上述无用 import，PTY probe 输出自己的换行边界并等待完整 OSC，隔离 zsh 用户配置使用发行版提供的 `skip_global_compinit`（不接受不安全目录，不改产品启动行为）。临时红绿入口仅重跑未完成的 profile/queue 两组；五组已有证据不重复，永久 build/SSH 门禁不变。修正后是否通过仍须实际 hosted 结果，不能从源码推断。

| 范围 | 状态与入口 |
|---|---|
| 格式和差异 | 仅 gofmt、git diff --check；不是产品验证 |
| 配置/登录/队列/身份/取消/SCP 局部回归 | 早期七组、5a0e7ea 五组、999e455 三组、b630ad2 三组有对应旧红新绿；不是 A–P 验收 |
| 后端/前端/race/vet | 7768c25 test job 通过；当前主 test 工件未下载，不冒充工件核验成功 |
| 真实 SSH 全策略与异常取消 | 7768c25 31项全绿；90f561c peer-writer system/rsync 夹具提前退出未修，不把重跑绿当稳定性修复 |
| macOS arm64 成品与系统输入 | 7768c25 编译及 hosted 显示准备完成；OS 路径输入第二候选仍失败，已停改此方向，未交付；没有操作用户本机 |
| 其余新增回归的旧红 | 未取证，尤其新 API 的工作区/系统原生退出、UID 策略和新原生输入 harness。必须保留 harness 再撤销相应产品行为，不能拿不能编译/缺 hook 当旧红 |
| A–P 需求逐项验收 | 全部保持未结案；任何局部 PASS 均不能自动代替完整主流程 |

合并前必须读取真实结果，区分代码、夹具和环境失败；缺 TCC/硬件/登录条件明确记未验，不退回 DOM 合成输入冒充系统键鼠。空间不足、网络篡改、before-pin 保存失败等边界仍需可核对证据。

## 协作和交付边界

原始会话独占实现，既有 `for dragfm-gui` 会话对同一 PR/SHA 集中独立评审；正式结论回到 Issue #3。用户已长期授权完整候选先推，再 hosted 验证旧红新绿，不再重复询问。最终保持一个完整交付提交，验收前不合并、不发布。

只先交付 macOS arm64；用户验证后再做新增多平台发布。旧 Linux Fyne 缺 libGL 的离线兼容问题不在本批修复范围。不得使用私有 runner、私人主机、真实保险库或真实凭据做测试。普通无标记 partial 的额外跨重启自动发现仅按 `postponed-tasks.md` 延期，不削减正常取消、合法登记资源回收和源安全要求。

本次按上述平台顺序关闭旧六平台矩阵的 PR/push 自动触发，保留手动入口；自动 macOS arm64、系统输入、前后端和真实 SSH 门禁不变。
