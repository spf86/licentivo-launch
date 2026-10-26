# Launch 修复与双方联调验证

日期：2026-10-24。Launch 的 A1、A3、A4 已修复；Licentivo 的 A2 使用对方更新后的实现复测。本轮没有修改 Licentivo 支付或授权源码。未上线、未发布 SDK、未提交或推送 Git。

## 修改结果

| 项目 | 当前行为 | 主要实现 |
| --- | --- | --- |
| A1 拒付与撤款 | Stripe 败诉争议、PayPal 捕获撤款、Paddle 已批准 chargeback 均先向原渠道核实交易、商户、环境和金额，再移除对应订单的购买覆盖，向 Licentivo 同步完整购买快照。未决争议不改变购买记录。 | `internal/payments/reversals.go`、`internal/app/payment_reversals.go` |
| A3 退款恢复 | 管理员可在退款页填写核对说明、可选原渠道退款编号，恢复同一个已批准任务。先核对原退款回执；Stripe / PayPal 无回执时，仅在原审批时间 23 小时内使用原幂等请求重试。超时后要求原渠道退款编号。 | `internal/app/refund_recovery.go`、`POST /api/v1/admin/refunds/{id}/reconcile` |
| A4 长文档翻译 | DeepL 同时限制单批条数和实际编码后的 JSON 字节数，预留请求头空间；发送前预检全部分批，保留各字段与译文的顺序。 | `internal/adapters/deepl/client.go` |

拒付只移除对应订单的覆盖；续费订单被撤款时，基础购买仍保留。原交易回调迟到不会重新产生购买覆盖。不同通知指向同一笔撤款时，使用唯一回执防止重复变更。历史待核对撤款通知可由渠道重新验签重发后恢复处理，沿用原事件身份。

退款恢复要求管理员权限、网站 CSRF 凭据和核对说明，并保留审计记录。恢复不更换原操作编号、审批时间或金额；旧回执必须匹配原交易、币种和整笔金额。Paddle / Waffo 的原退款查询可超出创建请求的重试窗口继续执行，不重新发起资金动作。

配套修复包括客户中心的撤款状态与内置界面翻译，以及新撤款回执表的备份兼容。旧库升级测试发现 GORM 不会自动扩展现有 MySQL 枚举，已补显式、可重复执行的订单状态迁移；旧版 v1 备份允许缺少新增的 `payment_reversals` 空表，其他表的完整性校验仍保留。

Launch 仍只维护商业事实和购买期限。授权许可、设备、功能执行和离线规则继续由 Licentivo 与官方 SDK 决定。

## 本机最终验证

使用本机 MySQL 8.0.32 新建独立实例、随机回环端口和专用数据库，未使用已有业务数据库。后半程按用户要求不再访问 `192.168.2.177`。

| 验证范围 | 结果 |
| --- | --- |
| Launch 全部源包与独立 MySQL 回归 | 190 条通过记录，无失败；包括子用例，跳过独立的 `TestProductUploadRoundTrip`，未计入通过 |
| Licentivo 全部 Go 源包本机回归 | 737 条通过记录，无失败；数据库等依赖未启用的测试明确跳过，另由专项库验证 A2 |
| Licentivo Waffo 退款专项 + 实际本机 MySQL | 39 条通过记录，无跳过、无失败 |
| 双方静态检查 | `go vet` 通过 |
| JavaScript / Python Launch SDK | 分别 8 / 7 项通过 |
| 前端接入、状态和套餐逻辑 | 37 项通过 |
| 验收脚本 | 11 项通过 |
| OpenAPI | 23 个操作的路由、引用、安全方案检查通过 |

真实本机服务联调使用当前 Launch 业务实现、Licentivo 业务实现、独立 MySQL 和真实 Go Launch SDK，全部通过：

- 购买绝对起止时间同步，Licentivo 确认 `applied` 后再确认完成。
- 身份兑换、签名许可和 `export` 功能执行；普通关闭后恢复。
- 显式退出、远程解绑、旧绑定代次保护、账号撤销后新登录。
- 续费、移除退款延长期、整笔退款后新的 SDK 登录失败。
- 连续 12 次未知退款后，管理员恢复原任务；请求编号与审批时间不变，Licentivo 应用终止购买快照后，新的 SDK 登录失败。
- 通过真实 Stripe 适配器验签和查询的败诉争议：撤销对应购买、处理历史待核对通知重发、去重重复通知，新的 SDK 登录失败。
- 合法长度的 160,000 字节产品文档完成九种目标语言翻译，各字段内容和顺序完整；最大实际请求体为 112,122 字节。

本机联调最初因测试脚本的随机访问端口与固定监听端口不一致失败；已修正脚本，在全新数据库重跑后全部通过。此项未涉及产品代码修改。旧数据库枚举升级的首次失败也保留为诊断证据，修复后独立复测通过。

Licentivo 网站和文档文件在联调期间出现后续变化，支付、授权后端源码保持本次验证版本；受影响的 `web` 包另行补测。证据摘要见 [验证数据](fixes-joint-evidence-2026-10-24.json)，本地原始记录位于 `.local/fixes-20261024/`。

## 渠道验证与环境状态

实际调用真实 Launch / Licentivo 服务和官方 Go SDK。支付捕获、退款、争议查询与 DeepL HTTP 响应由隔离模拟器提供，未使用真实商户凭据、未真实扣款退款、未消耗翻译额度；不能据此宣称支付商户账号已通过线上或官方沙箱验收。

Stripe、PayPal、Paddle 的处理分别对照 [Stripe 争议查询](https://docs.stripe.com/api/disputes/retrieve)、[PayPal 捕获详情](https://developer.paypal.com/api/payments/v2/captures-get)、[Paddle 调整通知](https://developer.paddle.com/webhooks/adjustments/adjustment-created/)。DeepL 分批依据其 [请求限制](https://developers.deepl.com/docs/resources/usage-limits)。

本机临时服务在验证后停止，隔离库数据和证据保留。远端连接中断后按用户要求停止访问，远端临时 Licentivo 服务的停止状态尚未确认，需待设备恢复连接后另行清理。
