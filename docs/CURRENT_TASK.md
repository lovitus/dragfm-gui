# 当前任务游标

- 当前批次：迁移临时源码到私有 GitHub 仓库，创建需求/未完成清单 Draft PR。
- 基线：Wails 主线、保留 Fyne、vendor、Linux agent/官方 Hans 资源；此前无 Git 提交。
- 组织：main 空基线，开发分支一个完整迁移提交；不自动合并或发布。
- 排除：dist、vault、node_modules、缓存、frontend 生成物、凭据与私有日志；历史口令示例替换为 EXAMPLE 值。
- 中断恢复：外置磁盘失联导致新增文档丢失，恢复后重建并检查；不把失败暂存视为提交。
- 本次真实冒烟：未执行原生登录/远端传输，未启动 GUI；迁移不等于恢复完成。
- 历史产物：2026-08-20 macOS arm64 review，历史 SHA256 ac6ef13fa5eb4e4404af5d947771d32b990121faaab352ba260cd59c05a66873，非本次构建。
- 需求见 USER_REQUIREMENTS.md，缺口见 UNFINISHED.md；REQUIREMENTS.md 的 Implemented 只表示代码存在。
- 唯一下一步：实际成品入口真实 SSH 双任务传输与历史恢复闭环。后续测试/build/release 用 GitHub workflow，不用私有 runner。
