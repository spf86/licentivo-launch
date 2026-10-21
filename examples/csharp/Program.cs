using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using Licentivo;

if (args.Length < 3) throw new ArgumentException("MERCHANT_ORIGIN LAUNCH_PRODUCT_ID PRODUCT_JSON [sandbox|live] [--sign-out]");
var origin = new Uri(args[0]);
var environment = args.Length > 3 ? args[3] : "live";
if (origin.UserInfo != "" || origin.Query != "" || origin.Fragment != "" || origin.AbsolutePath != "/" ||
    (origin.Scheme != "https" && !(origin.Scheme == "http" && origin.IsLoopback && environment == "sandbox")))
    throw new ArgumentException("Use an HTTPS merchant origin; local HTTP is only for sandbox development");
if (!Guid.TryParseExact(args[1], "D", out _)) throw new ArgumentException("Invalid Launch product ID");
using var http = new HttpClient(new HttpClientHandler { AllowAutoRedirect = false }) { BaseAddress = origin, Timeout = TimeSpan.FromSeconds(15) };
string token = "";
async Task<JsonElement> Call(string path, object body)
{
    using var request = new HttpRequestMessage(HttpMethod.Post, "/api/v1" + path) { Content = JsonContent.Create(body) };
    if (token != "") request.Headers.Authorization = new("Bearer", token);
    using var response = await http.SendAsync(request, HttpCompletionOption.ResponseHeadersRead);
    await using var stream = await response.Content.ReadAsStreamAsync();
    using var buffer = new MemoryStream();
    var chunk = new byte[4096]; int count;
    while ((count = await stream.ReadAsync(chunk)) != 0) { if (buffer.Length + count > 1048576) throw new IOException("Response too large"); buffer.Write(chunk, 0, count); }
    using var json = JsonDocument.Parse(buffer.ToArray());
    if (!response.IsSuccessStatusCode) throw new InvalidOperationException("Launch: " + json.RootElement.GetProperty("error").GetProperty("code").GetString());
    return json.RootElement.GetProperty("data").Clone();
}
Console.Write("Email: "); var email = Console.ReadLine() ?? "";
Console.Write("Password: "); var password = new StringBuilder();
while (true) { var key = Console.ReadKey(true); if (key.Key == ConsoleKey.Enter) break; if (key.Key == ConsoleKey.Backspace) { if (password.Length > 0) password.Length--; } else if (!char.IsControl(key.KeyChar)) password.Append(key.KeyChar); }
Console.WriteLine();
var grant = await Call("/auth/login", new { email, password = password.ToString(), kind = "desktop" }); password.Clear();
token = grant.GetProperty("access_token").GetString()!;
string? cachePath = null;
try
{
    var own = await Call($"/software/products/{args[1]}/credential", new { environment });
    if (own.GetProperty("product_id").GetString() != args[1]) throw new InvalidOperationException("Credential product mismatch");
    var config = JsonSerializer.Deserialize<Config>(await File.ReadAllTextAsync(args[2]), LicentivoClient.JsonOptions) ?? throw new JsonException("Invalid public product config");
    var scope = Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(origin.GetLeftPart(UriPartial.Authority) + "\n" + grant.GetProperty("user").GetProperty("id").GetString() + "\n" + args[1] + "\n" + environment))).ToLowerInvariant();
    var directory = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "LicentivoLaunch", scope); Directory.CreateDirectory(directory);
    cachePath = Path.Combine(directory, "lease.json");
    config = config with { LicenseKey = own.GetProperty("license_key").GetString()!, CachePath = cachePath };
    await using var sdk = new LicentivoClient(config);
    await sdk.Start(_ => Console.Error.WriteLine("Authorization changed; refresh application status"));
    await sdk.RunFeature("export", () => File.WriteAllTextAsync("licensed-report.txt", "Protected report\n"));
    Console.WriteLine("Protected export completed");
    if (args.Contains("--release-device")) await sdk.Deactivate();
}
finally
{
    try { await Call("/auth/logout", new { }); }
    finally { token = ""; if (args.Contains("--sign-out") && cachePath is not null) { File.Delete(cachePath); File.Delete(cachePath + ".credential.json"); } }
}
