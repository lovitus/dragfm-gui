# 当前交付游标

源码迁移已扩展为 Wails 功能修复、深度安全复审、真实 SSH 策略验收与公开六平台候选发布。原始需求保留在 `USER_REQUIREMENTS.md`，自动化映射见 `ACCEPTANCE.md`，外部证书与环境边界见 `UNFINISHED.md`。

维护入口：`cmd/dragfm-wails`。旧 `cmd/dragfm-gui` 是保留的 Fyne 原型，不应作为新版验收入口。

提交不等于发布。最终结果是 `.github/workflows/release.yml` 完整成功、release 非 draft，并附带六个 OS/CPU 包、对应源码、`PROVENANCE.json`、`TEST_EVIDENCE.zip`、`PACKAGED_TEST_EVIDENCE.zip` 和 `SHA256SUMS`。任一 gate 失败就保留失败证据，不把该候选称为完成。

中断恢复时，先读取 PR 当前 head、该 SHA 的三个 workflow 和 release 状态；不得只根据本游标或历史成功徽章宣称新提交通过。不要启动用户设备、私有 runner、真实私人服务器或旧保险库。凭据夹具只存在于 hosted runner 临时目录。
