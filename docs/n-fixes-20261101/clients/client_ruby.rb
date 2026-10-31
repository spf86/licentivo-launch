require 'json'
require_relative './sdk-source/sdk/ruby/lib/licentivo_launch'
base='/home/spf86/n-fixes-20261101'
name=ARGV.fetch(0)
report=JSON.parse(File.read(File.join(base,name+'-public.json')))
secret=JSON.parse(File.read(File.join(base,'deployment-private.json')))
product=report['product_config'].merge('allow_http'=>true)
store=LicentivoLaunch::MemoryStore.new
create=-> {LicentivoLaunch::Application.new(report['config'],product,store:store,cache_dir:File.join(base,'ruby-'+name))}
app=nil
calls=0
action=-> {calls+=1;File.write(File.join(base,'ruby-'+name+'-business.txt'),"Verified Launch SDK business\n")}
begin
 app=create.call
 app.login('alice@launch-joint.example',secret['customer_password'])
 if name=='fixed'
  operation='nfix-ruby-'+ENV.fetch('NFIX_RUN');meter_calls=0
  2.times { app.run_metered_feature('export',1,operation) { meter_calls+=1;File.write(File.join(base,'metered-ruby.txt'),operation) } }
  app.resolve_metered_feature(operation,true)
  raise 'METERING_REPEATED' unless meter_calls==1
 end

 app.run_feature('export',&action)
 denied=false
 begin;app.run_feature('not_entitled',&action);rescue StandardError;denied=true;end
 raise 'FEATURE_GATE_FAILED' unless denied&&calls==1
 app.close
 app=create.call
 app.restore
 app.run_feature('export',&action)
 confirmed=app.logout
 app.close
 app=create.call
 restored=app.restore
 app.close
 app=create.call
 code=''
 begin;app.login('bob@launch-joint.example',secret['customer_password']);rescue StandardError=>error;code=error.respond_to?(:code) ? error.code : error.class.name;end
 puts JSON.generate(ok:calls==2&&confirmed==true&&!restored&&code=='LAUNCH_ENTITLEMENT_REQUIRED',language:'Ruby',product:name,callbacks:calls,startup_denial:code,logout_rejects_restore:!restored)
rescue StandardError=>error
 puts JSON.generate(ok:false,language:'Ruby',product:name,code:error.respond_to?(:code) ? error.code : error.class.name)
 exit 1
ensure
 app&.close
end
