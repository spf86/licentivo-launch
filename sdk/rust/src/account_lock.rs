use crate::{hash, private_directory, Failure};
use std::{fs::{File, OpenOptions}, path::PathBuf, time::{Duration, Instant}};

pub(crate) struct AccountGuard(File);
fn intent_path(scope:&str,token:&str)->Result<PathBuf,Failure>{let base=if cfg!(windows){std::env::var_os("LOCALAPPDATA").map(PathBuf::from)}else{std::env::var_os("HOME").map(|home|PathBuf::from(home).join(".cache"))};Ok(base.ok_or_else(||Failure::new("ACCOUNT_LOGOUT_STORAGE_FAILED"))?.join("LicentivoLaunch/account-intents/rust").join(format!("{}.{}.signedout",hash(scope),hash(token))))}
pub(crate) fn remember(scope:&str,token:&str)->Result<(),Failure>{use std::io::Write;let path=intent_path(scope,token)?;private_directory(path.parent().unwrap())?;let mut options=OpenOptions::new();options.write(true).create(true).truncate(false);#[cfg(unix)]{use std::os::unix::fs::OpenOptionsExt;options.mode(0o600);}let mut file=options.open(path)?;file.write_all(b"1")?;file.sync_all()?;Ok(())}
pub(crate) fn has_intent(scope:&str,token:&str)->Result<bool,Failure>{match std::fs::metadata(intent_path(scope,token)?){Ok(_)=>Ok(true),Err(error)if error.kind()==std::io::ErrorKind::NotFound=>Ok(false),Err(error)=>Err(error.into())}}
pub(crate) fn clear_intent(scope:&str,token:&str)->Result<(),Failure>{match std::fs::remove_file(intent_path(scope,token)?){Ok(_)=>Ok(()),Err(error)if error.kind()==std::io::ErrorKind::NotFound=>Ok(()),Err(error)=>Err(error.into())}}
pub(crate) fn take(scope: &str) -> Result<AccountGuard, Failure> {
    let base = if cfg!(windows) { std::env::var_os("LOCALAPPDATA").map(PathBuf::from) }
        else { std::env::var_os("XDG_CACHE_HOME").map(PathBuf::from).or_else(|| std::env::var_os("HOME").map(|home| PathBuf::from(home).join(".cache"))) };
    let root = base.ok_or_else(|| Failure::new("ACCOUNT_LOCK_FAILED"))?.join("LicentivoLaunch/account-locks/rust");
    private_directory(&root)?;
    let path=root.join(format!("{}.lock",hash(scope)));
    let deadline=Instant::now()+Duration::from_secs(5);
    loop {
        let mut options=OpenOptions::new();options.read(true).write(true).create(true).truncate(false);
        #[cfg(windows)] { use std::os::windows::fs::OpenOptionsExt; options.share_mode(0); }
        #[cfg(unix)] { use std::os::unix::fs::OpenOptionsExt; options.mode(0o600); }
        match options.open(&path) {
            Ok(file)=>{
                #[cfg(unix)] {
                    use std::os::fd::AsRawFd;
                    extern "C" { fn flock(fd:i32, operation:i32)->i32; }
                    if unsafe { flock(file.as_raw_fd(),2|4) } != 0 {
                        if Instant::now()>=deadline{return Err(Failure::new("ACCOUNT_LOCK_BUSY"));}
                        std::thread::sleep(Duration::from_millis(20));continue;
                    }
                }
                return Ok(AccountGuard(file));
            }
            Err(error)=>{
                #[cfg(windows)] if error.raw_os_error()!=Some(32)&&error.raw_os_error()!=Some(33){return Err(error.into());}
                #[cfg(not(windows))] { return Err(error.into()); }
                #[cfg(windows)] {
                if Instant::now()>=deadline{return Err(Failure::new("ACCOUNT_LOCK_BUSY"));}
                std::thread::sleep(Duration::from_millis(20));
                }
            }
        }
    }
}
impl Drop for AccountGuard {
    fn drop(&mut self) {
        let _ = &self.0;
        #[cfg(unix)] { use std::os::fd::AsRawFd;extern "C" {fn flock(fd:i32,operation:i32)->i32;}unsafe {flock(self.0.as_raw_fd(),8);}}
        // Windows releases exclusive sharing when File is dropped, including
        // process termination; no stale filesystem lease needs to be removed.
    }
}
