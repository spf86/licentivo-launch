# 客戶測試伺服器的 HTTP 接入說明

[選擇語言](http-testing/README.md)

本文中的伺服器位址、IP 與主機名稱，均屬於客戶自行部署的 Launch。客戶不需要進入 Licentivo 或開發者的內部網路。客戶自己的公開測試伺服器及內網測試伺服器，皆可使用本說明。

SDK 依照 `merchant_origin` 中的位址選擇通訊協定：`https://` 使用 HTTPS；`http://` 僅在下述測試權限允許時使用。HTTPS 請求失敗不會自動降級為 HTTP。開關不會改寫位址，也不會安裝 TLS 憑證。

## 兩個獨立開關，預設皆關閉

| 開關 | 客戶在哪裡設定 | 控制範圍 |
| --- | --- | --- |
| 單一 API Key 的 **允許 HTTP（測試用）** | Licentivo → 開發與接入 → API Key → 建立 API Key，或現有 Key 的接入設定 | 允許使用這一個 Key，為 HTTP 位址的 Launch 網站建立或更新帳號授權連線。唯讀 Key 不會因此取得寫入權限。輪替 Key 會保留此設定。 |
| **允許 SDK 使用 HTTP（僅測試環境）** | 客戶自己的 Launch → 系統設定 → 網站與安全 → SSL 憑證與存取協定 | 允許該 Launch 接受 HTTP SDK 帳號請求，並產生 HTTP 測試接入程式碼。HTTP 執行階段身分交換僅限 `sandbox`；`live` 仍要求 HTTPS。 |

兩個開關皆由客戶自行管理，無須 Licentivo 平台管理員操作，也不需要將整個 Licentivo 平台切換為開發模式。它們只影響客戶的 Launch 位址。Launch 與官方授權 SDK 呼叫 Licentivo 時仍使用 HTTPS。

## 客戶如何設定測試伺服器

1. 更新 Licentivo 伺服器、Launch 伺服器及對應語言的 SDK。舊版 SDK 若只允許 localhost HTTP，必須升級；伺服器設定無法改變已安裝 SDK 的位址驗證。
2. 在客戶自己的 Licentivo 工作空間建立專用的 **讀寫** 測試 Key，或開啟有效 Key 的 **接入設定**，選擇 **允許 HTTP（測試用）**。
3. 在客戶自己的 Launch **網站與安全** 設定中，填入測試用戶端能存取的位址，例如 `http://192.168.1.20:18080`、`http://test-server:18080` 或客戶自己的測試網域。開啟 **允許 SDK 使用 HTTP（僅測試環境）** 並儲存。位址只包含協定、主機及選用連接埠，不帶路徑、查詢參數、片段或結尾斜線。
4. 使用該 Key 將 Launch 重新連線至 Licentivo。讀取到產品與策略只代表目錄可讀，還須確認帳號授權連線已建立。
5. 在 **程式接入 → 接入檢查與程式碼** 選擇 **sandbox 測試環境**，執行檢查，再複製新產生的程式碼。檢查通過後，HTTP 測試參數會自動填入。請使用測試帳號及測試購買。

客戶自己的內網不需要公開 IP 或公開 DNS，但測試用戶端必須能解析並存取所填的 Launch 位址。`localhost` 指執行 SDK 的那台機器，其他機器須使用伺服器的 IP 或內部主機名稱。Licentivo 只登記網站來源與固定簽章公鑰，不會回連該位址，也不會從中擷取公鑰。Launch 與線上授權用戶端仍須能向外存取 Licentivo 的 HTTPS 端點。完全斷網的內網無法進行線上帳號授權整合測試。

## SDK 如何選擇 HTTP 或 HTTPS

開啟客戶伺服器的 HTTP 測試後，產生的 Launch 公開設定相當於：

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

位址決定協定。`allow_http: true` 僅允許 `sandbox` 使用 HTTP 商戶位址，不會將 HTTPS 位址降級為 HTTP。官方授權產品的設定仍為 HTTPS。九種語言的直接呼叫參數如下：

| SDK | 明確的 HTTP 測試參數 |
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

這些參數需要新版 SDK 支援。SDK 不會收到或查詢商戶的管理 API Key。Launch 只匯出公開設定，並在伺服器檢查自身的 HTTP 開關；僅修改用戶端參數無法繞過關閉的伺服器開關。舊建構方式為相容性保留原有的 sandbox 本機 HTTP 例外，但不能繞過伺服器預設關閉的開關。

## 關閉測試或切換 HTTPS

HTTP 不會加密密碼、帳號權杖等資料，客戶自己的內網也可能遭監聽或入侵。管理員透過 HTTP 開啟 Launch 後台並輸入 API Key 時，瀏覽器到 Launch 的請求也可能洩漏 Key，即使 Launch 到 Licentivo 使用 HTTPS。測試請使用專用憑證；正式部署及敏感管理操作請使用 HTTPS。

為 Launch 設定可用的 HTTPS 監聽器或 TLS 代理後，應更新實際商戶位址、視需要重新連線 Licentivo，再重新產生接入設定並更新用戶端。已複製或編譯進程式的 HTTP 位址不會自動變更。測試結束後關閉兩個 HTTP 開關。無論開關如何設定，`live` SDK 身分交換仍須使用 HTTPS。

關閉 Launch 開關後，後續 HTTP SDK 請求會回傳 `SDK_HTTP_DISABLED`；`live` 的 HTTP 身分請求會回傳 `SDK_HTTPS_REQUIRED`。已簽發的離線許可仍遵循 Licentivo 正常的到期與撤銷規則。關閉 API Key 的 HTTP 權限後，該 Key 無法再建立或更新 HTTP 網站連線，但不會刪除現有連線、撤銷購買或使已簽發許可失效。需要停止現有連線授權時，須另行停用該連線。

## 連線失敗時如何排查

Launch 會顯示失敗階段（讀取產品或策略、查詢連線、建立 sandbox/live 連線）、具體原因，以及 Licentivo 回傳的錯誤碼和請求編號。已儲存的失敗資訊在重新整理後仍可查看。錯誤內容不會顯示管理 API Key。

| 錯誤 | 處理方式 |
| --- | --- |
| `LAUNCH_HTTPS_REQUIRED` | 為 Launch 實際使用的那個 API Key 開啟 HTTP，或設定可用的 HTTPS 商戶位址。 |
| `LICENSE_KEY_INVALID` | 檢查 Key 是否完整、到期、撤銷，以及建立者和工作空間是否有效。 |
| `LICENSE_KEY_FORBIDDEN` | 使用有效 Owner 建立的讀寫 Key。HTTP 權限不會增加寫入權限。 |
| `SDK_HTTP_DISABLED` | 在客戶自己的 Launch 開啟 SDK HTTP 測試開關，或改用 HTTPS。 |
| `SDK_HTTPS_REQUIRED` | `live` 必須使用 HTTPS；HTTP 測試請選擇 `sandbox`。 |
| `LICENTIVO_TIMEOUT` / `LICENTIVO_NETWORK_ERROR` | 檢查客戶測試機器的對外網路、DNS、防火牆與代理設定。 |
| `LICENTIVO_TLS_ERROR` | 檢查系統時間、受信任憑證及 HTTPS 代理，並保持 TLS 憑證驗證開啟。 |
