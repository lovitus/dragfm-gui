# 临时项目源码迁移与未完成需求基线

## 本 PR 的目的

把当前开发版纳入 Git 版本管理，保存用户沟通需求和已知验收缺口。此 PR 是 Draft，不代表生产可用、不代表全部需求完成，不自动合并或发布。

## 用户需求与范围变更

- 双独立本机/SSH 文件栏，路径文本框、ls -allt 类列表、目录级拖放、复制/移动/覆盖确认、Backspace/d/h。
- 每栏持久登录 Shell、用户环境、cwd 双向同步、每次命令刷新且提示符无残留。
- 全局单任务与 Pending、Running、事件输出、History，真实传输状态、取消、脱敏历史重启恢复。
- 严格无空行 Markdown 配置、物理行号、不软换行；FlySSH 多跳串作为一个主机，--keys 引用 vault 私钥；SOCKS 为 user:pass@ip:port。
- 直接正反向普通/提权传输 → SOCKS → 缓存 SSH 跳板 → 官方 Hans v1.7.0 → 控制机有界内存零落盘中转；优先 rsync。
- 跨机移动完整哈希和源快照校验后才删除；取消清理、凭据不进入参数/日志、主密码加密保险库和便携配置。
- 原 TUI 保留；旧 Fyne 与当前 Wails 明确区分。先验收 macOS arm64，之后再构建六平台。
- 离线麒麟旧版缺 libGL 的问题按用户要求暂不修；无桌面 Web UI 未授权。
- 后续测试/build/release 走 GitHub workflow，不用私有 runner。

完整沟通细节、脱敏格式示例、策略顺序和安全条件见 [USER_REQUIREMENTS.md](USER_REQUIREMENTS.md)。

## 未完成 / 未验收（保持未勾选）

- [ ] 实际成品入口真实 SSH 双任务：一笔运行、一笔排队、结束进入 History、重启恢复。
- [ ] 远端真实进度/步骤活性；当前动画不能证明传输推进。
- [ ] 终端提示符、登录环境、cwd 同步的连续实机验收。
- [ ] 拖放、无选中 Backspace、覆盖、权限失败与配置格式当前成品回归。
- [ ] 真正非 root sudo、所有方向与回退矩阵、跳板缓存失效、Hans 反转与取消。
- [ ] 代理/Hans 层 ncat 未完整重放：明确偏差，待安全实现或用户确认缩减。
- [ ] 原子提交、零落盘、源变更不删除、指纹变化阻断与脱敏整链验证。
- [ ] 用户验收后再扩展多平台原生烟测、发布和签名。

详见 [UNFINISHED.md](UNFINISHED.md)。旧完成台账已注明 Implemented 仅表示代码存在，不表示验收通过。

## 本批次变更与验证边界

- 一个源码迁移提交，main 仅空基线，用于保留完整 PR 审阅。
- 排除保险库、成品 dist、node_modules、缓存、frontend 生成资源与私有日志；口令示例改为 EXAMPLE 值。
- 保留 vendor、GPLv3、第三方许可证、官方 Hans 与 agent 嵌入资源。
- frontend 生成物不入 Git，因此 workflow 在 Go embed 编译前先构建前端。
- 只做迁移完整性与敏感文件检查；未重跑原生用户流程，历史 PASS 不当作本批次新证据，CI 结果以 GitHub 实际结果为准。

## 唯一下一步

按 [CURRENT_TASK.md](CURRENT_TASK.md) 从真实成品完成 SSH 双任务/历史闭环，先修阻断使用的问题，再验复杂策略，不用局部测试包装完成。
