use licentivo_launch::{read_public, Application, Config, Failure};
use std::{
    io::{self, IsTerminal, Write},
    path::PathBuf,
};
fn main() {
    if let Err(error) = run() {
        eprintln!("{}", error);
        std::process::exit(1);
    }
}
fn run() -> Result<(), Failure> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.len() < 2 || args.len() > 3 {
        eprintln!("usage: launch-rust-example launch-config.json product.json [--logout|--release-device]");
        return Err(Failure::new("INVALID_INPUT"));
    }
    if args
        .get(2)
        .is_some_and(|action| action != "--logout" && action != "--release-device")
    {
        return Err(Failure::new("INVALID_INPUT"));
    }
    let config = Config::load(&args[0])?;
    let product = read_public(&args[1])?;
    let cache = std::env::var_os("LOCALAPPDATA")
        .or_else(|| std::env::var_os("XDG_CACHE_HOME"))
        .map(PathBuf::from)
        .or_else(|| std::env::var_os("HOME").map(|home| PathBuf::from(home).join(".cache")))
        .ok_or_else(|| Failure::new("PRIVATE_CACHE_UNAVAILABLE"))?
        .join("LicentivoLaunchRust");
    let mut app = Application::new(config, product, cache)?;
    if args.get(2).map(String::as_str) == Some("--logout") {
        app.logout()?;
        println!("Local session cleared.");
        return Ok(());
    }
    println!(
        "Memory-only login. Inject an OS-vault-backed Store for remembered login across restarts."
    );
    match app.restore() {
        Ok(_) => {}
        Err(error) if error.code == "NO_SESSION" || error.code == "SESSION_EXPIRED" => {
            if !io::stdin().is_terminal() {
                return Err(Failure::new("INTERACTIVE_CONSOLE_REQUIRED"));
            }
            print!("Email: ");
            io::stdout().flush()?;
            let mut email = String::new();
            io::stdin().read_line(&mut email)?;
            print!("Password: ");
            io::stdout().flush()?;
            let mut password = password()?;
            let result = app.login(email.trim(), password.trim_end_matches(['\r', '\n']));
            // Clear the owned input bytes once login finishes (other temporary runtime
            // string allocations cannot be guaranteed zeroized by the language).
            unsafe {
                password.as_bytes_mut().fill(0);
            }
            result?;
        }
        Err(error) => return Err(error),
    }
    if let Some(action) = args.get(2) {
        if action != "--release-device" {
            return Err(Failure::new("INVALID_INPUT"));
        }
        app.release_device()?;
        println!("Device released.");
        return Ok(());
    }
    app.run_feature("export", || {
        std::fs::write("licensed-report.txt", "Protected report\n")?;
        Ok(())
    })?;
    println!("Protected export completed.");
    Ok(())
}
#[cfg(windows)]
fn password() -> Result<String, Failure> {
    use std::ffi::c_void;
    #[link(name = "kernel32")]
    extern "system" {
        fn GetStdHandle(id: u32) -> *mut c_void;
        fn GetConsoleMode(handle: *mut c_void, mode: *mut u32) -> i32;
        fn SetConsoleMode(handle: *mut c_void, mode: u32) -> i32;
    }
    struct Restore(*mut c_void, u32);
    impl Drop for Restore {
        fn drop(&mut self) {
            unsafe {
                SetConsoleMode(self.0, self.1);
            }
        }
    }
    unsafe {
        let handle = GetStdHandle((-10i32) as u32);
        let mut mode = 0;
        if GetConsoleMode(handle, &mut mode) == 0 || SetConsoleMode(handle, mode & !4) == 0 {
            return Err(Failure::new("INTERACTIVE_CONSOLE_REQUIRED"));
        }
        let _restore = Restore(handle, mode);
        let mut password = String::new();
        io::stdin().read_line(&mut password)?;
        println!();
        Ok(password)
    }
}
#[cfg(unix)]
fn password() -> Result<String, Failure> {
    struct Restore;
    impl Drop for Restore {
        fn drop(&mut self) {
            let _ = std::process::Command::new("stty").arg("echo").status();
        }
    }
    if !std::process::Command::new("stty")
        .arg("-echo")
        .status()?
        .success()
    {
        return Err(Failure::new("INTERACTIVE_CONSOLE_REQUIRED"));
    }
    let _restore = Restore;
    let mut password = String::new();
    io::stdin().read_line(&mut password)?;
    println!();
    Ok(password)
}
#[cfg(not(any(windows, unix)))]
fn password() -> Result<String, Failure> {
    Err(Failure::new("INTERACTIVE_CONSOLE_REQUIRED"))
}
