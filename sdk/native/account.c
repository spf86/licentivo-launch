#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif
#include "account.h"
#include <curl/curl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <limits.h>
#include <math.h>
#ifdef _WIN32
#include <conio.h>
#include <direct.h>
#include <windows.h>
#include <bcrypt.h>
#include <wincrypt.h>
#define MKDIR(path) _mkdir(path)
#define SEP "\\"
#else
#include <sys/stat.h>
#include <termios.h>
#include <unistd.h>
#include <openssl/sha.h>
#include <strings.h>
#define MKDIR(path) mkdir(path,0700)
#define SEP "/"
#endif

typedef struct buffer {char *data;size_t length;} buffer;
static char *copy(const char *s){size_t n=strlen(s)+1;char *v=(char*)malloc(n);if(v)memcpy(v,s,n);return v;}
static void wipe(char *v){if(v){volatile char *p=v;while(*p){*p++=0;}}}
static const char *text(cJSON *object,const char *field){cJSON *v=cJSON_GetObjectItemCaseSensitive(object,field);return cJSON_IsString(v)?v->valuestring:NULL;}
static size_t receive(char *data,size_t size,size_t count,void *context){buffer *b=(buffer*)context;if(size && count>1048576/size)return 0;size_t n=size*count;if(n>1048576-b->length)return 0;char *next=(char*)realloc(b->data,b->length+n+1);if(!next)return 0;b->data=next;memcpy(b->data+b->length,data,n);b->length+=n;b->data[b->length]=0;return n;}
static size_t receive_header(char *raw,size_t size,size_t count,void *context){
 size_t n=size*count;launch_account *a=(launch_account*)context;char line[256];if(n>=sizeof(line))return n;memcpy(line,raw,n);line[n]=0;
#ifdef _WIN32
 int match=_strnicmp(line,"Retry-After:",12)==0;
#else
 int match=strncasecmp(line,"Retry-After:",12)==0;
#endif
 if(match){char *value=line+12;while(*value==' '||*value=='\t')value++;char *end=NULL;long delay=strtol(value,&end,10);if(end==value){time_t when=curl_getdate(value,NULL);delay=when<0?0:(long)difftime(when,time(NULL));}a->retry_after_seconds=delay>INT_MAX?INT_MAX:delay>0?(int)delay:0;}return n;
}
static cJSON *request_method(launch_account *a,const char *path,cJSON *input,int get){
 a->http_status=0;a->retry_after_seconds=0;strcpy(a->error_code,"NETWORK_UNAVAILABLE");
 CURL *curl=curl_easy_init();if(!curl)return NULL;char *body=get?copy(""):cJSON_PrintUnformatted(input);size_t length=strlen(a->origin)+strlen(path)+9;char *url=(char*)malloc(length);char auth[256];buffer out={NULL,0};cJSON *data=NULL;struct curl_slist *headers=NULL;
 if(!body || !url)goto finish;snprintf(url,length,"%s/api/v1%s",a->origin,path);headers=curl_slist_append(headers,"Content-Type: application/json");
 if(a->token){if(strlen(a->token)>200)goto finish;snprintf(auth,sizeof(auth),"Authorization: Bearer %s",a->token);headers=curl_slist_append(headers,auth);}
 curl_easy_setopt(curl,CURLOPT_URL,url);if(get)curl_easy_setopt(curl,CURLOPT_HTTPGET,1L);else curl_easy_setopt(curl,CURLOPT_POSTFIELDS,body);curl_easy_setopt(curl,CURLOPT_HTTPHEADER,headers);curl_easy_setopt(curl,CURLOPT_TIMEOUT,15L);curl_easy_setopt(curl,CURLOPT_FOLLOWLOCATION,0L);curl_easy_setopt(curl,CURLOPT_WRITEFUNCTION,receive);curl_easy_setopt(curl,CURLOPT_WRITEDATA,&out);curl_easy_setopt(curl,CURLOPT_HEADERFUNCTION,receive_header);curl_easy_setopt(curl,CURLOPT_HEADERDATA,a);
 if(curl_easy_perform(curl)!=CURLE_OK)goto finish;long status=0;curl_easy_getinfo(curl,CURLINFO_RESPONSE_CODE,&status);cJSON *root=out.data?ln_json(out.data):NULL;
 a->http_status=(int)status;strcpy(a->error_code,"INVALID_RESPONSE");
 if(status>=200 && status<300 && root){cJSON *value=cJSON_GetObjectItemCaseSensitive(root,"data");if(cJSON_IsObject(value)){data=cJSON_Duplicate(value,1);a->error_code[0]=0;}}
 else{const char *code=root?text(cJSON_GetObjectItemCaseSensitive(root,"error"),"code"):NULL;if(code && strlen(code)<sizeof(a->error_code) && strspn(code,"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_0123456789")==strlen(code))strcpy(a->error_code,code);}cJSON_Delete(root);
 finish:curl_slist_free_all(headers);curl_easy_cleanup(curl);wipe(body);free(body);free(url);wipe(out.data);free(out.data);return data;
}
static cJSON *request(launch_account *a,const char *path,cJSON *input){return request_method(a,path,input,0);}
static char *file_text(const char *path){FILE *f=fopen(path,"rb");if(!f)return NULL;if(fseek(f,0,SEEK_END)!=0){fclose(f);return NULL;}long n=ftell(f);if(n<0 || n>1048576 || fseek(f,0,SEEK_SET)!=0){fclose(f);return NULL;}char *s=(char*)calloc((size_t)n+1,1);if(!s || fread(s,1,(size_t)n,f)!=(size_t)n){free(s);s=NULL;}fclose(f);return s;}
static int origin_ok(const char *origin,int sandbox){
 CURLU *url=curl_url();if(!url)return 0;int ok=0;char *scheme=NULL,*host=NULL,*part=NULL;if(curl_url_set(url,CURLUPART_URL,origin,0)!=CURLUE_OK)goto done;
 if(curl_url_get(url,CURLUPART_SCHEME,&scheme,0)!=CURLUE_OK || curl_url_get(url,CURLUPART_HOST,&host,0)!=CURLUE_OK)goto done;
 if(strcmp(scheme,"https") && !(sandbox && !strcmp(scheme,"http") && (!strcmp(host,"localhost") || !strcmp(host,"127.0.0.1") || !strcmp(host,"[::1]"))))goto done;
 for(int i=0;i<4;i++){CURLUPart field=i==0?CURLUPART_USER:i==1?CURLUPART_PASSWORD:i==2?CURLUPART_QUERY:CURLUPART_FRAGMENT;if(curl_url_get(url,field,&part,0)==CURLUE_OK){curl_free(part);part=NULL;goto done;}}
 if(curl_url_get(url,CURLUPART_PATH,&part,0)==CURLUE_OK && strcmp(part,"/"))goto done;ok=1;
 done:curl_free(part);curl_free(scheme);curl_free(host);curl_url_cleanup(url);return ok;
}
static int hash_scope(const char *source,char result[65]){unsigned char digest[32];
#ifdef _WIN32
 BCRYPT_ALG_HANDLE algorithm=NULL;BCRYPT_HASH_HANDLE hash=NULL;DWORD object_size=0,used=0;unsigned char *object=NULL;int ok=0;
 if(BCryptOpenAlgorithmProvider(&algorithm,BCRYPT_SHA256_ALGORITHM,NULL,0)!=0)goto finish;
 if(BCryptGetProperty(algorithm,BCRYPT_OBJECT_LENGTH,(PUCHAR)&object_size,sizeof(object_size),&used,0)!=0)goto finish;object=(unsigned char*)malloc(object_size);if(!object)goto finish;
 if(BCryptCreateHash(algorithm,&hash,object,object_size,NULL,0,0)!=0 || BCryptHashData(hash,(PUCHAR)source,(ULONG)strlen(source),0)!=0 || BCryptFinishHash(hash,digest,32,0)!=0)goto finish;ok=1;
 finish:if(hash)BCryptDestroyHash(hash);if(algorithm)BCryptCloseAlgorithmProvider(algorithm,0);free(object);if(!ok)return 0;
#else
 if(!SHA256((const unsigned char*)source,strlen(source),digest))return 0;
#endif
 for(int i=0;i<32;i++)snprintf(result+i*2,3,"%02x",digest[i]);result[64]=0;return 1;}
static int read_password(char password[129]){
#ifdef _WIN32
 size_t n=0;for(;;){int ch=_getch();if(ch=='\r')break;if(ch=='\b'){if(n)n--;continue;}if(ch==0 || ch==224){_getch();continue;}if(ch>=32 && n<128)password[n++]=(char)ch;}password[n]=0;puts("");return 1;
#else
 struct termios saved,hidden;if(tcgetattr(STDIN_FILENO,&saved)!=0)return 0;hidden=saved;hidden.c_lflag&=~ECHO;if(tcsetattr(STDIN_FILENO,TCSAFLUSH,&hidden)!=0)return 0;char *ok=fgets(password,129,stdin);tcsetattr(STDIN_FILENO,TCSAFLUSH,&saved);puts("");if(!ok)return 0;password[strcspn(password,"\r\n")]=0;return 1;
#endif
}
int launch_login(launch_account *a,const char *origin,const char *product_id,const char *config_path,const char *environment,const char *email,const char *password){
 memset(a,0,sizeof(*a));if(!origin || !product_id || !config_path || !environment || !email || !password)return 0;
 if(strcmp(environment,"sandbox") && strcmp(environment,"live"))return 0;if(strlen(product_id)!=36 || strspn(product_id,"0123456789abcdef-")!=36 || !origin_ok(origin,!strcmp(environment,"sandbox")))return 0;
 a->origin=copy(origin);if(!a->origin)return 0;size_t n=strlen(a->origin);if(n && a->origin[n-1]=='/')a->origin[n-1]=0;
 cJSON *login=cJSON_CreateObject();cJSON_AddStringToObject(login,"email",email);cJSON_AddStringToObject(login,"password",password);cJSON_AddStringToObject(login,"kind","desktop");cJSON *grant=request(a,"/auth/login",login);cJSON_Delete(login);if(!grant)return 0;
 const char *token=text(grant,"access_token"),*user=text(cJSON_GetObjectItemCaseSensitive(grant,"user"),"id");if(!token || !user){cJSON_Delete(grant);return 0;}a->token=copy(token);
 char scope_input[1024],scope[65],cache_root[2048],dir[2300],path[2400];if(snprintf(scope_input,sizeof(scope_input),"%s\n%s\n%s\n%s",a->origin,user,product_id,environment)>=(int)sizeof(scope_input) || !hash_scope(scope_input,scope)){cJSON_Delete(grant);return 0;}cJSON_Delete(grant);
 cJSON *input=cJSON_CreateObject();cJSON_AddStringToObject(input,"environment",environment);char endpoint[160];snprintf(endpoint,sizeof(endpoint),"/software/products/%s/credential",product_id);cJSON *own=request(a,endpoint,input);cJSON_Delete(input);if(!own)return 0;const char *key=text(own,"license_key"),*product=text(own,"product_id");if(!key || !product || strcmp(product,product_id)){cJSON_Delete(own);return 0;}a->key=copy(key);cJSON_Delete(own);
#ifdef _WIN32
 const char *root=getenv("LOCALAPPDATA");if(!root || snprintf(cache_root,sizeof(cache_root),"%s" SEP "LicentivoLaunch",root)>=(int)sizeof(cache_root))return 0;
#else
 const char *root=getenv("XDG_CACHE_HOME");if(root){if(snprintf(cache_root,sizeof(cache_root),"%s" SEP "LicentivoLaunch",root)>=(int)sizeof(cache_root))return 0;}else{root=getenv("HOME");if(!root || snprintf(cache_root,sizeof(cache_root),"%s/.cache",root)>=(int)sizeof(cache_root))return 0;MKDIR(cache_root);if(snprintf(cache_root,sizeof(cache_root),"%s/.cache/LicentivoLaunch",root)>=(int)sizeof(cache_root))return 0;}
#endif
 MKDIR(cache_root);if(snprintf(dir,sizeof(dir),"%s" SEP "%s",cache_root,scope)>=(int)sizeof(dir))return 0;MKDIR(dir);snprintf(path,sizeof(path),"%s" SEP "lease.json",dir);a->cache_path=copy(path);
 char *raw=file_text(config_path);cJSON *config=raw?ln_json(raw):NULL;free(raw);if(!config)return 0;cJSON_DeleteItemFromObjectCaseSensitive(config,"license_key");cJSON_AddStringToObject(config,"license_key","");cJSON_DeleteItemFromObjectCaseSensitive(config,"cache_path");cJSON_AddStringToObject(config,"cache_path",a->cache_path);char *public_config=cJSON_PrintUnformatted(config);cJSON_Delete(config);if(!public_config)return 0;
 snprintf(path,sizeof(path),"%s" SEP "account-product.json",dir);FILE *f=fopen(path,"wb");if(!f){free(public_config);return 0;}int ok=fputs(public_config,f)>=0;int closed=fclose(f)==0;free(public_config);if(!ok || !closed)return 0;a->public_config_path=copy(path);return a->token && a->key && a->cache_path && a->public_config_path;
}
void launch_sign_out(launch_account *a,int clear_cache){
 if(a->token){cJSON *input=cJSON_CreateObject();cJSON *result=request(a,"/auth/logout",input);cJSON_Delete(input);cJSON_Delete(result);}if(clear_cache && a->cache_path){remove(a->cache_path);size_t n=strlen(a->cache_path)+32;char *credential=(char*)malloc(n);if(credential){snprintf(credential,n,"%s.credential.json",a->cache_path);remove(credential);free(credential);}}
 wipe(a->token);wipe(a->key);free(a->token);free(a->key);free(a->origin);free(a->cache_path);free(a->public_config_path);memset(a,0,sizeof(*a));
}
int launch_sign_in(launch_account *a,int argc,char **argv){
 memset(a,0,sizeof(*a));if(argc<4){fputs("MERCHANT_ORIGIN LAUNCH_PRODUCT_ID PRODUCT_JSON [sandbox|live]\n",stderr);return 0;}
 char email[255],password[129]={0};printf("Email: ");if(!fgets(email,sizeof(email),stdin))return 0;email[strcspn(email,"\r\n")]=0;printf("Password: ");fflush(stdout);if(!read_password(password))return 0;
 int ok=launch_login(a,argv[1],argv[2],argv[3],argc>4?argv[4]:"live",email,password);wipe(password);return ok;
}

struct launch_application {
 launch_account account;
 char *product,*environment,*config_json,*scope,*record,*user;
 int remember;
 launch_secret_store store;
 ln_client *runtime;
 char error[80];
};
static int app_fail(launch_application *a,const char *code){snprintf(a->error,sizeof(a->error),"%s",code?code:"REQUEST_FAILED");return 0;}
const char *launch_application_error(const launch_application *a){return a?a->error:"INVALID_CONFIG";}
char *launch_application_status(launch_application *a){return a&&a->runtime?ln_status(a->runtime):copy("{\"state\":\"login_required\",\"allowed\":false}");}
static void app_wait(int seconds){
#ifdef _WIN32
 Sleep((DWORD)seconds*1000);
#else
 sleep((unsigned int)seconds);
#endif
}
static cJSON *app_credential(launch_application *a){
 char endpoint[160];snprintf(endpoint,sizeof(endpoint),"/software/products/%s/credential",a->product);
 for(int i=0;i<3;i++){
  cJSON *input=cJSON_CreateObject();cJSON_AddStringToObject(input,"environment",a->environment);cJSON *own=request(&a->account,endpoint,input);cJSON_Delete(input);
  if(own){const char *product=text(own,"product_id"),*key=text(own,"license_key");cJSON *version=cJSON_GetObjectItemCaseSensitive(own,"version");if(!product||strcmp(product,a->product)||!key||!*key||!cJSON_IsNumber(version)||!isfinite(version->valuedouble)||version->valuedouble<1||version->valuedouble!=floor(version->valuedouble)){cJSON_Delete(own);app_fail(a,"CREDENTIAL_MISMATCH");return NULL;}return own;}
  if(strcmp(a->account.error_code,"ENTITLEMENT_PENDING")||(a->account.http_status!=403&&a->account.http_status!=409)||a->account.retry_after_seconds>30||i==2)break;
  unsigned delay=a->account.retry_after_seconds?a->account.retry_after_seconds:1<<i;app_wait(delay<3?3:delay);
 }
 app_fail(a,a->account.error_code);return NULL;
}
static time_t app_expiry(const char *value){
 if(!value)return 0;struct tm t={0};int y,m,d,h,minute,s,n=0;
 if(sscanf(value,"%4d-%2d-%2dT%2d:%2d:%2d%n",&y,&m,&d,&h,&minute,&s,&n)!=6||y<1970||m<1||m>12||d<1||d>31||h>23||minute>59||s>59)return 0;
 const char *tail=value+n;if(*tail=='.'){tail++;while(*tail>='0'&&*tail<='9')tail++;}int offset=0;
 if(*tail=='Z'&&tail[1]==0){}else{int oh,om,used=0;char sign=*tail;if((sign!='+'&&sign!='-')||sscanf(tail+1,"%2d:%2d%n",&oh,&om,&used)!=2||tail[used+1]||oh>23||om>59)return 0;offset=(oh*3600+om*60)*(sign=='+'?1:-1);}
 t.tm_year=y-1900;t.tm_mon=m-1;t.tm_mday=d;t.tm_hour=h;t.tm_min=minute;t.tm_sec=s;
#ifdef _WIN32
 time_t result=_mkgmtime(&t);
#else
 time_t result=timegm(&t);
#endif
 return result<0?0:result-offset;
}
static int app_cache(launch_application *a){
 if(!a->user)return app_fail(a,"INVALID_SAVED_SESSION");char hash[65],input[2048],root[2048],dir[2300],path[2400];
 if(snprintf(input,sizeof(input),"%s\n%s",a->scope,a->user)>=(int)sizeof(input)||!hash_scope(input,hash))return app_fail(a,"INVALID_CONFIG");
#ifdef _WIN32
 const char *base=getenv("LOCALAPPDATA");if(!base||snprintf(root,sizeof(root),"%s" SEP "LicentivoLaunch",base)>=(int)sizeof(root))return app_fail(a,"CACHE_UNAVAILABLE");
#else
 const char *base=getenv("XDG_CACHE_HOME");if(base){if(snprintf(root,sizeof(root),"%s/LicentivoLaunch",base)>=(int)sizeof(root))return app_fail(a,"CACHE_UNAVAILABLE");}else{base=getenv("HOME");if(!base||snprintf(root,sizeof(root),"%s/.cache",base)>=(int)sizeof(root))return app_fail(a,"CACHE_UNAVAILABLE");MKDIR(root);if(snprintf(root,sizeof(root),"%s/.cache/LicentivoLaunch",base)>=(int)sizeof(root))return app_fail(a,"CACHE_UNAVAILABLE");}
#endif
 MKDIR(root);if(snprintf(dir,sizeof(dir),"%s" SEP "%s",root,hash)>=(int)sizeof(dir))return app_fail(a,"CACHE_UNAVAILABLE");MKDIR(dir);
#ifndef _WIN32
 if(chmod(dir,0700)!=0)return app_fail(a,"CACHE_UNAVAILABLE");
#endif
 snprintf(path,sizeof(path),"%s" SEP "lease.json",dir);free(a->account.cache_path);a->account.cache_path=copy(path);return a->account.cache_path!=NULL;
}
#ifdef _WIN32
static int app_record_path(launch_application *a,char path[2400]){
 const char *base=getenv("LOCALAPPDATA");char root[2200],hash[65];if(!base||!hash_scope(a->scope,hash)||snprintf(root,sizeof(root),"%s\\LicentivoLaunch",base)>=(int)sizeof(root))return 0;MKDIR(root);
 if(snprintf(root,sizeof(root),"%s\\LicentivoLaunch\\native-sessions",base)>=(int)sizeof(root))return 0;MKDIR(root);return snprintf(path,2400,"%s\\%s.dpapi",root,hash)<2400;
}
static int app_dpapi_load(launch_application *a,char **record){
 char path[2400];if(!app_record_path(a,path))return -1;FILE *f=fopen(path,"rb");if(!f)return GetFileAttributesA(path)==INVALID_FILE_ATTRIBUTES?0:-1;
 if(fseek(f,0,SEEK_END)){fclose(f);return -1;}long length=ftell(f);if(length<=0||length>65536||fseek(f,0,SEEK_SET)){fclose(f);return -1;}
 BYTE *raw=(BYTE*)malloc((size_t)length);if(!raw){fclose(f);return -1;}int read=fread(raw,1,(size_t)length,f)==(size_t)length;fclose(f);DATA_BLOB input={(DWORD)length,raw},output={0};
 int ok=read&&CryptUnprotectData(&input,NULL,NULL,NULL,NULL,CRYPTPROTECT_UI_FORBIDDEN,&output);free(raw);if(!ok)return -1;
 if(output.cbData>65536||memchr(output.pbData,0,output.cbData)){LocalFree(output.pbData);return -1;}*record=(char*)calloc((size_t)output.cbData+1,1);if(*record)memcpy(*record,output.pbData,output.cbData);SecureZeroMemory(output.pbData,output.cbData);LocalFree(output.pbData);return *record?1:-1;
}
static int app_dpapi_save(launch_application *a,const char *record){
 char path[2400],temp[2480];if(!app_record_path(a,path))return 0;ULONG random;if(BCryptGenRandom(NULL,(PUCHAR)&random,sizeof(random),BCRYPT_USE_SYSTEM_PREFERRED_RNG)!=0)return 0;
 snprintf(temp,sizeof(temp),"%s.%lu.%lu.tmp",path,(unsigned long)GetCurrentProcessId(),(unsigned long)random);DATA_BLOB input={(DWORD)strlen(record),(BYTE*)record},output={0};if(!CryptProtectData(&input,NULL,NULL,NULL,NULL,CRYPTPROTECT_UI_FORBIDDEN,&output))return 0;
 HANDLE file=CreateFileA(temp,GENERIC_WRITE,0,NULL,CREATE_NEW,FILE_ATTRIBUTE_NORMAL,NULL);DWORD written=0;int ok=file!=INVALID_HANDLE_VALUE&&WriteFile(file,output.pbData,output.cbData,&written,NULL)&&written==output.cbData&&FlushFileBuffers(file);
 if(file!=INVALID_HANDLE_VALUE)CloseHandle(file);LocalFree(output.pbData);if(ok)ok=MoveFileExA(temp,path,MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH);if(!ok)DeleteFileA(temp);return ok;
}
#endif
static int app_load_record(launch_application *a,char **record){
 *record=NULL;if(a->store.load)return a->store.load(a->store.context,a->scope,record);
 if(a->remember){
#ifdef _WIN32
 return app_dpapi_load(a,record);
#else
 return -1;
#endif
 }
 if(a->record){*record=copy(a->record);return *record?1:-1;}return 0;
}
static int app_save_record(launch_application *a,const char *record){
 int ok=1;if(a->store.save)ok=a->store.save(a->store.context,a->scope,record);else if(a->remember){
#ifdef _WIN32
 ok=app_dpapi_save(a,record);
#else
 ok=0;
#endif
 }
 if(!ok)return app_fail(a,"SECURE_STORE_FAILED");char *saved=copy(record);if(!saved)return app_fail(a,"SECURE_STORE_FAILED");wipe(a->record);free(a->record);a->record=saved;return 1;
}
static int app_forget(launch_application *a){
 if(a->runtime){ln_destroy(a->runtime);a->runtime=NULL;}int ok=1;if(a->store.remove)ok=a->store.remove(a->store.context,a->scope);else if(a->remember){
#ifdef _WIN32
 char path[2400];if(app_record_path(a,path))ok=DeleteFileA(path)||GetLastError()==ERROR_FILE_NOT_FOUND;else ok=0;
#else
 ok=0;
#endif
 }
 if(a->account.cache_path){char path[2450];remove(a->account.cache_path);snprintf(path,sizeof(path),"%s.credential.json",a->account.cache_path);remove(path);}
 wipe(a->record);free(a->record);a->record=NULL;wipe(a->account.token);free(a->account.token);a->account.token=NULL;wipe(a->account.key);free(a->account.key);a->account.key=NULL;free(a->user);a->user=NULL;return ok;
}
static int app_attach(launch_application *a,cJSON *record){
 const char *scope=text(record,"scope"),*user=text(record,"user_id"),*token=text(record,"access_token"),*key=text(record,"license_key");cJSON *version=cJSON_GetObjectItemCaseSensitive(record,"version");
 if(!cJSON_IsNumber(version)||version->valuedouble!=1||!scope||strcmp(scope,a->scope)||!user||!*user||!token||!*token||!key||!*key)return app_fail(a,"INVALID_SAVED_SESSION");
 free(a->user);a->user=copy(user);wipe(a->account.token);free(a->account.token);a->account.token=copy(token);wipe(a->account.key);free(a->account.key);a->account.key=copy(key);return a->user&&a->account.token&&a->account.key&&app_cache(a);
}
static int app_start(launch_application *a,int offline){
 if(a->runtime){ln_destroy(a->runtime);a->runtime=NULL;}char *raw=NULL;cJSON *config=ln_json(a->config_json);if(!config)return app_fail(a,"INVALID_CONFIG");
 cJSON_DeleteItemFromObjectCaseSensitive(config,"license_key");cJSON_AddStringToObject(config,"license_key",a->account.key);cJSON_DeleteItemFromObjectCaseSensitive(config,"cache_path");cJSON_AddStringToObject(config,"cache_path",a->account.cache_path);raw=cJSON_PrintUnformatted(config);cJSON_Delete(config);if(!raw)return app_fail(a,"INVALID_CONFIG");
 ln_client *sdk=ln_init(raw);wipe(raw);free(raw);if(!sdk)return app_fail(a,offline?"OFFLINE_UNAVAILABLE":"SDK_INIT_FAILED");
 if(offline){char *state=ln_status(sdk);cJSON *status=state?ln_json(state):NULL;int allowed=status&&cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(status,"allowed"));cJSON_Delete(status);ln_free_string(state);if(!allowed){ln_destroy(sdk);return app_fail(a,"OFFLINE_UNAVAILABLE");}}
 if(!ln_start(sdk)){ln_error_info info={0};ln_get_error_info(sdk,&info);app_fail(a,info.code[0]?info.code:"SDK_AUTHORIZATION_FAILED");ln_destroy(sdk);return 0;}a->runtime=sdk;a->error[0]=0;return 1;
}
static launch_application *app_open_config(const char *origin,const char *product,const char *public_json,const char *environment,int remember,const launch_secret_store *store){
 if(!origin||!product||!public_json||strlen(public_json)>65536||!environment||(strcmp(environment,"sandbox")&&strcmp(environment,"live"))||!origin_ok(origin,!strcmp(environment,"sandbox"))||strlen(product)!=36||strspn(product,"0123456789abcdefABCDEF-")!=36)return NULL;
 if(store&&(!store->load||!store->save||!store->remove))return NULL;
#ifndef _WIN32
 if(remember&&!store)return NULL;
#endif
 cJSON *config=ln_json(public_json);const char *sdk_product=config?text(config,"product_id"):NULL;if(!sdk_product){cJSON_Delete(config);return NULL;}
 launch_application *a=(launch_application*)calloc(1,sizeof(*a));if(!a){cJSON_Delete(config);return NULL;}
 a->account.origin=copy(origin);a->product=copy(product);a->environment=copy(environment);a->config_json=copy(public_json);a->remember=remember;if(store)a->store=*store;
 if(a->account.origin){size_t n=strlen(a->account.origin);if(n&&a->account.origin[n-1]=='/')a->account.origin[n-1]=0;}
 char scope[2048];int n=snprintf(scope,sizeof(scope),"%s\n%s\n%s\n%s",a->account.origin?a->account.origin:"",product,environment,sdk_product);cJSON_Delete(config);if(n<0||n>=(int)sizeof(scope)){launch_application_close(a);return NULL;}a->scope=copy(scope);
 if(!a->account.origin||!a->product||!a->environment||!a->config_json||!a->scope){launch_application_close(a);return NULL;}return a;
}
launch_application *launch_application_connect(const char *origin,const char *product,const char *licentivo_product,const char *trusted_keys_json,const char *environment,int remember,const launch_secret_store *store){
 if(!licentivo_product||!trusted_keys_json||strlen(trusted_keys_json)>16384)return NULL;
 cJSON *keys=ln_json(trusted_keys_json);if(!cJSON_IsObject(keys)||cJSON_GetArraySize(keys)<1||cJSON_GetArraySize(keys)>32){cJSON_Delete(keys);return NULL;}
 cJSON *pin=NULL;cJSON_ArrayForEach(pin,keys){if(!pin->string||!*pin->string||strlen(pin->string)>128||!cJSON_IsString(pin)||!pin->valuestring||strlen(pin->valuestring)!=43||strspn(pin->valuestring,"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_")!=43||!strchr("AEIMQUYcgkosw048",pin->valuestring[42])){cJSON_Delete(keys);return NULL;}}
 cJSON *config=cJSON_CreateObject();cJSON_AddStringToObject(config,"base_url","https://www.licentivo.com");cJSON_AddStringToObject(config,"product_id",licentivo_product);cJSON_AddItemToObject(config,"trusted_keys",keys);
 char *raw=cJSON_PrintUnformatted(config);cJSON_Delete(config);launch_application *a=raw?app_open_config(origin,product,raw,environment,remember,store):NULL;free(raw);return a;
}
launch_application *launch_application_open(const char *origin,const char *product,const char *config_path,const char *environment,int remember,const launch_secret_store *store){
 char *raw=file_text(config_path);launch_application *a=raw?app_open_config(origin,product,raw,environment,remember,store):NULL;free(raw);return a;
}
launch_application *launch_application_from_files(const char *launch_path,const char *product_path,int remember,const launch_secret_store *store){
 char *raw=file_text(launch_path);cJSON *config=raw?ln_json(raw):NULL;free(raw);raw=file_text(product_path);cJSON *product=raw?ln_json(raw):NULL;free(raw);launch_application *a=NULL;
 cJSON *version=config?cJSON_GetObjectItemCaseSensitive(config,"schema_version"):NULL;const char *expected=text(config,"licentivo_product_id"),*actual=text(product,"product_id");
 if(cJSON_IsNumber(version)&&version->valuedouble==1&&expected&&actual&&!strcmp(expected,actual))a=launch_application_open(text(config,"merchant_origin"),text(config,"launch_product_id"),product_path,text(config,"environment"),remember,store);
 cJSON_Delete(config);cJSON_Delete(product);return a;
}
int launch_application_logout(launch_application *a){
 if(!a)return 0;if(!a->account.token){char *raw=NULL;int found=app_load_record(a,&raw);if(found<0){app_forget(a);return app_fail(a,"SECURE_STORE_FAILED");}if(found){cJSON *saved=ln_json(raw);int valid=saved&&app_attach(a,saved);cJSON_Delete(saved);wipe(raw);free(raw);if(!valid){app_forget(a);return app_fail(a,"INVALID_SAVED_SESSION");}}}
 char *token=a->account.token?copy(a->account.token):NULL;int ok=app_forget(a);if(token){a->account.token=token;cJSON *input=cJSON_CreateObject();cJSON *result=request(&a->account,"/auth/logout",input);cJSON_Delete(input);cJSON_Delete(result);wipe(token);free(token);a->account.token=NULL;}
 if(!ok)return app_fail(a,"SECURE_STORE_FAILED");a->error[0]=0;return 1;
}
int launch_application_login(launch_application *a,const char *email,const char *password){
 if(!a||!email||!password)return 0;if(!launch_application_logout(a))return 0;cJSON *input=cJSON_CreateObject();cJSON_AddStringToObject(input,"email",email);cJSON_AddStringToObject(input,"password",password);cJSON_AddStringToObject(input,"kind","desktop");cJSON *grant=request(&a->account,"/auth/login",input);cJSON_Delete(input);if(!grant)return app_fail(a,a->account.error_code);
 const char *token=text(grant,"access_token"),*expires=text(grant,"expires_at"),*user=text(cJSON_GetObjectItemCaseSensitive(grant,"user"),"id");if(!token||!expires||!user){cJSON_Delete(grant);return app_fail(a,"INVALID_RESPONSE");}a->account.token=copy(token);if(!a->account.token){cJSON_Delete(grant);return app_fail(a,"SECURE_STORE_FAILED");}
 cJSON *own=app_credential(a);if(!own){char failure[80];strcpy(failure,a->error);cJSON_Delete(grant);launch_application_logout(a);return app_fail(a,failure);}
 cJSON *record=cJSON_CreateObject();cJSON_AddNumberToObject(record,"version",1);cJSON_AddStringToObject(record,"scope",a->scope);cJSON_AddStringToObject(record,"user_id",user);cJSON_AddStringToObject(record,"access_token",token);cJSON_AddStringToObject(record,"expires_at",expires);cJSON_AddStringToObject(record,"license_key",text(own,"license_key"));cJSON_Delete(own);cJSON_Delete(grant);
 int ok=app_attach(a,record)&&app_start(a,0);char *raw=cJSON_PrintUnformatted(record);cJSON_Delete(record);if(ok)ok=raw&&app_save_record(a,raw);wipe(raw);free(raw);if(!ok){char failure[80];strcpy(failure,a->error);launch_application_logout(a);app_fail(a,failure);}return ok;
}
int launch_application_restore(launch_application *a){
 if(!a)return -1;if(a->runtime){ln_destroy(a->runtime);a->runtime=NULL;}char *raw=NULL;int found=app_load_record(a,&raw);if(found==0)return 0;if(found<0){app_fail(a,"SECURE_STORE_FAILED");return -1;}
 cJSON *record=ln_json(raw);wipe(raw);free(raw);if(!record||!app_attach(a,record)){cJSON_Delete(record);app_forget(a);app_fail(a,"INVALID_SAVED_SESSION");return -1;}
 if(app_expiry(text(record,"expires_at"))<=time(NULL)){cJSON_Delete(record);app_forget(a);app_fail(a,"SESSION_EXPIRED");return -1;}
 cJSON *current=request_method(&a->account,"/me",NULL,1);int offline=0;
 if(!current){if(a->account.http_status>=500||!strcmp(a->account.error_code,"NETWORK_UNAVAILABLE"))offline=1;else{app_fail(a,a->account.error_code);goto rejected;}}
 else{const char *user=text(cJSON_GetObjectItemCaseSensitive(current,"user"),"id");int match=user&&!strcmp(user,a->user);cJSON_Delete(current);if(!match){app_fail(a,"SESSION_REJECTED");a->account.http_status=401;goto rejected;}}
 if(!offline){cJSON *own=app_credential(a);if(!own){if(a->account.http_status>=500||!strcmp(a->account.error_code,"NETWORK_UNAVAILABLE"))offline=1;else goto rejected;}else{const char *key=text(own,"license_key");wipe(a->account.key);free(a->account.key);a->account.key=copy(key);cJSON_DeleteItemFromObjectCaseSensitive(record,"license_key");cJSON_AddStringToObject(record,"license_key",key);cJSON_Delete(own);}}
 {int ok=app_start(a,offline);if(ok&&!offline){raw=cJSON_PrintUnformatted(record);ok=raw&&app_save_record(a,raw);wipe(raw);free(raw);if(!ok&&a->runtime){ln_destroy(a->runtime);a->runtime=NULL;}}cJSON_Delete(record);return ok?1:-1;}
 rejected:
 if(strcmp(a->error,"ENTITLEMENT_PENDING")&&(a->account.http_status==401||a->account.http_status==403||a->account.http_status==404||!strcmp(a->error,"ENTITLEMENT_EXPIRED")||!strcmp(a->error,"ENTITLEMENT_SUSPENDED")||!strcmp(a->error,"ENTITLEMENT_NOT_PURCHASED"))){char failure[80];strcpy(failure,a->error);app_forget(a);app_fail(a,failure);}cJSON_Delete(record);return -1;
}
int launch_application_run_feature(launch_application *a,const char *feature,ln_feature_action action,void *context){if(!a||!a->runtime)return a?app_fail(a,"LOGIN_REQUIRED"):0;return ln_run_feature(a->runtime,feature,action,context)?1:app_fail(a,"SDK_FEATURE_DENIED");}
int launch_application_release_device(launch_application *a){if(!a||!a->runtime)return a?app_fail(a,"LOGIN_REQUIRED"):0;if(!ln_deactivate(a->runtime))return app_fail(a,"SDK_RELEASE_FAILED");return launch_application_logout(a);}
void launch_application_close(launch_application *a){if(!a)return;if(a->runtime)ln_destroy(a->runtime);wipe(a->record);free(a->record);wipe(a->account.token);free(a->account.token);wipe(a->account.key);free(a->account.key);free(a->account.origin);free(a->account.cache_path);free(a->product);free(a->environment);free(a->config_json);free(a->scope);free(a->user);free(a);}
launch_application *launch_application_cli(int argc,char **argv){
 if(argc<4){fputs("--config LAUNCH_CONFIG PRODUCT_JSON [--remember] [--sign-out] [--release-device]\n",stderr);return NULL;}int remember=0;for(int i=1;i<argc;i++)if(!strcmp(argv[i],"--remember"))remember=1;
 launch_application *a=!strcmp(argv[1],"--config")?launch_application_from_files(argv[2],argv[3],remember,NULL):launch_application_open(argv[1],argv[2],argv[3],argc>4&&strncmp(argv[4],"--",2)?argv[4]:"live",remember,NULL);
 if(!a){fputs("Invalid configuration or secure store unavailable\n",stderr);return NULL;}for(int i=1;i<argc;i++)if(!strcmp(argv[i],"--sign-out"))return a;int restored=launch_application_restore(a);if(restored<0){fprintf(stderr,"Launch: %s\n",a->error);launch_application_close(a);return NULL;}if(restored)return a;
 char email[255],password[129]={0};printf("Email: ");if(!fgets(email,sizeof(email),stdin)){launch_application_close(a);return NULL;}email[strcspn(email,"\r\n")]=0;printf("Password: ");fflush(stdout);if(!read_password(password)){launch_application_close(a);return NULL;}int ok=launch_application_login(a,email,password);wipe(password);if(!ok){fprintf(stderr,"Launch: %s\n",a->error);launch_application_close(a);return NULL;}return a;
}
