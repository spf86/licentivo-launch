# Third-party licenses

Launch's own code uses the root [MIT license](../LICENSE). Dependencies retain their original licenses. The source and container include the notices below.

| Module | Version | License files |
| --- | --- | --- |
| `github.com/go-sql-driver/mysql` | 1.9.3 | `go-sql-driver-mysql.LICENSE` (MPL 2.0) |
| `gorm.io/gorm` | 1.31.1 | `gorm.LICENSE` (MIT) |
| `gorm.io/driver/mysql` | 1.6.0 | `gorm-mysql.LICENSE` (MIT) |
| `filippo.io/edwards25519` | 1.1.0 | `edwards25519.LICENSE` (BSD 3-Clause) |
| `github.com/jinzhu/inflection` | 1.0.0 | `inflection.LICENSE` (MIT) |
| `github.com/jinzhu/now` | 1.1.5 | `now.LICENSE` (MIT) |
| `golang.org/x/crypto` | 0.48.0 | `x-crypto.LICENSE`, `x-crypto.PATENTS` (BSD 3-Clause/patent grant) |
| `golang.org/x/sys` | 0.41.0 | `x-sys.LICENSE`, `x-sys.PATENTS` (BSD 3-Clause/patent grant) |
| `golang.org/x/text` | 0.34.0 | `x-text.LICENSE`, `x-text.PATENTS` (BSD 3-Clause/patent grant) |
| Go runtime | Build toolchain version | `go.LICENSE`, `go.PATENTS` (BSD 3-Clause/patent grant) |

Dependency sources are unmodified. `go.mod` and `go.sum` pin module versions. Source for the MySQL driver is available at [go-sql-driver/mysql v1.9.3](https://github.com/go-sql-driver/mysql/tree/v1.9.3); other modules can be retrieved by module path and pinned version. Update notices against the actual release bill of materials.

The separately prepared SDK under ignored `examples/vendor/` has its own license/notice files. Preserve those notices when distributing SDK-based applications.
