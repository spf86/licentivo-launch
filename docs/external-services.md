# External service setup

This checklist covers the external services used by Licentivo Launch, account links, administration settings, and verification. Enter secrets only in the authenticated Launch administration area or private server configuration. Never put them in frontend code, application installers, screenshots, or a source repository.

## DeepL: ten customer languages

1. Open [DeepL API plans](https://www.deepl.com/pro-api) and register for API Free or API Pro. A normal website translation subscription may not include API access.
2. Open [API Keys & Limits](https://www.deepl.com/your-account/keys), create a key dedicated to this site, and set usage limits as needed.
3. In Launch administration, open **Languages & translation**, choose the corresponding Free/Pro tier, and select the language used for original merchant content.
4. Enter the key and select **Verify and save DeepL**. The authenticated administrator session supplies authorization; another password entry is not required.
5. Check character usage and queued tasks. Existing content is queued automatically, and later changes to brands, products, and plans update translations.

See [language configuration](multilingual-deepl.md) and the [official API key guide](https://support.deepl.com/hc/en-us/articles/360020695820-API-key-for-DeepL-API).

## Stripe: card payments

1. Create a merchant account at [Stripe registration](https://dashboard.stripe.com/register).
2. Obtain a test or live secret key from **Developers > API keys**. Keep the two environments separate.
3. Open **Administration > System settings > Payment settings** in Launch. Select the payment environment and supply the merchant account ID, secret key, webhook signing secret, and pinned API version.
4. Create the required payment/refund webhook endpoint using the [Stripe webhook documentation](https://docs.stripe.com/webhooks), then save its signing secret in Launch.
5. Verify checkout, notifications, payment queries, and refunds in test mode before configuring live credentials.

## PayPal: PayPal payments

1. Sign in to the [PayPal Developer Dashboard](https://developer.paypal.com/dashboard/). Live payments also require an eligible PayPal Business account.
2. Under **Apps & Credentials**, select Sandbox or Live, create a REST app, and copy that environment's client ID and client secret.
3. Review [REST API getting started](https://developer.paypal.com/api/get-started/) and [webhooks](https://developer.paypal.com/api/rest/webhooks/). Add a webhook for capture/refund events and copy its webhook ID.
4. In Launch's payment settings, select the matching mode and enter the merchant ID, client ID, client secret, and webhook ID.
5. Complete acceptance with a [sandbox buyer account](https://developer.paypal.com/sandbox-testing/accounts/) before configuring Live separately.

## Licentivo: software licensing

Launch uses one Licentivo connection. Its sandbox/live labels distinguish payment environments, not separate Licentivo services. Use an isolated Licentivo test product for integration testing.

1. Sign in to [Licentivo](https://www.licentivo.com/) and create your products and licensing policies.
2. Create a **read/write management API key** with product/policy catalog access and license management permissions. A read-only key can display policies but cannot issue licenses after a payment.
3. Open **Administration > System settings > Licentivo connection**, enter the key, and select **Connect and read policies**.
4. Choose a policy by name when creating or editing each Launch sales plan. Launch reads the product, policy version, device limits, features, offline/heartbeat rules, floating seats, and device-release limits.

The service origin is fixed to `https://www.licentivo.com`. There is no administrator-defined licensing URL or licensing sandbox selector. See [licentivo-contract.md](licentivo-contract.md).

## Resend: account email

Launch's account-email integration uses [Resend](https://resend.com/) for verification, password reset, and email changes.

1. Create a [Resend account](https://resend.com/signup).
2. Add a domain you control using the [domain guide](https://resend.com/docs/dashboard/domains/introduction), configure the supplied DNS records, and wait for verification.
3. Create an [API key](https://resend.com/docs/dashboard/api-keys/introduction) dedicated to this Launch instance.
4. In **Administration > System settings > Email settings**, select the SMTP port/encryption mode, verified sender address, and API key. Save; no restart is required.
5. Enter a test recipient and send a real test email to check the connection, TLS, key, sender domain, and provider acceptance.
6. Complete registration and password recovery with a real mailbox to verify delivery and spam handling.

Defaults are host `smtp.resend.com`, username `resend`, port `465`, and implicit TLS, with the API key as the SMTP password. Administrators can edit host, username, port, and encryption. The key is encrypted with the instance master key and is not returned in plaintext. See [resend-email.md](resend-email.md).

## External acceptance before launch

Local test doubles verify requests, queues, encryption, and state changes; they do not establish that a third-party account works. Before live sales, verify real DeepL output, Stripe and PayPal test purchases/refunds, Licentivo license issuance, and Resend verification/password-reset delivery.

Administration labels above are English descriptions of the settings; the current administration UI uses Simplified Chinese.
