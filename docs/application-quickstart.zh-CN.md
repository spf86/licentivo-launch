# 程序接入快速开始

> 当前未发布改动采用账号身份交换，具体职责和迁移约束见 [购买期限与账号 SDK 边界](account-purchase-boundary.md)。Launch 计算购买期限；Licentivo SDK 执行授权。

九种语言均只安装对应的 Launch 接入包。商店账号、授权凭据获取、官方 SDK 适配器和链接配置由包内部处理。官方授权地址固定为 `https://www.licentivo.com`；Launch 商店地址按当前安装实例自动填写。

## 后台准备

1. 在 Licentivo 创建授权产品和策略，在 Launch 保存对应的管理连接。
2. 创建软件和销售套餐，通过策略选择完成已有产品映射，并在所需环境发布。
3. 在服务器私有 `config.yaml` 中配置实际对外 `public_url`，或在后台网站设置中保存有效公开地址。客户访问地址与 `http_addr` 监听地址不是同一个概念。
4. 打开 **程序接入 → 接入检查与代码**，选择软件、语言和环境，点击读取产品信息。

Launch 自动取得当前站点地址、软件对应的授权产品信息和签名公钥。无需接入者手动填写两个内部产品 ID，无需导入、生成或下载 JSON 配置文件。缺少映射或公钥时会显示需要完成的设置，不会伪造可用参数。

## 手动开发，不用 AI

复制页面上参数已填写的连接函数到自己的工程，只安装所选语言的 Launch 包，再自行编写登录界面和受保护业务。

| 语言 | 初始化入口 |
| --- | --- |
| Go | `launchclient.Connect` |
| Python | `Application.connect` |
| C# | `Application.Connect` |
| Java | `Launch.connect` |
| JavaScript / Node.js（含 TypeScript 声明） | `Application.connect` |
| Rust | `Application::connect` |
| Ruby | `Application.connect` |
| C | `launch_application_connect` |
| C++ | `Application::Connect` |

完整安装命令、登录、恢复、功能保护、退出与设备释放示例见 [9 种语言手动开发指南](manual-integration.md) 和各语言包 README。Web 后端另有 Go 示例，不额外算一种语言。

## 使用 AI 编程工具

打开 **运营工具 → AI 快速接入**，选择已有软件、语言、平台和功能，生成英文提示词。生成时会重新读取当前实例地址和已有产品信息，并附上对应语言已填写参数的直接初始化函数。接入者无需补写产品 ID 或制作配置文件。

项目说明请用英文填写。复制提示词并提供现有工程与 Launch 接入包，由自己的 AI 工具完成集成。Launch 不向 AI 服务发送内容。未配置好软件时仍可生成明确标注不可用于真实登录的草稿，待后台设置完成后重新生成。

详见 [AI 快速接入](ai-integration.md)。接入包版本为 `0.1.7`，内置官方 SDK `0.11.7`；公开源码、各语言安装方式和发布状态见 [Launch SDK 仓库](https://github.com/spf86/licentivo-launch-sdk)。不用 AI 时，按[手动开发指南](manual-integration.md)完成相同流程。

## 程序运行与验收

程序使用购买者在当前商店的账号登录。连接对象初始化只完成参数准备；账号登录后，由包获取该账号的凭据并启动官方 SDK。每次受保护操作必须放在功能校验回调中执行。

默认会话只在内存中。记住登录需使用包提供的受保护存储或系统凭据库；密码不能持久保存。普通关闭、退出账号和释放设备是不同操作。官方 SDK 的设备文件仍需限制目录权限，Launch 会话加密不等于全部缓存已加密。

只有临时网络或 HTTP 5xx 故障可尝试仍然有效、已被 SDK 验证的离线授权；明确拒绝、到期、暂停和账号不符不能离线绕过。首次激活和要求联网的策略仍需联网。

完成真实登录、授权获取、功能执行、重启、断网与离线到期、缓存篡改、账号切换、续费退款和设备限制验收。配置检查或本地测试成功，不代表真实支付和官网激活已验收。工具与边界见 [验收说明](acceptance.md)。


## 客户测试服务器使用 HTTP

客户自己的内网或公网测试服务器可以使用 HTTP，但须分别开启该 API Key 的 HTTP 站点权限和客户 Launch 的 SDK HTTP 测试开关，再选择 sandbox 生成新代码。地址决定协议；开关不改地址，也不会自动降级 HTTPS。详见[双开关、SDK 参数与 HTTPS 切换说明](http-testing.zh-CN.md)。
