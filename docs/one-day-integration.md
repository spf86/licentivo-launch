# Integrate Licentivo Launch in one day

**Add software sales, account login and licensing to your existing app in just one working day.** Launch supplies the storefront, customer accounts, payment integration and nine-language application packages; you connect your product and protect your business actions.

This is a practical implementation plan for a standard integration using Licentivo and an existing application. The suggested allocation totals eight working hours; it describes the work to complete, rather than a measured completion time for every project. Payment-provider onboarding and production acceptance follow their own schedules.

## Have these ready before you start

- An existing application and a working build toolchain for one supported language: Go, Python, C#, Java, JavaScript/Node.js, Rust, Ruby, C or C++.
- A server, MySQL database and client-reachable HTTPS domain. Use the server's [binary installation guide](https://github.com/spf86/licentivo-launch/blob/main/docs/binary-installation.md) and [configuration reference](https://github.com/spf86/licentivo-launch/blob/main/docs/configuration.md).
- A compatible Licentivo account/backend, a product and policy, and a management key with the required write scope. Check the [application integration requirements](software-sdk-integration.md) and the server's [Licentivo connection guide](https://github.com/spf86/licentivo-launch/blob/main/docs/licentivo-contract.md).
- Sandbox credentials for one supported payment provider and a reachable webhook URL. Start with the server's [payment guide](https://github.com/spf86/licentivo-launch/blob/main/docs/payments.md). Email delivery and translation services can be configured when your workflow needs them; the basic integration does not require every optional service.

## Your working-day plan

| Suggested time | Work | Completion evidence |
| --- | --- | --- |
| Hour 1 | Deploy Launch and configure the store | The server starts, the administrator signs in, and your brand and product appear on the storefront. |
| Hour 2 | Connect a payment sandbox and map the plan | A published sandbox plan uses your intended Licentivo product/policy and payment account; the integration page validates the public product settings. |
| Hours 3–6 | Add login and protected actions to your application | Your own application builds with one Launch package, logs in with a real store account, and executes protected business code through the SDK guard. |
| Hours 7–8 | Verify the purchase-to-use flow | A sandbox payment is confirmed, its purchase snapshot is applied, and a protected operation succeeds in your app. Restart and explicit logout behave as expected. |

### 1. Deploy and brand the storefront

Download the server package for your operating system, run `launch init`, configure the database and public URL, and start the server. Sign in and change the initial administrator password. Configure the store brand and create the software product.

The frontend is embedded in the executable. Installation does not require a separate frontend build. Follow the server's [installation and operations guide](https://github.com/spf86/licentivo-launch/blob/main/docs/installation.md) for your deployment environment.

### 2. Connect one product, plan and payment sandbox

Save the Licentivo management connection, select the existing product and policy for a sales-plan version, and configure one payment provider's sandbox credentials and webhook. Publish the sandbox plan.

Open **程序接入 → 接入检查与代码**, select the software, language and `sandbox` environment, and read the product information. Launch fills in the merchant URL, product mapping and signing public keys. Resolve reported configuration failures before using the generated connection function.

### 3. Integrate the existing application

Install only the Launch package for your language. Copy the generated connection function into the project, add your login interface, and wrap each protected business operation in the language's `RunFeature` equivalent. Use a feature name that actually exists in your Licentivo policy.

```text
Connect/connect → Login or online Restore → RunFeature → Close or explicit Logout
```

Launch obtains a short-lived identity assertion; the embedded official SDK exchanges it with Licentivo and decides runtime access. Applications do not fetch a long-term license key from Launch. The old `/credential` endpoint returns HTTP 410; use the current account-exchange package described in the [application integration guide](software-sdk-integration.md).

Use the [manual nine-language guide](manual-integration.md), [Chinese quick start](application-quickstart.zh-CN.md) and language README for exact APIs. Alternatively, use **运营工具 → AI 快速接入** to generate the same installation and lifecycle instructions for your own coding tool. See [AI integration](ai-integration.md).

Keep the application object alive for its runtime. Start with in-memory login if remembered login is not required. When adding remembered login, use the package's protected store or the operating system's credential vault. Ordinary close preserves binding; explicit logout handles both SDK exit and Launch session revocation, retaining pending recovery tasks until confirmed.

### 4. Verify the result in your own application

Use a dedicated test customer and complete a real provider sandbox checkout. Confirm the owned order is paid and its purchase snapshot reaches `applied`. Sign in to your application and execute one real protected action through the official SDK guard.

Close and restart the application, test login or online restoration, and verify explicit logout. Test a denied feature and confirm its business callback does not execute. A healthy server, generated code or successful login alone does not complete this integration.

## What you have at the end of the day

- A branded storefront with one purchasable sandbox plan.
- A verified sandbox payment synchronized with the intended Licentivo product and policy.
- Your application using the customer's store account and the embedded SDK's feature guard.
- Recorded purchase, protected-operation, restart and logout results, with any unexecuted checks identified.

Use the [acceptance matrix](acceptance.md) to complete renewal/refund, account switching, device limits, offline behavior and each supported release target before production rollout. The one-day implementation plan does not replace that release acceptance.
