# 九语言实际业务小程序

这些程序调用 Launch Application 的登录、计量执行和已知结果结算入口，使用真实 HTTP 服务与 MySQL。业务回调调用 `long_business.py`，持续写入进度，最后生成三份各 100 行的报告。每笔任务数量为 3。

177 上的固定工作目录为 `/home/spf86/launch-sdk-renewal-20261101`，隔离 Launch 为 18092、新装 Licentivo 为 18091。运行脚本需要该目录原有的私有配置、专用测试账号和 SDK 缓存；它们没有导出。九个专用账号现已停用，恢复测试前需由管理员启用。不要在其他实例直接套用 SQL 或清理脚本。

`run-matrix.py` 先逐个完成账号登录，再并发执行业务；每种语言使用独立测试客户、许可证和缓存。短任务运行 2 秒，长任务运行 920 秒。两轮均用新进程和同一 operation ID 重试，检查回调不重复、三份产物哈希一致、用量正确。`verify-installed-artifacts.py` 额外验证安装后的 C/C++ 库及最终 Python wheel。

C、C++、Go、Java、C#、Rust 需要先构建；Ruby 使用重新编译的私有 C 库，Python 使用实际安装的 wheel。`run-sdk-tests.py` 记录 Linux 构建和测试步骤。生产代码不依赖这些验收程序。
