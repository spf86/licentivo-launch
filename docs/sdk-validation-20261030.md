# 当前 SDK 源码验证（2026-10-30）

当前 Launch SDK 的 Go、Python、JavaScript、C#、Java、Rust、Ruby、C、C++ 九种语言已在以下环境全部通过：

| 环境 | 验证内容 | 结果 |
| --- | --- | --- |
| macOS Intel x64 | 账号生命周期、身份兑换、签名授权、离线检查、退出及恢复、原生 C/C++ 适配器 | 九语言通过 |
| macOS Apple Silicon arm64 | 同上，使用原生 ARM 工具链 | 九语言通过 |
| Linux x64 测试机 | 同上，使用独立测试目录和签名模拟服务 | 九语言通过 |

Windows 补测的 Go 账户测试和 JavaScript 21 项测试通过；本次没有重跑 Windows 全部九语言发行包安装验收。

Mac 通过记录为私有 CI：
https://github.com/spf86/licentivo/actions/runs/40997725272

测试提交 `a78fe06a5ed1847e9630c6e720ead01eeea80756` 中的独立源码快照同时验证了 Licentivo 九语言的真实测试库激活、电脑名称/系统/CPU/内存上报及 API Key 权限。Launch 的测试范围为本地签名商户与授权服务模拟，不包含真实商户环境的外部验收。

本次修正了测试脚本的 Mac Ruby 动态库后缀和 Homebrew 查找参数，保留 Linux 无 pkg-config 时的标准参数回退；根模块补齐了 go.sum 的依赖校验。运行时代码和依赖版本没有因这些测试入口修复而升级。

本轮未合并主分支、部署线上或发布 SDK。使用带 device_info 的本地 SDK 修订前，需要先部署相应 Licentivo 服务端及数据库结构 51。现有公开包的版本号不能单独用来证明已经包含这些本地更新。
