# C# 官方 SDK 运行时验收

```powershell
dotnet run --project sdk/csharp-runtime-tests/Launch.RuntimeTests.csproj
```

需要 .NET 8 SDK/ASP.NET Core 8 运行时，官方 Licentivo SDK 0.11.1 源码已内置于 Launch 包。依赖缺失时构建失败，不跳过测试。

5 组测试使用本机随机端口 HTTP 服务、随机 Ed25519 签名密钥和**默认官方 Runtime**，覆盖在线激活、创建新 Application 后的离线恢复、功能拒绝、篡改/缺失缓存拒绝、401/403/404/账号不匹配清理，以及明确释放设备。Windows 同时使用新建 DPAPI Store 实例恢复持久会话；其他系统使用共享内存 Store 验证应用实例重建。没有连接生产授权服务，也不使用假的授权 Runtime。测试创建的临时目录在完成后清除。
