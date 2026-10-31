#include "native_support.h"
#include "launch.hpp"
int main(int argc,char **argv){
 if(argc!=2)return 2;const char *base="/home/spf86/n-fixes-20261101",*name=argv[1];char launch[512],product[512],secret_path[512];
 snprintf(launch,sizeof(launch),"%s/%s-launch.json",base,name);snprintf(product,sizeof(product),"%s/%s-product.json",base,name);snprintf(secret_path,sizeof(secret_path),"%s/deployment-private.json",base);snprintf(output_path,sizeof(output_path),"%s/cpp-%s-business.txt",base,name);
 cJSON *secret=read_json(secret_path);if(!secret)return 2;const char *password=json_text(secret,"customer_password");memory_store memory={};launch_secret_store store={&memory,load_slot,save_slot,remove_slot};int blocked=0,ok=1;std::string code;
 try{
  {auto app=licentivo_launch::Application::FromFiles(launch,product,false,&store);app.Login("alice@launch-joint.example",password);
  if(!strcmp(name,"fixed")){std::string op=std::string("nfix-cpp-")+getenv("NFIX_RUN");snprintf(meter_path,sizeof(meter_path),"%s/metered-cpp.txt",base);for(int i=0;i<2;i++)app.RunMeteredFeature("export",1,op.c_str(),[]{if(!meter_business(NULL))throw std::runtime_error("BUSINESS_IO");});app.ResolveMeteredFeature(op.c_str(),true);if(metered_calls!=1)throw std::runtime_error("METERING_REPEATED");}
app.RunFeature("export",[]{if(!business(NULL))throw std::runtime_error("IO_FAILURE");});bool denied=false;try{app.RunFeature("not_entitled",[]{business(NULL);});}catch(...){denied=true;}ok=denied&&calls==1;}
  {auto app=licentivo_launch::Application::FromFiles(launch,product,false,&store);ok=ok&&app.Restore();app.RunFeature("export",[]{business(NULL);});app.Logout();}
  {auto app=licentivo_launch::Application::FromFiles(launch,product,false,&store);blocked=!app.Restore();ok=ok&&blocked;}
  {auto app=licentivo_launch::Application::FromFiles(launch,product,false,&store);try{app.Login("bob@launch-joint.example",password);}catch(const std::exception &e){code=e.what();}}
  ok=ok&&calls==2&&code=="LAUNCH_ENTITLEMENT_REQUIRED";
 }catch(const std::exception &e){ok=0;code=e.what();}
 emit("C++",name,ok,code.c_str(),blocked);cJSON_Delete(secret);return ok?0:1;
}
