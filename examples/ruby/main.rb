# frozen_string_literal: true
require 'io/console'
require 'optparse'
require_relative '../../sdk/ruby/lib/licentivo_launch'

options = {}
parser = OptionParser.new do |p|
  p.banner = 'ruby examples/ruby/main.rb launch-config.json product.json [--store-key-env NAME] [--logout|--release-device] [--feature NAME]'
  p.on('--store-key-env NAME') { |name| options[:key] = name }
  p.on('--feature NAME') { |name| options[:feature] = name }
  p.on('--logout') { raise OptionParser::InvalidArgument, 'conflicting modes' if options[:mode]; options[:mode] = :logout }
  p.on('--release-device') { raise OptionParser::InvalidArgument, 'conflicting modes' if options[:mode]; options[:mode] = :release }
  p.on('--help') { puts p; exit }
end
begin
  parser.parse!
  abort parser.to_s unless ARGV.length == 2
  store = if options[:key]
            key = Base64.strict_decode64(ENV.fetch(options[:key], ''))
            begin
              LicentivoLaunch::EncryptedFileStore.new(File.join(Dir.home, '.local/share/LicentivoLaunch/sessions'), key)
            ensure
              key.replace("\0" * key.bytesize)
            end
          else
            puts 'Memory session only. Restart requires login; no plaintext session file is written.'
            LicentivoLaunch::MemoryStore.new
          end
  # Prefer an installed compiled gem. The prepared source fallback needs its
  # native core built first, as described in sdk/ruby/README.md.
  begin
    require 'licentivo'
  rescue LoadError
    $LOAD_PATH.unshift(File.expand_path('../vendor/sdk/ruby/lib', __dir__))
    require 'licentivo'
  end
  token = LicentivoLaunch::CancellationToken.new
  Signal.trap('INT') { token.cancel }
  app = LicentivoLaunch::Application.from_files(*ARGV, store: store)
  if options[:mode] == :logout
    puts(app.logout(cancellation: token) ? 'Signed out.' : 'Local session removed; remote logout was not confirmed.')
  else
    state = app.restore(cancellation: token)
    unless state
      raise LicentivoLaunch::Error.new('TTY_REQUIRED') unless $stdin.tty?
      print 'Email: '; email = $stdin.gets&.strip
      password = $stdin.getpass('Password: ')
      begin
        state = app.login(email, password, cancellation: token)
      ensure
        password.replace("\0" * password.bytesize)
      end
    end
    if options[:mode] == :release
      app.release_device(cancellation: token); puts 'Device released and local session removed.'
    else
      puts(state[:offline_fallback] ? 'Signed in with a verified offline lease.' : 'Signed in; runtime started.')
      app.run_feature(options[:feature]) { puts 'Protected business callback executed.' } if options[:feature]
    end
  end
rescue StandardError, LoadError => e
  warn(e.respond_to?(:code) ? e.code : 'OPERATION_FAILED (check configuration and official native SDK installation)')
  exit_code = 1
ensure
  app&.close
end
exit(exit_code || 0)
