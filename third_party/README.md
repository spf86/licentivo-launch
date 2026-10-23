# Third-party licenses

Launch's own code uses the root [MIT license](../LICENSE). Dependencies retain their original licenses. The source and container include the notices below.

| Module | Version | License files |
| --- | --- | --- |
| `github.com/go-sql-driver/mysql` | 1.9.3 | `go-sql-driver-mysql.LICENSE` (MPL 2.0) |
| `gorm.io/gorm` | 1.31.1 | `gorm.LICENSE` (MIT) |
| `gorm.io/driver/mysql` | 1.6.0 | `gorm-mysql.LICENSE` (MIT) |
| `gopkg.in/yaml.v3` | 3.0.1 | `yaml-v3.LICENSE` (MIT/Apache 2.0) |
| `filippo.io/edwards25519` | 1.1.0 | `edwards25519.LICENSE` (BSD 3-Clause) |
| `github.com/jinzhu/inflection` | 1.0.0 | `inflection.LICENSE` (MIT) |
| `github.com/jinzhu/now` | 1.1.5 | `now.LICENSE` (MIT) |
| `golang.org/x/crypto` | 0.48.0 | `x-crypto.LICENSE`, `x-crypto.PATENTS` (BSD 3-Clause/patent grant) |
| `golang.org/x/sys` | 0.41.0 | `x-sys.LICENSE`, `x-sys.PATENTS` (BSD 3-Clause/patent grant) |
| `golang.org/x/text` | 0.34.0 | `x-text.LICENSE`, `x-text.PATENTS` (BSD 3-Clause/patent grant) |
| Go runtime | Build toolchain version | `go.LICENSE`, `go.PATENTS` (BSD 3-Clause/patent grant) |
| [Feather icons](https://github.com/feathericons/feather) | Bundled SVG subset | `feather-LICENSE` (MIT) |

Dependency sources are unmodified. `go.mod` and `go.sum` pin module versions. Source for the MySQL driver is available at [go-sql-driver/mysql v1.9.3](https://github.com/go-sql-driver/mysql/tree/v1.9.3); other modules can be retrieved by module path and pinned version. Update notices against the actual release bill of materials.

The separately prepared SDK under ignored `examples/vendor/` has its own license/notice files. Preserve those notices when distributing SDK-based applications.

Payment provider icons under `internal/httpapi/web/brands/` are unmodified public brand assets from the official [Stripe](https://stripe.com/), [PayPal](https://www.paypal.com/), [Waffo](https://waffo.com/), and [Paddle](https://www.paddle.com/) websites, retrieved on 2026-10-22. They identify supported integrations. Their names and marks remain the property of their respective owners and are not covered by this project's MIT license; no endorsement is implied.
## Embedded application SDKs

Launch application packages embed the reviewed MIT-licensed Licentivo SDK 0.11.0. The private Go copy is under pkg/launchclient/internal/licentivo; the other package copies are under sdk/*/vendor (Java classes are compiled from sdk/java/src/licentivo). Each copy carries the upstream LICENSE, NOTICE and version. sdk/managed-sdk-manifest.json records upstream and embedded-file SHA-256 values; changes are limited to private module/native-library locations. Authorization and cryptographic logic remain in the upstream implementation.

The maintainer-only preparation tool is scripts/prepare-managed-sdk.mjs. Consumers install only the Launch artifact and need no separate official SDK package. Windows Ruby, Rust and native distributions also include curl and the license notices for its bundled dependencies. Normal third-party language dependencies retain their package-manager licenses.
