# 顧客サーバーとプライベートネットワークでの HTTP テスト

[言語を選択](http-testing/README.md)

このガイドのサーバーアドレス、IP、ホスト名はすべて、顧客自身が導入した Launch 環境のものです。顧客が Licentivo や開発者のプライベートネットワークに参加する必要はありません。公開テストサーバーと顧客の社内ネットワーク上のサーバーの両方を利用できます。

SDK は `merchant_origin` に指定されたプロトコルを使います。`https://` は HTTPS、`http://` は以下のテスト許可がある場合に限り使用できます。HTTPS リクエストが失敗しても HTTP に自動で切り替わりません。スイッチはアドレスを書き換えず、TLS 証明書もインストールしません。

## 独立した 2 つのスイッチ（既定では両方とも無効）

| スイッチ | 顧客が設定する場所 | 効果 |
| --- | --- | --- |
| 個別の API キーの **HTTP を許可（テスト用）** | Licentivo → 開発と連携 → API Key → API キーの作成、または接続設定 | このキーで HTTP の販売者オリジンに対する Launch アカウント接続を作成・更新できます。読み取り専用キーに書き込み権限は付与されません。キーをローテーションしても設定は維持されます。 |
| **SDK の HTTP 使用を許可（テスト環境のみ）** | 顧客自身の Launch → システム設定 → Web サイトとセキュリティ → SSL 証明書とアクセスプロトコル | この Launch が SDK の HTTP アカウントリクエストを受け付け、HTTP テスト用コードを生成できるようになります。実行時認可用の ID 交換を HTTP で行えるのは `sandbox` のみで、`live` には HTTPS が必要です。 |

両方のスイッチを顧客自身が管理します。Licentivo のプラットフォーム管理者による操作や、プラットフォーム全体の開発モードは不要です。設定が影響するのは顧客の Launch アドレスだけです。Launch と公式ライセンス SDK は引き続き HTTPS で Licentivo に接続します。

## 顧客のテストサーバーを設定する

1. Licentivo サーバー、Launch サーバー、SDK パッケージを更新します。localhost の HTTP しか許可しない旧 SDK は更新が必要です。サーバー設定では、インストール済み SDK の URL 検証は変更できません。
2. 顧客の Licentivo ワークスペースで **読み書き** 可能な専用テストキーを作成するか、有効なキーの **接続設定** を開き、**HTTP を許可（テスト用）** を選択します。
3. 顧客の Launch の **Web サイトとセキュリティ** で、テストクライアントが到達できるアドレスを設定します。例：`http://192.168.1.20:18080`、`http://test-server:18080`、顧客のテストドメイン。**SDK の HTTP 使用を許可（テスト環境のみ）** を有効にして保存します。パス、クエリ、フラグメント、末尾のスラッシュを含まないオリジンを使用してください。
4. そのキーで Launch を Licentivo に再接続します。製品やポリシーを読み取れただけではアカウント接続は成立していません。アカウント認可接続の準備が完了していることも確認します。
5. **アプリケーション連携 → 連携チェックとコード** で **sandbox** を選び、チェックを実行して新しく生成されたコードをコピーします。チェックに成功すると HTTP テスト用オプションが自動で含まれます。テストアカウントとテスト購入を使用してください。

公開 IP や公開 DNS レコードは不要です。テストクライアントから設定済みの Launch アドレスを名前解決し、到達できる必要があります。`localhost` は SDK を実行するマシンを指します。別のマシンではサーバーの IP または内部ホスト名を使用してください。Licentivo はオリジンと固定の署名公開鍵を記録するだけで、そのアドレスへのコールバックや鍵取得は行いません。ただし Launch とオンラインライセンスクライアントは、Licentivo の HTTPS エンドポイントに外向きに接続できる必要があります。完全に切り離された社内ネットワークでは、オンラインのアカウント認可テストはできません。

## SDK による HTTP と HTTPS の選択

顧客サーバーで HTTP テストを有効にすると、生成される Launch の公開設定は次の内容に相当します。

```json
{
  "schema_version": 1,
  "merchant_origin": "http://192.168.1.20:18080",
  "launch_product_id": "11111111-1111-4111-8111-111111111111",
  "licentivo_product_id": "22222222-2222-4222-8222-222222222222",
  "environment": "sandbox",
  "allow_http": true
}
```

プロトコルはアドレスが決定します。`allow_http: true` は `sandbox` で HTTP の販売者オリジンを許可するだけで、HTTPS URL を HTTP に変更しません。公式ライセンス製品の設定は HTTPS のままです。9 言語での直接呼び出しに相当するオプションは次のとおりです。

| SDK | 明示的な HTTP テストオプション |
| --- | --- |
| Go | `launchclient.Config{Environment: "sandbox", AllowHTTP: true}` |
| Python | `Application.connect(..., environment="sandbox", allow_http=True)` |
| JavaScript / TypeScript | `Application.connect(..., {environment: "sandbox", allowHTTP: true})` |
| C# | `Application.Connect(..., "sandbox", allowHttp: true)` |
| Java | `Launch.connect(..., "sandbox", true)` |
| Rust | `Application::connect_with_http(..., "sandbox", true)` |
| Ruby | `Application.connect(..., environment: 'sandbox', allow_http: true)` |
| C | `launch_application_connect_http(..., "sandbox", 1, 0, NULL)` |
| C++ | `Application::ConnectHTTP(..., "sandbox")` |

これらのオプションには更新済みの SDK が必要です。SDK に販売者の管理 API キーを渡したり、SDK から問い合わせたりすることはありません。Launch は公開設定を出力し、サーバー側でも HTTP スイッチを確認します。そのためクライアントのフラグだけを変更しても、サーバーのスイッチが無効なら HTTP 接続は許可されません。従来のコンストラクターは互換性のため sandbox の localhost 例外を維持しますが、既定で無効のサーバースイッチを回避するものではありません。

## テストを終了する、または HTTPS に移行する

HTTP はパスワード、アカウントトークンなどのデータを暗号化しません。顧客の社内ネットワークでも盗聴や侵害の可能性があります。HTTP で開いた Launch 管理画面に API キーを入力すると、Launch から Licentivo が HTTPS であっても、ブラウザーから Launch へのリクエストは保護されません。専用のテスト認証情報を使い、本番環境や機密性の高い管理操作には HTTPS を使用してください。

動作する HTTPS リスナーまたは TLS プロキシを設定したら、Launch の実際の販売者オリジンを更新し、必要に応じて Licentivo に再接続してから、クライアントの連携設定を再生成・更新します。コピー済み、またはプログラムに組み込んだ HTTP URL は自動では変わりません。テスト後は両方の HTTP スイッチを無効にします。スイッチに関係なく、`live` の SDK ID 交換には HTTPS が必要です。

Launch のスイッチを無効にすると、以降の HTTP SDK リクエストは `SDK_HTTP_DISABLED` で拒否されます。`live` の HTTP ID リクエストには `SDK_HTTPS_REQUIRED` が返されます。発行済みの署名付きオフライン許可には、Licentivo の通常の有効期限と取り消し規則が引き続き適用されます。API キーの HTTP 許可を無効にすると、そのキーで HTTP 接続を新規作成・更新できなくなりますが、既存の接続、購入、発行済みの許可は削除・取り消しされません。認可を停止する必要がある接続は別途無効にしてください。

## 接続失敗の原因を調べる

Launch は失敗した段階（製品やポリシーの読み取り、接続の照会、sandbox/live 接続の作成）、具体的な原因、および Licentivo から提供されたエラーコードとリクエスト ID を表示します。保存された失敗情報はページを再読み込みしても確認できます。管理 API キーがエラーに表示されることはありません。

| エラー | 対処 |
| --- | --- |
| `LAUNCH_HTTPS_REQUIRED` | Launch が実際に使用する API キーで HTTP を有効にするか、動作する HTTPS の販売者 URL を設定してください。 |
| `LICENSE_KEY_INVALID` | キー全文、有効期限、取り消し、および所有者とワークスペースの状態を確認してください。 |
| `LICENSE_KEY_FORBIDDEN` | 有効な Owner の読み書き可能なキーを使用してください。HTTP 許可は書き込み権限を追加しません。 |
| `SDK_HTTP_DISABLED` | 顧客自身の Launch で SDK の HTTP テストスイッチを有効にするか、HTTPS を使用してください。 |
| `SDK_HTTPS_REQUIRED` | `live` では HTTPS を、HTTP テストでは `sandbox` を使用してください。 |
| `LICENTIVO_TIMEOUT` / `LICENTIVO_NETWORK_ERROR` | 顧客のテストマシンの外向き通信、DNS、ファイアウォール、プロキシ設定を確認してください。 |
| `LICENTIVO_TLS_ERROR` | システム時刻、信頼済み証明書、HTTPS プロキシを確認し、TLS 検証は有効のままにしてください。 |
