package com.licentivo.launch;

import javax.crypto.Cipher;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.security.*;
import java.util.*;

/** AES-GCM session files. Supply the key from the OS vault; never store it beside ciphertext. */
public final class EncryptedFileStore implements Launch.Store {
    private final Path directory;
    private final SecretKey key;
    public EncryptedFileStore(Path directory, SecretKey vaultKey) throws Exception {
        if (!"AES".equalsIgnoreCase(vaultKey.getAlgorithm())) throw new IllegalArgumentException("AES vault key required");
        this.directory = directory.toAbsolutePath().normalize(); this.key = vaultKey;
        Launch.privateDirectory(this.directory);
    }
    private Path path(String scope) throws Exception {
        String digest = HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(scope.getBytes(StandardCharsets.UTF_8)));
        return directory.resolve(digest + ".encrypted");
    }
    public synchronized byte[] load(String scope) throws Exception {
        Path path = path(scope); if (!Files.exists(path)) return null;
        if (Files.size(path) > (1 << 20) + 32) throw new Launch.Failure("SESSION_STORE_CORRUPT");
        byte[] data = Files.readAllBytes(path);
        if (data.length < 32 || data[0] != 'L' || data[1] != 'V' || data[2] != 'S' || data[3] != '1') throw new Launch.Failure("SESSION_STORE_CORRUPT");
        try {
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding"); cipher.init(Cipher.DECRYPT_MODE, key, new GCMParameterSpec(128, Arrays.copyOfRange(data, 4, 16)));
            cipher.updateAAD(scope.getBytes(StandardCharsets.UTF_8)); return cipher.doFinal(data, 16, data.length - 16);
        } catch (GeneralSecurityException error) { throw new Launch.Failure("SESSION_STORE_CORRUPT"); }
    }
    public synchronized void save(String scope, byte[] secret) throws Exception {
        if (secret.length > 1 << 20) throw new Launch.Failure("SESSION_TOO_LARGE");
        Launch.privateDirectory(directory);
        byte[] nonce = new byte[12]; new SecureRandom().nextBytes(nonce);
        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding"); cipher.init(Cipher.ENCRYPT_MODE, key, new GCMParameterSpec(128, nonce));
        cipher.updateAAD(scope.getBytes(StandardCharsets.UTF_8)); byte[] encrypted = cipher.doFinal(secret);
        byte[] data = new byte[16 + encrypted.length]; System.arraycopy(new byte[]{'L','V','S','1'}, 0, data, 0, 4); System.arraycopy(nonce, 0, data, 4, 12); System.arraycopy(encrypted, 0, data, 16, encrypted.length);
        Path temporary = Files.createTempFile(directory, ".session-", ".tmp");
        try {
            Files.write(temporary, data);
            try { Files.move(temporary, path(scope), StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING); }
            catch (AtomicMoveNotSupportedException unsupported) { Files.move(temporary, path(scope), StandardCopyOption.REPLACE_EXISTING); }
        } finally { Files.deleteIfExists(temporary); }
    }
    public synchronized void delete(String scope) throws Exception { Files.deleteIfExists(path(scope)); }
}
