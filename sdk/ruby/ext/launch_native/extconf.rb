# frozen_string_literal: true
require 'mkmf'
require 'shellwords'
package = File.expand_path('../..', __dir__)
core = File.join(package, 'vendor/c')
abort 'Launch bundled SDK core is missing' unless File.file?(File.join(core, 'licentivo.c'))
$INCFLAGS << " -I#{Shellwords.escape(core)}"
$VPATH.concat([core, File.join(core, 'vendor')])
$srcs = %w[licentivo.c cJSON.c monocypher.c monocypher-ed25519.c]
$CFLAGS << ' -std=c11'
bundled_curl = File.join(package, 'vendor/curl')
windows = RbConfig::CONFIG['host_os'].match?(/mswin|mingw/)
curl = ENV['LICENTIVO_CURL_ROOT'] || (windows && File.directory?(bundled_curl) ? bundled_curl : nil)
dir_config('curl', File.join(curl, 'include'), File.join(curl, 'lib')) if curl
pkg_config('libcurl') unless curl
abort 'A libcurl development package is required by this source build' unless have_header('curl/curl.h') && have_library('curl', 'curl_easy_init')
if RbConfig::CONFIG['host_os'].match?(/mswin|mingw/)
  $DLDFLAGS << ' -Wl,--export-all-symbols'
  have_library('bcrypt'); have_library('advapi32')
else
  pkg_config('openssl')
  abort 'OpenSSL development files are required' unless have_header('openssl/evp.h') && have_library('crypto', 'EVP_sha256')
  have_library('pthread'); have_library('m')
end
if curl && RbConfig::CONFIG['host_os'].match?(/mswin|mingw/)
  $INSTALLFILES << ['../../vendor/curl/bin/*.dll', '$(RUBYARCHDIR)', '../../vendor/curl/bin']
end
create_makefile('licentivo_launch/vendor/licentivo/native/licentivo_native')
