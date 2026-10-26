# 自定义授权服务：API 说明、对接流程与代码修改方案

日期：2026-10-25。状态：**原始设计草案；当前实现和正式接入方式见 [自定义授权接入指南](../external-licensing.md)**。

本文保留实施前的设计方案。当前已实现的接口、实际字段和接入范围请以[自定义授权接入指南](../external-licensing.md)为准；原“尚未实现”描述属于设计阶段记录。现有 Licentivo 协议继续保留，自定义商品使用商户自己的授权 SDK。

## 1. 目标与选择方式

商户在商品后台选择授权提供方，购买者照常注册、下单和付款：

| 提供方 | Launch 提供的能力 | 实际授权由谁负责 |
| --- | --- | --- |
| Licentivo | 现有购买同步、账号接入与管理流程 | Licentivo 服务和官方 SDK |
| 自定义授权服务 | 购买状态通知、处理结果查询、授权交付展示 | 商户已有授权系统和它自己的 SDK |

自定义方式可以在完全没有配置 Licentivo 的实例中运行。Launch 不提供免费的许可证生成、激活校验、设备控制或离线授权实现；商户可以开发一层 HTTP 适配服务，把本文协议转接到已有系统。

首版以“购买同步 + 授权交付”为范围。共用 Launch 账号登录应用、设备管理、策略目录发现作为独立扩展，不要求接入方实现整套 Licentivo API。

职责边界：

- Launch 确认支付和退款，计算购买覆盖、续费和升级后的绝对期限，并保存订单与审计记录。
- 自定义授权服务接受购买目标状态，自行创建、变更和撤销授权，并提供客户需要的交付信息。
- 应用使用商户自己的 SDK 或校验方式决定功能能否运行。Launch 的付款成功、同步完成和交付信息都不是运行许可。

## 2. 现有代码为什么需要修改

| 位置 | 当前耦合 | 修改方向 |
| --- | --- | --- |
| [connections.go](../../internal/app/connections.go) | 固定 Licentivo 地址、连接类型、产品和策略目录 | 统一连接模型，按提供方创建适配器 |
| [catalog.go](../../internal/app/catalog.go)、[orders.go](../../internal/app/orders.go) | 发布和下单要求 Licentivo 映射及 `registerLaunchPlan` | 根据提供方校验套餐配置，继续保留收款前检查 |
| [launch_events.go](../../internal/app/launch_events.go)、[launch_trust.go](../../internal/app/launch_trust.go) | 固定 Bridge、信任连接和策略校验 | 将 Licentivo 特有流程收进其适配器 |
| [purchase_sync.go](../../internal/app/purchase_sync.go) | 后台任务按 Licentivo 连接和回执处理 | 按任务固化的提供方与协议版本分发 |
| [integration_status.go](../../internal/app/integration_status.go) | 就绪判断会检查 Licentivo `LicenseBinding` | 按提供方区分同步、交付与运行授权状态 |
| [storage/models.go](../../internal/storage/models.go)、[launch_connections.go](../../internal/storage/launch_connections.go) | 连接、远端 UUID、环境信任与外键围绕 Licentivo 建模 | 增加通用绑定及明确迁移，保留现有数据 |
| [identity.go](../../internal/app/identity.go)、[integration.go](../../internal/httpapi/integration.go) | 身份断言和接入向导使用 Licentivo 语义 | 首版自定义方式不进入该路径；按提供方展示接入指南 |
| [web/integration.js](../../internal/httpapi/web/integration.js)、[web/app.js](../../internal/httpapi/web/app.js) | 后台与应用接入默认 Licentivo | 增加选择、连接检查、映射和交付展示 |

仅添加 API 文档，或允许修改一个服务地址，都不能完成这次解耦。

## 3. 商户接入流程

1. 在已有授权系统前实现本文四个接口；授权系统已有的许可证逻辑继续保留。
2. 在 Launch 新建“自定义授权服务”连接，配置 HTTPS 地址、连接标识、环境及签名密钥。
3. Launch 调用能力检查接口，确认协议版本、永久/限时购买支持和交付方式。
4. 给商品选择该连接；将销售套餐版本映射到外部 `product_ref` 和 `plan_ref`。外部编号允许使用商户自己的字符串，不强制使用 Licentivo UUID。
5. 在 sandbox 中完成付款、续费、升级、退款和故障恢复验收，然后单独发布 live 配置。
6. Launch 验证付款后，在同一数据库事务内保存订单、购买覆盖和待发送任务。
7. 后台把完整购买快照发送给自定义授权服务；该服务持久化接收记录后再确认接收。
8. 授权服务处理目标状态。Launch 查询原事件回执，只有 `applied` 才确认本次同步完成。
9. Launch 为已登录且拥有该购买的客户读取交付信息，在客户中心展示授权码、激活入口或纯文本说明。应用接入商户自己的授权系统。
10. 续费、升级、退款、撤款、暂停和到期均产生新版本的完整快照。授权服务按最新目标状态处理。

同一商品的 sandbox/live 分别绑定连接。已有购买后，更换提供方需要独立迁移流程；旧订单和旧任务不能跟随后台选择被静默送到新服务。

## 4. 自定义授权服务需要实现的 API

以下接口均由**商户授权服务接收，Launch 服务端调用**。`base_url` 可以包含固定路径前缀；表中路径追加在该前缀之后。

| 方法与路径 | 用途 |
| --- | --- |
| `GET /launch/v1/capabilities` | 连接检查、协议与能力声明 |
| `POST /launch/v1/events` | 接收完整购买目标状态；重试沿用原事件 |
| `GET /launch/v1/events/{event_id}` | 查询该事件是否实际应用，恢复丢失的响应 |
| `GET /launch/v1/entitlements/{entitlement_ref}/delivery` | 读取已有授权的交付信息，不创建第二份授权 |

首版采用主动查询回执，不新增外部回调修改订单的入口。自定义服务不具有确认付款、修改价格或改写购买期限的权限。

### 4.1 通用约定与鉴权

- JSON 使用 UTF-8；时间使用 UTC RFC 3339；`version` 使用正整数。
- 成功响应为 `{"data": {...}, "request_id": "..."}`；错误响应为 `{"error": {"code": "...", "message": "..."}, "request_id": "..."}`。
- 每个环境和连接使用独立密钥。密钥为至少 32 个随机字节，在 Launch 中引用现有加密密钥存储，不放进前端配置、示例或应用安装包。
- 每个请求携带 `X-Launch-Key-Id`、`X-Launch-Timestamp`、`X-Launch-Nonce`、`X-Launch-Signature`。
- 时间戳为十进制 Unix 秒。nonce 为至少 128 位随机值的无填充 base64url 编码，每次网络尝试生成新值。

签名输入是下列七项用单个换行符连接的 UTF-8 字节，末尾不添加换行：

```text
v1
<key_id>
<timestamp>
<nonce>
<大写 HTTP 方法>
<实际编码后的完整路径及原始 query；包括 base_url 的路径前缀>
<原始请求体的 SHA-256 小写十六进制摘要>
```

`X-Launch-Signature` 为 `v1=` 加 `HMAC-SHA256` 结果的无填充 base64url 编码。GET 请求体为空。接收方先验证签名和时间窗，再反序列化；使用恒定时间比较，默认允许正负 300 秒偏差，并至少保留 nonce 600 秒防重放。跨重试不复用 nonce，但事件 ID 和业务请求体必须不变。

签名不替代 HTTPS。请求不携带客户密码、支付密钥或 Licentivo 管理密钥；默认仅发送匿名账号引用，不发送邮箱。服务端 HTTP 客户端需要超时、响应限长和禁止携带凭据跟随重定向。配置自定义地址时校验目标地址；访问内网应是管理员明确配置的部署能力。

### 4.2 能力检查

`GET /launch/v1/capabilities` 返回 HTTP 200，例如：

```json
{
  "data": {
    "protocol": "launch-external-licensing-v1",
    "connection_ref": "merchant-01-sandbox",
    "environment": "sandbox",
    "purchase_types": ["perpetual", "fixed_term"],
    "delivery_types": ["license_key", "activation_url", "instructions"],
    "devices": false,
    "identity_exchange": false
  },
  "request_id": "check-001"
}
```

`protocol`、`connection_ref`、`environment`、`purchase_types`、`delivery_types` 必填。后两项必须为非空数组，未支持的购买类型不能发布销售。首版 `devices` 和 `identity_exchange` 必须为 `false`；只有扩展契约和实现完成后才允许声明支持。

### 4.3 购买快照

`POST /launch/v1/events` 的请求示例：

```json
{
  "schema": "launch-external-licensing-v1",
  "event_id": "7bf84b91-2749-4b01-a6a7-bd527c488ee8",
  "connection_ref": "merchant-01-sandbox",
  "environment": "sandbox",
  "type": "entitlement.snapshot",
  "change_reason": "purchase",
  "account_ref": "customer_opaque_001",
  "entitlement_ref": "purchase_opaque_001",
  "launch_product_id": "c6dd463e-50f7-4b76-8239-0a8ec6fc304b",
  "launch_plan_version_id": "5459a073-9362-4b46-949d-9b0c5d84421a",
  "product_ref": "desktop-editor",
  "plan_ref": "professional-yearly-v1",
  "version": 1,
  "desired_state": "active",
  "valid_from": "2026-10-25T00:00:00Z",
  "valid_until": "2027-10-25T00:00:00Z"
}
```

上例所有字段均必填。`valid_until` 为 `null` 表示永久购买；其他值必须晚于 `valid_from`。

| 字段 | 约束和含义 |
| --- | --- |
| `event_id` | Launch 生成的 UUID；同一事件重试不变 |
| `connection_ref` / `environment` | 必须与签名密钥所属连接、环境一致 |
| `account_ref` | 稳定匿名账号引用；接收方以连接、环境和该引用共同识别客户 |
| `entitlement_ref` | 一次购买关系的稳定引用；全部撤销后重新购买使用新引用 |
| `launch_product_id` / `launch_plan_version_id` | Launch 的商品与不可变套餐版本 UUID |
| `product_ref` / `plan_ref` | 接入方配置的外部引用；1–128 个 ASCII 字符，允许字母、数字、`_`、`-`、`.`；不自动创建外部套餐 |
| `version` | 同一连接、环境、购买引用下单调递增的目标版本 |
| `desired_state` | `active`、`suspended`、`expired`、`revoked` |
| `change_reason` | `purchase`、`renewal`、`upgrade`、`refund`、`reversal`、`suspension`、`resumption`、`expiry`、`reconciliation` |

`account_ref`、`entitlement_ref` 使用 1–128 位字母、数字、下划线或短横线。`schema`、`type` 固定为示例中的值。接收方按 `desired_state` 和完整绝对期限应用结果，不根据原因自行加天数或撤销整份授权。

特别是退款：退掉续费或升级订单后仍可能保留有效基础购买，此时快照仍可为 `active`，但期限缩短或套餐改变。只有账本重算后不再有有效购买覆盖，才发送对应终止状态。

### 4.4 接收、回执与恢复

接收方必须在持久化事件 ID、原始请求体摘要及待执行任务之后，才返回 HTTP 202；已经同步完成的重复请求可以返回 HTTP 200。两者都返回下述回执结构。

```json
{
  "data": {
    "event_id": "7bf84b91-2749-4b01-a6a7-bd527c488ee8",
    "connection_ref": "merchant-01-sandbox",
    "environment": "sandbox",
    "entitlement_ref": "purchase_opaque_001",
    "version": 1,
    "payload_sha256": "<原始事件请求体的64位小写十六进制SHA-256摘要>",
    "status": "pending",
    "error_code": null
  },
  "request_id": "receipt-001"
}
```

摘要占位符需要替换为真实摘要。回执中上述字段均必填；`error_code` 无错误时为 `null`。查询 `GET /launch/v1/events/{event_id}` 返回相同结构，HTTP 200，状态可为：

| `status` | 含义 | Launch 的处理 |
| --- | --- | --- |
| `pending` | 已持久化，尚未完成实际授权变更 | 延后查询，不能标记同步完成 |
| `applied` | 授权服务确认目标状态已实际应用 | 核对全部关联字段和摘要后确认同步完成 |
| `rejected` | 确定无法应用，例如外部套餐不存在 | 停止自动重试，显示明确错误 |
| `manual_review` | 结果不确定或状态冲突，需要人工核对 | 保留原任务和事件，等待可审计处理 |

交付状态独立于同步回执；`applied` 不能直接当成客户应用的运行许可。

可靠性要求：

- 至少一次投递，接收方必须保证业务幂等；不能承诺网络层“只调用一次”。
- 同一事件 ID、同一原始请求体返回已有结果；同一 ID、不同请求体返回 HTTP 409 `IDEMPOTENCY_CONFLICT`。
- 旧版本不得覆盖新版本。同一购买版本的不同事件或不同目标不能静默接受，应返回 HTTP 409 `STATE_VERSION_CONFLICT` 供核对。
- 丢失 POST 响应或进程重启后，Launch 先 GET 原事件；明确返回 `EVENT_NOT_FOUND` 后才重发原 ID、原请求体。
- 远端实际发放结果不明确时，接入方也应使用自己的持久化幂等引用核对，不能重发一次就再生成一份授权。
- HTTP 429 遵循 `Retry-After`；超时和 5xx 退避恢复；超过上限进入人工核对。鉴权、配置和业务拒绝不得被当成同步成功。

建议错误码：`SIGNATURE_INVALID`（401）、`SIGNATURE_EXPIRED`（401）、`NONCE_REPLAYED`（409）、`SCOPE_MISMATCH`（403）、`EVENT_NOT_FOUND`（404）、`IDEMPOTENCY_CONFLICT`（409）、`STATE_VERSION_CONFLICT`（409）、`PLAN_UNKNOWN`（422）、`PURCHASE_TYPE_UNSUPPORTED`（422）、`RATE_LIMITED`（429）、`SERVICE_UNAVAILABLE`（503）。错误消息不得包含凭据。

### 4.5 授权交付

`GET /launch/v1/entitlements/{entitlement_ref}/delivery` 只能读取该连接及环境中的已有交付结果，不得把查询当作新发放命令。HTTP 200 示例：

```json
{
  "data": {
    "connection_ref": "merchant-01-sandbox",
    "environment": "sandbox",
    "entitlement_ref": "purchase_opaque_001",
    "version": 1,
    "status": "ready",
    "delivery": {
      "type": "license_key",
      "value": "EXAMPLE-ONLY-NOT-A-REAL-LICENSE",
      "expires_at": null
    }
  },
  "request_id": "delivery-001"
}
```

`status` 为 `pending`、`ready` 或 `unavailable`；非 ready 时 `delivery` 为 `null`。ready 时 `type` 必须为已声明的 `license_key`、`activation_url` 或 `instructions`，`value` 最多 4096 个 UTF-8 字节，`expires_at` 为 UTC 时间或 `null`。所有响应关联字段必须与 Launch 当前购买版本一致，旧版本交付不能当作当前结果。

授权码属于敏感数据；交付响应使用 `Cache-Control: no-store`，不记录正文，需要持久化时使用加密存储。激活 URL 仅接受连接配置允许的 HTTPS 来源；instructions 按纯文本展示，不执行 HTML 或脚本。已撤销、全额退款或无有效购买的账号不能继续通过 Launch 取得交付信息；已交付凭据的实际失效由外部系统执行。

## 5. Launch 自身拟新增或调整的 API

下表为拟议接口，不能用商户连接签名密钥代替 Launch 账号鉴权：

| 方法与路径 | 权限和用途 |
| --- | --- |
| `GET /api/v1/admin/licensing/connections` | 管理员读取连接及脱敏配置 |
| `POST /api/v1/admin/licensing/connections` | 管理员新增 `licentivo` 或 `custom_http` 连接 |
| `PUT /api/v1/admin/licensing/connections/{id}` | 管理员编辑；影响历史任务的地址/协议变更创建新连接版本 |
| `POST /api/v1/admin/licensing/connections/{id}/check` | 管理员验证能力、鉴权与环境 |
| `PUT /api/v1/admin/products/{id}/licensing` | 管理员配置商品在所选环境中的提供方和套餐映射；已有购买时禁止直接替换 |
| `GET /api/v1/me/products/{id}/delivery?environment=sandbox` | 当前客户读取自己在所选商品、环境中的自定义授权交付 |

后台 Cookie 写操作继续要求 CSRF；客户接口按实际会话校验账号、商品、环境、购买归属与当前状态，不能仅凭外部授权编号取交付。Licentivo 方式继续使用原账号 SDK 链路；新增 delivery 仅用于声明支持交付的自定义提供方，不恢复已移除的 `/credential` 接口。

管理接口的完整请求/响应 schema、字段必填条件和错误码，需与后端实现一起补入正式 OpenAPI；不能提前把本草案路径加入现有接口清单并宣称可用。

## 6. 代码实施顺序

### 第一阶段：提供方和数据模型

1. 在 `internal/ports` 定义通用商业授权适配器，包含连接检查、套餐校验、快照提交、事件查询和交付读取。设备与账号身份交换使用独立可选接口。
2. 将现有 Licentivo Bridge 和策略读取封装为默认适配器；新增 `internal/adapters/externallicensing` 实现本文 HTTP 协议。客户无需实现 Licentivo 的信任登记、策略目录或 runtime 协议。
3. 统一 Go 连接模型并新增 provider、协议版本、环境、能力和连接版本。可以先保留现有 `licentivo_connections` 物理表名，显式回填旧记录为 `licentivo`，降低外键迁移风险；旧接口通过兼容层读取同一数据。
4. 外部商品/套餐字符串引用单独建字段，不能塞进现有 `char(36)` Licentivo UUID 字段。商品、套餐版本、订单和任务保存可追溯的连接/映射快照。
5. 保留 Licentivo 的身份信任连接语义；自定义首版不使用仅按 environment 建键的现有信任表，不把它当成通用多提供方路由表。

### 第二阶段：交易与任务流程

1. 修改套餐发布、下单和报价校验，根据选中的适配器验证映射和支持的购买类型，消除对 `registerLaunchPlan` 的强制调用。
2. 保留现有金额计算、付款核验、购买覆盖账本与事务边界；只有经验证的商业变化才能排队。
3. 复用任务持久化、租约、幂等、退避和审计能力，按任务捕获的提供方、连接版本与协议分发。旧任务仍发送给原接收方。
4. 自定义流程按本文快照同步目标期限和状态。退款/撤款通过账本重算，不简单映射成“撤销全部授权”。
5. 根据提供方处理就绪状态和交付，去掉自定义分支对 Licentivo `LicenseBinding` 的要求。外部失败时保持 pending/review，不回退调用 Licentivo，也不自动放行功能。
6. 增加客户交付读取和管理员连接/检查接口；自定义设备和账号身份接口返回明确的不支持结果。

### 第三阶段：后台与接入文档

1. 后台增加“Licentivo / 自定义授权服务”选择、连接检查、外部套餐引用及处理错误展示。
2. 自定义商品的接入页展示外部授权服务说明和交付方式；不生成指向 Licentivo 的初始化代码。已有九语言 Launch SDK 不自动获得自定义 runtime 能力。
3. 正式发布时增加自定义授权 OpenAPI、签名测试向量、curl 请求/响应样例及薄适配器示例。示例只处理协议和调用商户系统，不实现许可证生成或校验。
4. 更新 README、现有 OpenAPI、手动接入指南与责任边界文档；将旧管理发 license 说明与当前账号交换流程区分，避免把已返回 410 的 `/credential` 写成可用路径。

## 7. 验收条件

- 未配置任何 Licentivo 凭据时，自定义商品可以完成配置检查、发布、付款、授权交付和退款；观测不到发往 Licentivo 的请求。
- 商户原有客户端确实可以使用其自有 SDK 验证交付；仅接口返回 200 不算完整验收。
- 重复通知、POST 响应丢失、进程重启和多工作进程不会重复发放或叠加期限；旧版本不会覆盖新版本。
- 续费、升级、部分购买被退回、全部购买被退回和确认撤款后，目标状态与账本一致。
- 跨账号、商品、连接或环境不能读取他人的交付；无效签名、过期请求和重放被拒绝。
- 未实现设备/共享登录扩展的提供方，后台和接入页不会声称支持这些能力。
- 旧库升级、备份恢复和历史任务继续指向原连接；现有 Licentivo 端到端回归通过。

该方案的核心后端、后台选择、授权交付与文档已实施；验证范围见[验证记录](../verification.md)。共享登录与设备扩展仍按可选扩展处理，不属于自定义提供方的首版能力。
