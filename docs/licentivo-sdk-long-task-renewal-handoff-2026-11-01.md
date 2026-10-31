# Licentivo 官方 SDK 八种语言长任务续期修复要求

> 后续状态：官方 SDK 修复已同步到 Launch，九种语言通过 177 上实际 Launch Application 的 920 秒续期、结算及新进程重试。见[后续联合回归](launch-sdk-renewal-regression-2026-11-01.md)。下文保留为当时的缺陷与修复要求记录。

请在 **`D:\GoProject\Licenova`** 修复官方 SDK 的计量任务自动续期，覆盖 **Go、JavaScript、Java、C#、C、C++、Rust、Ruby**。Launch 已补齐高层计量执行和中断结算入口，本次应把续期实现放在官方 SDK。修复完成后，再由 Launch 同步内置 SDK、构建安装包并在 177 联合回归。

## 问题归属与影响

问题是 `RunMeteredFeature`、`runMeteredFeature`、`run_metered_feature` 等高层计量方法没有在业务回调运行期间保持预占有效。许可证心跳保持运行授权，计量预占则占用这一次业务所需的额度；前者不能代替后者。

当前服务端默认预占 **900 秒**。上述实现的流程是：初次 reserve → 执行业务回调 → commit 或 cancel。业务可能已经实际完成，但提交时预占已过期，服务端拒绝提交，客户端任务可能保留在 `commit_pending`。此时错误不代表业务没有发生；更换 operation ID 重新执行业务可能产生重复产物和业务次数与扣量不一致。

**证据范围：八种语言缺少续期机制已通过源码检查确认，尚未逐语言运行超过 900 秒的失败复现。** 旧版 Python 曾真实复现相同问题；修复后的 Python 已在 177 通过超过 15 分钟的实测。请先给其余语言补复现和测试，再修复，不要把源码判断写成八种语言都已实际超时失败。

## 五套实现与八个入口

以下路径均相对于 `D:\GoProject\Licenova`，是官方源代码，不是 Launch 里的 vendor 副本。

| 语言 | 主要修改位置 | 当前缺口 |
|---|---|---|
| Go | `sdk/go/feature_tasks.go`，`Client.RunMeteredFeature` | 初次 `Reserve` 后直接执行回调，没有预占续期协程 |
| JavaScript | `sdk/javascript/feature-tasks.mjs`，`runMetered` | 初次 `client.reserve` 后等待业务，没有续期调度 |
| Java | `sdk/java/src/licentivo/FeatureTasks.java` | 初次 `client.reserve` 后执行回调，没有续期工作任务 |
| C# | `sdk/csharp/FeatureTasks.cs`，`RunMeteredFeature` | 初次 `Reserve` 后执行回调，没有续期任务 |
| C | `sdk/c/feature_tasks.h`，`ln_run_metered_feature` | 初次 `ln_reserve` 后执行 `action(context)`，期间没有续期 |
| C++ | `sdk/cpp/licentivo.hpp` | `RunMeteredFeature` 调用 C 核心，需随 C 修复并单独验收 |
| Rust | `sdk/rust/src/lib.rs` | `run_metered_feature` 经 FFI 调用 C 核心，需随 C 修复并单独验收 |
| Ruby | `sdk/ruby/lib/licentivo.rb` | `run_metered_feature` 经 Fiddle 调用 C 核心，需随 C 修复并单独验收 |

因此是 **五套计量实现、八个语言入口**。C 核心修复后，需要重新构建 C++、Rust、Ruby 所用的原生库和包；仅修改 C 头文件而继续安装旧二进制不算完成。

## 已有服务端和 Python 修复

- 服务端参考：`internal/httpapi/feature_reservations.go` 中的 `reservationOperation`。
- 已增加的服务端测试：`internal/httpapi/feature_reservation_renewal_test.go`。
- Python 参考：`sdk/python/licentivo_tasks.py` 中的 `_renewing` 和 `run`。
- Python 回归参考：`sdk/python/test_tasks.py`。

服务端已支持：对**尚未过期、状态为 pending 的同一笔预占**，重放原 reserve 请求可延长 `expires_at`。必须保持原来的 feature、quantity、operation ID、device 和 activation 上下文。重新 reserve 不得产生第二笔预占或增加已消费用量。

服务端仍须检查当前授权、应用版本、额度和原账期。已经过期、取消或提交的预占不能靠重放变成新任务；续期不能跨越原账期限制，也不能绕过授权撤回或额度缩减。现有服务端补丁应保留，不要把“允许提交所有过期预占”作为 SDK 续期的替代修复。

Python 实测基准：默认剩余时间约 899.92 秒，业务生成 903 行 CSV，最后进度记录 904.15 秒，同一个 reservation ID 获得初次加 15 次续期共 16 个回执；用量从 6 增到 7，恢复进程并取得新身份后重试，业务仍只执行一次。

## 实现要求

1. **业务运行期间自动续期。** 初次 reserve 成功并持久化业务状态后，在业务开始前建立续期生命周期；业务运行期间使用原请求续期，结束时停止并回收续期任务，然后完成结算。无需应用开发者手写 timer。
2. **依据服务器截止时间调度。** 使用每次响应的 `expires_at`，在截止之前留出请求和重试余量。可参考 Python 的不超过约 60 秒、接近截止时更短的策略，但不要固定等待 900 秒。账期边界可能让实际预占时间明显短于 900 秒。
3. **始终保持同一笔业务。** feature、quantity、operation ID、reservation ID 及服务端要求的绑定上下文保持一致；不要生成新 ID、重新激活设备、重复运行回调或创建新的预占来模拟续期。验证回执仍属于原预占，及时处理已关闭或不匹配的回执。
4. **保留业务真实结果。** 回调成功后即使续期或 commit 失败，也不能当作“业务没发生”，不能自动再次运行回调或伪造已结算。持久化可恢复的状态，向调用方明确报告结算待确认。回调失败时保留原业务错误，尝试取消预占；取消失败保留恢复记录。
5. **正确处理网络不确定性。** 覆盖超时、暂时断网、响应丢失、服务端已续期但客户端未收到响应等情况。仅重试幂等的续期或结算请求，不重试业务；达到真实截止或授权被拒后，不能继续声称预占有效。无法确认的结果继续保持可恢复状态。
6. **保持任务日志与恢复语义。** `business_running`、`commit_pending`、`cancel_pending`、`committed`、`canceled` 等现有状态应兼容。已完成任务重复调用不执行回调；结果未知时要求先核对真实业务，再使用 resolve。续期任务不应与另一个调用并发改写同一日志，也不应破坏已有锁与 N04 回归。
7. **回收线程及其他资源。** 成功、回调异常、调用取消、Close/Dispose、启动失败都要覆盖；没有泄漏的 goroutine、线程、timer、未完成异步任务或回调结束后的持续续期。注意锁顺序、在途网络请求、销毁后的客户端访问，避免等待续期退出时死锁。
8. **保持语言层异常和返回值。** C++ 回调异常要回到调用方，Rust 错误及 Executed/AlreadyCompleted 语义、JavaScript/Python 完成重放无返回业务值等行为不能被续期改变。后台续期的错误也不能无保护地覆盖主业务错误状态。
9. **说明 JavaScript 主线程阻塞边界。** 只加 `setInterval` 无法覆盖长时间同步阻塞的 CPU 回调。必须给出经过测试的支持方式或明确的调用限制；不能用一个会让出事件循环的异步等待用例，声称同步 CPU 密集回调也已覆盖。Ruby/C FFI 与原生线程也应检查对应运行时约束。
10. **维持授权边界。** 只在现有授权、许可有效期、会话、额度和账期允许的范围内续期。授权到期或被撤回后，不能通过续期绕过服务端检查；不可中断回调的业务产物必须如实保留和报告，不能声称已被回滚。

不要仅增大默认 TTL、缩短业务、修改测试服务器时间，或让 Launch 外层再实现一套预占协议。不要关闭签名验证或绕过额度校验来使测试通过。

## 复现与验收方法

先在允许计量的测试授权上使用稳定 operation ID 运行一个实际输出文件的长回调，再核对服务端预占、业务日志和用量。Python 已通过，可作为正向对照。业务测试可以每秒写一行并同步落盘，最后通过行数、哈希及回调计数核验，不能只打印“调用成功”。

| 场景 | 验收标准 |
|---|---|
| 默认 900 秒预占，真实业务运行 905 至 930 秒 | 八种语言各有实际测试；初次加多次续期始终为同一个 reservation ID，业务一次，扣量一次，最终 committed |
| 普通短任务 | 成功及正常返回值不退化，没有遗留续期请求 |
| 回调抛错或返回失败 | 原业务错误保留，取消成功或保留 cancel_pending，不因测试失败而再次执行业务 |
| 暂时断网后恢复 | 截止允许范围内恢复续期和提交，不创建第二笔业务或额外扣量 |
| 续期请求服务端成功但响应丢失 | 后续幂等请求可恢复，不误判为新任务，不把首次响应截止时间当作服务器最终状态的唯一证据 |
| 连续续期失败并真实过期 | 明确报告未结算或过期，业务状态与结算状态分开；不能假称提交成功或盲目重跑 |
| commit 响应丢失 | 原任务恢复后只重试结算，已消费额度只增加一次 |
| 进程在回调中退出 | 续期资源随进程结束；恢复后未知结果阻止自动重跑，核对实际产物后 resolve 可按原有有效性规则处理 |
| 回调取消、Close/Dispose | 无死锁、资源泄漏、客户端销毁后访问，不能错误提交未知业务结果 |
| 续期期间撤回授权、缩减额度、限制版本 | 服务端原有拒绝逻辑仍有效，SDK 保留正确错误和业务结果 |
| 已过期、已提交、已取消的预占 | 不能被续期复活为新 pending；完成重放不再调用业务 |
| 原账期或授权有效期边界 | 不跨越服务端截止限制，不把正常授权边界误报为“长任务修复失败后可绕过” |
| 并发与恢复回归 | 原有幂等、任务锁、身份轮换、日志恢复相关测试继续通过，不引入重复回调或重复扣量 |

快速单元测试可以用短截止时间或可控时钟覆盖故障分支；**最终八语言验收应保留默认 900 秒的真实 HTTP + MySQL 测试**。隔离不同测试的授权和用量，避免多个任务同时运行时用全局用量差误算某一笔扣量；优先核对 operation ID 对应的预占及消费回执。

## 与 Launch 的交付边界

官方 SDK 修复完成后，请交付修改文件、明确的源码版本或提交号、构建哈希、逐语言实际测试结果和未覆盖项。C++、Rust、Ruby 要交付重新构建的对应原生制品及其测试结果。保持现有公开接口兼容；需要新 API 时说明原因及适配工作。

Launch 当前已提供九语言 Application 的计量执行与 resolve 转发。后续需要把审核后的官方 SDK 更新同步到 `D:\GoProject\Licentivo Launch`，重新构建对应分发包和原生库，再在 177 通过 Launch Application 验证长任务、恢复和结算。**只改 Licentivo 源码，不更新 Launch 内置 SDK 和实际安装的制品，不算联合修复完成。**

现有工作区版本号仍可能显示官方 SDK 0.11.7、Launch SDK 0.1.7；不能仅凭相同版本号认定已安装修复。提交结果时要说明修复是否仅在源码中、是否已构建、部署或发布，并提供实际使用制品的哈希。

## 已有测试证据与小程序

以下文件在 Launch 仓库中；不含真实密钥，可作为复现和联合验收基线。

- [上一轮修复与联合回归报告](fix-regression-2026-11-01.md)
- [Python 长任务的实际回执和结果](n-fixes-20261101/long-result.json)
- [Python 长任务业务 CSV](n-fixes-20261101/long-business.csv)
- [实际长任务测试程序](n-fixes-20261101/clients/long_regression.py)
- [进程中断与结算测试程序](n-fixes-20261101/clients/settlement_regression.py)
- [九语言实际小程序说明](n-fixes-20261101/clients/README.md)
- [Launch 计量入口和恢复约定](metered-feature-tasks.md)

这些程序依赖 177 的隔离测试目录和私有配置，不是无需配置即可在任意机器运行的脚本。复用时创建新的运行目录、operation ID 和证据文件，保留上一轮结果；最终提交的报告中区分源码审查、单元测试和真实联合验收。
