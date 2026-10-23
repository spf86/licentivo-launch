# frozen_string_literal: true
require 'json'
require 'uri'
require 'net/http'
require 'openssl'
require 'base64'
require 'digest'
require 'securerandom'
require 'fileutils'
require 'time'
require 'monitor'

module LicentivoLaunch
  VERSION = '0.1.2'
  LIMIT = 1 << 20
  UUID = /\A[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\z/
  class Error < StandardError
    attr_reader :code, :status, :retry_after_seconds
    def initialize(code, status: 0, retry_after_seconds: 0)
      @code = code.to_s.match?(/\A[A-Z][A-Z0-9_]{0,79}\z/) ? code.to_s : 'REQUEST_FAILED'
      @status, @retry_after_seconds = status, [retry_after_seconds, 0].max
      super("Launch: #{@code}")
    end
  end
  # Cancellation is cooperative. A running native SDK operation finishes before
  # cancellation is observed; it is never reclassified as an offline success.
  class CancellationToken
    def cancel = @cancelled = true
    def check! = (raise Error.new('CANCELLED') if @cancelled)
  end
  class MemoryStore
    def initialize = @values = {}
    def persistent? = false
    def load(scope) = @values.key?(scope) ? JSON.parse(JSON.generate(@values[scope])) : nil
    def save(scope, record) = @values[scope] = JSON.parse(JSON.generate(record))
    def delete(scope) = @values.delete(scope)
  end
  # The application must obtain this encryption key from its OS credential vault.
  # Never save the key beside the encrypted records or inside product.json.
  class EncryptedFileStore
    def initialize(directory, key)
      raise Error.new('STORE_KEY_REQUIRED') unless key.is_a?(String) && key.bytesize == 32
      @root, @key = File.expand_path(directory), key.b.dup
    end
    def persistent? = true
    def secure? = true
    def path(scope) = File.join(@root, "#{Digest::SHA256.hexdigest(scope)}.aesgcm")
    def load(scope)
      return nil unless File.exist?(path(scope))
      raise Error.new('INVALID_SAVED_SESSION') if File.size(path(scope)) > LIMIT
      raw = File.binread(path(scope))
      begin
        raise ArgumentError unless raw.bytesize >= 30 && raw.getbyte(0) == 1
        cipher = OpenSSL::Cipher.new('aes-256-gcm').decrypt
        cipher.key, cipher.iv, cipher.auth_tag = @key, raw.byteslice(1, 12), raw.byteslice(13, 16)
        cipher.auth_data = scope
        plain = cipher.update(raw.byteslice(29..)) + cipher.final
        JSON.parse(plain)
      rescue OpenSSL::Cipher::CipherError, JSON::ParserError, ArgumentError
        raise Error.new('INVALID_SAVED_SESSION')
      ensure
        plain&.replace("\0" * plain.bytesize)
      end
    end
    def save(scope, record)
      FileUtils.mkdir_p(@root, mode: 0o700)
      plain, iv = JSON.generate(record), SecureRandom.random_bytes(12)
      raise Error.new('INVALID_SAVED_SESSION') if plain.bytesize > LIMIT - 64
      cipher = OpenSSL::Cipher.new('aes-256-gcm').encrypt
      cipher.key, cipher.iv, cipher.auth_data = @key, iv, scope
      encrypted = cipher.update(plain) + cipher.final
      temporary = File.join(@root, ".session-#{SecureRandom.uuid}")
      File.open(temporary, File::WRONLY | File::CREAT | File::EXCL, 0o600) { |f| f.binmode; f.write("\1" + iv + cipher.auth_tag + encrypted) }
      File.rename(temporary, path(scope))
    ensure
      FileUtils.rm_f(temporary) if temporary
      plain&.replace("\0" * plain.bytesize)
    end
    def delete(scope) = FileUtils.rm_f(path(scope))
  end

  def self.origin(value, environment)
    uri = URI.parse(value)
    allowed = uri.is_a?(URI::HTTPS) || environment == 'sandbox' && uri.is_a?(URI::HTTP) && %w[localhost 127.0.0.1 [::1]].include?(uri.host)
    raise Error.new('INVALID_CONFIG') unless allowed && uri.host && !uri.userinfo && !uri.query && !uri.fragment && ['', '/'].include?(uri.path)
    uri.path = ''; uri.to_s
  rescue URI::InvalidURIError, TypeError
    raise Error.new('INVALID_CONFIG')
  end
  def self.validate_config(launch, product)
    fields = %w[schema_version merchant_origin launch_product_id licentivo_product_id environment]
    unless launch.is_a?(Hash) && (launch.keys - fields).empty? && launch['schema_version'] == 1 && UUID.match?(launch['launch_product_id'].to_s) && UUID.match?(launch['licentivo_product_id'].to_s) && %w[sandbox live].include?(launch['environment'])
      raise Error.new('INVALID_CONFIG')
    end
    public_fields = %w[product_id trusted_keys base_url app_version license_key cache_path device_name allow_http]
    unless product.is_a?(Hash) && (product.keys - public_fields).empty? && product['product_id'] == launch['licentivo_product_id'] && [nil, ''].include?(product['license_key']) && [nil, ''].include?(product['cache_path']) && [nil, true, false].include?(product['allow_http'])
      raise Error.new('PUBLIC_CONFIG_INVALID')
    end
    %w[app_version device_name].each do |field|
      value = product[field]
      raise Error.new('PUBLIC_CONFIG_INVALID') if value && (!value.is_a?(String) || value.length > (field == 'app_version' ? 80 : 128))
    end
    keys = product['trusted_keys']
    unless keys.is_a?(Hash) && (1..32).cover?(keys.length) && keys.all? { |id, key| id.is_a?(String) && (1..128).cover?(id.length) && key.is_a?(String) && key.match?(/\A[A-Za-z0-9_-]{43}\z/) && Base64.urlsafe_encode64(Base64.urlsafe_decode64(key), padding: false) == key }
      raise Error.new('PUBLIC_CONFIG_INVALID')
    end
    runtime_origin = origin(product['base_url'] || 'https://www.licentivo.com', launch['environment'])
    raise Error.new('PUBLIC_CONFIG_INVALID') if runtime_origin.start_with?('http:') && product['allow_http'] != true
    safe = { 'product_id' => product['product_id'], 'trusted_keys' => keys, 'base_url' => runtime_origin }
    safe['app_version'] = product['app_version'] if product['app_version']
    safe['allow_http'] = true if product['allow_http'] == true
    [JSON.parse(JSON.generate(launch.merge('merchant_origin' => origin(launch['merchant_origin'], launch['environment'])))), JSON.parse(JSON.generate(safe))]
  end
  def self.load_config(launch_path, product_path)
    read = lambda do |path|
      raise Error.new('CONFIG_TOO_LARGE') if File.size(path) > 65_536
      JSON.parse(File.read(path, encoding: 'bom|utf-8'))
    end
    validate_config(read.call(launch_path), read.call(product_path))
  end

  # Net::HTTP never follows redirects. Proxy inheritance is disabled to avoid
  # accidentally routing merchant credentials through an application proxy.
  def self.default_transport(request)
    request[:cancellation]&.check!
    uri = URI(request.fetch(:url))
    http = Net::HTTP.new(uri.hostname, uri.port, nil)
    http.use_ssl = uri.scheme == 'https'
    http.verify_mode = OpenSSL::SSL::VERIFY_PEER
    http.open_timeout = 10; http.read_timeout = 15; http.write_timeout = 15
    http.max_retries = 0
    req = (request[:method] == 'POST' ? Net::HTTP::Post : Net::HTTP::Get).new(uri.request_uri, request[:headers])
    req.body = request[:body] if request[:body]
    result = nil
    # Explicit start keeps the connection alive until the response is consumed.
    # Net::HTTP's implicit start adds Connection: close, which can cause a TCP
    # reset on Windows when an endpoint ignores a small request body (logout).
    http.start do |connection|
      connection.request(req) do |response|
        status = response.code.to_i
        raise Error.new('REDIRECT_REJECTED', status: status) if (300..399).cover?(status)
        body = +''
        response.read_body do |chunk|
          request[:cancellation]&.check!
          body << chunk
          raise Error.new('RESPONSE_TOO_LARGE') if body.bytesize > LIMIT
        end
        result = { status: status, headers: response.to_hash.transform_values(&:first), body: body }
      end
    end
    request[:cancellation]&.check!
    result
  rescue IOError, SystemCallError, SocketError, Timeout::Error, OpenSSL::SSL::SSLError, Net::HTTPBadResponse, Net::HTTPHeaderSyntaxError
    request[:cancellation]&.check!
    raise Error.new('NETWORK_UNAVAILABLE')
  end

  class Application
    def initialize(launch, product, store: MemoryStore.new, transport: nil, runtime_factory: nil, cache_dir: nil, sleep: nil, pending_attempts: 5)
      @launch, @product = LicentivoLaunch.validate_config(launch, product)
      raise Error.new('SECURE_STORE_REQUIRED') unless store.respond_to?(:persistent?) && (!store.persistent? || store.respond_to?(:secure?) && store.secure?)
      raise Error.new('INVALID_RETRY_POLICY') unless pending_attempts.is_a?(Integer) && (1..5).cover?(pending_attempts)
      @store, @transport = store, transport || LicentivoLaunch.method(:default_transport)
      @factory = runtime_factory || lambda do |config|
        require_relative 'licentivo_launch/vendor/licentivo'
        raise Error.new('SDK_VERSION_MISMATCH') unless Licentivo::VERSION == '0.11.1'
        Licentivo::Client.new(config)
      end
      @root = File.expand_path(cache_dir || File.join(Dir.home, '.cache', 'LicentivoLaunch'))
      @scope = ['launch-session-v1', @launch['merchant_origin'], @launch['launch_product_id'], @launch['licentivo_product_id'], @launch['environment']].join("\n")
      @lock, @closed, @in_callback, @attempts = Monitor.new, false, false, pending_attempts
      @wait = sleep || lambda { |seconds, cancellation| (seconds * 10).ceil.times { cancellation&.check!; Kernel.sleep(0.1) }; cancellation&.check! }
    end
    def self.connect(merchant_origin, product_id, licentivo_product_id, trusted_keys, environment: 'live', **options)
      new({ 'schema_version' => 1, 'merchant_origin' => merchant_origin, 'launch_product_id' => product_id, 'licentivo_product_id' => licentivo_product_id, 'environment' => environment },
          { 'base_url' => 'https://www.licentivo.com', 'product_id' => licentivo_product_id, 'trusted_keys' => trusted_keys }, **options)
    end
    def self.from_files(launch_path, product_path, **options) = new(*LicentivoLaunch.load_config(launch_path, product_path), **options)
    def login(email, password, cancellation: nil)
      serial do
        cancellation&.check!; logout_internal(cancellation)
        grant = call('/auth/login', method: 'POST', body: { email: email, password: password, kind: 'desktop' }, cancellation: cancellation)
        @record = { 'version' => 1, 'scope' => @scope, 'user' => grant['user'], 'token' => grant['access_token'], 'expires_at' => grant['expires_at'] }
        begin
          raise Error.new('INVALID_SESSION') unless valid_identity?(@record) && expiry(@record) > Time.now
          @record['credential'] = credential(cancellation)
          state = start_runtime(false, cancellation); @store.save(@scope, @record); state
        rescue StandardError
          logout_internal(nil); raise
        end
      end
    end
    def restore(cancellation: nil)
      serial do
        cancellation&.check!; stop; @record = saved
        next nil unless @record
        if expiry(@record) <= Time.now
          clear; raise Error.new('SESSION_EXPIRED')
        end
        begin
          me = call('/me', cancellation: cancellation)
          raise Error.new('SESSION_MISMATCH', status: 401) unless me.dig('user', 'id') == @record.dig('user', 'id')
          @record['credential'] = credential(cancellation); @record['user'] = me['user']
        rescue Error => e
          cancellation&.check!
          next start_runtime(true, cancellation) if e.code == 'NETWORK_UNAVAILABLE' || (500..599).cover?(e.status)
          clear if e.code != 'ENTITLEMENT_PENDING' && ([401, 403, 404].include?(e.status) || %w[SESSION_EXPIRED SESSION_MISMATCH ENTITLEMENT_EXPIRED ENTITLEMENT_SUSPENDED ENTITLEMENT_NOT_PURCHASED].include?(e.code))
          raise
        end
        begin
          state = start_runtime(false, cancellation); @store.save(@scope, @record); state
        rescue StandardError
          clear; raise
        end
      end
    end
    def run_feature(feature, &action)
      serial do
        raise Error.new('LOGIN_REQUIRED') unless @runtime
        @runtime.run_feature(feature) do
          begin
            @in_callback = true; action.call
          ensure
            @in_callback = false
          end
        end
      end
    end
    def logout(cancellation: nil) = serial { logout_internal(cancellation) }
    def release_device(cancellation: nil)
      serial do
        cancellation&.check!
        raise Error.new('LOGIN_REQUIRED') unless @runtime
        begin
          @runtime.deactivate; cancellation&.check!
        ensure
          logout_internal(cancellation)
        end
      end
    end
    def close
      @lock.synchronize do
        raise Error.new('WORKFLOW_BUSY') if @in_callback
        return if @closed
        @closed = true
        begin; stop; ensure; @record = nil; end
      end
    end

    private
    def serial
      @lock.synchronize do
        raise Error.new('APPLICATION_CLOSED') if @closed
        raise Error.new('WORKFLOW_BUSY') if @in_callback
        yield
      end
    end
    def cache(record) = File.join(@root, Digest::SHA256.hexdigest(@scope), Digest::SHA256.hexdigest(record.dig('user', 'id')), 'lease.json')
    def expiry(record) = Time.iso8601(record['expires_at'])
    def valid_identity?(record)
      record.is_a?(Hash) && record['version'] == 1 && record['scope'] == @scope && record['user'].is_a?(Hash) && UUID.match?(record.dig('user', 'id').to_s) && record['token'].is_a?(String) && record['token'].match?(/\A[A-Za-z0-9_-]{43}\z/) && record['expires_at'].is_a?(String) && record['expires_at'].match?(/(?:Z|[+-]\d\d:\d\d)\z/) && expiry(record)
    rescue ArgumentError, TypeError
      false
    end
    def saved
      record = @store.load(@scope)
      if record && !(valid_identity?(record) && record['credential'].is_a?(Hash) && record.dig('credential', 'product_id') == @launch['launch_product_id'] && record.dig('credential', 'license_key').is_a?(String) && !record.dig('credential', 'license_key').empty?)
        clear(record); raise Error.new('INVALID_SAVED_SESSION')
      end
      record
    rescue StandardError
      @store.delete(@scope); raise
    end
    def call(path, method: 'GET', body: nil, cancellation: nil, token: @record&.fetch('token', nil))
      cancellation&.check!
      headers = { 'Accept' => 'application/json', 'Content-Type' => 'application/json' }
      headers['Authorization'] = "Bearer #{token}" if token
      response = @transport.call(url: @launch['merchant_origin'] + '/api/v1' + path, method: method, headers: headers, body: body && JSON.generate(body), cancellation: cancellation)
      cancellation&.check!
      status = response[:status]
      raise Error.new('INVALID_RESPONSE') unless status.is_a?(Integer) && response[:body].is_a?(String) && response[:body].bytesize <= LIMIT
      raise Error.new('REDIRECT_REJECTED', status: status) if (300..399).cover?(status)
      begin
        data = JSON.parse(response[:body])
      rescue JSON::ParserError
        raise Error.new((500..599).cover?(status) ? 'SERVICE_UNAVAILABLE' : 'INVALID_RESPONSE', status: status)
      end
      raise Error.new('INVALID_RESPONSE', status: status) unless data.is_a?(Hash)
      if status != 200
        retry_header = response.fetch(:headers, {})['retry-after'].to_s
        seconds = retry_header.match?(/\A\d+\z/) ? retry_header.to_i : (Time.httpdate(retry_header) - Time.now).ceil rescue 0
        raise Error.new(data.dig('error', 'code') || 'REQUEST_FAILED', status: status, retry_after_seconds: seconds)
      end
      raise Error.new('INVALID_RESPONSE') unless data['data'].is_a?(Hash)
      data['data']
    end
    def credential(cancellation)
      @attempts.times do |attempt|
        begin
          value = call("/software/products/#{@launch['launch_product_id']}/credential", method: 'POST', body: { environment: @launch['environment'] }, cancellation: cancellation)
          raise Error.new('CREDENTIAL_MISMATCH') unless value['product_id'] == @launch['launch_product_id'] && value['license_key'].is_a?(String) && !value['license_key'].empty? && value['version'].is_a?(Integer) && value['version'] >= 1
          return value
        rescue Error => e
          raise unless e.status == 403 && e.code == 'ENTITLEMENT_PENDING' && attempt + 1 < @attempts && e.retry_after_seconds <= 30
          @wait.call([3, e.retry_after_seconds].max, cancellation); cancellation&.check!
        end
      end
    end
    def start_runtime(offline, cancellation)
      cancellation&.check!
      path = cache(@record); FileUtils.mkdir_p(File.dirname(path), mode: 0o700)
      runtime = @factory.call(@product.merge('license_key' => @record.dig('credential', 'license_key'), 'cache_path' => path))
      begin
        cancellation&.check!
        if offline
          state = runtime.status
          raise Error.new('OFFLINE_UNAVAILABLE') unless state['allowed'] == true && state['state'] == 'offline_valid'
        end
        runtime.start; cancellation&.check!; @runtime = runtime
        { user: JSON.parse(JSON.generate(@record['user'])), expires_at: @record['expires_at'], offline_fallback: offline }
      rescue StandardError
        runtime.close; raise
      end
    end
    def stop
      runtime, @runtime = @runtime, nil
      runtime&.close
    end
    def clear(record = @record)
      begin
        stop
      ensure
        @record = nil
        begin
          @store.delete(@scope)
        ensure
          FileUtils.rm_rf(File.dirname(cache(record))) if record.is_a?(Hash) && record['user'].is_a?(Hash) && UUID.match?(record.dig('user', 'id').to_s)
        end
      end
    end
    def logout_internal(cancellation)
      record = @record || saved
      clear(record)
      return true unless record
      begin
        call('/auth/logout', method: 'POST', body: {}, cancellation: cancellation, token: record['token']); true
      rescue StandardError
        false
      end
    end
  end
end
