using System.Collections.Concurrent;
using System.Security.Cryptography;
using System.Text;

namespace Licentivo.Launch;

internal sealed class AccountGuard : IDisposable
{
    private static string IntentPath(string scope,string token)=>Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"LicentivoLaunch","account-intents","csharp",Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(scope)))+"."+Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(token)))+".signedout");
    internal static void Remember(string scope,string token){var path=IntentPath(scope,token);Directory.CreateDirectory(Path.GetDirectoryName(path)!);using var file=new FileStream(path,FileMode.OpenOrCreate,FileAccess.Write,FileShare.ReadWrite);file.WriteByte(1);file.Flush(true);}
    internal static bool HasIntent(string scope,string token){try{File.GetAttributes(IntentPath(scope,token));return true;}catch(FileNotFoundException){return false;}catch(DirectoryNotFoundException){return false;}}
    internal static void ClearIntent(string scope,string token)=>File.Delete(IntentPath(scope,token));
    private static readonly ConcurrentDictionary<string, SemaphoreSlim> Locks = new();
    private readonly SemaphoreSlim gate;
    private readonly FileStream file;
    private AccountGuard(SemaphoreSlim gate, FileStream file) { this.gate = gate; this.file = file; }
    internal static async Task<AccountGuard> Take(string scope, CancellationToken ct)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(ct);
        timeout.CancelAfter(TimeSpan.FromSeconds(5));
        var gate = Locks.GetOrAdd(scope, _ => new SemaphoreSlim(1, 1));
        await gate.WaitAsync(timeout.Token);
        try
        {
            var root = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "LicentivoLaunch", "account-locks", "csharp");
            Directory.CreateDirectory(root);
            var path = Path.Combine(root, Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(scope))) + ".lock");
            while (true)
            {
                timeout.Token.ThrowIfCancellationRequested();
                try { return new AccountGuard(gate, new FileStream(path, FileMode.OpenOrCreate, FileAccess.ReadWrite, FileShare.None)); }
                catch (IOException) { await Task.Delay(20, timeout.Token); }
            }
        }
        catch { gate.Release(); throw; }
    }
    public void Dispose() { file.Dispose(); gate.Release(); }
}
