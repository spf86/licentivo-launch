# frozen_string_literal: true
# Requires the actual official 0.11 SDK and native core. Missing prerequisites
# fail this test; this is deliberately separate from the stdlib-only unit suite.
require 'tmpdir'
require_relative '../lib/licentivo_launch'
require_relative '../lib/licentivo_launch/vendor/licentivo'
raise 'expected official SDK 0.11.1' unless Licentivo::VERSION == '0.11.1'
def assert(value) = (raise 'assertion failed' unless value)
Dir.mktmpdir('launch-ruby-runtime-') do |root|
  keypair = OpenSSL::PKey.generate_key('ED25519')
  launch = { 'schema_version' => 1, 'merchant_origin' => 'https://merchant.example', 'launch_product_id' => '11111111-1111-4111-8111-111111111111', 'licentivo_product_id' => '22222222-2222-4222-8222-222222222222', 'environment' => 'sandbox' }
  product = { 'product_id' => launch['licentivo_product_id'], 'base_url' => 'http://127.0.0.1:1', 'allow_http' => true, 'trusted_keys' => { 'test' => Base64.urlsafe_encode64(keypair.public_to_der.byteslice(-32, 32), padding: false) } }
  user = { 'id' => '33333333-3333-4333-8333-333333333333' }; license_key = 'lv_lic_' + 'L' * 43; now = Time.now.to_i
  first, cache_path, cache, down = true, nil, nil, false
  factory = lambda do |config|
    cache_path = config['cache_path']
    if first
      first = false
      probe = Licentivo::Client.new(config)
      begin
        device = probe.offline_request['device_id']
      ensure
        probe.close
      end
      claims = { version: 1, key_id: 'test', tenant_id: user['id'], product_id: product['product_id'], license_id: user['id'], device_id: device, activation_id: user['id'], mode: 'perpetual', features: ['export'], max_devices: 2, issued_at: now - 1, expires_at: now + 3600, runtime_policy: { heartbeat_seconds: 0, offline_mode: 'permanent', offline_seconds: 0 } }
      raw = JSON.generate(claims)
      lease = { version: 1, key_id: 'test', payload: Base64.urlsafe_encode64(raw, padding: false), signature: Base64.urlsafe_encode64(keypair.sign(nil, "LicenovaLease/v1\n" + raw), padding: false) }
      cache = { version: 1, license_hash: Digest::SHA256.hexdigest(license_key), observed_at: now, lease: lease }
      File.write(cache_path, JSON.generate(cache))
    end
    Licentivo::Client.new(config)
  end
  transport = lambda do |request|
    raise LicentivoLaunch::Error.new('NETWORK_UNAVAILABLE') if down
    data = if request[:url].end_with?('/auth/login')
             { user: user, access_token: 'T' * 43, expires_at: (Time.now + 3600).utc.iso8601 }
           elsif request[:url].end_with?('/me')
             { user: user }
           elsif request[:url].end_with?('/credential')
             { product_id: launch['launch_product_id'], license_key: license_key, version: 1 }
           else; {}; end
    { status: 200, headers: {}, body: JSON.generate(data: data) }
  end
  options = { store: LicentivoLaunch::MemoryStore.new, transport: transport, runtime_factory: factory, cache_dir: root }
  app = LicentivoLaunch::Application.new(launch, product, **options)
  app.login('test', 'secret'); assert(app.run_feature('export') { 42 } == 42); app.close
  down = true; restored = LicentivoLaunch::Application.new(launch, product, **options)
  assert(restored.restore[:offline_fallback]); assert(restored.run_feature('export') { 43 } == 43); restored.close
  cache[:lease][:signature] = 'A' * 86; File.write(cache_path, JSON.generate(cache))
  tampered = LicentivoLaunch::Application.new(launch, product, **options)
  begin
    tampered.restore; raise 'tampered lease allowed'
  rescue LicentivoLaunch::Error => e
    assert(e.code == 'OFFLINE_UNAVAILABLE')
  ensure
    tampered.close
  end
end
puts 'PASS official Ruby SDK 0.11: signed cache, feature execution, offline restore and tamper rejection'
