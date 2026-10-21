# Resend email configuration

Launch uses a persistent encrypted email queue for registration verification, password reset, and email-change confirmation. Temporary failures use backoff and retry. Other transactional notifications can reuse the connection and queue.

## 1. Create an account

Register at [Resend](https://resend.com/signup). API keys are server credentials. Do not include them in desktop applications, frontend code, screenshots, chats, or source control.

## 2. Verify a sender domain

1. Open [Domains](https://resend.com/domains).
2. Add a domain you control. A dedicated subdomain such as `mail.example.com` or `account.example.com` helps separate transactional email.
3. Add the DNS records currently supplied by Resend at your DNS provider. Do not copy another domain's values.
4. Wait for the domain to become verified.
5. Choose a sender such as `support@mail.example.com` under that verified domain.

See [verified domains](https://resend.com/docs/dashboard/domains/introduction).

## 3. Create a dedicated API key

1. Open [API Keys](https://resend.com/api-keys).
2. Create a key for this Launch site and label it accordingly.
3. Restrict it to the verified sender domain if that option is available.
4. Copy the generated `re_` key immediately; its complete value is not displayed again.

A separate key supports independent auditing, rotation, and revocation. See [API key management](https://resend.com/docs/dashboard/api-keys/introduction).

## 4. Save the connection in Launch

1. Sign in as an administrator.
2. Open **System settings > Email settings**.
3. Select an SMTP port and encryption mode. The default is **465 + implicit TLS**. Where 465 is unavailable, **587 + STARTTLS** is another supported choice.
4. Enter the sender and API key, then save.
5. Enter a test recipient, send a test message, and check both the inbox and Resend delivery status.

Settings apply immediately without restarting or reentering the administrator password. When updating only the sender later, leave the key blank to retain the stored key. The UI does not reveal the saved key.

| Setting | Value |
| --- | --- |
| Host | Default `smtp.resend.com`; editable |
| Port | Implicit TLS: `465`, `2465`; STARTTLS: `25`, `587`, `2587` |
| Encryption | Implicit TLS or STARTTLS; default 465 + implicit TLS |
| Username | Default `resend`; editable |
| Password | The saved Resend API key |

Defaults follow the [Resend SMTP guide](https://resend.com/docs/send-with-smtp). The host, username, port, encryption mode, sender, and key remain administrator configurable.

## 5. Test and accept delivery

1. Send a real test message through the saved connection. Success means the SMTP provider accepted the message; it does not guarantee inbox delivery.
2. Inspect the recipient, sender, and delivery result in Resend's Emails page.
3. Register a test customer with a mailbox you control.
4. Receive and complete the verification link.
5. Request a password-reset email and confirm delivery and link validity.
6. Check spam folders. Review SPF, DKIM, DMARC, and the sender subdomain when delivery is classified as spam.

Queued messages use a stable `Resend-Idempotency-Key` across retries to reduce duplicate deliveries after network timeouts.

## 6. Rotate or revoke the key

Create a new key in Resend, save it in Launch, and verify delivery before removing the old key. If compromise is suspected, revoke the affected key promptly and review sending records.

## Troubleshooting

| Symptom | Checks |
| --- | --- |
| Invalid API key format | Use a `re_` API key without whitespace or line breaks |
| Sender rejected | Verify the sender domain in Resend |
| Test email fails | Check port/encryption pairing, key validity, domain verification, sender, and provider records |
| Messages stay queued | Save the connection, inspect waiting/failed counts, and retry failed messages after fixing the cause |
| Sent but not received | Inspect Resend status, bounces, spam, and domain DNS |
| Change sender only | Enter the new address and save with the API key field empty |

The setting names are described in English here; the administration UI currently uses Simplified Chinese.
