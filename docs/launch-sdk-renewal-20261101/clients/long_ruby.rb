require 'json'
require_relative './sdk-source/sdk/ruby/lib/licentivo_launch'
root='/home/spf86/launch-sdk-renewal-20261101';report=JSON.parse(File.read(File.join(root,'meter-public.json')));secret=JSON.parse(File.read(File.join(root,'deployment-private.json')))
app=LicentivoLaunch::Application.new(report['config'],report['product_config'].merge('allow_http'=>true),store:LicentivoLaunch::MemoryStore.new,cache_dir:File.join(root,'runtime/ruby'));calls=0;phase=ENV.fetch('LAUNCH_PHASE');operation=ENV.fetch('LAUNCH_OPERATION')
begin
 app.login(ENV.fetch('LAUNCH_EMAIL'),secret['customer_password'])
 app.run_metered_feature('export',3,operation){calls+=1;raise 'Business helper failed' unless system('python3',File.join(root,'long_business.py'))}
 app.resolve_metered_feature(operation,true) if phase=='replay'
 raise 'Unexpected callback count' unless calls==(phase=='run' ? 1 : 0)
 puts JSON.generate(ok:true,language:'ruby',phase:phase,callbacks:calls)
ensure
 app.close
end
