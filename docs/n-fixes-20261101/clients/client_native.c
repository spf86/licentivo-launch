#include "native_support.h"
int main(int argc,char **argv){
 if(argc!=2)return 2;
 const char *base="/home/spf86/n-fixes-20261101",*name=argv[1];char launch[512],product[512],secret_path[512];
 snprintf(launch,sizeof(launch),"%s/%s-launch.json",base,name);snprintf(product,sizeof(product),"%s/%s-product.json",base,name);snprintf(secret_path,sizeof(secret_path),"%s/deployment-private.json",base);snprintf(output_path,sizeof(output_path),"%s/c-%s-business.txt",base,name);
 cJSON *secret=read_json(secret_path);if(!secret)return 2;const char *password=json_text(secret,"customer_password");memory_store memory={0};launch_secret_store store={&memory,load_slot,save_slot,remove_slot};
 launch_application *app=launch_application_from_files(launch,product,0,&store);int ok=app&&launch_application_login(app,"alice@launch-joint.example",password);
 if(ok&&!strcmp(name,"fixed")){char op[80];snprintf(op,sizeof(op),"nfix-c-%s",getenv("NFIX_RUN"));snprintf(meter_path,sizeof(meter_path),"%s/metered-c.txt",base);ok=launch_application_run_metered_feature(app,"export",1,op,meter_business,NULL)&&launch_application_run_metered_feature(app,"export",1,op,meter_business,NULL)&&launch_application_resolve_metered_feature(app,op,1)&&metered_calls==1;}

 ok=ok&&launch_application_run_feature(app,"export",business,NULL);int denied=app?!launch_application_run_feature(app,"not_entitled",business,NULL):0;ok=ok&&denied&&calls==1;launch_application_close(app);
 app=launch_application_from_files(launch,product,0,&store);ok=ok&&app&&launch_application_restore(app)==1;ok=ok&&launch_application_run_feature(app,"export",business,NULL);ok=ok&&launch_application_logout(app);launch_application_close(app);
 app=launch_application_from_files(launch,product,0,&store);int blocked=app&&launch_application_restore(app)==0;ok=ok&&blocked;launch_application_close(app);
 app=launch_application_from_files(launch,product,0,&store);int accepted=app&&launch_application_login(app,"bob@launch-joint.example",password);char code[96];snprintf(code,sizeof(code),"%s",app?launch_application_error(app):"INVALID_CONFIG");ok=ok&&!accepted&&!strcmp(code,"LAUNCH_ENTITLEMENT_REQUIRED")&&calls==2;emit("C",name,ok,code,blocked);launch_application_close(app);cJSON_Delete(secret);return ok?0:1;
}
