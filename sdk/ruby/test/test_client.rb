# frozen_string_literal: true
# Only Ruby standard libraries are needed: no testing gem installation.
require 'tmpdir'
require 'socket'
require_relative '../lib/licentivo_launch'
include LicentivoLaunch

LAUNCH = { 'schema_version' => 1, 'merchant_origin' => 'https://merchant.example', 'launch_product_id' => '11111111-1111-4111-8111-111111111111', 'licentivo_product_id' => '22222222-2222-4222-8222-222222222222', 'environment' => 'sandbox' }.freeze
PRODUCT = { 'product_id' => LAUNCH['licentivo_product_id'], 'trusted_keys' => { 'test' => Base64.urlsafe_encode64("\1" * 32, padding: false) } }.freeze
USER = { 'id' => '33333333-3333-4333-8333-333333333333', 'email' => 'unit@example.test' }.freeze
GRANT = { 'user' => USER, 'access_token' => 'T' * 43, 'expires_at' => (Time.now + 3600).utc.iso8601 }.freeze
CREDENTIAL = { 'product_id' => LAUNCH['launch_product_id'], 'licentivo_product_id' => PRODUCT['product_id'], 'environment' => 'sandbox', 'launch_connection_id' => '44444444-4444-4444-8444-444444444444', 'identity_token' => 'I' * 64, 'expires_at' => (Time.now + 120).utc.iso8601 }.freeze
def assert(value, message = 'assertion failed') = (raise message unless value)
def expect(code)
  begin
    yield
  rescue LicentivoLaunch::Error => e
    assert(e.code == code, "expected #{code}, got #{e.code}"); return
  end
  raise "expected #{code}, succeeded instead"
end
def reply(data) = { status: 200, headers: {}, body: JSON.generate(data: data) }
def api_failure(code, status = 403, retry_after = '3') = { status: status, headers: { 'retry-after' => retry_after }, body: JSON.generate(error: { code: code }) }
class InspectStore < MemoryStore
  attr_reader :scope, :record
  attr_accessor :fail_saves, :fail_deletes
  def load(scope); @scope = scope; super; end
  def save(scope, record); raise Error.new('TEST_STORAGE_FAILED') if @fail_saves; @scope, @record = scope, JSON.parse(JSON.generate(record)); super; end
  def delete(scope); raise Error.new('TEST_STORAGE_FAILED') if @fail_deletes; super; end
end
class FakeRuntime
  attr_reader :config, :started, :closed, :released
  def initialize(config, fixture) = (@config, @fixture, @started, @closed, @released = config, fixture, 0, 0, 0)
  def status = { 'allowed' => @fixture.offline, 'state' => @fixture.offline ? 'offline_valid' : 'verification_required' }
  def start = @started += 1
  def close = @closed += 1
  def deactivate = @released += 1
  def run_feature(feature)
    raise LicentivoLaunch::Error.new('FEATURE_NOT_ALLOWED') unless feature == 'export'
    yield
  end
end
class Fixture
  attr_reader :app, :store, :calls, :runtimes, :waits, :root, :options
  attr_accessor :failure, :pending, :offline, :logout_outage
  def initialize(root)
    @root, @store, @calls, @runtimes, @waits, @pending, @offline = root, InspectStore.new, [], [], [], 0, true
    @grant=GRANT.merge('access_token'=>Base64.urlsafe_encode64(OpenSSL::Random.random_bytes(32),padding:false))
    transport = lambda do |request|
      @calls << request
      raise LicentivoLaunch::Error.new('NETWORK_UNAVAILABLE') if @logout_outage && request[:url].end_with?('/auth/logout')
      next reply({}) if request[:url].end_with?('/auth/logout')
      if @failure && !(request[:url].end_with?('/me') && @failure.is_a?(Hash) && @failure[:body].include?('ENTITLEMENT_PENDING'))
        raise @failure if @failure.is_a?(Exception)
        next @failure
      end
      next reply(@grant) if request[:url].end_with?('/auth/login')
      next reply('user' => USER, 'session_id' => 'opaque') if request[:url].end_with?('/me')
      if @pending > 0
        @pending -= 1; next api_failure('ENTITLEMENT_PENDING')
      end
      reply(CREDENTIAL)
    end
    factory = lambda { |config| value = FakeRuntime.new(config, self); @runtimes << value; value }
    @options = { store: @store, transport: transport, runtime_factory: factory, cache_dir: root, sleep: ->(seconds, _) { @waits << seconds } }
    @app = Application.new(LAUNCH, PRODUCT, **@options)
  end
end
$passed = 0
def test(name)
  Dir.mktmpdir('launch-ruby-') do |root|
    fixture = Fixture.new(root)
    begin
      yield fixture
      $passed += 1; puts "PASS #{name}"
    ensure
      fixture.app.close
    end
  end
rescue StandardError => e
  warn "FAIL #{name}: #{e.class}: #{e.message}\n#{e.backtrace.first(5).join("\n")}"; exit 1
end

test('login, callback guard, restore and release') do |f|
  f.app.login('a', 'secret'); assert(f.calls[0][:headers]['Authorization'].nil?)
  assert(JSON.parse(f.calls[0][:body])['kind'] == 'desktop')
  assert(JSON.parse(f.calls[1][:body])['environment'] == 'sandbox')
  assert(f.app.run_feature('export') { 42 } == 42)
  ran = false; expect('FEATURE_NOT_ALLOWED') { f.app.run_feature('other') { ran = true } }; assert(!ran)
  expect('WORKFLOW_BUSY') { f.app.run_feature('export') { f.app.close } }
  f.app.close; assert(f.store.load(f.store.scope))
  next_app = Application.new(LAUNCH, PRODUCT, **f.options)
  assert(!next_app.restore.key?(:offline_fallback)); next_app.release_device
  assert(f.runtimes[1].released == 1 && f.store.load(f.store.scope).nil?); next_app.close
end
test('failed saves during offline logout remove the restorable account') do |f|
  f.app.login('a','b');scope=f.store.scope;f.store.fail_saves=true;f.logout_outage=true
  expect('ACCOUNT_LOGOUT_STORAGE_FAILED'){f.app.logout};assert(f.store.load(scope).nil?)
  expect('LOGIN_REQUIRED'){f.app.run_feature('export'){raise 'signed-out callback'}}
  f.app.close;f.store.fail_saves=false;restarted=Application.new(LAUNCH,PRODUCT,**f.options)
  begin;count=f.calls.length;assert(restarted.restore.nil?);assert(f.calls.length==count);ensure;restarted.close;end
end
test('total store failure reports an error and stops local work') do |f|
  f.app.login('a','b');f.store.fail_saves=f.store.fail_deletes=true;f.logout_outage=true
  expect('ACCOUNT_LOGOUT_STORAGE_FAILED'){f.app.logout}
  expect('LOGIN_REQUIRED'){f.app.run_feature('export'){raise 'signed-out callback'}}
  restarted=Application.new(LAUNCH,PRODUCT,**f.options)
  begin;expect('ACCOUNT_LOGOUT_STORAGE_FAILED'){restarted.restore};ensure;restarted.close;end
end
test('a completed logout cannot adopt a later login on retry') do |f|
  f.app.login('a','b');assert(f.app.logout);newer=Application.new(LAUNCH,PRODUCT,**f.options)
  begin;newer.login('a','b');scope=f.store.scope;assert(f.app.logout);assert(f.store.load(scope));assert(newer.restore);ensure;newer.close;end
end
test('old instance keeps its retry without overwriting a new login') do |f|
  count=0;block_old=false;original=f.options[:transport];old='A'*43;newer='B'*43
  options=f.options.merge(transport:lambda{|request|
    if request[:url].end_with?('/auth/login');count+=1;next reply(GRANT.merge('access_token'=>count==1 ? old : newer));end
    raise Error.new('NETWORK_UNAVAILABLE') if block_old&&request[:url].end_with?('/auth/logout')&&request[:headers]['Authorization']=='Bearer '+old
    original.call(request)
  })
  a=Application.new(LAUNCH,PRODUCT,**options);b=Application.new(LAUNCH,PRODUCT,**options);c=nil
  begin
    a.login('a','b');b.login('a','b');scope=f.store.scope;block_old=true;assert(a.logout==false)
    assert(f.store.load(scope)['token']==newer&&!f.store.load(scope)['logout_pending'])
    a.close;block_old=false;c=Application.new(LAUNCH,PRODUCT,**options);opens=f.runtimes.length
    assert(c.restore);assert(f.runtimes.length==opens+1);assert(f.store.load(scope)['token']==newer)
    assert(f.store.load(scope+"\naccount-logout-queue-v1").nil?)
  ensure;a.close;b.close;c&.close;end
end
[[401, 'AUTH_REQUIRED'], [403, 'ENTITLEMENT_EXPIRED'], [403, 'ENTITLEMENT_SUSPENDED'], [404, 'ENTITLEMENT_NOT_PURCHASED']].each do |status, code|
  test("explicit #{status} #{code} clears account state and preserves SDK files") do |f|
    f.app.login('a', 'b'); path = f.runtimes[0].config['cache_path']; File.write(path, 'signed-cache-placeholder')
    f.failure = api_failure(code, status); expect(code) { f.app.restore }
    assert(f.store.load(f.store.scope).nil? && File.exist?(path) && f.runtimes.length == 1)
  end
end
test('Launch outage never chooses an offline grant') do |f|
  f.app.login('a', 'b'); f.failure = LicentivoLaunch::Error.new('NETWORK_UNAVAILABLE')
  expect('NETWORK_UNAVAILABLE') { f.app.restore }; assert(f.runtimes.length == 1)
  f.failure = { status: 503, headers: {}, body: 'unavailable' }; expect('SERVICE_UNAVAILABLE') { f.app.restore }
  token = CancellationToken.new; token.cancel; expect('CANCELLED') { f.app.restore(cancellation: token) }
end

test('identity is ephemeral and pending never grants access') do |f|
  f.app.login('a', 'b'); saved = JSON.generate(f.store.record)
  assert(!saved.include?('I' * 64) && !saved.include?('license_key'))
  assert(f.runtimes[0].config['identity_token'] == 'I' * 64 && f.runtimes[0].config['license_key'] == '')
  f.failure = api_failure('ENTITLEMENT_PENDING'); expect('ENTITLEMENT_PENDING') { f.app.restore }
  assert(f.waits.empty? && f.runtimes.length == 1)
end

test('account logout persists across restart and restore only retries revocation') do |f|
  f.app.login('a','b');f.logout_outage=true
  assert(!f.app.logout);assert(f.store.record['logout_pending']);expect('LOGIN_REQUIRED'){f.app.run_feature('export'){raise 'signed-out callback'}}
  f.app.close;other=Application.new(LAUNCH,PRODUCT,**f.options);before=f.calls.length
  expect('ACCOUNT_LOGOUT_PENDING'){other.restore};assert(f.calls[before..].all?{|r|r[:url].end_with?('/auth/logout')})
  f.logout_outage=false;before=f.calls.length;assert(other.restore.nil?);assert(f.calls[before..].any?{|r|r[:url].end_with?('/auth/logout')});assert(f.store.load(f.store.scope).nil?);other.close
end

test('expired session and account mismatch fail closed') do |f|
  f.app.login('a', 'b'); record = f.store.record; record['expires_at'] = '2020-01-01T00:00:00Z'; f.store.save(f.store.scope, record)
  expect('SESSION_EXPIRED') { f.app.restore }; assert(f.runtimes.length == 1)
  f.app.login('a', 'b'); f.failure = reply('user' => { 'id' => '44444444-4444-4444-8444-444444444444' })
  expect('SESSION_MISMATCH') { f.app.restore }; assert(f.store.load(f.store.scope).nil?)
end
test('scope and public configuration') do |f|
  f.app.login('a', 'b')
  [{ 'environment' => 'live' }, { 'merchant_origin' => 'https://other.example' }].each do |change|
    app = Application.new(LAUNCH.merge(change), PRODUCT, **f.options); assert(app.restore.nil?); app.close
  end
  expect('PUBLIC_CONFIG_INVALID') { LicentivoLaunch.validate_config(LAUNCH, PRODUCT.merge('management_token' => 'secret')) }
  expect('PUBLIC_CONFIG_INVALID') { LicentivoLaunch.validate_config(LAUNCH, PRODUCT.merge('license_key' => 'secret')) }
  expect('INVALID_CONFIG') { LicentivoLaunch.validate_config(LAUNCH.merge('merchant_origin' => 'http://untrusted.example'), PRODUCT) }
end
test('AES-GCM encrypted store rejects tamper and isolates scope') do |f|
  store = EncryptedFileStore.new(f.root, 'k' * 32)
  store.save('scope', 'token' => 'secret-token'); assert(store.load('scope') == { 'token' => 'secret-token' })
  file = Dir[File.join(f.root, '*.aesgcm')].first; raw = File.binread(file); assert(!raw.include?('secret-token'))
  raw.setbyte(raw.bytesize - 1, raw.getbyte(raw.bytesize - 1) ^ 1); File.binwrite(file, raw)
  expect('INVALID_SAVED_SESSION') { store.load('scope') }; assert(store.load('other').nil?)
end
test('real HTTP transport never follows bearer redirect') do |_f|
  server = TCPServer.new('127.0.0.1', 0)
  thread = Thread.new do
    socket = server.accept
    begin
      loop { line = socket.gets; break if line.nil? || line == "\r\n" }
      socket.write("HTTP/1.1 302 Found\r\nLocation: http://127.0.0.1:1/steal\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
    ensure
      socket.close
    end
  end
  begin
    expect('REDIRECT_REJECTED') { LicentivoLaunch.default_transport(url: "http://127.0.0.1:#{server.addr[1]}/", method: 'GET', headers: { 'Authorization' => 'Bearer secret' }) }
  ensure
    server.close; thread.join(2); thread.kill if thread.alive?
  end
end
test('HTTP connection remains open until an unread-body response is consumed') do |_f|
  server = TCPServer.new('127.0.0.1', 0); headers = []
  thread = Thread.new do
    socket = server.accept
    begin
      loop { line = socket.gets; break if line.nil? || line == "\r\n"; headers << line }
      # Like the logout endpoint, respond without parsing the request body.
      socket.write("HTTP/1.1 200 OK\r\nContent-Length: 11\r\nContent-Type: application/json\r\n\r\n{\"data\":{}}")
      socket.read(2)
    ensure
      socket.close
    end
  end
  begin
    result = LicentivoLaunch.default_transport(url: "http://127.0.0.1:#{server.addr[1]}/logout", method: 'POST', headers: { 'Content-Type' => 'application/json' }, body: '{}')
    assert(result[:status] == 200 && JSON.parse(result[:body]) == { 'data' => {} })
    assert(headers.none? { |h| h.match?(/\AConnection:\s*close\s*\z/i) }, 'implicit Connection: close risks an unread-body reset')
  ensure
    server.close; thread.join(2); thread.kill if thread.alive?
  end
end
puts "#{$passed} Ruby wrapper tests passed"
