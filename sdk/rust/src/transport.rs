use crate::{Failure, Response, Transport};
use std::{
    ffi::{c_char, c_int, c_long, c_void, CString},
    sync::OnceLock,
};

// libcurl is already a required native dependency of the official SDK. All
// request secrets stay in memory; redirects are disabled and TLS verification
// remains enabled. No shell process receives a bearer or password argument.
#[link(name = "curl")]
extern "C" {
    fn curl_global_init(flags: c_long) -> c_int;
    fn curl_easy_init() -> *mut c_void;
    fn curl_easy_cleanup(handle: *mut c_void);
    fn curl_easy_setopt(handle: *mut c_void, option: c_int, ...) -> c_int;
    fn curl_easy_getinfo(handle: *mut c_void, info: c_int, ...) -> c_int;
    fn curl_easy_perform(handle: *mut c_void) -> c_int;
    fn curl_slist_append(list: *mut c_void, value: *const c_char) -> *mut c_void;
    fn curl_slist_free_all(list: *mut c_void);
}
pub struct CurlTransport;
impl CurlTransport {
    pub fn new() -> Result<Self, Failure> {
        static INITIALIZED: OnceLock<bool> = OnceLock::new();
        if !*INITIALIZED.get_or_init(|| unsafe { curl_global_init(3) == 0 }) {
            return Err(Failure::service());
        }
        Ok(Self)
    }
}
struct Capture {
    body: Vec<u8>,
    retry_after: String,
    oversized: bool,
}
unsafe extern "C" fn write_body(
    data: *mut c_char,
    size: usize,
    count: usize,
    context: *mut c_void,
) -> usize {
    let Some(length) = size.checked_mul(count) else {
        return 0;
    };
    let capture = &mut *(context as *mut Capture);
    if length > (1 << 20) - capture.body.len() {
        capture.oversized = true;
        return 0;
    }
    capture
        .body
        .extend_from_slice(std::slice::from_raw_parts(data.cast::<u8>(), length));
    length
}
unsafe extern "C" fn header(
    data: *mut c_char,
    size: usize,
    count: usize,
    context: *mut c_void,
) -> usize {
    let Some(length) = size.checked_mul(count) else {
        return 0;
    };
    if length > 65536 {
        return 0;
    }
    let capture = &mut *(context as *mut Capture);
    let line = String::from_utf8_lossy(std::slice::from_raw_parts(data.cast::<u8>(), length));
    if let Some((name, value)) = line.split_once(':') {
        if name.eq_ignore_ascii_case("Retry-After") {
            capture.retry_after = value.trim().to_owned();
        }
    }
    length
}
struct Resources {
    easy: *mut c_void,
    headers: *mut c_void,
}
impl Drop for Resources {
    fn drop(&mut self) {
        unsafe {
            curl_slist_free_all(self.headers);
            curl_easy_cleanup(self.easy);
        }
    }
}
impl Transport for CurlTransport {
    fn request(
        &mut self,
        method: &str,
        url: &str,
        bearer: &str,
        body: &[u8],
    ) -> Result<Response, Failure> {
        let url = CString::new(url).map_err(|_| Failure::new("INVALID_INPUT"))?;
        let method = CString::new(method).map_err(|_| Failure::new("INVALID_INPUT"))?;
        if bearer.contains(['\r', '\n']) {
            return Err(Failure::new("INVALID_INPUT"));
        }
        let mut capture = Capture {
            body: Vec::new(),
            retry_after: String::new(),
            oversized: false,
        };
        unsafe {
            let mut resources = Resources {
                easy: curl_easy_init(),
                headers: std::ptr::null_mut(),
            };
            if resources.easy.is_null() {
                return Err(Failure::service());
            }
            for value in [
                "Content-Type: application/json".to_string(),
                format!("Authorization: Bearer {}", bearer),
            ] {
                if bearer.is_empty() && value.starts_with("Authorization") {
                    continue;
                }
                let value = CString::new(value).map_err(|_| Failure::new("INVALID_INPUT"))?;
                let updated = curl_slist_append(resources.headers, value.as_ptr());
                if updated.is_null() {
                    return Err(Failure::service());
                }
                resources.headers = updated;
            }
            let handle = resources.easy;
            let options = [
                curl_easy_setopt(handle, 10002, url.as_ptr()), // CURLOPT_URL
                curl_easy_setopt(handle, 10036, method.as_ptr()), // CUSTOMREQUEST
                curl_easy_setopt(handle, 10023, resources.headers),
                curl_easy_setopt(handle, 52, 0 as c_long), // FOLLOWLOCATION
                curl_easy_setopt(handle, 64, 1 as c_long), // SSL_VERIFYPEER
                curl_easy_setopt(handle, 81, 2 as c_long), // SSL_VERIFYHOST
                curl_easy_setopt(handle, 99, 1 as c_long), // NOSIGNAL
                curl_easy_setopt(handle, 78, 5 as c_long), // CONNECTTIMEOUT
                curl_easy_setopt(handle, 13, 15 as c_long), // TIMEOUT
                curl_easy_setopt(
                    handle,
                    20011,
                    write_body
                        as unsafe extern "C" fn(*mut c_char, usize, usize, *mut c_void) -> usize,
                ),
                curl_easy_setopt(handle, 10001, &mut capture as *mut Capture as *mut c_void),
                curl_easy_setopt(
                    handle,
                    20079,
                    header as unsafe extern "C" fn(*mut c_char, usize, usize, *mut c_void) -> usize,
                ),
                curl_easy_setopt(handle, 10029, &mut capture as *mut Capture as *mut c_void),
                curl_easy_setopt(handle, 10015, body.as_ptr()),
                curl_easy_setopt(handle, 60, body.len() as c_long),
            ];
            if options.iter().any(|code| *code != 0) {
                return Err(Failure::new("TRANSPORT_CONFIGURATION_ERROR"));
            }
            let result = curl_easy_perform(handle);
            if capture.oversized {
                return Err(Failure::new("INVALID_RESPONSE"));
            }
            if result != 0 {
                return Err(Failure::service());
            }
            let mut status: c_long = 0;
            if curl_easy_getinfo(handle, 0x200002, &mut status as *mut c_long) != 0 {
                return Err(Failure::service());
            }
            Ok(Response {
                status: status as u16,
                body: capture.body,
                retry_after: capture.retry_after,
            })
        }
    }
}
