# Launch 购买同步、升级计价与续费恢复修复

日期：2026-10-24。

## 修复结果

| 问题 | 修复后的行为 | 实现 |
| --- | --- | --- |
| 旧正向任务进入审核后阻塞退款降级 | 已确认退款或撤款、且撤销覆盖后生成的完整快照，可以越过旧任务。新快照得到 Licentivo 的 applied 确认后，旧任务才记录为 PURCHASE_SNAPSHOT_SUPERSEDED，并保留原始载荷。迁移任务继续作为前置约束。 | [purchase_sync.go](../internal/app/purchase_sync.go) |
| 多周期升级只收单期差价 | 使用当前覆盖来源订单实际购买的期数与成交金额，对照目标套餐同期限的价格，计算整段差价，再按剩余时间向上取整。日历累计期数只用于排期；连续升级使用前一次报价保存的完整目标价格，历史报价按不可变套餐版本恢复该价格。 | [quotes.go](../internal/app/quotes.go)、[quotes_test.go](../internal/app/quotes_test.go) |
| 自然到期先推进版本，导致续费付款进入审核 | 核验从原报价版本到当前版本的完整任务历史、购买身份、原报价与付款、覆盖记录和未改变的期限。只有自然到期造成的版本变化可继续入账。到期前付款接续原锚点；到期后付款从原渠道确认的实际付款时间开始。 | [ledger.go](../internal/app/ledger.go) |

退款订单的原正向快照不会因为订单后来变为 refunded 或 reversed 而获得越队资格。已覆盖“自然到期任务待审核 → 续费付款 → 续费退款”的顺序，确认发送的是撤销后的完整状态。

无折扣三期基础套餐 30 美元、目标套餐同期限 60 美元，刚购买后的升级差价现在为 30 美元。折扣用例覆盖三期 27 → 51 美元收取 24 美元、继续升级到 72 美元收取 21 美元；另覆盖多次续费后的累计期数与本次成交期数不同、部分时间剩余和不足一分向上取整。

## 已付款审核订单的恢复入口

使用现有管理端接口 POST /api/v1/admin/orders/{id}/reconcile，提供 reason。管理员或有管理权限的运营人员可执行，沿用原权限和请求保护。

恢复使用已确认的原付款记录，核验订单、金额、币种、环境、交易编号和付款时间；安全重放续费覆盖的入账。成功后清除 fulfillment_review，并产生 order.fulfillment_recovered 审计记录。重复核对不会再次添加覆盖。

存在退款、撤款、套餐变化、冻结、报价与付款不一致或版本历史缺失时，继续保留审核，管理端返回 FULFILLMENT_REVIEW_REQUIRED。自动恢复要求实际付款时原报价仍有效。

实现见 [order_recovery.go](../internal/app/order_recovery.go) 与 [operations.go](../internal/app/operations.go)。

## 验证

| 验证范围 | 结果 |
| --- | --- |
| 修复前复现 | 三期升级实际只收 1000 分；退款快照被审核任务阻塞；自然到期后续费没有覆盖；管理端核对仍无覆盖，四项均按预期失败 |
| 最终全量主模块 Go 回归 | 214 条通过记录，含父测试和子用例；无失败，2 项环境依赖测试跳过 |
| 最终购买与升级专项 | 42 条通过记录，无失败、无跳过 |
| go vet | 通过 |

两个跳过项是 TestProductUploadRoundTrip（缺少 LAUNCH_E2E_CONFIG_FILE）和 TestLaunchAccountLogoutRecovery（缺少独立退出审计数据库）。本次购买、计价与续费恢复的 MySQL 专项均已实际执行。

验证使用工作区内独立 MySQL 数据目录、随机回环端口和七个全新测试库。支付渠道与 Licentivo 通过本地合约测试替身验证。测试结束后临时 MySQL 已关闭，日志和修复前文件基线保留在 .local/commerce-fix-20261024/。

[验证数据与最终源码摘要](commerce-fixes-evidence-2026-10-24.json)。
