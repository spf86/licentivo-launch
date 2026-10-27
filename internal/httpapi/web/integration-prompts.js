// Pure prompt construction. Reports are untrusted input; only reconstructed public
// configuration can be embedded. Do not add network, clipboard or storage access here.
export const integrationTargets = Object.freeze(Object.fromEntries(Object.entries({
  go: {label: "Go", wrapperPath: "pkg/launchclient", examplePath: "examples/go/main.go", publicReadme: "https://github.com/spf86/licentivo-launch-sdk/blob/main/pkg/launchclient/README.md"},
  python: {label: "Python", wrapperPath: "sdk/python", examplePath: "examples/python/main.py", publicReadme: "https://github.com/spf86/licentivo-launch-sdk/blob/main/sdk/python/README.md"},
  csharp: {label: "C# / .NET", wrapperPath: "sdk/csharp", examplePath: "examples/csharp/Program.cs", publicReadme: "https://github.com/spf86/licentivo-launch-sdk/blob/main/sdk/csharp/README.md"},
  java: {label: "Java", wrapperPath: "sdk/java", examplePath: "examples/java/Main.java", publicReadme: "https://github.com/spf86/licentivo-launch-sdk/blob/main/sdk/java/README.md"},
  javascript: {label: "JavaScript / Node.js", wrapperPath: "sdk/javascript", examplePath: "examples/javascript/main.mjs", publicReadme: "https://github.com/spf86/licentivo-launch-sdk/blob/main/sdk/javascript/README.md"},
  rust: {label: "Rust", wrapperPath: "sdk/rust", examplePath: "examples/rust/src/main.rs", publicReadme: "https://github.com/spf86/licentivo-launch-sdk/blob/main/sdk/rust/README.md"},
  ruby: {label: "Ruby", wrapperPath: "sdk/ruby", examplePath: "examples/ruby/main.rb", publicReadme: "https://github.com/spf86/licentivo-launch-sdk/blob/main/sdk/ruby/README.md"},
  c: {label: "C", wrapperPath: "sdk/native", examplePath: "examples/c/main.c", publicReadme: "https://github.com/spf86/licentivo-launch-sdk/blob/main/sdk/native/README.md"},
  cpp: {label: "C++", wrapperPath: "sdk/native", examplePath: "examples/cpp/main.cpp", publicReadme: "https://github.com/spf86/licentivo-launch-sdk/blob/main/sdk/native/README.md"},
  web: {label: "Go Web backend", wrapperPath: "pkg/launchclient", examplePath: "examples/web-backend/main.go", publicReadme: "https://github.com/spf86/licentivo-launch-sdk/blob/main/pkg/launchclient/README.md"},
}).map(([key, value]) => [key, Object.freeze(value)])));

const profiles = {
  go: {
    api: "launchclient.Connect(merchantOrigin, launchProductID, licentivoProductID, trustedKeys, launchclient.Config{Environment: environment}) with the built-in runtime adapter; Login(ctx, email, password), Restore(ctx), RunFeature(name, action), Close(), Logout(ctx), ReleaseDevice(ctx). Official SDK dependencies and the default RuntimeFactory are owned by the Launch package; do not copy an adapter. Creating a Launch Client alone does not authorize protected work.",
    requirements: "Go 1.26+. Install with go get github.com/spf86/licentivo-launch-sdk/pkg/launchclient@v0.1.5 and import github.com/spf86/licentivo-launch-sdk/pkg/launchclient. The Go library defaults to MemoryStore; make the application's remember-login choice explicit. Local replace directives are needed only when developing the source examples.",
    test: "# Start at the repository root.\ngo test ./pkg/launchclient\ncd examples/go\ngo test ./...\ncd ../..",
  },
  python: {
    api: "licentivo_launch.Application.connect(merchant_origin, launch_product_id, licentivo_product_id, trusted_keys, environment=environment); login, restore, run_feature, close, logout, release_device. The with context closes the runtime. Check each method's parameters in the README instead of borrowing signatures from another language.",
    requirements: "Python 3.10+. Install with python -m pip install licentivo-launch==0.1.5 from PyPI. The private SDK module and adapter are embedded; cryptography is resolved by pip. The supplied wheel or source bundle also supports local installation.",
    test: "python -m unittest discover -s sdk/python/tests -v",
  },
  csharp: {
    api: "Licentivo.Launch.Application.Connect(merchantOrigin, launchProductID, licentivoProductID, trustedKeys, environment); await Login/Restore/RunFeature/Logout/ReleaseDevice. Use await using or DisposeAsync to stop the runtime.",
    requirements: ".NET 8+. Install with dotnet add package Licentivo.Launch --version 0.1.5 from NuGet.org. Official SDK classes are embedded in the Launch assembly; NuGet handles its ordinary BouncyCastle dependency. The source example uses only the Launch ProjectReference.",
    test: "dotnet run --project sdk/csharp-tests/Launch.Tests.csproj\ndotnet run --project sdk/csharp-runtime-tests/Launch.RuntimeTests.csproj",
  },
  java: {
    api: "com.licentivo.launch.Launch.connect(merchantOrigin, launchProductID, licentivoProductID, trustedKeys, environment); login(email, char[]), restore, runFeature, logout, releaseDevice, close. Use try-with-resources for close.",
    requirements: "JDK 17+. Add only com.licentivo.launch:launch-client:0.1.5 from Maven Central to Maven or Gradle, or use launch-client-0.1.5.jar from the matching GitHub release. Official SDK classes are embedded; no second SDK JAR is required. Source run.ps1 uses PowerShell and JAVA_HOME.",
    test: "pwsh -File scripts/test-integration-packages.ps1 -Languages java\n# This entry point compiles and runs LaunchTests and OfficialSDKTests.",
  },
  javascript: {
    api: "Application.connect(merchantOrigin, launchProductID, licentivoProductID, trustedKeys, {environment}) from @licentivo/launch; await login/restore/runFeature/logout/releaseDevice/close. Run it only in Node.js or a trusted Electron main process.",
    requirements: "Node.js 20.3+. Install with npm install @licentivo/launch@0.1.5 from npm. TypeScript declarations, private official SDK modules and the default adapter are embedded; no official peer dependency or external SDK path is required.",
    test: "node --test sdk/javascript/test/*.test.mjs",
  },
  rust: {
    api: "licentivo_launch::Application::connect(merchant_origin, launch_product_id, licentivo_product_id, &trusted_key_pairs, environment); login, restore, run_feature, logout, release_device, close/Drop. with_components accepts trusted Store/Transport/RuntimeFactory implementations.",
    requirements: "Rust 1.85+. Install with cargo add licentivo-launch@=0.1.5 from crates.io. Its build.rs compiles the embedded C11 SDK and configures linking automatically. Windows GNU packages include curl and copy runtime DLLs; other platforms need normal OS curl/OpenSSL development libraries and a C11 compiler. Preserve Cargo.lock.",
    test: "cargo test --manifest-path sdk/rust/Cargo.toml --locked\ncargo check --manifest-path examples/rust/Cargo.toml --locked",
  },
  ruby: {
    api: "LicentivoLaunch::Application.connect(merchant_origin, launch_product_id, licentivo_product_id, trusted_keys, environment: environment); login, restore, run_feature with a block, logout, release_device, close. Call close from ensure.",
    requirements: "Ruby 3.1+. Install with gem install licentivo-launch --version 0.1.5 from RubyGems.org. RubyGems selects the platform package. Its private SDK core and Windows curl runtime are bundled. The generic source gem compiles its own embedded core automatically and needs a matching C11 toolchain and OS curl/OpenSSL development libraries.",
    test: "ruby sdk/ruby/test/test_client.rb\nruby sdk/ruby/test/test_runtime.rb",
  },
  c: {
    api: "launch_application_connect(merchant_origin, launch_product_id, licentivo_product_id, trusted_keys_json, environment, remember, store) and launch_application_login/restore/run_feature/logout/release_device/close from account.h. Check restore's three-state result and launch_application_error; do not execute the callback after failure.",
    requirements: "C11 and CMake. Download the matching Windows amd64 GNU or Linux amd64 native archive from https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.5 and verify SHA256SUMS. Use find_package(LicentivoLaunch CONFIG REQUIRED) and licentivo_launch_link(your_target). Launch owns SDK linking and bundled Windows runtime DLLs. Source builds need normal OS curl/OpenSSL development libraries. Read account.h for ownership rules.",
    test: "cmake -S examples/native -B .cache/native -DCMAKE_BUILD_TYPE=Release -DLICENTIVO_LAUNCH_BUILD_TESTS=ON\ncmake --build .cache/native --config Release\npwsh -File scripts/test-integration-packages.ps1 -Languages c -NativeBuildDirectory .cache/native\n# Prepare runtime libraries per sdk/native/README.md and pass -NativeRuntimeDirectories if needed. Missing prerequisites must fail; never skip signature verification.",
  },
  cpp: {
    api: "licentivo_launch::Application::Connect(merchant_origin, launch_product_id, licentivo_product_id, trusted_keys_json, environment) from launch.hpp; Login, Restore, RunFeature, Logout, ReleaseDevice. RAII destruction stops the runtime. Handle C ABI failures and C++ callback exceptions according to the header.",
    requirements: "C++17 and CMake. Download the matching Windows amd64 GNU or Linux amd64 native archive from https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.5 and verify SHA256SUMS. Use find_package(LicentivoLaunch CONFIG REQUIRED) and licentivo_launch_link(your_target CXX). Launch owns SDK linking and bundled Windows runtime DLLs. Source builds need normal OS curl/OpenSSL development libraries.",
    test: "cmake -S examples/native -B .cache/native -DCMAKE_BUILD_TYPE=Release -DLICENTIVO_LAUNCH_BUILD_TESTS=ON\ncmake --build .cache/native --config Release\npwsh -File scripts/test-integration-packages.ps1 -Languages cpp -NativeBuildDirectory .cache/native\n# Prepare runtime libraries per sdk/native/README.md and pass -NativeRuntimeDirectories if needed. Missing prerequisites must fail; never skip signature verification.",
  },
  web: {
    api: "This is a Go Web backend reference, not a tenth SDK language. Reuse the CurrentUser and CredentialWithRetry methods of launchclient.Client and the official SDK's RunFeature. examples/web-backend/main.go provides a runtimePool isolated by authenticated account with serialized access. Do not assume that the example includes complete browser login or a BFF.",
    requirements: "Go 1.26+. Install with go get github.com/spf86/licentivo-launch-sdk/pkg/launchclient@v0.1.5. examples/web-backend is a separate development module. Complete the host application's server-side login, session and deployment policies using docs/software-sdk-integration.md.",
    test: "# Start at the repository root.\ncd examples/web-backend\ngo test ./...\n# Return to the repository root before running the official SDK signature checks.\ncd ../..\npwsh -File scripts/test-integration-packages.ps1 -Languages go",
  },
};

const kinds = {desktop: "desktop application", web: "Web application", service: "background service"};
const platforms = {windows: "Windows", macos: "macOS", linux: "Linux", "cross-platform": "cross-platform"};
const cjk = /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}\p{Script=Bopomofo}\u3000-\u303f\uff00-\uffef]/u;
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const productOrigin = "https://www.licentivo.com";
const encoder = new TextEncoder();
const configFields = ["schema_version", "merchant_origin", "launch_product_id", "licentivo_product_id", "environment", "allow_http"];
const productFields = ["base_url", "product_id", "trusted_keys", "app_version"];

function record(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const prototype = Object.getPrototypeOf(value);
  return prototype === Object.prototype || prototype === null;
}
function own(value, key) {
  const descriptor = Object.getOwnPropertyDescriptor(value, key);
  return descriptor && "value" in descriptor ? descriptor.value : undefined;
}
function fields(value, allowed) {
  return record(value) && Reflect.ownKeys(value).every(key => typeof key === "string" && allowed.includes(key) && "value" in Object.getOwnPropertyDescriptor(value, key));
}
function origin(value, environment, allowHTTP=false) {
  if (typeof value !== "string" || value.length > 2048 || /[\s\\\u0000-\u001f\u007f]/u.test(value)) return null;
  let url;
  try { url = new URL(value); } catch { return null; }
  if (value !== url.origin && value !== `${url.origin}/`) return null;
  if (url.username || url.password || url.search || url.hash || url.pathname !== "/") return null;
  if (url.protocol === "https:") return url.origin;
  if (environment === "sandbox" && url.protocol === "http:" && (allowHTTP || ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname))) return url.origin;
  return null;
}
function publicReport(report, language) {
  try {
    if (!record(report) || own(report, "ready") !== true || own(report, "language") !== language) return null;
    const mapping = own(report, "config"), product = own(report, "product_config");
    if (!fields(mapping, configFields) || !fields(product, productFields)) return null;
    if (mapping.schema_version !== 1 || !["sandbox", "live"].includes(mapping.environment)) return null;
    if (typeof mapping.launch_product_id !== "string" || !uuid.test(mapping.launch_product_id) || typeof mapping.licentivo_product_id !== "string" || !uuid.test(mapping.licentivo_product_id)) return null;
    if (mapping.allow_http !== undefined && typeof mapping.allow_http !== "boolean" || mapping.allow_http === true && mapping.environment !== "sandbox") return null;
    const merchant = origin(mapping.merchant_origin, mapping.environment, mapping.allow_http === true);
    if (!merchant || product.product_id !== mapping.licentivo_product_id || origin(product.base_url, "live") !== productOrigin) return null;
    const keys = product.trusted_keys;
    if (!record(keys)) return null;
    const ids = Reflect.ownKeys(keys);
    if (ids.length < 1 || ids.length > 32) return null;
    for (const id of ids) {
      if (typeof id !== "string" || !id || encoder.encode(id).length > 128 || /[\u0000-\u001f\u007f]/u.test(id)) return null;
      const encoded = own(keys, id);
      // A canonical, unpadded Base64URL representation of exactly 32 bytes.
      if (typeof encoded !== "string" || !/^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$/.test(encoded)) return null;
    }
    const version = own(product, "app_version");
    if (version !== undefined && (typeof version !== "string" || encoder.encode(version).length > 80 || /[\u0000-\u001f\u007f]/u.test(version))) return null;
    const cleanProduct = {base_url: productOrigin, product_id: product.product_id, trusted_keys: Object.fromEntries(ids.sort().map(id => [id, own(keys, id)]))};
    if (version) cleanProduct.app_version = version;
    return {
      config: {schema_version: 1, merchant_origin: merchant, launch_product_id: mapping.launch_product_id, licentivo_product_id: mapping.licentivo_product_id, environment: mapping.environment, ...(mapping.allow_http===true?{allow_http:true}:{})},
      product: cleanProduct,
    };
  } catch { return null; }
}
function json(value) {
  // Escape UTF-16 code units individually (no /u flag): astral pairs, combining
  // marks and public identifiers survive JSON.parse unchanged, with ASCII output.
  // Report objects are never serialized directly, so report.toJSON cannot run.
  return JSON.stringify(value, null, 2).replace(/[<>&\u007f-\uffff]/g, character => `\\u${character.charCodeAt(0).toString(16).padStart(4, "0")}`);
}

function initializationCode(language, mapping, product) {
  const q = value => json(value);
  // Go/C/C++ accept byte escapes, preserving arbitrary UTF-8 public key IDs.
  const bytes = value => '"' + [...encoder.encode(value)].map(byte => byte === 34 ? '\\"' : byte === 92 ? '\\\\' : byte >= 32 && byte < 127 ? String.fromCharCode(byte) : '\\' + byte.toString(8).padStart(3, '0')).join('') + '"';
  const python = value => '"' + [...value].map(char => char.codePointAt(0) > 65535 ? '\\U' + char.codePointAt(0).toString(16).padStart(8, '0') : char.codePointAt(0) >= 127 ? '\\u' + char.codePointAt(0).toString(16).padStart(4, '0') : JSON.stringify(char).slice(1, -1)).join('') + '"';
  const ruby = value => '"' + [...value].map(char => char === '#' ? '\\x23' : char.codePointAt(0) >= 127 ? `\\u{${char.codePointAt(0).toString(16)}}` : JSON.stringify(char).slice(1, -1)).join('') + '"';
  const rust = value => '"' + [...value].map(char => char.codePointAt(0) >= 127 ? `\\u{${char.codePointAt(0).toString(16)}}` : JSON.stringify(char).slice(1, -1)).join('') + '"';
  const keys = Object.entries(product.trusted_keys);
  const args = [mapping.merchant_origin, mapping.launch_product_id, mapping.licentivo_product_id];
  const environment = mapping.environment, allowHTTP=mapping.allow_http===true;
  switch (language) {
    case 'go': case 'web': return {filename:'launch_connection.go', language:'go', content:`package main\n\nimport "github.com/spf86/licentivo-launch-sdk/pkg/launchclient"\n\n// Generated by Launch; public parameters are already filled in.\nfunc connectLaunch() (*launchclient.Application, error) {\n    return launchclient.Connect(${args.map(bytes).join(', ')},\n        map[string]string{${keys.map(([id, key]) => `${bytes(id)}: ${bytes(key)}`).join(', ')}},\n        launchclient.Config{Environment: ${bytes(environment)}${allowHTTP?", AllowHTTP: true":""}})\n}\n`};
    case 'python': return {filename:'launch_connection.py', language:'python', content:`from licentivo_launch import Application\n\n# Generated by Launch; all public parameters are already filled in.\ndef connect_launch():\n    return Application.connect(\n        ${args.map(python).join(',\n        ')},\n        {${keys.map(([id, key]) => `${python(id)}: ${python(key)}`).join(', ')}},\n        environment=${python(environment)},${allowHTTP?"\n        allow_http=True,":""}\n    )\n`};
    case 'javascript': return {filename:'launch-connection.mjs', language:'javascript', content:`import {Application} from '@licentivo/launch';\n\n// Generated by Launch; no configuration files are required.\nexport function connectLaunch() {\n  return Application.connect(${args.map(q).join(', ')},\n    ${json(product.trusted_keys)}, {environment: ${q(environment)}${allowHTTP?", allowHTTP: true":""}});\n}\n`};
    case 'csharp': return {filename:'LaunchConnection.cs', language:'csharp', content:`using System.Collections.Generic;\nusing Licentivo.Launch;\n\npublic static class LaunchConnection\n{\n    // Generated by Launch; no configuration files are required.\n    public static Application Open() => Application.Connect(${args.map(q).join(', ')},\n        new Dictionary<string, string> { ${keys.map(([id, key]) => `[${q(id)}] = ${q(key)}`).join(', ')} }, ${q(environment)}${allowHTTP?", allowHttp: true":""});\n}\n`};
    case 'java': return {filename:'LaunchConnection.java', language:'java', content:`import com.licentivo.launch.Launch;\nimport java.util.Map;\n\npublic final class LaunchConnection {\n    // Generated by Launch; no configuration files are required.\n    public static Launch open() throws Exception {\n        return Launch.connect(${args.map(q).join(', ')},\n            Map.ofEntries(${keys.map(([id, key]) => `Map.entry(${q(id)}, ${q(key)})`).join(', ')}), ${q(environment)}${allowHTTP?", true":""});\n    }\n}\n`};
    case 'rust': return {filename:'launch_connection.rs', language:'rust', content:`use licentivo_launch::{Application, Failure};\n\n// Generated by Launch; no configuration files are required.\npub fn connect_launch() -> Result<Application, Failure> {\n    Application::${allowHTTP?"connect_with_http":"connect"}(${args.map(rust).join(', ')},\n        &[${keys.map(([id, key]) => `(${rust(id)}, ${rust(key)})`).join(', ')}], ${rust(environment)}${allowHTTP?", true":""})\n}\n`};
    case 'ruby': return {filename:'launch_connection.rb', language:'ruby', content:`require 'licentivo_launch'\n\n# Generated by Launch; all public parameters are already filled in.\ndef connect_launch\n  LicentivoLaunch::Application.connect(\n    ${args.map(ruby).join(',\n    ')},\n    { ${keys.map(([id, key]) => `${ruby(id)} => ${ruby(key)}`).join(', ')} },\n    environment: ${ruby(environment)}${allowHTTP?",\n    allow_http: true":""}\n  )\nend\n`};
    case 'c': return {filename:'launch_connection.c', language:'c', content:`#include <account.h>\n\n/* Generated by Launch; no configuration files are required. */\nlaunch_application *connect_launch(void) {\n    return ${allowHTTP?"launch_application_connect_http":"launch_application_connect"}(${args.map(bytes).join(', ')},\n        ${bytes(json(product.trusted_keys))}, ${bytes(environment)}, ${allowHTTP?"1, ":""}0, NULL);\n}\n`};
    case 'cpp': return {filename:'launch_connection.cpp', language:'cpp', content:`#include <launch.hpp>\n\n// Generated by Launch; no configuration files are required.\nlicentivo_launch::Application connectLaunch() {\n    return licentivo_launch::Application::${allowHTTP?"ConnectHTTP":"Connect"}(${args.map(bytes).join(', ')},\n        ${bytes(json(product.trusted_keys))}, ${bytes(environment)});\n}\n`};
  }
}
function selection(value, choices, message) {
  if (typeof value !== "string" || !Object.hasOwn(choices, value)) throw new TypeError(message);
  return value;
}
function storage(language, platform, appKind) {
  const lines = ["Distinguish an in-memory session from remembered login across processes. Use the password only for login and never persist it. If secure storage is unavailable, remain in memory-only mode and tell the user explicitly. Do not put bearer tokens, activation credentials or encryption keys in ordinary JSON, source code, logs or command arguments."];
  if (platform === "windows") {
    lines.push(["go", "python", "csharp", "c", "cpp"].includes(language)
      ? "Windows: explicitly configure the current-user DPAPI Store or remember option described in this language's README. A background service must use an identity that can consistently decrypt its own records; do not assume that a service account can read a desktop user's DPAPI data."
      : "Windows: this language defaults to memory-only sessions; provide an OS-vault-backed Store. The AES-GCM Store implementations in Java/Rust/Node.js/Ruby require a key supplied by the OS vault. The wrapper does not generate and securely retain that key for you.");
  } else {
    lines.push(`${platforms[platform]}: integrate the operating system's credential vault or an explicit secure Store. Consider Keychain on macOS and Secret Service on Linux; implement each supported platform separately. Services without a desktop vault need a service-specific secret store. A passing Windows DPAPI test does not establish support on other systems.`);
  }
  if (appKind === "service" || appKind === "web" || language === "web") lines.push("Keep server credentials in the deployment's secret store and isolate them by authenticated customer. The browser holds only the application's protected session cookie, never a Launch desktop bearer token or SDK activation/device credential.");
  lines.push("Launch session encryption does not encrypt the official SDK cache. The official 0.11.5 device-credential files retain the upstream storage format. Store them only in a directory private to the current user or service identity, restrict permissions, and exclude them from installers, logs and cloud synchronization.");
  return lines.join("\n");
}

export function buildIntegrationPrompt(options = {}) {
  if (!record(options)) throw new TypeError("Integration options must be an object.");
  const option = (key, fallback) => own(options, key) === undefined ? fallback : own(options, key);
  const language = selection(option("language", "go"), integrationTargets, "Choose a supported language or the Go Web backend option.");
  const appKind = selection(option("appKind", "desktop"), kinds, "Project type must be desktop, web or service.");
  const platform = selection(option("platform", "windows"), platforms, "Platform must be windows, macos, linux or cross-platform.");
  const notes = option("projectNotes", ""), feature = option("feature", "export");
  if (typeof notes !== "string" || notes.length > 600) throw new TypeError("Project notes must be a string of at most 600 characters.");
  if (cjk.test(notes)) throw new TypeError("Write project notes in English. CJK characters are not accepted.");
  if (typeof feature !== "string" || !/^[A-Za-z0-9_.-]{1,64}$/.test(feature)) throw new TypeError("Feature names must contain 1-64 ASCII letters, digits, underscores, dots or hyphens, matching the official SDK limit.");
  const target = integrationTargets[language], profile = profiles[language];
  const deploymentEnvironment = option("environment", "sandbox");
  if (!["sandbox", "live"].includes(deploymentEnvironment)) throw new TypeError("Environment must be sandbox or live.");
  const suppliedOrigin = own(options, "merchantOrigin");
  const checked = publicReport(own(options, "report"), language);
  const httpSetting = own(options, "allowHTTP");
  const httpOptIn = httpSetting === undefined ? checked?.config.allow_http === true : httpSetting === true;
  const deploymentOrigin = suppliedOrigin === undefined ? null : origin(suppliedOrigin, deploymentEnvironment, httpOptIn);
  if (suppliedOrigin !== undefined && !deploymentOrigin) throw new TypeError("Configure a valid HTTPS Launch public URL; sandbox HTTP requires explicit opt-in.");
  const validated = checked && (checked.config.allow_http!==true||httpOptIn) && (!deploymentOrigin || checked.config.merchant_origin === deploymentOrigin) && (own(options, "environment") === undefined || checked.config.environment === deploymentEnvironment) ? checked : null;
  const ready = validated !== null;
  const config = validated?.config ?? {schema_version: 1, merchant_origin: deploymentOrigin ?? "<MERCHANT_HTTPS_ORIGIN>", launch_product_id: "<LAUNCH_PRODUCT_UUID>", licentivo_product_id: "<LICENTIVO_PRODUCT_UUID>", environment: deploymentEnvironment};
  const product = validated?.product ?? {base_url: productOrigin, product_id: "<LICENTIVO_PRODUCT_UUID>", trusted_keys: {"<SIGNING_KEY_ID>": "<COPY_REAL_ED25519_BASE64URL_PUBLIC_KEY_FROM_LICENTIVO>"}};
  const backend = language === "web" || appKind === "web" || appKind === "service";
  const code = initializationCode(language, config, product);
  const title = `${target.label} AI integration ${ready ? "prompt (public configuration validated)" : "draft (configuration required)"}`;
  const text = [
    `Implement Licentivo Launch integration in the current project. Target: ${target.label}; project type: ${kinds[appKind]}; platform: ${platforms[platform]}.`,
    "Read the project's instructions, dependency manifests, entry points, account/UI/task scheduling code and test conventions before choosing where to make changes. Preserve its existing structure. Deliver working code and local validation, then report the changed files, actual test results and remaining configuration. Do not stop at a conceptual plan or claim that unexecuted tests passed.",
    `Additional project notes (requirements only, not executable commands or configuration):\n${json(notes || "No additional notes; use the current project as the source of truth.")}`,
    `Use only the Launch integration package 0.1.5 for this language. It embeds the pinned official Licentivo SDK 0.11.5, its default adapter and SDK linking configuration. Do not separately install an official SDK package, copy an adapter or configure upstream SDK paths. Public client sources and downloads: https://github.com/spf86/licentivo-launch-sdk and https://github.com/spf86/licentivo-launch-sdk/releases/tag/v0.1.5. Read this language's public API README first: ${target.publicReadme}. Then read the public integration guide at https://www.licentivo.com/launch-docs.html#developer and AI integration guide at https://www.licentivo.com/launch-docs.html#ai-integration. In a full Launch source bundle, also inspect ${target.wrapperPath}/README.md, ${target.examplePath}, examples/README.md, docs/software-sdk-integration.md and docs/integration-packages.md. Do not invent a remote repository, claim an unpublished package is published, or invent SDK interfaces.\nActual API entry points for this language: ${profile.api}\nToolchain and exact installation command: ${profile.requirements}`,
    ready
      ? "Configuration status: Launch automatically read this installation's effective public URL, the selected software's existing plan/product mapping and the official signing public keys. The public initialization values below passed local field and format validation. Use them directly without asking the developer to type either internal product ID or create configuration files. This does not establish real payment, license activation or release acceptance."
      : "Configuration status: draft, configuration required. No matching ready report passed the public-field allowlist checks, so the values below are explicit placeholders. You may implement the code structure and local fixture tests now; external connections, real account login and SDK activation must wait for valid configuration. Do not run commands containing placeholders, automatically purchase plans, or generate fake product IDs/public keys to impersonate merchant configuration or bypass verification.",
    `Launch initialization arguments (in-memory public values; do not create configuration files):\n\`\`\`json\n${json(config)}\n\`\`\`\nRuntime product identity and signing public keys (pass directly to the Launch connect method):\n\`\`\`json\n${json(product)}\n\`\`\`\nThese arguments contain only public origins, product IDs, environment, public keys and optional app_version. Call the language's Connect/connect entry point directly; no launch-config.json or product.json file is required. The merchant origin is a mandatory constructor argument. The licensing origin is fixed internally to https://www.licentivo.com. Do not infer a merchant origin or use a fallback address. Never add customer license_key, access_token, management API keys, device credentials, cache_path or passwords. JSON Unicode escapes preserve identifier and version data exactly; decode them normally without translating or renaming the values.`,
    `Direct initialization reference (${ready ? 'public values supplied by Launch' : 'draft only; do not execute placeholders'}):\n\`\`\`${code.language}\n${code.content}\`\`\`\nIntegrate this connection function into the existing application, then use the returned application's login/restore and protected feature methods. Both internal product identifiers are already supplied by Launch; do not ask the integrator or customer to fill them in. Initialization alone does not grant access.`,
    "Deployment-specific configuration: this is an independently hosted open-source merchant service. Pass this installation's merchant_origin directly to Connect/connect using the value supplied by Launch; never hard-code a development IP, localhost, an example domain or another merchant's address, and never fall back to one after a connection failure. The installer's private config.yaml public_url and the site's effective public origin must describe the actual client-reachable merchant origin; production requires HTTPS, and sandbox HTTP requires the checked Launch configuration allow_http=true and both customer-controlled HTTP test permissions. The listening http_addr is not the customer endpoint. The runtime product ID and trusted signing keys identify the independent official licensing service. Keep both product IDs distinct and preserve sandbox/live. Do not auto-detect the merchant from an untrusted Host header or rewrite an HTTPS URL to HTTP. A domain/IP/port or product mapping change requires updated validated initialization arguments and a fresh login with isolated local state. Before declaring the delivered application usable, run the sandbox preflight from the application's deployment network, verify DNS/TLS/proxy/API routing without disabling certificate checks, then prove real customer login, owned credential retrieval and a protected SDK operation. Connectivity errors are not successful authorization. When the installer environment is unavailable, mark these checks not run and list the missing configuration.",
    "Implementation flow: restore on application startup. When no session exists or it has expired, offer login using the merchant account's email and password, then obtain a short-lived identity_token for the official SDK. Check the saved expires_at, current user ID, product and environment. A failed restore must never silently become authorized. Display an available state only after real official SDK activation or signed-cache validation. Successful authentication, a paid order, valid_until or a visible button cannot replace runtime authorization.",
    `Put protected business work inside the callback or block of this language's real feature guard, using feature ${json(feature)}. export is only the default example: first confirm that the name matches a feature granted by the actual Licentivo policy. If it is unconfigured, request the real feature name instead of inventing one. Invoke the official SDK check on every execution, not only once at login, and never replace the production verifier with a test runtime.`,
    "Lifecycle: ordinary close/Dispose/Drop stops the SDK and its background tasks while preserving intentionally remembered login; floating seats follow the official policy. Explicit logout calls the official SDK unified logout first, then clears the Launch account session. The official SDK owns credential removal and its durable .logout.json recovery record; never delete its cache directory. The release device entry (ReleaseDevice) is a compatibility alias for the same explicit logout flow; a pending remote release must be reported honestly. Return values and remote-failure behavior differ by language, so follow the package README and never treat close as release. Serialize lifecycle calls as required by the package, avoid reentry from protected callbacks, keep network waits off the UI thread, and bind SDK heartbeats to the application lifetime.",
    storage(language, platform, appKind),
    "Isolation and restoration: scope sessions and SDK caches by merchant origin, account, Launch product, Licentivo product and sandbox/live environment. Preserve the package's existing scope logic and include product-mapping isolation when selecting the host cache root. Account switching must not reuse the previous account's authorization, and writable caches must not be shared across processes. Launch Restore requires online account confirmation and a fresh identity assertion. Launch must never select an offline grant or infer permission from local purchase dates. Applications needing offline startup use the official SDK recovery contract directly. Explicit 401/403/404 account denial clears only Launch-owned account data. HTTP 5xx means service unavailability. Cancellation and 429 never become offline success. The official SDK alone owns signed permission, offline expiry, clock rollback, device credentials and logout recovery.",
    "Errors and recovery UI: distinguish ENTITLEMENT_PENDING (synchronization in progress), ENTITLEMENT_EXPIRED (renewal), ENTITLEMENT_SUSPENDED (contact merchant), ENTITLEMENT_NOT_PURCHASED (purchase entry point), EMAIL_VERIFICATION_REQUIRED (verify email first), expired sessions, device limits, network failure and service unavailability. If the application retries pending identity or purchase synchronization, use a finite bound and honor Retry-After. Do not assume every language wrapper retries automatically. Do not shorten the server's delay, loop indefinitely or label HTTP 5xx as not purchased. Never display raw responses or secrets; show only sanitized error codes and request IDs allowed by the package.",
    backend
      ? "Server boundary: follow examples/web-backend/main.go. The trusted backend isolates instances and caches using the authenticated user ID; never trust a request-body user_id or share one SDK instance across all customers. Serialize each account's SDK lifecycle, bound the instance pool, implement cleanup and shutdown, and distinguish 401/403 from dependency service failures. The Go example's Bearer interface is for trusted callers. For browser access, add the application's BFF/server-side session with Secure/HttpOnly/SameSite cookies and appropriate CSRF protection. Browsers and Electron renderers must not receive Launch desktop bearer tokens, short-lived identity assertions, SDK session/device credentials or management keys; expose only business results and necessary sanitized status."
      : "Desktop boundary: account login and the official SDK run in a trusted local process. Untrusted UI such as an Electron renderer uses a restricted business bridge and must not receive bearer tokens, activation credentials, device credentials or arbitrary file access.",
    "Integrate the direct Connect/connect call into the current project using the exact public values above and ordinary language data structures. Keep the merchant address explicit and reject an empty/invalid value before login. Do not generate, require or download configuration files as a prerequisite. Use the project's own build/run entry point, document the actual command after implementation and avoid commands containing placeholders. Read the source examples for lifecycle behavior; a CLI example is not a required installation step.",
    `Local validation reference (configure actual toolchain paths; missing prerequisites must be reported as failed/not run, never skipped and called successful):\n\`\`\`text\n${profile.test}\n\`\`\`\nUnified validation entry point: scripts/test-integration-packages.ps1. Read docs/acceptance.md for test procedures and evidence records. Add or run this language's real official SDK signature tests: online identity exchange -> close -> online account restore in a new instance/process -> offline permission checked directly by the official SDK during a network outage -> unlicensed feature callback never executes -> tampered/expired cache is rejected. Also verify logout, account switching, product/environment isolation, bounded pending retries, cancellation, cleanup after explicit denial and device-release errors. Mocks and successful compilation alone are insufficient.`,
    "Record external acceptance separately and mark it not run when external configuration is incomplete. Once configuration and authorization are available, follow docs/acceptance.md and scripts/acceptance.py sandbox preflight/verify to check registration/email verification, first purchase and activation, renewal, refunds, device limits and offline expiry. Do not automatically initiate real purchases, payments or refunds, or enable live mode; the user must explicitly arrange those actions. Locally generated random keys are for isolated test fixtures only and must never enter delivered public configuration. Report completed code, actual local results and outstanding external acceptance, without equating configuration ready with acceptance_complete.",
  ].join("\n\n");
  return {text, ready, title, code};
}
