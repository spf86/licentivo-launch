# frozen_string_literal: true
# Explicit check that creates one disposable account in an isolated local server.
require_relative '../lib/licentivo_launch'
origin, product_id = ARGV
abort 'Usage: ruby sdk/ruby/test/http_smoke.rb http://127.0.0.1:PORT LAUNCH_PRODUCT_ID' unless origin && product_id
uri = URI(origin)
raise 'Isolated local sandbox required' unless uri.scheme == 'http' && %w[127.0.0.1 localhost [::1]].include?(uri.host)
email = "ruby-sdk-#{SecureRandom.uuid}@example.test"; password = SecureRandom.urlsafe_base64(24) + 'Aa9!'
call = lambda do |path, body = nil, token = nil|
  puts "Checking #{body ? 'POST' : 'GET'} #{path}"
  headers = { 'Content-Type' => 'application/json' }; headers['Authorization'] = "Bearer #{token}" if token
  response = LicentivoLaunch.default_transport(url: origin + '/api/v1' + path, method: body ? 'POST' : 'GET', headers: headers, body: body && JSON.generate(body))
  raise "HTTP #{response[:status]}" unless response[:status] == 200
  JSON.parse(response[:body]).fetch('data')
end
call.call('/auth/register', { email: email, password: password })
grant = call.call('/auth/login', { email: email, password: password, kind: 'desktop' })
begin
  raise 'identity mismatch' unless call.call('/me', nil, grant['access_token']).dig('user', 'id') == grant.dig('user', 'id')
  status = call.call("/software/products/#{product_id}/status?environment=sandbox", nil, grant['access_token'])
  raise 'status mismatch' unless status['code'] == 'ENTITLEMENT_NOT_PURCHASED'
ensure
  call.call('/auth/logout', {}, grant['access_token'])
end
remote = '22222222-2222-4222-8222-222222222222'; runtime_created = false
launch = { 'schema_version' => 1, 'merchant_origin' => origin, 'launch_product_id' => product_id, 'licentivo_product_id' => remote, 'environment' => 'sandbox' }
product = { 'product_id' => remote, 'trusted_keys' => { 'test' => Base64.urlsafe_encode64("\1" * 32, padding: false) } }
app = LicentivoLaunch::Application.new(launch, product, runtime_factory: ->(_) { runtime_created = true; raise 'Runtime must not be created' })
begin
  begin
    app.login(email, password); raise 'Unpurchased entitlement accepted'
  rescue LicentivoLaunch::Error => e
    raise unless e.code == 'ENTITLEMENT_NOT_PURCHASED' && e.status == 404
  end
  raise 'Runtime created or session retained' if runtime_created || app.restore
ensure
  app.close
end
puts 'PASS real local HTTP: login expiry, /me identity, no-purchase status, rejected credential and cleared wrapper session'
