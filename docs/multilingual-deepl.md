# Ten customer languages and DeepL configuration

The customer website supports Simplified Chinese, English, Japanese, Korean, German, French, Spanish, Brazilian Portuguese, Italian, and Russian.

The merchant maintains one source language. Changes to the brand, software products, and plans are saved locally and queued for translation into the other customer languages. Customers choose a language in the header. When a translation is missing, the site shows source content and a fallback notice without hiding the product or preventing purchase.

## Register for DeepL API

A normal DeepL translation account may not include API access.

1. Select API Free or API Pro at [DeepL API plans](https://www.deepl.com/pro-api). Eligibility and quotas are determined by DeepL.
2. Open **Account > API Keys & Limits**, or use the [direct account page](https://www.deepl.com/your-account/keys).
3. Create a key dedicated to this site. Keep it out of browser code, application installers, screenshots, chats, and Git.
4. Set provider-side usage limits as needed. Launch reads usage and quota through `/v2/usage`.

See the [official key guide](https://support.deepl.com/hc/en-us/articles/360020695820-API-key-for-DeepL-API). Requests use `Authorization: DeepL-Auth-Key ...`; Launch does not put the key in a URL.

## Connect from Launch

1. Sign in as an administrator and open **Languages & translation**.
2. Select the correct tier: API Free uses `https://api-free.deepl.com`; API Pro uses `https://api.deepl.com`.
3. Select the source language for future brand, product, and plan content.
4. Enter the key and choose **Verify and save DeepL**. The current administrator session supplies authorization.
5. After connection succeeds, existing content is queued automatically. Future content saves update the affected translations.

The service origin follows the tier; there is no manually entered DeepL URL. This avoids sending Free keys to Pro endpoints or credentials to another server.

## Translation scope

- Brand name, tagline, and description.
- Product names, summaries, descriptions, feature lists, and system requirements.
- Sales-plan names. Technical feature identifiers and licensing parameters are not translated.
- Customer-facing fixed text in storefront, download, authentication, account, purchase, renewal, upgrade, order, refund, and device pages.

Logos, theme colors, prices, currency codes, software versions, download URLs, email addresses, slugs, provider identifiers, and licensing identifiers retain their values. The administration interface currently uses Simplified Chinese. Merchant-authored content uses the configured source language.

## Jobs, usage, and storage

- Source content is committed to MySQL before asynchronous translation, so a provider outage does not lose it.
- SHA-256 source hashes prevent repeated translation of unchanged content.
- Retries process missing target languages without redoing completed ones.
- Failed jobs back off automatically. After repeated failure, the administration area flags them for attention. Correct the key, tier, quota, or network, then regenerate translations if needed.
- Keys are encrypted `SecretRecord` values using the instance master key. Configuration reads and customer APIs do not return the key.
- GORM models create the required tables; no separate SQL migration file is needed.

## Troubleshooting

| Symptom | Checks |
| --- | --- |
| Cannot connect | Correct API tier and API key, rather than the DeepL login password |
| Zero quota or translation failure | Key status and usage limits in API Keys & Limits |
| Source content remains after switching language | Pending jobs and the fallback notice; inspect retries |
| Old translations no longer match the source language | Save the new source and regenerate translations |
| Suspected key disclosure | Disable the old key, create a new one, and verify/save it in Launch |

Local tests use test doubles for queues, locale mapping, encryption, and translated reads. Real translation quality, account quota, and network connectivity require a merchant-supplied key and separate acceptance.
