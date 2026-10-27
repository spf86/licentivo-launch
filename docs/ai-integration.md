# 使用 AI 接入 Licentivo Launch

> 当前未发布改动采用账号身份交换，具体职责和迁移约束见 [购买期限与账号 SDK 边界](account-purchase-boundary.md)。Launch 计算购买期限；Licentivo SDK 执行授权。

Launch 后台的 **运营工具 → AI 快速接入** 会生成一份可交给 AI 编程工具的英文任务说明。它使用当前商店、已有软件和已发布套餐的真实公开配置，并要求 AI 安装公开的 Launch 客户端包、调用真实 API、把权限检查放到业务操作中，再完成接入测试。

程序接入页的 **接入检查与代码 / AI 快速接入** 导航也可进入同一功能。手动开发不需要使用 AI，可直接阅读 [九种语言手动接入指南](manual-integration.md)。

## 生成前准备

1. 在 Launch 中创建软件和销售套餐，并把套餐关联到 Licentivo 产品与授权策略。
2. 将套餐发布到要测试的 `sandbox` 或 `live` 环境。
3. 确认 Launch 的公开地址可被客户软件访问，正式环境使用 HTTPS。
4. 在 **程序接入 → 接入检查与代码** 读取产品信息，确认产品映射和签名公钥通过检查。
5. 明确要保护的真实业务动作，例如 `export`、`save` 或 `render`，不要只写“整个软件”。

没有完成产品配置时可以生成草稿，但草稿会明确标出待填写项，不能作为可交付接入配置。完成产品与环境配置后应重新生成。

## 在后台生成提示词

1. 选择 Go、Python、C#、Java、JavaScript / Node.js、Rust、Ruby、C 或 C++。Web 后端方案使用 Go，不另算一种 SDK 语言。
2. 选择 Windows、Linux、macOS 或适用平台，以及桌面、Web 后端或后台服务项目。
3. 使用英文填写已有项目结构、启动方式、界面框架和要保护的业务功能。不要填写密码、密钥、客户数据或私有源码。
4. 选择已有软件和 `sandbox` / `live` 环境。Launch 自动读取当前商店地址、Launch 与 Licentivo 的产品映射、环境和签名公钥。
5. 检查预览中的包版本、安装命令、文档链接和已填写连接函数，然后复制或下载提示词。
6. 把提示词交给自己的 AI 编程工具，让它在你的工程中完成修改、编译和本地测试。

Launch 只在浏览器中生成文本。它不会自动连接任何 AI 服务，不会上传项目，也不会代替开发者执行生成的程序。

## 提示词会告诉 AI 在哪里下载 SDK

当前 0.1.6 的九种语言公开安装验证已全部通过（9/9），下列注册表命令和原生下载均已验证。Java 的 Maven Central 文件及四份签名、Rust 的 crates.io 精确版本和全部后台生成的初始化代码也已完成独立消费者检查；[v0.1.6 GitHub Release](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.6) 同时提供原生包和离线文件。

生成的提示词必须包含与所选语言对应的、固定版本的公开安装方式：

```text
Go:         go get github.com/spf86/licentivo-launch-sdk/pkg/launchclient@v0.1.6
Python:     python -m pip install licentivo-launch==0.1.6
C# / .NET: dotnet add package Licentivo.Launch --version 0.1.6
Java:       com.licentivo.launch:launch-client:0.1.6
Node.js:    npm install @licentivo/launch@0.1.6
Rust:       cargo add licentivo-launch@=0.1.6
Ruby:       gem install licentivo-launch --version 0.1.6
C / C++:    https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.6
```

公开源码、各语言 README 和当前发布状态位于：

- [Licentivo Launch SDK 仓库](https://github.com/spf86/licentivo-launch-sdk)
- [v0.1.6 GitHub Release](https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.6)
- [九种语言公开安装验证](https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/registry-verification.json)
- [版本与包说明](integration-packages.md)
- [九种语言手动接入指南](manual-integration.md)
- [应用接入架构与安全要求](software-sdk-integration.md)
- [接入验收清单](acceptance.md)

只安装所选语言的 Launch 包。v0.1.6 已内置官方 Licentivo SDK v0.11.6、默认适配器和链接配置，不再单独安装第二份官方 SDK。

## 提示词会告诉 AI 如何接入

已配置的提示词包含后台生成的连接函数。该函数已经填入商店公开地址、Launch 产品与 Licentivo 产品映射、签名公钥和支付环境。AI 不需要猜测或手填两个内部产品编号，也不需要创建 `launch-config.json` 或 `product.json`。

服务端的私有 `config.yaml` 只用于运行 Launch 网站，绝不能放进客户应用、提示词或安装包。

AI 必须按以下生命周期修改客户软件：

```text
Connect/connect → Login or Restore → RunFeature → Logout / Close
                                      └──────────→ ReleaseDevice（仅明确解绑设备时）
```

- `Connect/connect` 创建 Launch 客户端，并初始化内置官方 SDK；它本身不代表账号已经登录或功能已经授权。
- `Login` 使用客户在当前商店注册的邮箱和密码取得该账号的产品凭据。
- `Restore` 从安全存储恢复已记住的会话；没有会话时显示登录，明确拒绝不能当作离线成功。
- `RunFeature` 必须包住导出、保存、渲染或后台任务等真实业务动作。只禁用按钮不能替代执行位置的检查。
- `Logout` 先调用官方 SDK 统一退出，再清理账号会话；`ReleaseDevice` 是同一明确退出流程的兼容入口。`Close` 普通关闭保留设备绑定。
- 客户端对象应在应用运行期间保持存活，以便 SDK 执行心跳、浮动席位和运行时规则。

Web 应用只在可信后端执行授权检查。Node.js / Electron 将凭据和 SDK 放在主进程或服务端，浏览器与渲染进程不保存授权秘密。

## AI 应交付什么

提示词要求 AI 完成并报告以下内容：

1. 检查现有项目的语言版本、依赖管理、启动与退出流程，以及需要保护的真实业务操作。
2. 使用上面的固定版本命令安装一个 Launch 包，并核对对应语言 README 中的真实接口。
3. 加入客户账号登录、错误提示和安全会话恢复，不在源码或日志中保存密码和完整许可证。
4. 使用后台提供的连接函数初始化，不自行发明产品编号、签名公钥或配置文件。
5. 在每个受保护业务动作的执行位置使用 `RunFeature` 或对应语言方法，并正确处理计次与重试。
6. 区分账号失败、邮箱待验证、权益同步中、未购买、到期、暂停、设备已满、网络故障和离线到期。
7. 实现普通关闭、账号退出和明确设备释放，并按商店、账号、产品和环境隔离缓存。
8. 运行适合该项目的编译与测试，最后列出修改文件、功能映射、实际结果和客户验收步骤。

AI 输出代码后，开发者仍需核对功能名与 Licentivo 策略完全一致，并在真实的 sandbox 商店账号上完成端到端测试。

## 不进入提示词的内容

提示词只包含可公开的商店来源、产品映射、环境、包版本和签名公钥。以下内容不得进入提示词：

- 客户邮箱、密码、登录会话和刷新令牌；
- 完整许可证、设备凭据和本地授权缓存；
- Licentivo 管理 API Key；
- Stripe、PayPal、Waffo 或 Paddle 密钥；
- 数据库连接、`master_key` 和服务端 `config.yaml`；
- 不需要交给外部工具的私有源码、业务数据或日志。

Launch 的公开配置检查、AI 草稿和本地编译都不能证明真实支付、许可证签发或外部激活已经成功。支付沙箱、商店账号和 Licentivo 环境需单独验收。

## 接入后的验收清单

- 新客户能够登录，未验证邮箱与错误密码得到明确但不过度泄露信息的提示。
- 已购买账号可以运行目标功能；未购买、到期或暂停账号无法执行同一业务代码。
- 重启后只有成功恢复会话并通过授权检查时才能继续使用。
- 有效离线策略可以工作，离线许可到期后拒绝；明确的 401/403/404 不降级为离线成功。
- 功能次数只在业务成功时正确提交，失败与重试不会重复扣减。
- 普通关闭保留设备绑定；明确退出先调用官方 SDK 统一退出，保留待撤销日志并报告真实结果。
- 浮动席位心跳在应用运行时持续，非正常退出后的回收时间符合当前授权策略。
- 切换商店、账号、产品或环境不会复用另一身份的缓存。

更完整的自动与外部验收边界见 [acceptance.md](acceptance.md)。

## 在源码分发包中使用提示词生成器

`licentivo-launch-0.1.6-source.zip` 保留下列文件，便于在自己的工具中生成相同结构的提示词：

- `internal/httpapi/web/integration-prompts.js`：不依赖页面或服务端的提示词生成模块；
- `scripts/integration-prompts.test.mjs`：本地 Node 测试；
- `internal/httpapi/web/integration-state.js` 与 `scripts/integration-state.test.mjs`：配置草稿隔离和旧验证结果清理；
- 本指南、各语言接入包、示例和验收说明。

模块导出 `integrationTargets` 与 `buildIntegrationPrompt(options)`。选项包括 `language`、`appKind`、`platform`、`projectNotes`、`feature`、`merchantOrigin`、`environment` 和已验证的 `report`，返回 `{ text, ready, title, code }`。不提供 `report` 时只生成草稿；不能通过手工篡改报告绕过公开配置检查。

在支持 ES 模块的开发工具中可以从源码包根目录生成本地草稿：

```powershell
@'
import { readFile, writeFile } from 'node:fs/promises';
const source = await readFile('internal/httpapi/web/integration-prompts.js', 'utf8');
const { buildIntegrationPrompt } = await import(
  'data:text/javascript;base64,' + Buffer.from(source).toString('base64')
);
const prompt = buildIntegrationPrompt({
  language: 'python',
  appKind: 'desktop',
  platform: 'windows',
  projectNotes: 'Existing Python desktop application. Add account sign-in and protect report exports.',
  feature: 'export'
});
await writeFile('integration-prompt.md', prompt.text, 'utf8');
console.log(prompt.title);
'@ | node --input-type=module
```

这个命令只生成未配置草稿，不调用 AI 或授权服务。真实接入应回到自己的 Launch 实例，选择已有软件和环境重新生成带公开配置的版本。

验证本地生成器：

```powershell
node --test scripts/integration-prompts.test.mjs scripts/integration-state.test.mjs
```
