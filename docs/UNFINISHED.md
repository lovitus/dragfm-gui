# 交付边界与后续事项

本文件替代最初迁移时“全部未验收”的旧清单。可执行验收映射见 [ACCEPTANCE.md](ACCEPTANCE.md)，实际发布结果以 release 的 `PROVENANCE.json`、两个测试证据包和 GitHub workflow 结果为准；源码中存在测试不等于该次测试已通过。

## 已纳入强制发布门禁

双栏真实操作、SSH 双任务 Running/Pending/History、失败与取消、重启恢复、CodeMirror 配置保存/遮罩、权限确认、受保护本机下载、指纹变化阻断、六平台原生窗口与打包后再次运行，均由可复现 workflow 检查。复杂策略包含 48 个 direct/SOCKS/SSH 方向/权限/方法用例、8 个官方 Hans 角色/方法用例、真实 TCP 阻断回退、缓存失效、取消延迟和清理。

旧描述“SOCKS/SSH/Hans 不支持 ncat 重放”已过时：当前通过认证加密载体重放 ncat，不把代理口令放进进程参数。旧描述“六平台全部延期”也已过时：当前发布矩阵要求六个平台真实启动，而不是仅交叉编译。

## 仍需外部发布者身份材料

- Apple Developer ID 签名与 notarization 没有配置；macOS 使用 ad-hoc 签名并验证完整性。
- Windows Authenticode 发布者签名没有配置。

未提供证书/账号身份，不能声称签名认证已完成。应用和 workflow 不关闭 Gatekeeper、SmartScreen 或其他系统安全设置。

## 测试不能替代的边界

任意用户 shell 插件、自定义提示符、多日运行、所有网络拓扑和旧版操作系统不可能由一组固定 CI 用例穷尽。发布候选表示所列自动化验收通过，不表示每个环境已获人工认可。Linux GUI 仍有系统图形依赖；远端主要完整支持 Linux。网络完全断开时无法保证立即清理远端文件；只处理本程序有合法所有权标记的临时资源，不通过扩大删除范围换取“零残留”口号。

## 保持不在本批次范围

原 TUI 不替换；旧 Fyne 离线麒麟 libGL/X11 问题仍按原决定保留；无桌面服务器 Web UI 未加入。没有新增后台监视或私有 runner。
