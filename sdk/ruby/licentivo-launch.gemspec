# frozen_string_literal: true
Gem::Specification.new do |spec|
  spec.name = 'licentivo-launch'
  spec.version = '0.1.3'
  spec.summary = 'Merchant account lifecycle with the official Licentivo runtime'
  spec.description = 'Login, scoped secure sessions, verified offline recovery and feature guards for Licentivo Launch.'
  spec.authors = ['Licentivo Launch contributors']
  spec.license = 'MIT'
  spec.homepage = 'https://github.com/spf86/licentivo-launch-sdk'
  spec.metadata['source_code_uri'] = 'https://github.com/spf86/licentivo-launch-sdk'
  spec.metadata['documentation_uri'] = 'https://github.com/spf86/licentivo-launch-sdk/blob/main/docs/manual-integration.md'
  spec.metadata['bug_tracker_uri'] = 'https://github.com/spf86/licentivo-launch-sdk/issues'
  spec.metadata['allowed_push_host'] = 'https://rubygems.org'
  spec.required_ruby_version = '>= 3.1'
  spec.files = Dir['lib/**/*', 'vendor/**/*', 'ext/**/*.rb'] + %w[README.md LICENSE]
  spec.require_paths = ['lib']
  spec.add_dependency 'base64', '>= 0.2', '< 1'
  if ENV['LAUNCH_GEM_PRECOMPILED'] == '1'
    spec.platform = Gem::Platform.local
  else
    spec.extensions = ['ext/launch_native/extconf.rb']
  end
end
