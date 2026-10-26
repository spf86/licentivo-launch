using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Text;

namespace Licentivo.Launch;

/// <summary>OS-user/host vault with atomic replacement and multiple opaque keys.
/// Active login and account logout tasks use distinct keys. Never persist plaintext.</summary>
public interface ISessionStore
{
    byte[]? Load(string key);
    void Save(string key, byte[] value);
    void Delete(string key);
}

public sealed class MemoryStore : ISessionStore
{
    private readonly Dictionary<string, byte[]> values = new();
    public byte[]? Load(string key) => values.TryGetValue(key, out var value) ? value.ToArray() : null;
    public void Save(string key, byte[] value) { Delete(key); values[key] = value.ToArray(); }
    public void Delete(string key) { if (values.Remove(key, out var value)) CryptographicOperations.ZeroMemory(value); }
}

/// <summary>Current-user Windows DPAPI. No unencrypted fallback.</summary>
public sealed class WindowsDPAPIStore : ISessionStore
{
    private readonly string directory;
    public WindowsDPAPIStore(string? directory = null)
    {
        if (!OperatingSystem.IsWindows()) throw new PlatformNotSupportedException("Provide an OS-protected ISessionStore on this platform");
        this.directory = directory ?? Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "LicentivoLaunch", "sessions");
        Directory.CreateDirectory(this.directory);
    }
    private string PathFor(string key) => Path.Combine(directory, Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(key))) + ".dpapi");
    [StructLayout(LayoutKind.Sequential)] private struct Blob { public int Size; public IntPtr Data; }
    [DllImport("crypt32.dll", SetLastError = true)] [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool CryptProtectData(ref Blob input, IntPtr description, IntPtr entropy, IntPtr reserved, IntPtr prompt, uint flags, out Blob output);
    [DllImport("crypt32.dll", SetLastError = true)] [return: MarshalAs(UnmanagedType.Bool)]
    private static extern bool CryptUnprotectData(ref Blob input, IntPtr description, IntPtr entropy, IntPtr reserved, IntPtr prompt, uint flags, out Blob output);
    [DllImport("kernel32.dll")] private static extern IntPtr LocalFree(IntPtr ptr);
    private static byte[] Transform(byte[] value, bool decrypt)
    {
        var input = new Blob { Size = value.Length, Data = Marshal.AllocHGlobal(value.Length) };
        Blob output = default;
        try
        {
            Marshal.Copy(value, 0, input.Data, value.Length);
            bool ok = decrypt ? CryptUnprotectData(ref input, IntPtr.Zero, IntPtr.Zero, IntPtr.Zero, IntPtr.Zero, 1, out output)
                : CryptProtectData(ref input, IntPtr.Zero, IntPtr.Zero, IntPtr.Zero, IntPtr.Zero, 1, out output);
            if (!ok) throw new CryptographicException("OS secret protection failed");
            byte[] result = new byte[output.Size]; Marshal.Copy(output.Data, result, 0, output.Size); return result;
        }
        finally
        {
            for (int i = 0; i < input.Size; i++) Marshal.WriteByte(input.Data, i, 0);
            Marshal.FreeHGlobal(input.Data);
            if (output.Data != IntPtr.Zero) LocalFree(output.Data);
        }
    }
    public byte[]? Load(string key)
    {
        string path = PathFor(key); if (!File.Exists(path)) return null;
        if (new FileInfo(path).Length > 65536) throw new IOException("Invalid remembered session");
        return Transform(File.ReadAllBytes(path), true);
    }
    public void Save(string key, byte[] value)
    {
        string temp = Path.Combine(directory, ".session-" + Guid.NewGuid());
        try { File.WriteAllBytes(temp, Transform(value, false)); File.Move(temp, PathFor(key), true); }
        finally { File.Delete(temp); }
    }
    public void Delete(string key) => File.Delete(PathFor(key));
}
