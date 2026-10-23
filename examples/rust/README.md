# Rust 示例

准备 Rust 1.85+、GNU/MinGW（Windows）、libcurl 开发文件，并设置 `LICENTIVO_CURL_ROOT` 和相应 `PATH`，然后从统一包根目录运行：

```sh
cargo run --manifest-path examples/rust/Cargo.toml -- launch-config.json product.json
```

示例先尝试恢复会话，再在终端隐藏输入密码。默认仅内存保存登录；可注入安全 Store 实现跨进程记住登录。详细依赖、安全约束和 API 见 `sdk/rust/README.md`。
