# frozen_string_literal: true
require 'tmpdir'
require 'socket'
require_relative '../lib/licentivo_launch'
require_relative '../lib/licentivo_launch/vendor/licentivo'
def assert(value, message = 'assertion failed') = (raise message unless value)
def b64(value) = Base64.urlsafe_encode64(value, padding: false)
product_id = '22222222-2222-4222-8222-222222222222'
launch_id = '11111111-1111-4111-8111-111111111111'
connection = '33333333-3333-4333-8333-333333333333'
activation = '55555555-5555-4555-8555-555555555555'
operation = '66666666-6666-4666-8666-666666666666'
key = OpenSSL::PKey.generate_key('ED25519')
server = TCPServer.new('127.0.0.1', 0)
origin = "http://127.0.0.1:#{server.addr[1]}"
state = { offline: false, releases: 0 }
# Handle each request on the server thread; all calls have bounded SDK timeouts.
thread = Thread.new do
 loop do
  socket = server.accept
  begin
   first = socket.gets; next unless first
   path = first.split[1]; headers = {}
   loop { line = socket.gets; break if line.nil? || line == "\r\n"; name, value = line.split(':', 2); headers[name.downcase] = value.strip }
   raw = socket.read(headers.fetch('content-length', '0').to_i); body = raw.empty? ? {} : JSON.parse(raw)
   status = state[:offline] ? 503 : 200
   data = if status == 503 then {}
    elsif path.end_with?('/auth/login') then { user: { id: connection }, access_token: 'T' * 43, expires_at: (Time.now + 3600).utc.iso8601 }
    elsif path.end_with?('/me') then { user: { id: connection } }
    elsif path.end_with?('/identity') then { product_id: launch_id, licentivo_product_id: product_id, launch_connection_id: connection, environment: 'sandbox', identity_token: 'I' * 64, expires_at: (Time.now + 120).utc.iso8601 }
    elsif path.end_with?('/auth/logout') then {}
    elsif path.end_with?('/sessions/logout')
     state[:releases] += 1
     { operation_id: operation, activation_id: activation, expected_generation: 1, status: 'succeeded' }
    else
     raise 'unexpected SDK route' unless %w[/api/v1/launch/exchange /api/v1/validate /api/v1/heartbeat].include?(path)
     assert(body['device_name'].is_a?(String) && !body['device_name'].strip.empty?, 'SDK device name missing')
     assert(body.dig('device_info', 'system_type').is_a?(String) && !body.dig('device_info', 'system_type').empty?, 'SDK display details missing')
     now = Time.now.to_i
     claims = { version: 1, key_id: 'test', tenant_id: connection, product_id: product_id, license_id: '44444444-4444-4444-8444-444444444444', device_id: body['device_id'], activation_id: activation, mode: 'timed', features: ['export'], max_devices: 1, issued_at: now, expires_at: now + 3600, runtime_policy: { offline_mode: 'limited', offline_seconds: 3600, heartbeat_seconds: 600 }, capabilities: { license_model: 'node_locked', launch_connection_id: connection, environment: 'sandbox', session_id: '77777777-7777-4777-8777-777777777777', session_expires_at: now + 3600, binding_version: 1 } }
     payload = JSON.generate(claims)
     { activation_id: activation, device_key: 'lv_dev_' + 'd' * 43, lease: { version: 1, key_id: 'test', payload: b64(payload), signature: b64(key.sign(nil, "LicenovaLease/v1\n" + payload)) }, heartbeat_seconds: 600, logout_ticket: { operation_id: operation, product_id: product_id, activation_id: activation, expected_generation: 1, release_token: 'lv_out_' + 'o' * 43 } }
    end
   payload = JSON.generate(data: data)
   socket.write("HTTP/1.1 #{status} Status\r\nContent-Type: application/json\r\nContent-Length: #{payload.bytesize}\r\nConnection: close\r\n\r\n#{payload}")
  ensure
   socket.close
  end
 end
rescue IOError, Errno::EBADF
end
begin
 Dir.mktmpdir('launch-ruby-account-') do |root|
  launch = { 'schema_version' => 1, 'merchant_origin' => origin, 'launch_product_id' => launch_id, 'licentivo_product_id' => product_id, 'environment' => 'sandbox' }
  product = { 'base_url' => origin, 'product_id' => product_id, 'allow_http' => true, 'trusted_keys' => { 'test' => b64(key.public_to_der.byteslice(-32, 32)) } }
  store = LicentivoLaunch::MemoryStore.new
  app = LicentivoLaunch::Application.new(launch, product, store: store, cache_dir: root)
  app.login('a@example.invalid', 'password'); assert(app.run_feature('export') { 42 } == 42); app.close
  cache = Dir.glob('**/lease.json', base: root).map { |name| File.join(root, name) }.first; assert(cache, 'SDK cache missing')
  state[:offline] = true
  app = LicentivoLaunch::Application.new(launch, product, store: store, cache_dir: root)
  begin; app.restore; raise 'Launch selected offline authorization'; rescue LicentivoLaunch::Error; end
  sdk_config = product.merge('license_key' => '', 'identity_token' => '', 'launch_connection_id' => connection, 'environment' => 'sandbox', 'cache_path' => cache)
  sdk = Licentivo::Client.new(sdk_config); sdk.start; assert(sdk.run_feature('export') { 43 } == 43)
  ran = false; begin; sdk.run_feature('absent') { ran = true }; rescue StandardError; end; assert(!ran); sdk.close
  assert(!app.logout, 'pending logout reported success'); app.close
  assert(File.exist?(cache + '.logout.json') && File.exist?(cache + '.credential.json'))
  state[:offline] = false; sdk = Licentivo::Client.new(sdk_config); sdk.retry_pending_logouts; assert(!sdk.status['allowed']); sdk.close
  assert(state[:releases] > 0)
 end
 puts 'Ruby official SDK: account exchange, signed permission, SDK-owned offline checks, feature denial and durable logout passed.'
ensure
 server.close; thread.join(2); thread.kill if thread.alive?
end
