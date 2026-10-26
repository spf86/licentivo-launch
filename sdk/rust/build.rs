use std::{env, fs, path::{Path, PathBuf}};

fn copy_runtime_dlls(root: &Path) {
    // Cargo puts build-script output below <profile>/build/<package>/out.
    // Standalone programs and test executables must both find the bundled DLLs.
    let output = PathBuf::from(env::var_os("OUT_DIR").unwrap());
    let profile = output.ancestors().nth(3).expect("Cargo profile directory");
    println!("cargo:rerun-if-changed={}", root.join("bin").display());
    for entry in fs::read_dir(root.join("bin")).expect("Launch curl runtime directory") {
        let source = entry.expect("Launch curl runtime file").path();
        if source.extension().and_then(|value| value.to_str()).map(str::to_ascii_lowercase).as_deref() != Some("dll") { continue; }
        for directory in [profile.to_path_buf(), profile.join("deps")] {
            fs::create_dir_all(&directory).expect("Launch runtime destination");
            let destination = directory.join(source.file_name().unwrap());
            let bytes = fs::read(&source).expect("Read Launch runtime DLL");
            if fs::read(&destination).ok().as_deref() != Some(bytes.as_slice()) {
                fs::write(&destination, &bytes).expect("Install Launch runtime DLL beside the application");
            }
        }
    }
}

fn main() {
    let package = PathBuf::from(env::var_os("CARGO_MANIFEST_DIR").unwrap());
    let core = package.join("vendor/c");
    let os = env::var("CARGO_CFG_TARGET_OS").unwrap();
    let target_env = env::var("CARGO_CFG_TARGET_ENV").unwrap_or_default();
    let bundled_curl = package.join("vendor/curl");
    let mut build = cc::Build::new();
    build.include(&core).std("c11").warnings(false);
    for name in ["licentivo.c", "vendor/cJSON.c", "vendor/monocypher.c", "vendor/monocypher-ed25519.c"] { build.file(core.join(name)); }
    println!("cargo:rerun-if-changed={}", core.display());
    println!("cargo:rerun-if-env-changed=LICENTIVO_CURL_ROOT");
    let curl = env::var_os("LICENTIVO_CURL_ROOT").map(PathBuf::from)
        .or_else(|| (os == "windows" && bundled_curl.join("include/curl/curl.h").is_file()).then_some(bundled_curl));
    if let Some(root) = curl {
        build.include(root.join("include"));
        println!("cargo:rustc-link-search=native={}", root.join("lib").display());
        if os == "windows" { copy_runtime_dlls(&root); }
    } else {
        for name in ["libcurl", "openssl"] {
            if let Ok(library) = pkg_config::Config::new().cargo_metadata(false).probe(name) {
                for path in library.include_paths { build.include(path); }
                for path in library.link_paths { println!("cargo:rustc-link-search=native={}", path.display()); }
            }
        }
    }
    if os == "windows" && target_env == "msvc" { panic!("The bundled C11 SDK requires a Windows GNU/MinGW Rust toolchain; MSVC is not supported by SDK 0.11.2."); }
    build.compile("licentivo_launch_core");
    println!("cargo:rustc-link-lib=curl");
    if os == "windows" {
        println!("cargo:rustc-link-lib=bcrypt"); println!("cargo:rustc-link-lib=advapi32");
    } else {
        println!("cargo:rustc-link-lib=crypto"); println!("cargo:rustc-link-lib=pthread"); println!("cargo:rustc-link-lib=m");
    }
}
