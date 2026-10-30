# 手动开发接入指南（9 种语言，无需 AI）

本指南适用于 Licentivo 提供方。已有其他授权系统的商户，可在后台“授权选择与自定义”选择自定义服务，按[自定义授权 API 与对接流程](external-licensing.md)接入，并继续使用自有授权系统的 SDK。

> 当前未发布改动采用账号身份交换，具体职责和迁移约束见 [购买期限与账号 SDK 边界](account-purchase-boundary.md)。Launch 计算购买期限；Licentivo SDK 执行授权。

只安装对应语言的 Launch 接入包。Launch 包内部包含官方 SDK、默认适配器和链接配置；官方授权地址固定为 `https://www.licentivo.com`。程序使用购买者在当前 Launch 商店注册的账号登录。

## 取得已填写的初始化代码

1. 在 Launch 后台完成软件、销售套餐和 Licentivo 授权策略的关联，并在对应环境发布。
2. 打开 **程序接入 → 接入检查与代码**，选择软件、语言和测试/正式环境。
3. 点击“读取产品信息并生成接入代码”。Launch 自动读取站点当前公开地址、已有产品映射和签名公钥。
4. 复制页面上的连接函数到自己的工程，按下文手动编写界面和业务调用。也可以查看自动填好的参数后自行调用对应语言的 `Connect/connect`。

无需手填两个内部产品 ID，无需生成 `launch-config.json` 或 `product.json`。页面可保存的是源码连接函数。服务器私有 `config.yaml` 只供 Launch 服务启动，不能随客户软件发布。

站点地址来自当前实例的有效公开地址（启动配置 `public_url`，或后台网站设置中的有效公开地址）。它可以是域名或包含端口的 IP 地址，但须带协议；正式接入使用有效 HTTPS。监听地址 `http_addr` 不是客户访问地址。更换站点地址、产品映射或环境后，从当前站点重新取得连接函数；旧站点的会话和缓存不得作为新站点授权。

两个产品编号是系统已有的内部对应关系：Launch 软件商品和 Licentivo 授权产品。接入页根据所选软件自动填入；购买者只输入商店账号。签名公钥由服务器使用已配置的 Licentivo 连接读取，管理密钥和客户授权凭据不会进入源码。

## `Connect/connect` 的作用

`Connect/connect` 是不同语言对“创建 Launch 应用客户端”这一操作的命名。Go、C# 和部分原生接口使用首字母大写的 `Connect`，Python、JavaScript、Ruby 等语言按各自风格使用 `connect`。后台生成的 `connectLaunch()`、`connect_launch()` 或 `LaunchConnection.Open()` 是把公开参数预先填好的项目函数，内部调用的就是对应语言入口。

连接只完成初始化：它验证商店地址、产品映射、环境与签名公钥，创建 Launch 客户端，并配置包内官方 Licentivo SDK。它不会验证客户身份，也不会直接开放付费功能。完整顺序为：

```text
Connect/connect → Login 或 Restore → RunFeature → Logout / Close
                                       └──────────→ ReleaseDevice（仅明确解绑设备时）
```

应用运行期间应保留同一个客户端对象，让 SDK 正常处理心跳与浮动席位。普通关闭保留设备绑定；客户明确退出与 ReleaseDevice 兼容入口调用同一官方 SDK 退出流程。

## 安装与初始化入口

0.1.7 的九种语言公开安装验证已全部通过（9/9），下表安装方式均已验证。[GitHub Release](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.7) 也提供原生包和离线文件，使用方式见[下载包安装说明](integration-packages.md#install-a-downloaded-distribution)。

| 语言 | 只安装 Launch | 页面提供的连接函数 | 包内直接初始化 API |
| --- | --- | --- | --- |
| Go | `go get github.com/spf86/licentivo-launch-sdk/pkg/launchclient@v0.1.7` | `connectLaunch()` | `launchclient.Connect` |
| Python 3.10+ | `python -m pip install licentivo-launch==0.1.7` | `connect_launch()` | `Application.connect` |
| C# / .NET 8+ | `dotnet add package Licentivo.Launch --version 0.1.7` | `LaunchConnection.Open()` | `Application.Connect` |
| Java 17+ | Maven/Gradle：`com.licentivo.launch:launch-client:0.1.7` | `LaunchConnection.open()` | `Launch.connect` |
| Node.js 20.3+ | `npm install @licentivo/launch@0.1.7` | `connectLaunch()` | `Application.connect` |
| Rust 1.85+ | `cargo add licentivo-launch@=0.1.7` | `connect_launch()` | `Application::connect` |
| Ruby 3.1+ | `gem install licentivo-launch --version 0.1.7` | `connect_launch` | `Application.connect` |
| C11 | 从 [GitHub Release](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.7) 下载原生包，CMake 导入 | `connect_launch()` | `launch_application_connect` |
| C++17 | 使用同一 Launch 原生包 | `connectLaunch()` | `Application::Connect` |

公开源码和各语言 README 见 [Launch SDK 仓库](https://github.com/spf86/licentivo-launch-sdk)，原生包与离线文件见 [v0.1.7 Release](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.7)，公开安装结果见 [九种语言注册表验证](https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-verification.json)。每个包自己的 README 提供安装命令和 API。JavaScript 包包含 TypeScript 声明。普通语言依赖由包管理器解析，例如 Python 的 cryptography、C# 的 BouncyCastle 和 Ruby 的 base64；无需安装官方 SDK 包。

Go 公开模块名为 `github.com/spf86/licentivo-launch-sdk`，导入路径为 `github.com/spf86/licentivo-launch-sdk/pkg/launchclient`，应用无需本地 replace。Windows 原生包采用 amd64 GNU/MinGW，另有 Linux amd64 原生包。Ruby 平台包和 Rust/native 的 Windows 分发包含 curl；其他平台源码构建需要常规 C11、curl/OpenSSL 开发依赖。平台工具链说明见各包 README。

## 登录与受保护操作

以下片段使用上一步复制的连接函数。`email`、`password` 来自自己的登录界面；不得将密码写入源码或配置。`export` 必须替换为套餐策略中实际存在的功能名。

### Go

```go
app, err := connectLaunch()
if err != nil { return err }
defer app.Close()
if _, err := app.Login(ctx, email, password); err != nil { return err }
return app.RunFeature("export", func() error {
    return exportReport()
})
```

`ctx` 使用应用生命周期上下文，不能在登录请求结束时立即取消心跳。网络操作放在后台工作线程。恢复调用 `Restore(ctx)`；退出账号调用 `Logout(ctx)`；释放设备调用 `ReleaseDevice(ctx)`。

### Python

```python
with connect_launch() as app:
    app.login(email, password)
    result = app.run_feature("export", export_report)
```

`restore()` 恢复已有安全会话；无会话时显示登录界面。`logout()` 清理账号；`release_device()` 明确释放设备。不要让同步网络操作阻塞桌面事件循环。

### C# / .NET

```csharp
await using var app = LaunchConnection.Open();
await app.Login(email, password);
await app.RunFeature("export", async () => {
    await ExportReport();
});
```

使用 `await Restore()`、`await Logout()` 和 `await ReleaseDevice()`。保持应用存活期间的 SDK 生命周期，使用异步界面流程。

### Java

```java
try (var app = LaunchConnection.open()) {
    app.login(email, passwordChars);
    app.runFeature("export", () -> exportReport());
}
```

密码使用 `char[]`。`restore()` 无会话时返回 `NO_SESSION` 失败，应显示登录界面；其它失败不能视为授权。`logout()` 和 `releaseDevice()` 是不同操作。try-with-resources 负责关闭。

### JavaScript / Node.js / TypeScript

```javascript
const app = connectLaunch();
try {
  await app.login(email, password);
  const result = await app.runFeature('export', () => exportReport());
} finally {
  await app.close();
}
```

`restore()` 没有会话时返回 `null`；正常恢复后才允许执行业务。`logout()` 的布尔返回值表示远端退出是否确认，设备释放失败应显示错误。代码运行在 Node.js、Electron 主进程或可信后端；浏览器和 Electron 渲染进程不保存授权凭据。

### Rust

```rust
let mut app = connect_launch()?;
app.login(&email, &password)?;
let result = app.run_feature("export", || {
    export_report()?;
    Ok(())
})?;
app.close();
```

回调返回 `Result<T, Failure>`。`restore()`、`logout()`、`release_device()` 返回结果，必须处理失败。`Drop` 也负责关闭运行时。默认原生核心由 build.rs 编译和链接，不需自己链接官方 SDK。

### Ruby

```ruby
app = connect_launch
begin
  app.login(email, password)
  result = app.run_feature('export') { export_report }
ensure
  app.close
end
```

`restore` 恢复会话，`logout` 退出账号，`release_device` 释放设备。保护操作使用代码块；异常不能转换为成功。

### C

```c
static int export_action(void *context) {
    return export_report(context); /* Return 1 only on success. */
}

launch_application *app = connect_launch();
if (!app) return 0;
int ok = launch_application_login(app, email, password);
if (ok) ok = launch_application_run_feature(app, "export", export_action, context);
/* Check launch_application_error(app) on failure; never log credentials. */
launch_application_close(app);
return ok;
```

`launch_application_restore` 为三态：`1` 已恢复、`0` 没有会话、`-1` 失败。只有没有会话时正常显示登录；拒绝或异常不能当作成功。`logout` 和 `release_device` 的返回值也必须检查。

CMake 只导入 Launch：

```cmake
find_package(LicentivoLaunch CONFIG REQUIRED)
add_executable(myapp main.c)
licentivo_launch_link(myapp)
```

### C++

```cpp
auto app = connectLaunch();
app.Login(email.c_str(), password.c_str());
app.RunFeature("export", [&] { exportReport(); });
```

`Restore()`、`Logout()`、`ReleaseDevice()` 遵循 C++ 异常语义。RAII 析构负责关闭。CMake 使用 `licentivo_launch_link(myapp CXX)`，Launch 自动处理 SDK 链接和分发包内运行库。

## 生命周期、安全存储与错误

创建连接对象只完成初始化。程序必须登录或恢复成功，并在每次业务操作中使用功能保护回调。订单已付款、会话存在或按钮可点击都不能代替 SDK 验证。

默认会话仅保存在内存。记住登录需要安全存储：Go、Python、C# 和原生 C/C++ 提供 Windows 当前用户 DPAPI；Java、Node、Rust 和 Ruby 提供使用调用方密钥的 AES-GCM 存储。跨平台应用应对接系统凭据库。不能把密钥和密文放在同一普通目录。

普通关闭保留用户主动记住的会话并停止运行时。退出账号先调用官方 SDK 统一退出，再清理 Launch 自有账号记录。ReleaseDevice 是同一流程的兼容入口，远端撤销待完成时不能提示释放成功。不要清理官方 SDK 的缓存目录或退出恢复日志。Launch 的会话保护不代表官方 SDK 的全部设备文件都已加密，应限制缓存目录权限。

| 状态 | 程序处理 |
| --- | --- |
| 账号或密码失败 | 显示通用登录失败，不泄露账号是否存在 |
| EMAIL_VERIFICATION_REQUIRED | 引导完成商店邮箱验证 |
| ENTITLEMENT_PENDING | 显示同步中，按包的有限重试和 Retry-After 等待 |
| ENTITLEMENT_NOT_PURCHASED | 引导购买或切换账号 |
| ENTITLEMENT_EXPIRED | 引导续费，不使用旧凭据绕过拒绝 |
| ENTITLEMENT_SUSPENDED | 停止受保护功能，联系商家 |
| 设备数量已满 | 引导管理或明确释放设备 |
| 网络/HTTP 5xx | 仅可使用 SDK 已验证、仍在有效期内的离线授权 |

明确的 401/403/404、账号不符、取消或限流不能变成离线授权成功。首次激活和禁止离线的策略仍需联网。服务端域名/IP 无法访问时应显示连接问题，不能偷偷连接其它站点。

## 验收与参考

验证真实登录、所属账号授权、受保护操作、重启恢复、有效离线与离线到期、缓存篡改、退出、账号切换和设备释放。本文不要求使用 AI。测试工具和外部验收边界见 [acceptance.md](acceptance.md)；本地包和构建说明见 [integration-packages.md](integration-packages.md)。

配置检查、代码生成和本地测试不会自动证明真实支付、授权签发或官网激活成功。外部商店和支付沙箱需配置后单独验收。
