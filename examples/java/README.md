# Java 示例

在统一源码包根目录设置 JDK 17+ 的 `JAVA_HOME`，运行：

```powershell
./examples/java/run.ps1 ./launch-config.json ./product.json
```

脚本编译 `sdk/java` 包及其中内置的官方 SDK 0.11.2，再运行 `Main.java`；无需另装官方 SDK JAR。默认内存登录，密码通过控制台隐藏输入。此参考 CLI 使用 JSON 输入，正式应用可直接复制 Launch 提供的连接函数。公开 0.1.3 包安装命令、完整生命周期、安全存储接入和验证说明见 `sdk/java/README.md`。
