use crate::{hash, private_directory, Failure, Store};
use aes_gcm::{
    aead::{Aead, AeadCore, KeyInit, OsRng, Payload},
    Aes256Gcm, Nonce,
};
use std::{fs, io::Write, path::PathBuf};

/// AES-256-GCM files with a key supplied by an OS vault. Never write that key
/// beside the files or embed it in application source. The default remains MemoryStore.
pub struct EncryptedFileStore {
    root: PathBuf,
    key: [u8; 32],
}
impl EncryptedFileStore {
    pub fn new(root: PathBuf, vault_key: [u8; 32]) -> Result<Self, Failure> {
        private_directory(&root)?;
        Ok(Self {
            root,
            key: vault_key,
        })
    }
    fn path(&self, scope: &str) -> PathBuf {
        self.root.join(format!("{}.encrypted", hash(scope)))
    }
}
impl Store for EncryptedFileStore {
    fn load(&mut self, scope: &str) -> Result<Option<Vec<u8>>, Failure> {
        let path = self.path(scope);
        if !path.exists() {
            return Ok(None);
        }
        if fs::metadata(&path)?.len() > (1 << 20) + 32 {
            return Err(Failure::new("SESSION_STORE_CORRUPT"));
        }
        let data = fs::read(path)?;
        if data.len() < 32 || &data[..4] != b"LVS1" {
            return Err(Failure::new("SESSION_STORE_CORRUPT"));
        }
        let cipher = Aes256Gcm::new_from_slice(&self.key)
            .map_err(|_| Failure::new("SESSION_STORE_CORRUPT"))?;
        cipher
            .decrypt(
                Nonce::from_slice(&data[4..16]),
                Payload {
                    msg: &data[16..],
                    aad: scope.as_bytes(),
                },
            )
            .map(Some)
            .map_err(|_| Failure::new("SESSION_STORE_CORRUPT"))
    }
    fn save(&mut self, scope: &str, secret: &[u8]) -> Result<(), Failure> {
        if secret.len() > 1 << 20 {
            return Err(Failure::new("SESSION_TOO_LARGE"));
        }
        private_directory(&self.root)?;
        let cipher = Aes256Gcm::new_from_slice(&self.key)
            .map_err(|_| Failure::new("SESSION_STORE_CORRUPT"))?;
        let nonce = Aes256Gcm::generate_nonce(&mut OsRng);
        let encrypted = cipher
            .encrypt(
                &nonce,
                Payload {
                    msg: secret,
                    aad: scope.as_bytes(),
                },
            )
            .map_err(|_| Failure::new("SESSION_STORE_CORRUPT"))?;
        let path = self
            .root
            .join(format!(".session-{}.tmp", hash(&format!("{:?}", nonce))));
        let result = (|| {
            let mut options = fs::OpenOptions::new();
            options.write(true).create_new(true);
            #[cfg(unix)]
            {
                use std::os::unix::fs::OpenOptionsExt;
                options.mode(0o600);
            }
            let mut file = options.open(&path)?;
            file.write_all(b"LVS1")?;
            file.write_all(&nonce)?;
            file.write_all(&encrypted)?;
            file.sync_all()?;
            drop(file);
            fs::rename(&path, self.path(scope))?;
            Ok(())
        })();
        let _ = fs::remove_file(path);
        result
    }
    fn delete(&mut self, scope: &str) -> Result<(), Failure> {
        match fs::remove_file(self.path(scope)) {
            Ok(()) => Ok(()),
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(error) => Err(error.into()),
        }
    }
}
impl Drop for EncryptedFileStore {
    fn drop(&mut self) {
        self.key.fill(0);
    }
}
