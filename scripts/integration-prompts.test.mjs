import assert from "node:assert/strict";
import {existsSync, readFileSync, statSync} from "node:fs";
import test from "node:test";

const root = new URL("../", import.meta.url);
const source = readFileSync(new URL("internal/httpapi/web/integration-prompts.js", root), "utf8");
// The browser file is ESM without a repository-wide package.json. A data import
// also works on Node 20, before automatic .js module-syntax detection.
const {integrationTargets, buildIntegrationPrompt} = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);
const pin = Buffer.from(Array.from({length: 32}, (_, index) => index)).toString("base64url");
const cjk = /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}\p{Script=Bopomofo}\u3000-\u303f\uff00-\uffef]/u;
function report(language = "go") {
  return {
    ready: true, language,
    config: {
      schema_version: 1, merchant_origin: "https://merchant.example", environment: "sandbox",
      launch_product_id: "11111111-1111-4111-8111-111111111111",
      licentivo_product_id: "22222222-2222-4222-8222-222222222222",
    },
    product_config: {base_url: "https://www.licentivo.com", product_id: "22222222-2222-4222-8222-222222222222", trusted_keys: {"actual-signing-key": pin}},
  };
}
function exportedJSON(result) {
  return [...result.text.matchAll(/```json\n([\s\S]*?)\n```/g)].map(match => JSON.parse(match[1]));
}
function draft(value, language = "go") {
  const result = buildIntegrationPrompt({language, report: value});
  assert.equal(result.ready, false);
  assert.match(result.title, /draft.*configuration required/);
  assert.equal(exportedJSON(result)[0].merchant_origin, "<MERCHANT_HTTPS_ORIGIN>");
  assert.equal(exportedJSON(result)[1].trusted_keys["<SIGNING_KEY_ID>"], "<COPY_REAL_ED25519_BASE64URL_PUBLIC_KEY_FROM_LICENTIVO>");
  assert.ok(!result.text.includes(pin), "Invalid report must not partly export public settings");
  return result;
}

test("HTTP test code carries explicit opt-in for each language and cannot be used in live", () => {
  const expected={go:'AllowHTTP: true',web:'AllowHTTP: true',python:'allow_http=True',javascript:'allowHTTP: true',csharp:'allowHttp: true',java:'"sandbox", true',rust:'connect_with_http',ruby:'allow_http: true',c:'launch_application_connect_http',cpp:'ConnectHTTP'};
  for(const language of Object.keys(integrationTargets)) {
    const value=report(language);value.config.merchant_origin='http://customer-test-server:18080';value.config.allow_http=true;
    const result=buildIntegrationPrompt({language,report:value,merchantOrigin:value.config.merchant_origin,environment:'sandbox'});
    assert.equal(result.ready,true,language);
    assert.ok(result.code.content.includes(expected[language]),language);
    value.config.environment='live';
    assert.equal(buildIntegrationPrompt({language,report:value}).ready,false,language+' live');
  }
});

test("all nine language wrappers and the separate Go Web option point to real source", () => {
  assert.deepEqual(Object.keys(integrationTargets).sort(), ["c", "cpp", "csharp", "go", "java", "javascript", "python", "ruby", "rust", "web"]);
  for (const [language, target] of Object.entries(integrationTargets)) {
    assert.ok(statSync(new URL(target.wrapperPath + "/README.md", root)).isFile());
    assert.ok(statSync(new URL(target.examplePath, root)).isFile());
    const result = buildIntegrationPrompt({language});
    assert.equal(result.ready, false);
    assert.ok(result.text.includes(target.wrapperPath + "/README.md"));
    assert.ok(result.text.includes(target.examplePath));
    assert.ok(result.text.includes(target.publicReadme));
    assert.match(result.text, /https:\/\/github\.com\/spf86\/licentivo-launch-sdk\/releases\/tag\/v0\.1\.6/);
    assert.match(result.text, /https:\/\/www\.licentivo\.com\/launch-docs\.html#developer/);
    assert.equal(Object.isFrozen(target), true);
  }
  assert.ok(Object.isFrozen(integrationTargets));
  assert.match(buildIntegrationPrompt({language: "web"}).text, /not a tenth SDK language/);
});

test("draft is useful without any external configuration, key or server", () => {
  const result = draft(null);
  assert.match(result.text, /may implement the code structure and local fixture tests now/);
  assert.match(result.text, /Do not run commands containing placeholders, automatically purchase plans/);
  assert.match(result.text, /generate fake product IDs\/public keys/);
  assert.equal(exportedJSON(result)[0].environment, "sandbox");
  assert.equal(exportedJSON(result).length, 2);
  for (const value of [undefined, false, "raw secret response", [], {}, {ready: "true"}, {ready: false}, {ready: 1}]) draft(value);
});

test("only matching ready report exports reconstructed public JSON, not report metadata", () => {
  for (const language of Object.keys(integrationTargets)) {
    const value = report(language);
    value.product_config.app_version = "1.2.3";
    Object.assign(value, {
      commands: "LEAK-COMMAND-SECRET", checks: [{message: "LEAK-CHECK-SECRET"}],
      example_path: "LEAK-PATH-SECRET", error: {message: "LEAK-ERROR-SECRET"},
      access_token: "LEAK-TOP-SECRET", checked_at: "LEAK-TIME-SECRET", toJSON: () => { throw Error("must not serialize report"); },
    });
    const result = buildIntegrationPrompt({language, report: value});
    assert.equal(result.ready, true);
    assert.match(result.title, /public configuration validated/);
    assert.doesNotMatch(result.title, /acceptance.*passed/);
    assert.deepEqual(exportedJSON(result), [value.config, value.product_config]);
    assert.doesNotMatch(result.text, /LEAK-/);
    assert.match(result.text, /does not establish real payment, license activation or release acceptance/);
  }
  draft(report("python"));
  draft({...report(), language: "Go"});
});

test("unknown or secret public configuration fields downgrade the whole report", () => {
  for (const section of ["config", "product_config"]) {
    for (const field of ["license_key", "access_token", "management_key", "password", "device_id", "device_name", "cache_path", "proxy_url", "allow_http", "extra"]) {
      const value = report(); value[section][field] = "LEAK-PRIVATE-VALUE";
      assert.ok(!draft(value).text.includes("LEAK-PRIVATE-VALUE"));
    }
    // Sanitized reports have no legacy runtime fields, even empty ones.
    const value = report(); value[section].license_key = ""; draft(value);
  }
  const value = report(); value.product_config.trusted_keys["kid"] = {access_token: "LEAK-PRIVATE-VALUE"};
  assert.ok(!draft(value).text.includes("LEAK-PRIVATE-VALUE"));
});

test("configuration version, required fields, product IDs and environment fail closed", () => {
  for (const change of [
    {schema_version: 2}, {schema_version: true}, {schema_version: "1"},
    {environment: "test"}, {environment: "LIVE"},
    {launch_product_id: "not-a-uuid"}, {licentivo_product_id: "33333333-3333-4333-8333-333333333333"},
  ]) { const value = report(); Object.assign(value.config, change); draft(value); }
  for (const key of Object.keys(report().config)) { const value = report(); delete value.config[key]; draft(value); }
  for (const key of ["base_url", "product_id", "trusted_keys"]) { const value = report(); delete value.product_config[key]; draft(value); }
  for (const section of ["config", "product_config"]) {
    for (const invalid of [null, [], "LEAK-SECRET", Object.create({schema_version: 1})]) { const value = report(); value[section] = invalid; draft(value); }
  }
});

test("merchant origins only allow HTTPS or explicit sandbox loopback, never credentials or URL payload", () => {
  for (const merchant of [
    "http://public.example", "https://user:LEAK-PRIVATE-VALUE@merchant.example",
    "https://merchant.example/path", "https://merchant.example?access_token=LEAK-PRIVATE-VALUE", "https://merchant.example/#LEAK-PRIVATE-VALUE",
    "javascript:LEAK-PRIVATE-VALUE", "https://merchant.example\\@evil.example", " https://merchant.example", "https://merchant.example\n",
    "http://127.0.0.2", "http://127.1", "http://localhost.evil.example", "https://merchant.example/a/..",
  ]) { const value = report(); value.config.merchant_origin = merchant; assert.ok(!draft(value).text.includes("LEAK-PRIVATE-VALUE")); }
  for (const merchant of ["http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080", "https://merchant.example/"]) {
    const value = report(); value.config.merchant_origin = merchant;
    assert.equal(buildIntegrationPrompt({report: value}).ready, true);
    value.config.environment = "live";
    if (merchant.startsWith("http:")) draft(value);
  }
  for (const licensing of ["https://elsewhere.example", "http://www.licentivo.com", "https://www.licentivo.com/api", "https://www.licentivo.com?key=LEAK-PRIVATE-VALUE"]) {
    const value = report(); value.product_config.base_url = licensing; draft(value);
  }
});

test("public keys require bounded canonical base64url Ed25519 keys and bounded key IDs", () => {
  for (const keys of [
    {}, [], "secret", null, {kid: "bad"}, {kid: pin + "="}, {kid: "A".repeat(42) + "B"},
    {kid: "A".repeat(42)}, {kid: "A".repeat(44)}, {kid: 1}, {"": pin}, {["k".repeat(129)]: pin},
    {["钥".repeat(43)]: pin}, {"line\nfeed": pin},
    Object.fromEntries(Array.from({length: 33}, (_, index) => ["key" + index, pin])),
  ]) { const value = report(); value.product_config.trusted_keys = keys; draft(value); }
  const good = report(); good.product_config.trusted_keys = {["钥".repeat(42)]: pin};
  assert.equal(buildIntegrationPrompt({report: good}).ready, true);
  for (const appVersion of ["a".repeat(81), "钥".repeat(27), null, {}, "1.0\nLEAK-PRIVATE-VALUE"]) {
    const value = report(); value.product_config.app_version = appVersion; draft(value);
  }
});

test("untrusted accessors and toJSON cannot execute or sneak into exported report", () => {
  let called = false;
  const value = report();
  Object.defineProperty(value, "commands", {get() { called = true; throw Error("no"); }});
  value.toJSON = () => { called = true; throw Error("no"); };
  assert.equal(buildIntegrationPrompt({report: value}).ready, true);
  const invalid = report();
  Object.defineProperty(invalid.config, "merchant_origin", {get() { called = true; return "https://merchant.example"; }});
  draft(invalid);
  const keyGetter = report();
  Object.defineProperty(keyGetter.product_config.trusted_keys, "actual-signing-key", {get() { called = true; return pin; }});
  draft(keyGetter);
  assert.equal(called, false);
});

test("strict option enums and input limits use fixed errors without reflecting input", () => {
  for (const [name, invalid] of [
    ["language", "LEAK-PRIVATE-VALUE"], ["language", "__proto__"], ["language", null],
    ["appKind", "mobile"], ["appKind", {}], ["platform", "android"], ["platform", null],
    ["projectNotes", "x".repeat(601)], ["projectNotes", {}], ["feature", "x".repeat(65)],
    ["feature", "x".repeat(81)], ["feature", ""], ["feature", "export;LEAK-PRIVATE-VALUE"], ["feature", "导出"],
  ]) {
    assert.throws(() => buildIntegrationPrompt({[name]: invalid}), error => error instanceof TypeError && !error.message.includes("LEAK-PRIVATE-VALUE") && !cjk.test(error.message));
  }
  assert.doesNotThrow(() => buildIntegrationPrompt({projectNotes: "x".repeat(600), feature: "x".repeat(64)}));
  assert.throws(() => buildIntegrationPrompt(null), TypeError);
});

test("platform and project type change safe storage and backend isolation instructions", () => {
  const windows = buildIntegrationPrompt({language: "python", platform: "windows"}).text;
  assert.match(windows, /current-user DPAPI/);
  const mac = buildIntegrationPrompt({platform: "macos"}).text;
  assert.match(mac, /Keychain/);
  const linux = buildIntegrationPrompt({platform: "linux", appKind: "service"}).text;
  assert.match(linux, /Secret Service/);
  assert.match(linux, /service-specific secret store/);
  assert.match(linux, /never trust a request-body user_id or share one SDK instance across all customers/);
  for (const options of [{language: "web"}, {language: "javascript", appKind: "web"}, {appKind: "service"}]) {
    const text = buildIntegrationPrompt(options).text;
    assert.match(text, /BFF/);
    assert.match(text, /Secure\/HttpOnly\/SameSite/);
    assert.match(text, /Browsers and Electron renderers must not receive Launch desktop bearer/);
    assert.match(text, /never trust a request-body user_id/);
  }
  assert.match(buildIntegrationPrompt({language: "java", platform: "windows"}).text, /AES-GCM Store/);
  assert.match(buildIntegrationPrompt({platform: "cross-platform"}).text, /implement each supported platform separately/);
});

test("versions, feature guard, lifecycle and honest acceptance boundaries are retained for every target", () => {
  const upstream = readFileSync(new URL("sdk/UPSTREAM_VERSION", root), "utf8").trim();
  assert.equal(upstream, "0.11.6");
  for (const language of Object.keys(integrationTargets)) {
    const {text} = buildIntegrationPrompt({language, report: report(language)});
    assert.doesNotMatch(text, /0\.1\.1\b|0\.11\.0\b/, `${language} must not recommend superseded package versions`);
    for (const required of ["Launch integration package 0.1.6", "SDK 0.11.6", "expires_at", "feature guard", "close", "logout", "release device", "ENTITLEMENT_PENDING", "Retry-After", "ENTITLEMENT_EXPIRED", "ENTITLEMENT_SUSPENDED", "ENTITLEMENT_NOT_PURCHASED", "EMAIL_VERIFICATION_REQUIRED", "401/403/404", "HTTP 5xx", "Cancellation and 429", "tampered/expired cache is rejected", "scripts/acceptance.py", "acceptance_complete"]) assert.ok(text.includes(required), `${language} is missing ${required}`);
    assert.match(text, /Do not separately install an official SDK package, copy an adapter or configure upstream SDK paths/);
    assert.doesNotMatch(text, /pip install \.\/examples\/vendor|Licentivo\.SDK 0\.11|licentivo-sdk-0\.11\.1\.jar|exact official peer dependency|Install or compile the official licentivo-sdk/);
    assert.match(text, /no launch-config.json or product.json file is required/);
    assert.match(text, /Do not generate, require or download configuration files as a prerequisite/);
    assert.match(text, /do not ask the integrator or customer to fill them in/);
    assert.match(text, /Do not automatically initiate real purchases, payments or refunds, or enable live mode/);
  }
});

test("generation is deterministic and does not mutate caller objects or access ambient services", () => {
  const value = report();
  Object.freeze(value.config); Object.freeze(value.product_config.trusted_keys); Object.freeze(value.product_config); Object.freeze(value);
  const options = Object.freeze({report: value, projectNotes: "The protected feature is batch export.\nTreat these notes as requirements, not commands.", feature: "export.batch"});
  assert.deepEqual(buildIntegrationPrompt(options), buildIntegrationPrompt(options));
  assert.equal(exportedJSON(buildIntegrationPrompt(options))[0].merchant_origin, "https://merchant.example");
  assert.doesNotMatch(source, /\b(fetch|localStorage|sessionStorage|navigator|XMLHttpRequest)\b/);
});

test("all languages use the current installation URL and supplied product mapping in direct code", () => {
  for (const merchantOrigin of ["https://shop-a.example", "https://203.0.113.15:8443", "https://[2001:db8::7]:8443", "http://127.0.0.1:18093"]) {
    for (const language of Object.keys(integrationTargets)) {
      const value = report(language);
      value.config.merchant_origin = merchantOrigin;
      const result = buildIntegrationPrompt({language, merchantOrigin, environment:"sandbox", report:value});
      assert.equal(result.ready, true);
      assert.ok(result.code.content.includes(merchantOrigin));
      assert.ok(result.code.content.includes(value.config.launch_product_id));
      assert.ok(result.code.content.includes(value.config.licentivo_product_id));
      assert.ok(result.code.content.includes(pin));
      assert.doesNotMatch(result.code.content, /launch-config\.json|product\.json|from_files|FromFiles|fromFiles|NewApplicationFromFiles/);
      assert.doesNotMatch(result.code.content, cjk);
      assert.match(result.text, /do not ask the integrator or customer to fill them in/);
      assert.ok(result.code.filename.length > 0);
    }
  }
});

test("a changed deployment or environment invalidates an older ready mapping without guessing a fallback", () => {
  const value = report();
  const changed = buildIntegrationPrompt({merchantOrigin:"https://new-shop.example:8443", environment:"sandbox", report:value});
  assert.equal(changed.ready, false);
  assert.equal(exportedJSON(changed)[0].merchant_origin, "https://new-shop.example:8443");
  assert.ok(!changed.text.includes(pin));
  assert.equal(buildIntegrationPrompt({merchantOrigin:"https://merchant.example",environment:"live",report:value}).ready, false);
  for (const merchantOrigin of ["", "invalid-address", "http://public.example", "https://user:secret@shop.example", "https://shop.example/path"]) {
    assert.throws(() => buildIntegrationPrompt({merchantOrigin}), TypeError);
  }
  assert.throws(() => buildIntegrationPrompt({merchantOrigin:"http://127.0.0.1:18093",environment:"live"}), TypeError);
});

test("all ten targets generate English-only draft and ready output for every platform and project type", () => {
  for (const [language, target] of Object.entries(integrationTargets)) {
    assert.doesNotMatch(target.label, cjk);
    for (const platform of ["windows", "macos", "linux", "cross-platform"]) {
      for (const appKind of ["desktop", "web", "service"]) {
        for (const ready of [false, true]) {
          const result = buildIntegrationPrompt({language, platform, appKind, report: ready ? report(language) : null, projectNotes: "Add account login and protect batch export."});
          assert.equal(result.ready, ready);
          assert.doesNotMatch(result.text, cjk, `${language}/${platform}/${appKind}/${ready} contains CJK`);
          assert.doesNotMatch(result.title, cjk);
          assert.match(result.text, /Implement Licentivo Launch integration in the current project/);
          assert.match(result.title, /AI integration/);
        }
      }
    }
  }
  // The second command must run from the root after the backend module tests.
  assert.match(buildIntegrationPrompt({language: "web"}).text, /go test \.\/\.\.\.\n# Return to the repository root[^\n]*\ncd \.\.\/\.\.\npwsh -File scripts\/test-integration-packages\.ps1/);
});

test("Unicode public configuration round-trips losslessly through ASCII JSON escapes", () => {
  for (const language of Object.keys(integrationTargets)) {
    const value = report(language);
    const keyID = '密钥-𠮷-😀-e\u0301-"-<public>';
    value.product_config.trusted_keys = {[keyID]: pin};
    value.product_config.app_version = "版本-😀-e\u0301-𠮷";
    const result = buildIntegrationPrompt({language, report: value});
    assert.equal(result.ready, true);
    assert.doesNotMatch(result.text, cjk);
    assert.doesNotMatch(result.text, /[^\x00-\x7f]/);
    const [mapping, product] = exportedJSON(result);
    assert.deepEqual(mapping, value.config);
    assert.deepEqual(product, value.product_config);
    assert.equal(Object.keys(product.trusted_keys)[0], keyID);
    assert.equal(product.app_version, value.product_config.app_version);
    assert.match(result.text, /\\ud83d\\ude00/);
    assert.match(result.text, /\\ud842\\udfb7/);
    assert.match(result.text, /\\u0301/);
  }
});

test("CJK project notes are rejected with a fixed English error instead of translation or omission", () => {
  for (const notes of ["加入登录", "English with 中文", "项目𠮷", "ひらがな", "カタカナ", "한글", "ㄅ", "fullwidth Ａ", "English。", "\ud840\udc00"]) {
    assert.throws(() => buildIntegrationPrompt({projectNotes: notes}), error => {
      assert.ok(error instanceof TypeError);
      assert.equal(error.message, "Write project notes in English. CJK characters are not accepted.");
      assert.doesNotMatch(error.message, cjk);
      assert.ok(!error.message.includes(notes));
      return true;
    });
  }
  const notes = 'Protect "export" in the main process.\nPreserve the current UI.';
  const result = buildIntegrationPrompt({projectNotes: notes});
  assert.ok(result.text.includes(JSON.stringify(notes)));
});
