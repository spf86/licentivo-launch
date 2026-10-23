using System.Text;
using System.Text.Json;
using Licentivo;
using Licentivo.Launch;

if (args.Length < 3) throw new ArgumentException("MERCHANT_ORIGIN LAUNCH_PRODUCT_ID PRODUCT_JSON [sandbox|live] [--remember] [--sign-out] [--release-device]");
var environment = args.Length > 3 && !args[3].StartsWith("--") ? args[3] : "live";
var config = JsonSerializer.Deserialize<Config>(await File.ReadAllTextAsync(args[2]), LicentivoClient.JsonOptions) ?? throw new JsonException("Invalid public product.json");
ISessionStore? store = args.Contains("--remember") ? new WindowsDPAPIStore() : null;
await using var app = args[0] == "--config" ? Application.FromFiles(args[1], args[2], store) : new Application(args[0], args[1], config, environment, store);
try
{
    if (args.Contains("--sign-out") && args.Contains("--release-device")) throw new ArgumentException("Choose either --sign-out or --release-device");
    if (args.Contains("--sign-out")) { await app.Logout(); Console.WriteLine("Signed out locally"); return; }
    if (!await app.Restore())
    {
        Console.Write("Email: "); var email = Console.ReadLine() ?? "";
        Console.Write("Password: "); var password = new StringBuilder();
        while (true) { var key = Console.ReadKey(true); if (key.Key == ConsoleKey.Enter) break; if (key.Key == ConsoleKey.Backspace) { if (password.Length > 0) password.Length--; } else if (!char.IsControl(key.KeyChar)) password.Append(key.KeyChar); }
        Console.WriteLine();
        try { await app.Login(email, password.ToString()); } finally { password.Clear(); }
    }
    if (args.Contains("--release-device")) { await app.ReleaseDevice(); Console.WriteLine("Device released and signed out"); return; }
    await app.RunFeature("export", () => File.WriteAllTextAsync("licensed-report.txt", "Protected report\n"));
    Console.WriteLine("Protected export completed");
}
catch (Exception error)
{
    Console.Error.WriteLine(error.Message + "\nCheck your merchant account center for purchase and authorization status.");
    Environment.ExitCode = 1;
}
