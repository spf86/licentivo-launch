/* Keep regression assertions active in optimized Release test builds. */
#undef NDEBUG
#include "../account.c"
#include <assert.h>
static char *saved=NULL;
static int load_record(void *context,const char *scope,char **record){(void)context;(void)scope;*record=saved?copy(saved):NULL;return *record?1:0;}
static int save_record(void *context,const char *scope,const char *record){(void)context;(void)scope;wipe(saved);free(saved);saved=copy(record);return saved!=NULL;}
static int delete_record(void *context,const char *scope){(void)context;(void)scope;wipe(saved);free(saved);saved=NULL;return 1;}
static int calls=0;
static int action(void *unused){(void)unused;calls++;return 1;}
static void mode(const char *origin,const char *value){launch_account control={0};control.origin=(char*)origin;char path[128];snprintf(path,sizeof(path),"/fixture/%s",value);cJSON *result=request_method(&control,path,NULL,1);assert(result);cJSON_Delete(result);}
int main(int argc,char **argv){
 assert(argc==3);const char *origin=argv[1],*config=argv[2],*product="11111111-1111-4111-8111-111111111111";launch_secret_store store={NULL,load_record,save_record,delete_record};
 const char *cases[]={"offline","missing","tampered","denied","expired","ENTITLEMENT_EXPIRED","ENTITLEMENT_SUSPENDED","ENTITLEMENT_NOT_PURCHASED","ENTITLEMENT_PENDING","logout"};
 for(size_t i=0;i<sizeof(cases)/sizeof(cases[0]);i++){
  mode(origin,"online");launch_application *first=launch_application_open(origin,product,config,"sandbox",0,&store);assert(first);int login=launch_application_login(first,"account@example.test","secret");if(!login)fprintf(stderr,"Login: %s\n",launch_application_error(first));assert(login);
  assert(launch_application_run_feature(first,"export",action,NULL));int before=calls;assert(!launch_application_run_feature(first,"denied",action,NULL));assert(calls==before);
  char *cache=copy(first->account.cache_path);launch_application_close(first);assert(saved);
  if(!strcmp(cases[i],"missing"))remove(cache);
  if(!strcmp(cases[i],"tampered")){FILE *f=fopen(cache,"wb");assert(f);fputs("{}",f);fclose(f);}
  if(!strcmp(cases[i],"expired")){cJSON *record=ln_json(saved);cJSON_DeleteItemFromObjectCaseSensitive(record,"expires_at");cJSON_AddStringToObject(record,"expires_at","2000-01-01T00:00:00Z");char *raw=cJSON_PrintUnformatted(record);save_record(NULL,NULL,raw);wipe(raw);free(raw);cJSON_Delete(record);}
  mode(origin,(!strcmp(cases[i],"missing")||!strcmp(cases[i],"tampered"))?"offline":cases[i]);
  launch_application *next=launch_application_open(origin,product,config,"sandbox",0,&store);assert(next);
  if(!strcmp(cases[i],"logout")){assert(launch_application_logout(next));assert(!saved);FILE *f=fopen(cache,"rb");assert(!f);}
  else if(!strcmp(cases[i],"offline")){assert(launch_application_restore(next)==1);assert(launch_application_run_feature(next,"export",action,NULL));assert(calls==before+1);}
  else{assert(launch_application_restore(next)==-1);assert(!launch_application_run_feature(next,"export",action,NULL));assert(calls==before);if(strcmp(cases[i],"missing")&&strcmp(cases[i],"tampered")&&strcmp(cases[i],"ENTITLEMENT_PENDING"))assert(!saved);}
  launch_application_close(next);delete_record(NULL,NULL);remove(cache);char credential[2450];snprintf(credential,sizeof(credential),"%s.credential.json",cache);remove(credential);free(cache);
 }
 const char *invalid[]={"bad_product","bad_key","bad_version","missing_version","fraction_version","pending_server_error"};
 for(size_t i=0;i<sizeof(invalid)/sizeof(invalid[0]);i++){
  mode(origin,invalid[i]);launch_application *bad=launch_application_open(origin,product,config,"sandbox",0,&store);assert(bad);int before=calls;
  assert(!launch_application_login(bad,"account@example.test","secret"));assert(!strcmp(launch_application_error(bad),!strcmp(invalid[i],"pending_server_error")?"ENTITLEMENT_PENDING":"CREDENTIAL_MISMATCH"));assert(!bad->runtime&&!saved);
  assert(!launch_application_run_feature(bad,"export",action,NULL)&&calls==before);launch_application_close(bad);
 }
#ifdef _WIN32
 mode(origin,"online");launch_application *first=launch_application_open(origin,product,config,"sandbox",1,NULL);assert(first);assert(launch_application_login(first,"account@example.test","secret"));char path[2400];assert(app_record_path(first,path));FILE *encrypted=fopen(path,"rb");assert(encrypted);char bytes[8192]={0};size_t length=fread(bytes,1,sizeof(bytes)-1,encrypted);fclose(encrypted);assert(length>0);for(size_t i=0;i+13<length;i++)assert(memcmp(bytes+i,"secret-bearer",13));launch_application_close(first);
 mode(origin,"offline");launch_application *second=launch_application_open(origin,product,config,"sandbox",1,NULL);assert(second);assert(launch_application_restore(second)==1);assert(launch_application_run_feature(second,"export",action,NULL));assert(launch_application_logout(second));launch_application_close(second);assert(GetFileAttributesA(path)==INVALID_FILE_ATTRIBUTES);
#endif
 mode(origin,"online");puts("Native lifecycle: signed activation/offline, denied feature, missing/tampered cache, expired/denied/pending account, logout and DPAPI passed.");return 0;
}
