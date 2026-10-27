# Pengujian HTTP pada server pelanggan dan jaringan privat

[Pilih bahasa](http-testing/README.md)

Semua alamat server, IP, dan nama host dalam panduan ini merupakan milik instalasi Launch pelanggan sendiri. Pelanggan tidak perlu bergabung dengan jaringan privat Licentivo atau pengembang. Server pengujian publik maupun server di jaringan privat pelanggan didukung.

SDK menggunakan protokol dalam `merchant_origin`: `https://` memakai HTTPS; `http://` hanya diizinkan oleh izin pengujian di bawah. Permintaan HTTPS yang gagal tidak pernah otomatis beralih ke HTTP. Sakelar tidak menulis ulang alamat atau memasang sertifikat TLS.

## Dua sakelar terpisah, keduanya nonaktif secara default

| Sakelar | Lokasi pengaturan oleh pelanggan | Efek |
| --- | --- | --- |
| **Izinkan HTTP (pengujian)** untuk satu kunci API | Licentivo → Pengembangan dan integrasi → API Key → Buat API Key atau Pengaturan akses | Mengizinkan kunci ini saja membuat atau memperbarui koneksi akun Launch untuk origin merchant HTTP. Kunci hanya-baca tidak memperoleh izin tulis. Rotasi kunci mempertahankan pengaturan ini. |
| **Izinkan SDK menggunakan HTTP (hanya lingkungan pengujian)** | Launch milik pelanggan → Pengaturan sistem → Situs dan keamanan → Sertifikat SSL dan protokol akses | Mengizinkan instalasi Launch ini menerima permintaan akun SDK melalui HTTP dan menghasilkan kode pengujian HTTP. Pertukaran identitas untuk otorisasi saat berjalan melalui HTTP dibatasi ke `sandbox`; `live` memerlukan HTTPS. |

Pelanggan mengelola kedua sakelar sendiri. Administrator platform Licentivo maupun mode pengembangan seluruh platform tidak diperlukan. Pengaturan ini hanya memengaruhi alamat Launch pelanggan. Launch dan SDK lisensi resmi tetap menghubungi Licentivo melalui HTTPS.

## Menyiapkan server pengujian pelanggan

1. Perbarui server Licentivo, server Launch, dan paket SDK. SDK lama yang hanya mengizinkan HTTP pada localhost harus diperbarui; pengaturan server tidak dapat mengubah validasi URL SDK yang sudah terpasang.
2. Di ruang kerja Licentivo pelanggan, buat kunci pengujian khusus dengan izin **baca/tulis**, atau buka **Pengaturan akses** kunci yang aktif dan pilih **Izinkan HTTP (pengujian)**.
3. Di pengaturan **Situs dan keamanan** Launch pelanggan, masukkan alamat yang dapat dijangkau klien pengujian, misalnya `http://192.168.1.20:18080`, `http://test-server:18080`, atau domain pengujian pelanggan. Aktifkan **Izinkan SDK menggunakan HTTP (hanya lingkungan pengujian)** lalu simpan. Gunakan origin tanpa path, kueri, fragmen, atau garis miring di akhir.
4. Hubungkan kembali Launch ke Licentivo dengan kunci tersebut. Membaca produk dan kebijakan saja belum membentuk koneksi akun; pastikan koneksi otorisasi akun juga siap.
5. Di **Integrasi aplikasi → Pemeriksaan integrasi dan kode**, pilih **sandbox**, jalankan pemeriksaan, lalu salin kode yang baru dibuat. Setelah pemeriksaan berhasil, opsi pengujian HTTP disertakan otomatis. Gunakan akun dan pembelian pengujian.

IP publik atau catatan DNS publik tidak diperlukan. Klien pengujian harus dapat meresolusikan dan menjangkau alamat Launch yang dikonfigurasi. `localhost` berarti mesin yang menjalankan SDK; mesin lain harus menggunakan IP server atau nama host internalnya. Licentivo mencatat origin dan kunci tanda tangan yang dipatok, tanpa menghubungi kembali origin itu atau mengambil kunci darinya. Launch dan klien lisensi online tetap memerlukan akses keluar ke endpoint HTTPS Licentivo. Jaringan privat yang sepenuhnya terputus tidak dapat menjalankan otorisasi akun online.

## Cara SDK memilih HTTP atau HTTPS

Konfigurasi publik Launch yang dihasilkan untuk pengujian aktif pada server pelanggan setara dengan:

```json
{
  "schema_version": 1,
  "merchant_origin": "http://192.168.1.20:18080",
  "launch_product_id": "11111111-1111-4111-8111-111111111111",
  "licentivo_product_id": "22222222-2222-4222-8222-222222222222",
  "environment": "sandbox",
  "allow_http": true
}
```

Alamat menentukan protokol. `allow_http: true` hanya mengizinkan origin merchant HTTP dalam `sandbox`; URL HTTPS tidak pernah diturunkan ke HTTP. Konfigurasi produk lisensi resmi tetap menggunakan HTTPS. Opsi pemanggilan langsung yang setara untuk sembilan bahasa:

| SDK | Opsi pengujian HTTP eksplisit |
| --- | --- |
| Go | `launchclient.Config{Environment: "sandbox", AllowHTTP: true}` |
| Python | `Application.connect(..., environment="sandbox", allow_http=True)` |
| JavaScript / TypeScript | `Application.connect(..., {environment: "sandbox", allowHTTP: true})` |
| C# | `Application.Connect(..., "sandbox", allowHttp: true)` |
| Java | `Launch.connect(..., "sandbox", true)` |
| Rust | `Application::connect_with_http(..., "sandbox", true)` |
| Ruby | `Application.connect(..., environment: 'sandbox', allow_http: true)` |
| C | `launch_application_connect_http(..., "sandbox", 1, 0, NULL)` |
| C++ | `Application::ConnectHTTP(..., "sandbox")` |

Opsi ini memerlukan SDK yang diperbarui. SDK tidak pernah menerima atau meminta kunci API pengelolaan merchant. Launch mengekspor konfigurasi publik dan juga memeriksa sakelar HTTP-nya di server; mengubah parameter klien saja tidak dapat mengaktifkan HTTP jika sakelar server nonaktif. Konstruktor lama mempertahankan pengecualian localhost sandbox untuk kompatibilitas, tetapi tidak dapat melewati sakelar server yang nonaktif secara default.

## Mengakhiri pengujian atau beralih ke HTTPS

HTTP tidak mengenkripsi kata sandi, token akun, atau data lain. Jaringan privat pelanggan pun dapat dipantau atau disusupi. Memasukkan kunci API ke administrasi Launch melalui HTTP juga mengekspos permintaan browser ke Launch, meskipun Launch menghubungi Licentivo melalui HTTPS. Gunakan kredensial pengujian khusus; gunakan HTTPS untuk produksi dan administrasi sensitif.

Setelah menyiapkan listener HTTPS atau proxy TLS yang berfungsi, perbarui origin merchant efektif Launch, hubungkan kembali Licentivo bila perlu, lalu buat ulang dan perbarui konfigurasi integrasi klien. URL HTTP yang disalin atau dikompilasi tidak berubah otomatis. Nonaktifkan kedua sakelar HTTP setelah pengujian selesai. Pertukaran identitas SDK dalam `live` selalu memerlukan HTTPS, apa pun pengaturan kedua sakelar.

Menonaktifkan sakelar Launch menolak permintaan SDK HTTP berikutnya dengan `SDK_HTTP_DISABLED`; permintaan identitas HTTP dalam `live` mengembalikan `SDK_HTTPS_REQUIRED`. Izin offline yang sudah ditandatangani tetap mengikuti aturan kedaluwarsa dan pencabutan normal Licentivo. Menonaktifkan izin HTTP kunci API memblokir pembuatan atau pembaruan koneksi HTTP berikutnya oleh kunci itu; tidak menghapus koneksi lama, mencabut pembelian, atau membatalkan izin yang telah diterbitkan. Nonaktifkan koneksi secara terpisah jika harus berhenti memberikan otorisasi.

## Mendiagnosis kegagalan koneksi

Launch menampilkan tahap yang gagal (membaca produk atau kebijakan, mencari koneksi, atau membuat koneksi sandbox/live), penyebab yang spesifik, serta kode kesalahan dan ID permintaan Licentivo bila tersedia. Kegagalan tersimpan tetap terlihat setelah halaman dimuat ulang. Pesan kesalahan tidak pernah menampilkan kunci API pengelolaan.

| Kesalahan | Tindakan |
| --- | --- |
| `LAUNCH_HTTPS_REQUIRED` | Aktifkan HTTP untuk kunci API yang benar-benar digunakan Launch, atau konfigurasikan URL merchant HTTPS yang berfungsi. |
| `LICENSE_KEY_INVALID` | Periksa kelengkapan kunci, kedaluwarsa, pencabutan, serta status pemilik dan ruang kerjanya. |
| `LICENSE_KEY_FORBIDDEN` | Gunakan kunci baca/tulis milik Owner yang valid. Izin HTTP tidak menambah izin tulis. |
| `SDK_HTTP_DISABLED` | Aktifkan sakelar pengujian HTTP SDK di Launch milik pelanggan, atau gunakan HTTPS. |
| `SDK_HTTPS_REQUIRED` | Gunakan HTTPS untuk `live`; pengujian HTTP harus memakai `sandbox`. |
| `LICENTIVO_TIMEOUT` / `LICENTIVO_NETWORK_ERROR` | Periksa jaringan keluar, DNS, firewall, dan proxy pada mesin pengujian pelanggan. |
| `LICENTIVO_TLS_ERROR` | Periksa jam sistem, sertifikat tepercaya, dan proxy HTTPS. Tetap aktifkan verifikasi TLS. |
