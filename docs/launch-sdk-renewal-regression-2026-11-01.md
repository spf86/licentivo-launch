# Launch SDK 更新与长任务联合回归（2026-11-01）

九种语言通过 Launch Application 连接 177 上的真实 Launch、Licentivo 与 MySQL，均完成了超过默认预占期限的业务、结算及新进程重试。**920 指业务持续 920 秒，即 15 分 20 秒；它不是许可证期限，也不是扣量单位。** 默认预占为 900 秒，本轮故意跨过这一边界。

## SDK 更新情况

- 另一个 AI 已将官方 SDK 修复同步到 Launch。重新核对了 **132 个内置文件**，官方源码、内置文件及默认 `examples/vendor/sdk` 均与清单相符，没有再覆盖一遍相同代码。[逐文件校验](launch-sdk-renewal-20261101/source-audit.json)
- 内置修订为 `working-tree/n09-original-reservation-renewal-2026-11-01`。Launch SDK 仍为 **0.1.7 源码补丁构建**，内置官方 SDK 为 **0.11.7**；同版本号的旧安装包不能据此视为已修复。
- 在 177 重新构建九个业务小程序及 C/C++、Rust、Ruby 的原生代码，实际安装 Python wheel，并构建可通过 `find_package(LicentivoLaunch CONFIG REQUIRED)` 使用的 Linux 原生安装包。Windows 分发包也重新构建，安装检查单独记录。[安装产物重放结果](launch-sdk-renewal-20261101/installed-artifact-results.json)
- Licentivo 服务器已包含匹配修复，本轮无需再次更换服务二进制。最终 Licentivo 二进制 SHA-256 为 `547c9614befbc6d8f3d65af5f9201e1b1f62216624c6e811c4fdb490b929672e`，与开始时一致。

新分发包保存在本机 `.local/launch-sdk-renewal-20261101/packages-final`，其中包含 Linux 原生安装包。没有向公共包仓库发布。客户程序需要用新包重新安装或编译；网页刷新不能更新已经安装的 SDK。

## 实际业务结果

北京时间 **2026-11-01 06:57:47–07:14:20**。每种语言使用独立测试客户、许可证和缓存；先依次完成账号登录，再并发执行业务。服务时钟没有修改，预占 TTL 没有缩短或放大。

每个回调持续写入进度，最后真正生成三份各 100 行的 CSV 报告。任务预占 **3 单位**，报告文件的 SHA-256 会在重试后再次核对。进度 CSV 的行数不是计费数量。

| 语言 | 长任务实际业务秒数 | 回调次数 | 长任务扣量 | 新进程重试再执行/再扣量 |
|---|---:|---:|---:|---|
| Python | 920.030 | 1 | 3 | 0 / 0 |
| Go | 920.012 | 1 | 3 | 0 / 0 |
| JavaScript | 920.008 | 1 | 3 | 0 / 0 |
| Java | 920.029 | 1 | 3 | 0 / 0 |
| C# | 920.018 | 1 | 3 | 0 / 0 |
| Ruby | 920.016 | 1 | 3 | 0 / 0 |
| C | 920.016 | 1 | 3 | 0 / 0 |
| C++ | 920.020 | 1 | 3 | 0 / 0 |
| Rust | 920.016 | 1 | 3 | 0 / 0 |

- **63 项矩阵检查全部通过。** 九笔初始预占各为 900 秒；业务开始约 905 秒后，原来的九笔预占仍有效，共占用 27 单位，新增扣量为 0，截止时间均已延长。
- 完成后原来的九笔预占全部成为 `committed`，每笔 quantity=3。九张许可证分别增加 3 单位，总计增加 **27 单位**。
- 新进程重新登录，使用同一 operation ID 重试并显式结算，回调不再执行，实际用量不增加。
- 长任务之前的九笔短任务和新进程重试也通过，共扣 27 单位。本轮九张独立许可证最终累计用量为 **54：短任务 27 + 长任务 27**。
- 最终分发 wheel 安装到 177 后再次重放 Python；从安装前缀链接的独立 C、C++ 消费者也重放同一已完成任务。三者回调均为 0，真实总用量保持 **54 → 54**。

依据：[完整矩阵](launch-sdk-renewal-20261101/matrix-result.json)、[长任务产物](launch-sdk-renewal-20261101/cases/long)、[实际小程序](launch-sdk-renewal-20261101/clients/README.md)。

## 构建检查与本轮补修

177 上 **17 个 SDK 构建及测试步骤通过**，涵盖九种语言的包装层及内置运行时。Python 的 Windows DPAPI 测试按 Linux 平台跳过，另在 Windows 安装检查中执行。前端逻辑测试 **65/65 通过**。[Linux 结果](launch-sdk-renewal-20261101/sdk-unit-results.json)、[前端结果](launch-sdk-renewal-20261101/frontend-tests.log)

Windows 上 Go、Python、C#、Java、JavaScript、Ruby、Rust 的 **7/7 种分发包独立安装检查通过**，包括库测试、内置运行时及生成的直接初始化代码。C/C++ 安装前缀消费者在 Linux 上通过，不能将其写成九种 Windows 包全部通过。[Windows 安装结果](launch-sdk-renewal-20261101/package-installs-final.json)

分发检查另外发现并修复了三项 Launch 仓库问题：

1. **Go 分发包带入服务端集成测试。** `_integration_test.go` 依赖未随 SDK 分发的 Launch 服务端包和 MySQL，导致独立消费者执行库测试时编译失败。打包脚本现在排除这些仓库集成测试，仓库原测试保留。修复后独立库测试及生成的初始化代码通过。公开客户端仓库导出脚本原本已有相同筛选，不需要改动。
2. **Rust 测试夹具共享全局账号锁。** 不同并行用例使用同一个假商户，偶发触发 `ACCOUNT_LOCK_BUSY`。现在不同夹具使用不同测试商户，同一夹具重开的实例保持同一作用域。保留真实账号锁及并行执行，**21 项库测试在 16 个线程下通过**。[并行测试记录](launch-sdk-renewal-20261101/rust-fixture-isolation.log)
3. **分发源码清单仍记录 Go 导入改写之前的哈希。** 独立模块改写私有导入后，四个 Go 文件的分发内容与旧清单不一致。打包脚本现在据分发内容重新记录目标哈希，保留官方源文件哈希，并在压缩前逐个核对全部 132 个目标。源码构建验证及最终归档检查均通过。该项只修正分发清单，不改变运行时代码。

打包脚本增加了 `SourceRevision` 参数，保留实际内置修订标识。最终包复用已通过安装检查的相同库文件，补入重新验证的源码包和清单；Python wheel 与 177 实际安装的文件完全相同。[最终产物 SHA-256](launch-sdk-renewal-20261101/final-artifacts.json)、[分发源码校验](launch-sdk-renewal-20261101/package-source-audit.json)

## 保留的失败与验证限制

正式矩阵之前的两次启动尝试保留在服务器，没有混入 63 项通过检查。[预检失败摘要](launch-sdk-renewal-20261101/preflight-attempts.json)

第一次同时启动多个登录，发生账号锁冲突。第二次 C 的自定义缓存目录尚未建立，发生 `CACHE_UNAVAILABLE`；共享客户的部分其他客户端还观察到 `DEVICE_CREDENTIAL_REVOKED`，其直接因果链没有完全查明，不能断言都是缓存失败引起。正式矩阵使用独立客户、许可证、预建缓存和依次登录，没有修改旧账本、删除旧任务日志或伪造结算来获得通过。

首轮分发安装发现 Go 包问题，第二轮发现上述 Rust 并行测试干扰，原结果仍保留：[首轮安装](launch-sdk-renewal-20261101/package-installs-r1.json)、[第二轮安装](launch-sdk-renewal-20261101/package-installs-r2.json)。Rust 源码本机构建的首次尝试因漏配 curl 开发库路径未能编译，补上现有工具链路径后才执行并通过测试；该次编译失败没有算作通过。

本轮是 SDK 更新后的定向回归，**没有重新跑 4–8 小时 soak，也不代表授权策略和许可证的所有组合均已穷尽**。之前的固定/浮动授权、进程中断、失败回调、额度耗尽、MySQL 接口及页面回归保留在[前次修复报告](fix-regression-2026-11-01.md)。另一个 AI 的官方 SDK 验收属于单独证据，没有重复计入本轮 63 项。

JavaScript 本轮等待异步子进程，事件循环保持可运行；长时间同步阻塞 JavaScript 线程仍会阻止续期定时器，需要异步任务、定期让出线程或 worker。自动续期也不能越过许可证失效、授权撤销或计量周期失效等限制。本轮未重新覆盖这些故障和长期断网的全部组合。

本轮未做真实支付、邮件和翻译验收，没有新建订单或支付记录。Windows C/C++ 预编译包及 macOS/ARM 平台本轮未构建验证；C/C++ 安装前缀消费者在 Linux amd64 验证。

本轮新增九个专用客户已通过管理 API 停用，同步完成，保留许可证、产物和扣量记录。原 Launch 18080、隔离 Launch 18092、Licentivo 18091 的最终 `/readyz` 均为 200。[最终状态](launch-sdk-renewal-20261101/final-state.json)
