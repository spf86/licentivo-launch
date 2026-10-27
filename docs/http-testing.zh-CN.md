# 客户测试服务器的 HTTP 接入说明

[选择语言](http-testing/README.md)

本文中的服务器地址、IP 和主机名，均属于客户自己部署的 Launch。客户不需要进入 Licentivo 或开发者的内网。客户自己的公网测试服务器和内网测试服务器均可使用本说明。

SDK 按 `merchant_origin` 中的地址选择协议：`https://` 使用 HTTPS；`http://` 仅在下述测试权限允许时使用。HTTPS 请求失败不会自动降级为 HTTP。开关不会改写地址，也不会安装 TLS 证书。

## 两个独立开关，默认均关闭

| 开关 | 客户在哪里设置 | 控制范围 |
| --- | --- | --- |
| 单个 API Key 的 **允许 HTTP（测试用）** | Licentivo → 开发与接入 → API Key → 创建 API Key，或已有 Key 的接入设置 | 允许使用这一个 Key 为 HTTP 地址的 Launch 站点创建或更新账号授权连接。只读 Key 不会因此获得写权限。轮换 Key 会保留此设置。 |
| **允许 SDK 使用 HTTP（仅测试环境）** | 客户自己的 Launch → 系统设置 → 网站与安全 → SSL 证书与访问协议 | 允许该 Launch 接受 HTTP SDK 账号请求并生成 HTTP 测试接入代码。HTTP 运行身份兑换仅限 `sandbox`；`live` 仍要求 HTTPS。 |

两个开关均由客户自行管理，无须 Licentivo 平台管理员操作，也不需要将整个 Licentivo 平台切换为开发模式。它们只影响客户的 Launch 地址。Launch 和官方授权 SDK 调用 Licentivo 时仍使用 HTTPS。

## 客户如何配置测试服务器

1. 更新 Licentivo 服务端、Launch 服务端和对应语言的 SDK。旧版 SDK 若只允许 localhost HTTP，必须升级；服务端设置不能改变已安装 SDK 的地址校验。
2. 在客户自己的 Licentivo 工作空间创建专用的 **读写** 测试 Key，或打开有效 Key 的 **接入设置**，选择 **允许 HTTP（测试用）**。
3. 在客户自己的 Launch **网站与安全** 设置中填写测试客户端能访问的地址，例如 `http://192.168.1.20:18080`、`http://test-server:18080` 或客户自己的测试域名。开启 **允许 SDK 使用 HTTP（仅测试环境）** 并保存。地址只包含协议、主机和可选端口，不带路径、查询参数、片段或末尾斜杠。
4. 使用该 Key 将 Launch 重新连接到 Licentivo。读取到产品和策略仅说明目录可读，还须确认账号授权连接已建立。
5. 在 **程序接入 → 接入检查与代码** 选择 **sandbox 测试环境**，运行检查，再复制新生成的代码。检查通过后，HTTP 测试参数会自动填入。使用测试账号和测试购买。

客户自己的内网不需要公网 IP 或公网 DNS，但测试客户端必须能解析并访问所填的 Launch 地址。`localhost` 指运行 SDK 的那台机器，其他机器须使用服务器的 IP 或内部主机名。Licentivo 只登记站点来源与固定签名公钥，不会回连该地址，也不会从中抓取公钥。Launch 和在线授权客户端仍须能够出站访问 Licentivo 的 HTTPS 接口。完全断网的内网不能进行在线账号授权联调。

## SDK 如何选择 HTTP 或 HTTPS

开启客户服务器的 HTTP 测试后，生成的 Launch 公开配置相当于：

```json
{
  "schema_version": 1,
  "merchant_origin": "http://192.168.1.20:18080",
  "launch_product_id": "11111111-1111-4111-8111-111111111111",
  "licentivo_product_id": "22222222-2222-4222-8222-222222222222",
  "environment": "sandbox",
  "allow_http": true
}
```

地址决定协议。`allow_http: true` 仅允许 `sandbox` 使用 HTTP 商户地址，不会将 HTTPS 地址降级为 HTTP。官方授权产品的配置仍为 HTTPS。九种语言的直接调用参数如下：

| SDK | 明确的 HTTP 测试参数 |
| --- | --- |
| Go | `launchclient.Config{Environment: "sandbox", AllowHTTP: true}` |
| Python | `Application.connect(..., environment="sandbox", allow_http=True)` |
| JavaScript / TypeScript | `Application.connect(..., {environment: "sandbox", allowHTTP: true})` |
| C# | `Application.Connect(..., "sandbox", allowHttp: true)` |
| Java | `Launch.connect(..., "sandbox", true)` |
| Rust | `Application::connect_with_http(..., "sandbox", true)` |
| Ruby | `Application.connect(..., environment: 'sandbox', allow_http: true)` |
| C | `launch_application_connect_http(..., "sandbox", 1, 0, NULL)` |
| C++ | `Application::ConnectHTTP(..., "sandbox")` |

这些参数需要新版 SDK 支持。SDK 不会收到或查询商户的管理 API Key。Launch 只导出公开配置，并在服务端检查自身的 HTTP 开关；仅修改客户端参数不能绕过关闭的服务端开关。旧构造方式为兼容保留原有的 sandbox 本机 HTTP 例外，但不能绕过服务端默认关闭的开关。

## 关闭测试或切换 HTTPS

HTTP 不加密密码、账号令牌等数据，客户自己的内网也可能被监听或入侵。管理员通过 HTTP 打开 Launch 后台并输入 API Key 时，浏览器到 Launch 的请求也可能泄露 Key，即使 Launch 到 Licentivo 使用 HTTPS。测试请使用专用凭据；正式部署和敏感管理操作使用 HTTPS。

为 Launch 配置可用的 HTTPS 监听或 TLS 代理后，应更新实际商户地址、按需重新连接 Licentivo，再重新生成接入配置并更新客户端。已复制或编译进程序的 HTTP 地址不会自动变化。测试结束后关闭两个 HTTP 开关。无论开关如何设置，`live` SDK 身份兑换仍须使用 HTTPS。

关闭 Launch 开关后，后续 HTTP SDK 请求返回 `SDK_HTTP_DISABLED`；`live` 的 HTTP 身份请求返回 `SDK_HTTPS_REQUIRED`。已签发的离线许可仍遵循 Licentivo 正常的到期和撤销规则。关闭 API Key 的 HTTP 权限后，该 Key 不能再创建或更新 HTTP 站点连接，但不会删除已有连接、撤销购买或使已签发许可失效。需要停止已有连接授权时，须另行停用该连接。

## 连接失败时如何排查

Launch 会显示失败阶段（读取产品或策略、查询连接、创建 sandbox/live 连接）、具体原因，以及 Licentivo 返回的错误码和请求编号。保存的失败信息在刷新后仍可查看。错误内容不会显示管理 API Key。

| 错误 | 处理方法 |
| --- | --- |
| `LAUNCH_HTTPS_REQUIRED` | 为 Launch 实际使用的那个 API Key 开启 HTTP，或配置可用的 HTTPS 商户地址。 |
| `LICENSE_KEY_INVALID` | 检查 Key 是否完整、到期、吊销，以及创建者和工作空间是否有效。 |
| `LICENSE_KEY_FORBIDDEN` | 使用有效 Owner 创建的读写 Key。HTTP 权限不会增加写权限。 |
| `SDK_HTTP_DISABLED` | 在客户自己的 Launch 开启 SDK HTTP 测试开关，或改用 HTTPS。 |
| `SDK_HTTPS_REQUIRED` | `live` 必须使用 HTTPS；HTTP 测试选择 `sandbox`。 |
| `LICENTIVO_TIMEOUT` / `LICENTIVO_NETWORK_ERROR` | 检查客户测试机器的出站网络、DNS、防火墙和代理设置。 |
| `LICENTIVO_TLS_ERROR` | 检查系统时间、受信证书和 HTTPS 代理，并保持 TLS 证书校验开启。 |
