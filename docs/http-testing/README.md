# HTTP / HTTPS testing · 14 languages / 14 种语言

Choose a complete guide below. All examples refer to the customer’s own deployment. / 请选择完整说明；所有示例均指客户自己的部署环境。

| Language / 语言 | Code / 代码 | Guide / 说明 |
| --- | --- | --- |
| English | `en` | [HTTP testing on customer servers and private networks](../http-testing.md) |
| Deutsch | `de` | [HTTP-Tests auf Kundenservern und in privaten Netzwerken](../http-testing.de.md) |
| Italiano | `it` | [Test HTTP sui server del cliente e nelle reti private](../http-testing.it.md) |
| 简体中文 | `zh` | [客户测试服务器的 HTTP 接入说明](../http-testing.zh-CN.md) |
| 繁體中文 | `zh-hant` | [客戶測試伺服器的 HTTP 接入說明](../http-testing.zh-hant.md) |
| हिन्दी | `hi` | [ग्राहक के सर्वर और निजी नेटवर्क पर HTTP परीक्षण](../http-testing.hi.md) |
| Español | `es` | [Pruebas HTTP en servidores del cliente y redes privadas](../http-testing.es.md) |
| العربية | `ar` | [اختبار HTTP على خوادم العميل والشبكات الخاصة](../http-testing.ar.md) |
| Français | `fr` | [Tests HTTP sur les serveurs du client et les réseaux privés](../http-testing.fr.md) |
| বাংলা | `bn` | [গ্রাহকের সার্ভার ও ব্যক্তিগত নেটওয়ার্কে HTTP পরীক্ষা](../http-testing.bn.md) |
| Português | `pt` | [Testes HTTP em servidores do cliente e redes privadas](../http-testing.pt.md) |
| Bahasa Indonesia | `id` | [Pengujian HTTP pada server pelanggan dan jaringan privat](../http-testing.id.md) |
| اردو | `ur` | [صارف کے سرور اور نجی نیٹ ورک پر HTTP کی آزمائش](../http-testing.ur.md) |
| 日本語 | `ja` | [顧客サーバーとプライベートネットワークでの HTTP テスト](../http-testing.ja.md) |

## Maintaining these guides / 维护说明

All 14 guides must be updated together when HTTP behavior changes. Edit `translations.json`, review every language, and update its `source_sha256` after reviewing the English source. API field names, SDK options, JSON examples and error codes are generated unchanged for every language. / HTTP 行为变化时须同步更新全部 14 种语言；API 字段、SDK 参数、JSON 示例和错误码保持一致。

```sh
node scripts/http-testing-docs.mjs
node scripts/http-testing-docs.mjs --check
```

The SDK source export and server release archive include every listed guide and this language index. / SDK 源码导出和服务端发布压缩包均包含全部译文及本目录。
