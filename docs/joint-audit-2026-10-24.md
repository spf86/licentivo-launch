# Launch / Licentivo 联调审查

日期：2026-10-24。设备：`192.168.2.177`，Ubuntu x86_64，MySQL `8.4.11`。

后续：Launch 的 A1、A3、A4 已修复，Licentivo 的 A2 更新也已复测，见 [修复与联调验证](fixes-joint-verification-2026-10-24.md)。以下保留首次审查时的原始问题与证据。

结论：主授权链路通过真实服务联调，另外确认 4 个问题。以下修改均为建议，尚未实施；业务代码未改，未上线、未发布 SDK。

## 已确认问题

| 编号 | 建议优先级 | 修改方 | 问题及影响 |
| --- | --- | --- | --- |
| A1 | 高 | Launch 支付事件与购买账本 | Stripe 已败诉拒付后，事件仅进入待核对，购买仍有效，新的 SDK 登录仍能取得授权。 |
| A2 | 高 | Licentivo 支付适配与退款流程 | Waffo 明确拒绝、未生成退款单，仍留下无法恢复的退款预留；余额恢复后核对和重复审核都不能继续。 |
| A3 | 中 | Launch 退款运维流程 | 退款超时达到上限进入人工核对后，没有有效恢复入口；支付服务恢复也无法继续退款。 |
| A4 | 中 | Launch 翻译任务分批 | 合法长度的多份产品文档可以形成超大 DeepL 请求，导致翻译任务重复失败。 |

### A1：败诉拒付没有回收对应购买覆盖

复现：在已付款、已同步至真实 Licentivo 的购买上，提交通过实际 Stripe 适配器验签的 `charge.dispute.closed` 通知，争议状态为 `lost`。事件最终为 `review`，购买仍为 `active`。随后重新完成 Launch 登录，真实 Go Launch SDK 仍成功兑换授权并执行 `export` 功能。

根因：[payment_events.go](../internal/app/payment_events.go#L124) 的事件分支处理付款及退款，没有处理争议或撤款。Stripe 官方区分争议创建、资金扣回和争议结束等事件，不能将它们等同于普通退款。[Stripe 事件说明](https://docs.stripe.com/api/events/types)

建议：Launch 查询并核实原交易、争议结果、金额及所属商户；确认败诉或资金撤回后，更新对应订单的商业状态和购买覆盖，再向 Licentivo 同步完整快照。争议尚未结束时是否暂停购买，由你决定。Licentivo 继续负责实际授权撤销，SDK 无需增加支付判断。

补充静态检查：PayPal 的 `PAYMENT.CAPTURE.REVERSED` 也未进入该分支；Paddle 的 `chargeback` 调整会被当前仅接受 `action=refund` 的退款读取逻辑拒绝。动态证明目前覆盖 Stripe；其余应在修改时分别增加原渠道用例。[PayPal 事件](https://developer.paypal.com/api/rest/webhooks/event-names/)、[Paddle 调整事件](https://developer.paddle.com/webhooks/adjustments/adjustment-created/)

### A2：明确拒绝的 Waffo 退款永久保留预留

复现：在独立 Licentivo 测试库中，客户余额 20 美元，审核原渠道退款 5 美元；Waffo 返回官方定义的 HTTP 409“可用余额不足”，并未创建退款单。之后模拟渠道余额已恢复，执行管理员核对和重复审核。

结果：创建请求总数仍为 1，退款状态 `unknown`、`reserved=true`、无渠道退款单编号；5 美元预留未释放，客户可用余额仍为 15 美元。

根因：Waffo [适配器](../../Licenova/internal/waffo/client.go#L86) 将全部非成功状态折叠为普通错误；[退款流程](../../Licenova/internal/httpapi/provider_refunds.go#L326) 在尝试后保守地转为未知，随后只查询，不再创建。明确拒绝与响应丢失因此共用一个无法退出的状态。

建议：保留渠道状态码和业务错误类别。对已核实“明确拒绝且没有生成退款”的结果，结束失败并释放预留，允许再次审核；对真正的未知结果继续查询原业务引用，避免重复退款。官方文档明确列出余额恢复后重试的场景。[Waffo Create Refund Ticket](https://docs.waffo.ai/api-reference/endpoints/refunds/create-refund-ticket)

### A3：Launch 人工核对退款没有恢复动作

复现：支付适配器连续返回不确定结果，通过调整本次测试行的下次检查时间加速 12 次真实任务执行。任务进入 `unknown / review_required=true`。恢复渠道后，依次执行处理任务、客户重新申请、管理员批准和核对原付款。

结果：渠道调用次数始终为 12；重新申请得到同一条未知退款，审核标志仍为真。

根因：[请求入口](../internal/app/refunds.go#L55) 直接返回已有退款，[批准入口](../internal/app/refunds.go#L142) 只处理 `requested`，[后台领取](../internal/app/refunds.go#L180) 排除需要核对的任务。核对原付款也不会核对这笔退款。

建议：增加可审计的管理员退款核对/恢复入口。先查询原退款，核实渠道回执与交易、金额、币种，再恢复同一任务；超出渠道幂等窗口后不能盲目重新提交。

### A4：文档翻译只按条数分批

复现：保存描述及多份产品文档，每项均不超过现有 16,000 字节限制。实际翻译批次为 160,131 字节、少于 50 条；真实 DeepL 适配器在发起网络请求前拒绝，任务进入 `retrying`。

根因：[任务组装](../internal/app/translations.go#L850) 仅按 50 条分批，[适配器](../internal/adapters/deepl/client.go#L46) 则限制编码后的请求体大小。DeepL 官方总请求上限为 128 KiB。[DeepL 请求限制](https://developers.deepl.com/docs/resources/usage-limits)

建议：同时按条数和实际 JSON 字节分批，为请求头预留空间；保留文档片段和译文的对应关系。改 DeepL Key 或原样重试不能解决当前失败。

## 主流程和回归证据

真实联调运行在同一 Linux 设备上：当前 Launch 服务、当前 Licentivo 服务、远端 MySQL 和真实 Go Launch SDK。仅通过已有测试注入点改变临时服务地址和支付模拟器，未修改业务实现。

通过的链路包括：产品/策略映射、绝对购买期限同步、身份签名兑换、SDK 功能执行、普通关闭恢复、显式退出、远程解绑、旧绑定代次保护、续费及退款移除延长期、账号撤销回执与新登录，以及整笔退款后的授权拒绝。支付捕获和退款资金动作使用隔离模拟器。

| 检查 | 结果 |
| --- | --- |
| Launch 全部源包回归，Windows 经 SSH 隧道连接远端 MySQL | 首次出现排队时序失败；全部失败用例随后在 Linux 同机原用例中通过 |
| Launch 购买快照与身份撤销用例组，Linux | 16 条通过记录，含子用例；无失败 |
| Launch 新支付渠道用例组，Linux | 4 条通过记录，含子用例；无失败 |
| Launch 完整应用流程，Linux | 通过 |
| Licentivo 全部 Go 包，Windows 经 SSH 隧道连接远端 MySQL | 首次只有默认额度通知保护用例失败；Linux 同机复核通过 |
| Licentivo 完整 HTTP 回归，Linux | 444 条通过记录，19 条跳过，含子用例；无失败 |
| 前端接入、状态与套餐逻辑 | 37 项通过 |
| OpenAPI 路由、引用、安全方案检查 | 22 个操作通过 |
| 业务源文件哈希比对 | 两个原始仓库的被审查源文件均未变化 |

跳过的测试不计作通过，包含未启用的部分跨语言 SDK 进程用例。本次真实服务联调明确覆盖 Go SDK，不能据此宣称九种 SDK 均已完成真实服务验收。

独立问题复现不属于上述正常回归：A1–A4 的测试专门验证当前问题能够发生。

## 环境与外部验证边界

两台设备采样显示约一秒时钟差：Linux 已启用 NTP 并已同步，Windows `W32Time` 停止。数据库默认时间与应用时间混用，会使刚入队的任务暂时尚未到处理时间，也影响刚创建租户的通知保护时间比较。原用例在 Linux 同机复核全部通过。建议先恢复 Windows 时间同步；本次未调整任何系统时钟或时间服务。

支付审查对照了 Stripe、PayPal、Paddle、Waffo Payment API 和 Waffo Pancake 的官方资料；邮件对照了 [Resend SMTP](https://resend.com/docs/send-with-smtp)、[Brevo](https://developers.brevo.com/reference/send-transac-email)、[Postmark](https://postmarkapp.com/developer/api/email-api) 和 [Mailgun](https://documentation.mailgun.com/docs/mailgun/api-reference/send/mailgun/messages/post-v3--domain-name--messages)。Resend 的 SMTP 幂等头和当前端口配置与官方契约一致，没有列为问题。

未使用真实支付商户凭据，没有真实扣款、退款、邮件投递或付费翻译；这些外部服务的实际账号权限、域名和沙箱验收仍未验证。

原 MySQL 和既有历史测试库保留。临时服务、连接隧道在审查后停止；隔离测试库和证据保留于设备的 `/home/spf86/licentivo-joint-audit-20261024`。授权上传的源码快照不含 Git 历史、配置密钥和缓存。

本地原始证据保存在 `.local/audit-20261024/remote-evidence/`，摘要见 [审查证据](joint-audit-evidence-2026-10-24.json)。建议先处理 A1、A2，再处理 A3、A4；是否修改由你决定。
