# 自定义授权系统接入指南

当前源码支持两种授权提供方：Licentivo 和 `custom_http`。商户在 **系统设置 → 授权系统** 统一选择并启用其中一个，所有商品和 sandbox/live 环境使用同一提供方；自定义连接与套餐映射仍按商品和环境分别配置。此功能不包含自建授权系统；商户继续使用自己的授权服务和 SDK。

现有 Licentivo 账号交换、九语言 SDK 和设备管理继续使用原流程。自定义商品通过本文协议同步购买状态，客户在账号中心读取授权交付，不进入 Licentivo SDK 流程。

## 快速开始

1. 在自己的服务实现下面四个 HTTP 接口，配置每个连接独立的 HMAC 签名密钥和标识。
2. 在“授权系统”选择“自定义授权服务”，新增连接，填写地址、环境、连接标识、key ID 和标准 Base64 密钥，验证后保存，点击“启用自定义授权服务”。
3. 创建商品和销售套餐草稿。购买类型和价格在 Launch 管理；授权规则在自有系统管理。
4. 选择商品及环境，选择连接，填写外部商品与套餐引用并保存。提供方由全站选择决定，不能为其他商品同时启用 Licentivo。
5. 在销售套餐页发布测试版，完成付款、交付、续费、升级、退款及故障恢复验收，再独立配置正式环境。

地址、环境、连接标识和网络访问策略创建后不可更改；需要时创建新连接。签名密钥可轮换，留空保留原密钥。存在另一授权系统的订单时，不能直接切换全站提供方，需要先完成迁移。切换后原套餐转为仅预览，需要重新发布；保存的连接凭证和映射保留。已有订单的商品不能直接切换连接，销售中的映射也不能直接更换；新价格或映射使用套餐新版本。

通过 API 配置时，先调用 `PUT /api/v1/admin/licensing/provider`，请求体为 `{"provider":"custom_http"}` 或 `{"provider":"licentivo"}`，再保存商品映射。未启用的提供方不能保存商品授权绑定或 Licentivo 套餐策略映射。

连接检查确认协议、鉴权、环境及支持的购买类型，不会替商户创建或确认外部套餐。正式收款前必须用真实外部套餐完成 sandbox 接入验收。

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

## Launch 管理与客户 API

这些接口位于商户部署的 Launch origin 下，不使用外部服务的 HMAC 密钥鉴权。管理员使用现有网站会话，写请求必须带 `X-CSRF-Token`。客户只能读取自己的购买。

| 方法与路径 | 用途 |
| --- | --- |
| `GET /api/v1/admin/licensing/connections` | 返回 `connections`、`bindings`、`mappings` 和 `default_provider`；连接列表只包含自定义连接，Licentivo 沿用原管理接口 |
| `POST /api/v1/admin/licensing/connections` | 验证并新增自定义连接 |
| `PUT /api/v1/admin/licensing/connections/{id}` | 编辑名称、允许的激活来源及签名配置；历史接收地址不变 |
| `POST /api/v1/admin/licensing/connections/{id}/check` | 重新检查；HTTP 200 中的 `last_check_error` 非 null 表示检查失败 |
| `PUT /api/v1/admin/products/{id}/licensing` | 为商品所选环境配置提供方、连接与外部套餐映射 |
| `GET /api/v1/me/products/{id}/delivery?environment=sandbox` | 当前客户读取当前有效、自定义购买的授权交付；Cookie 或 desktop Bearer 鉴权 |
| `GET /api/v1/licensing/documentation` | 此文档，无需登录 |
| `GET /api/v1/licensing/protocol` | 自定义服务需要实现的 OpenAPI，无需登录 |

新增自定义连接的请求体：

```json
{
  "provider": "custom_http",
  "name": "My licensing service",
  "base_url": "https://licenses.example.com",
  "environment": "sandbox",
  "connection_ref": "merchant-01-sandbox",
  "key_id": "key-1",
  "signing_key": "<32至128个随机字节的标准Base64编码>",
  "allow_private_network": false,
  "allow_local_http": false,
  "activation_origins": ["https://activate.example.com"]
}
```

响应 `data` 是脱敏后的连接对象，不返回签名密钥或 secret ID。allow_local_http 仅允许 sandbox 的 localhost/回环地址；私网 HTTPS 需要管理员显式允许。密钥留空仅限编辑已有连接。Licentivo 仍通过 `POST /api/v1/admin/connections/licentivo` 设置。

配置商品的请求体：

```json
{
  "provider": "custom_http",
  "environment": "sandbox",
  "connection_id": "<新增连接返回的id>",
  "mappings": [
    {
      "version_id": "<Launch套餐版本UUID>",
      "product_ref": "desktop-editor",
      "plan_ref": "professional-yearly-v1"
    }
  ]
}
```

选择 Licentivo 时发送 `{"provider":"licentivo","environment":"sandbox","mappings":[]}`，其产品/策略映射仍使用原接口。首次切换使已有发布变为预览，需要按新提供方重新检查并发布。mappings 可仅包含要新增或保留的版本；省略不会删除旧映射。

客户交付为 HTTP 200 的标准 data envelope；无购买返回 404，Licentivo 商品返回 409 `LICENSING_DELIVERY_UNSUPPORTED`，未同步、暂停、到期或全额退款返回 409 `ENTITLEMENT_NOT_READY`，外部暂不可用或回执不匹配返回 503。客户中心的“查看授权交付”会重新校验归属和状态，不把凭据写入 URL 或日志。

自定义商品的 Licentivo identity、device、integration-check 接口返回 409 `CUSTOM_LICENSING_SDK_REQUIRED`。旧 `/credential` 继续返回 410。应用应使用商户自己的 SDK，而不是把 Launch 的 `ready` 状态当作运行授权。

## 示例与验收

[Python 薄适配器示例](../examples/custom-licensing/README.md) 提供签名、nonce、事件去重和回执持久化的接收框架；实际授权处理需要接入商户系统。

自定义协议的 [OpenAPI](external-licensing-openapi.json) 与 Launch 自身的 [OpenAPI](openapi.json) 分别描述两个方向。接收方无需实现 Licentivo 的产品目录、策略目录或身份信任接口。

本地验证使用隔离 MySQL、真实 HTTP 适配器和模拟授权/支付服务，覆盖响应丢失、重启、续费、升级、退款、账号停用、回执不匹配及交付隔离。生产验收还需使用商户自己的授权服务和应用。未声明的设备、共享账号登录及离线能力不由 Launch 补齐。
