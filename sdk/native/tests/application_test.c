/* Keep regression assertions active in optimized Release test builds. */
#undef NDEBUG
#include "../account.c"
#include <assert.h>
#undef assert
#define assert(value) do { if(!(value)){fprintf(stderr,"Assertion failed: %s (%s:%d)\n",#value,__FILE__,__LINE__);exit(1);} } while(0)
static char *saved=NULL,*pending_saved=NULL;static int fail_saves=0,fail_deletes=0;
static int queue_scope(const char *scope){return scope&&strstr(scope,"\naccount-logout-queue-v1")!=NULL;}
static int load_record(void *context,const char *scope,char **record){(void)context;char *value=queue_scope(scope)?pending_saved:saved;*record=value?copy(value):NULL;return *record?1:0;}
static int save_record(void *context,const char *scope,const char *record){(void)context;if(fail_saves)return 0;char **value=queue_scope(scope)?&pending_saved:&saved;wipe(*value);free(*value);*value=copy(record);return *value!=NULL;}
static int delete_record(void *context,const char *scope){(void)context;if(fail_deletes)return 0;char **value=queue_scope(scope)?&pending_saved:&saved;wipe(*value);free(*value);*value=NULL;if(!scope){wipe(pending_saved);free(pending_saved);pending_saved=NULL;}return 1;}
static int calls=0;
static int action(void *unused){(void)unused;calls++;return 1;}
static void mode(const char *origin,const char *value){launch_account control={0};control.origin=(char*)origin;char path[128];snprintf(path,sizeof(path),"/fixture/%s",value);cJSON *result=request_method(&control,path,NULL,1);assert(result);cJSON_Delete(result);}
int main(int argc,char **argv){
 assert(argc==3);const char *origin=argv[1],*config=argv[2],*product="11111111-1111-4111-8111-111111111111";launch_secret_store store={NULL,load_record,save_record,delete_record};
 const char *cases[]={"offline","missing","tampered","denied","expired","ENTITLEMENT_EXPIRED","ENTITLEMENT_SUSPENDED","ENTITLEMENT_NOT_PURCHASED","ENTITLEMENT_PENDING","logout"};
 for(size_t i=0;i<sizeof(cases)/sizeof(cases[0]);i++){
  mode(origin,"online");launch_application *first=launch_application_open(origin,product,config,"sandbox",0,&store);assert(first);int login=launch_application_login(first,"account@example.test","secret");if(!login)fprintf(stderr,"Login: %s\n",launch_application_error(first));assert(login);assert(saved&&!strstr(saved,"discard-this-unexpected-secret")&&!strstr(saved,"identity_token"));
  assert(launch_application_run_feature(first,"export",action,NULL));int before=calls;assert(!launch_application_run_feature(first,"denied",action,NULL));assert(calls==before);
  char *cache=copy(first->account.cache_path);launch_application_close(first);assert(saved);
  if(!strcmp(cases[i],"missing"))remove(cache);
  if(!strcmp(cases[i],"tampered")){FILE *f=fopen(cache,"wb");assert(f);fputs("{}",f);fclose(f);}
  if(!strcmp(cases[i],"expired")){cJSON *record=ln_json(saved);cJSON_DeleteItemFromObjectCaseSensitive(record,"expires_at");cJSON_AddStringToObject(record,"expires_at","2000-01-01T00:00:00Z");char *raw=cJSON_PrintUnformatted(record);save_record(NULL,NULL,raw);wipe(raw);free(raw);cJSON_Delete(record);}
  mode(origin,(!strcmp(cases[i],"missing")||!strcmp(cases[i],"tampered"))?"offline":cases[i]);
  launch_application *next=launch_application_open(origin,product,config,"sandbox",0,&store);assert(next);
  if(!strcmp(cases[i],"logout")){assert(launch_application_logout(next));assert(!saved);FILE *f=fopen(cache,"rb");assert(f);fclose(f);assert(!launch_application_run_feature(next,"export",action,NULL));}
  else if(!strcmp(cases[i],"offline")){assert(launch_application_restore(next)==-1);assert(!launch_application_run_feature(next,"export",action,NULL));assert(calls==before);assert(saved);}
  else{assert(launch_application_restore(next)==-1);assert(!launch_application_run_feature(next,"export",action,NULL));assert(calls==before);if(strcmp(cases[i],"missing")&&strcmp(cases[i],"tampered")&&strcmp(cases[i],"ENTITLEMENT_PENDING"))assert(!saved);}
  launch_application_close(next);delete_record(NULL,NULL);remove(cache);char credential[2450];snprintf(credential,sizeof(credential),"%s.credential.json",cache);remove(credential);free(cache);
 }
 const char *invalid[]={"bad_product","bad_key","bad_version","missing_version","fraction_version","pending_server_error"};
 for(size_t i=0;i<sizeof(invalid)/sizeof(invalid[0]);i++){
  mode(origin,invalid[i]);launch_application *bad=launch_application_open(origin,product,config,"sandbox",0,&store);assert(bad);int before=calls;
  assert(!launch_application_login(bad,"account@example.test","secret"));assert(!strcmp(launch_application_error(bad),!strcmp(invalid[i],"pending_server_error")?"ENTITLEMENT_PENDING":"IDENTITY_MISMATCH"));assert(!bad->runtime&&!saved);
  assert(!launch_application_run_feature(bad,"export",action,NULL)&&calls==before);launch_application_close(bad);
 }
 mode(origin,"online");launch_application *offline=launch_application_open(origin,product,config,"sandbox",0,&store);assert(offline&&launch_application_login(offline,"account@example.test","secret"));
 mode(origin,"offline");assert(!launch_application_logout(offline));assert(saved&&strstr(saved,"\"logout_pending\":true")&&!offline->runtime);assert(!launch_application_run_feature(offline,"export",action,NULL));launch_application_close(offline);
 launch_application *retry=launch_application_open(origin,product,config,"sandbox",0,&store);assert(retry);assert(launch_application_restore(retry)==-1);assert(saved&&!retry->runtime);
 mode(origin,"online");assert(launch_application_restore(retry)==0);assert(!saved&&!retry->runtime);launch_application_close(retry);
 mode(origin,"online");retry=launch_application_open(origin,product,config,"sandbox",0,&store);assert(retry&&launch_application_login(retry,"account@example.test","secret"));mode(origin,"logout_denied");assert(launch_application_logout(retry));assert(!saved);launch_application_close(retry);
 mode(origin,"online");launch_application *fault=launch_application_open(origin,product,config,"sandbox",0,&store);assert(fault&&launch_application_login(fault,"account@example.test","secret"));fail_saves=1;mode(origin,"offline");assert(!launch_application_logout(fault));assert(!strcmp(launch_application_error(fault),"ACCOUNT_LOGOUT_STORAGE_FAILED"));assert(!fault->runtime&&!saved&&!pending_saved);fail_saves=0;launch_application_close(fault);fault=launch_application_open(origin,product,config,"sandbox",0,&store);assert(fault&&launch_application_restore(fault)==0);launch_application_close(fault);
 mode(origin,"online");launch_application *old=launch_application_open(origin,product,config,"sandbox",0,&store);assert(old&&launch_application_login(old,"account@example.test","secret"));launch_application *newer=launch_application_open(origin,product,config,"sandbox",0,&store);assert(newer&&launch_application_login(newer,"account@example.test","secret"));char *new_record=copy(saved);mode(origin,"offline");assert(!launch_application_logout(old));assert(saved&&!strcmp(saved,new_record)&&pending_saved);launch_application_close(old);mode(origin,"online");fault=launch_application_open(origin,product,config,"sandbox",0,&store);assert(fault&&launch_application_restore(fault)==1);cJSON *restored_record=ln_json(saved);assert(saved&&!strcmp(text(restored_record,"access_token"),newer->account.token)&&!pending_saved);cJSON_Delete(restored_record);launch_application_close(fault);launch_application_close(newer);wipe(new_record);free(new_record);delete_record(NULL,NULL);
#ifdef _WIN32
 mode(origin,"online");launch_application *first=launch_application_open(origin,product,config,"sandbox",1,NULL);assert(first);assert(launch_application_login(first,"account@example.test","secret"));char path[2400];assert(app_record_path(first,path));FILE *encrypted=fopen(path,"rb");assert(encrypted);char bytes[8192]={0};size_t length=fread(bytes,1,sizeof(bytes)-1,encrypted);fclose(encrypted);assert(length>0);for(size_t i=0;i+13<length;i++)assert(memcmp(bytes+i,"secret-bearer",13));launch_application_close(first);
 mode(origin,"offline");launch_application *second=launch_application_open(origin,product,config,"sandbox",1,NULL);assert(second);assert(launch_application_restore(second)==-1);assert(!launch_application_run_feature(second,"export",action,NULL));assert(!launch_application_logout(second));launch_application_close(second);assert(GetFileAttributesA(path)!=INVALID_FILE_ATTRIBUTES);
 second=launch_application_open(origin,product,config,"sandbox",1,NULL);assert(second&&launch_application_restore(second)==-1);mode(origin,"online");assert(launch_application_restore(second)==0);launch_application_close(second);assert(GetFileAttributesA(path)==INVALID_FILE_ATTRIBUTES);
#endif
 mode(origin,"online");old=launch_application_open(origin,product,config,"sandbox",0,&store);assert(old&&launch_application_login(old,"account@example.test","secret")&&launch_application_logout(old));newer=launch_application_open(origin,product,config,"sandbox",0,&store);assert(newer&&launch_application_login(newer,"account@example.test","secret"));assert(launch_application_logout(old)&&saved);assert(launch_application_restore(newer)==1);assert(launch_application_logout(newer));launch_application_close(old);launch_application_close(newer);
 mode(origin,"online");puts("Native lifecycle: identity exchange, SDK feature authority, outage denial, missing/tampered cache, expired/denied/pending account, logout, failed-save recovery, stale/completed logout ownership and DPAPI passed.");return 0;
}
