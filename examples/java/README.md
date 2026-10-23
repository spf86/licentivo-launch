# Java 示例

在统一源码包根目录设置 JDK 17+ 的 `JAVA_HOME`，运行：

```powershell
./examples/java/run.ps1 ./launch-config.json ./product.json
```

脚本编译官方 SDK 和 `sdk/java` 包，再运行 `Main.java`。默认内存登录，密码通过控制台隐藏输入。完整生命周期、安全存储接入和验证说明见 `sdk/java/README.md`。
