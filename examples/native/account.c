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
#include <fcntl.h>
#include <sys/file.h>
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
int launch_login(launch_account *a,const char *origin,const char *product,const char *config,const char *environment,const char *email,const char *password){(void)origin;(void)product;(void)config;(void)environment;(void)email;(void)password;if(a)snprintf(a->error_code,sizeof(a->error_code),"LEGACY_INTEGRATION_REMOVED");return 0;}
void launch_sign_out(launch_account *a,int clear_cache){
 if(a->token){cJSON *input=cJSON_CreateObject();cJSON *result=request(a,"/auth/logout",input);cJSON_Delete(input);cJSON_Delete(result);}(void)clear_cache;
 wipe(a->token);wipe(a->key);free(a->token);free(a->key);free(a->origin);free(a->cache_path);free(a->public_config_path);memset(a,0,sizeof(*a));
}
int launch_sign_in(launch_account *a,int argc,char **argv){
 memset(a,0,sizeof(*a));if(argc<4){fputs("MERCHANT_ORIGIN LAUNCH_PRODUCT_ID PRODUCT_JSON [sandbox|live]\n",stderr);return 0;}
 char email[255],password[129]={0};printf("Email: ");if(!fgets(email,sizeof(email),stdin))return 0;email[strcspn(email,"\r\n")]=0;printf("Password: ");fflush(stdout);if(!read_password(password))return 0;
 int ok=launch_login(a,argv[1],argv[2],argv[3],argc>4?argv[4]:"live",email,password);wipe(password);return ok;
}

struct launch_application {
 launch_account account;
 char *product,*environment,*config_json,*scope,*record,*user,*identity_context,*pending_records;
 int remember,logout_pending,account_cleared;
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
static cJSON *app_public_identity(const cJSON *input){
 cJSON *result=cJSON_CreateObject();if(!result)return NULL;
 const char *names[]={"product_id","licentivo_product_id","launch_connection_id","environment","expires_at"};
 for(size_t i=0;i<sizeof(names)/sizeof(names[0]);i++){const cJSON *value=cJSON_GetObjectItemCaseSensitive(input,names[i]);if(value){cJSON *copy_value=cJSON_Duplicate(value,1);if(!copy_value||!cJSON_AddItemToObject(result,names[i],copy_value)){cJSON_Delete(copy_value);cJSON_Delete(result);return NULL;}}}
 return result;
}
static cJSON *app_credential(launch_application *a){
 char endpoint[160];snprintf(endpoint,sizeof(endpoint),"/software/products/%s/identity",a->product);cJSON *input=cJSON_CreateObject();cJSON_AddStringToObject(input,"environment",a->environment);cJSON *own=request(&a->account,endpoint,input);cJSON_Delete(input);if(!own){app_fail(a,a->account.error_code);return NULL;}
 cJSON *public_config=ln_json(a->config_json);const char *expected=text(public_config,"product_id"),*actual=text(own,"licentivo_product_id"),*product=text(own,"product_id"),*environment=text(own,"environment"),*connection=text(own,"launch_connection_id"),*token=text(own,"identity_token");
 int ok=expected&&actual&&!strcmp(expected,actual)&&product&&!strcmp(product,a->product)&&environment&&!strcmp(environment,a->environment)&&connection&&strlen(connection)==36&&token&&strlen(token)>=32&&strlen(token)<=12288;cJSON_Delete(public_config);if(!ok){cJSON_Delete(own);app_fail(a,"IDENTITY_MISMATCH");return NULL;}return own;
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
static char *app_queue_key(launch_application *a){size_t n=strlen(a->scope)+32;char *key=(char*)malloc(n);if(key)snprintf(key,n,"%s\naccount-logout-queue-v1",a->scope);return key;}
static int app_key_load(launch_application *a,const char *key,char **raw){
 *raw=NULL;if(a->store.load)return a->store.load(a->store.context,key,raw);if(a->remember){
#ifdef _WIN32
 launch_application view=*a;view.scope=(char*)key;return app_dpapi_load(&view,raw);
#else
 return -1;
#endif
 }if(a->pending_records){*raw=copy(a->pending_records);return *raw?1:-1;}return 0;
}
static int app_key_save(launch_application *a,const char *key,const char *raw){
 if(a->store.save)return a->store.save(a->store.context,key,raw);if(a->remember){
#ifdef _WIN32
 launch_application view=*a;view.scope=(char*)key;return app_dpapi_save(&view,raw);
#else
 return 0;
#endif
 }char *saved=copy(raw);if(!saved)return 0;wipe(a->pending_records);free(a->pending_records);a->pending_records=saved;return 1;
}
static int app_key_delete(launch_application *a,const char *key){
 if(a->store.remove)return a->store.remove(a->store.context,key);if(a->remember){
#ifdef _WIN32
 launch_application view=*a;view.scope=(char*)key;char path[2400];return app_record_path(&view,path)&&(DeleteFileA(path)||GetLastError()==ERROR_FILE_NOT_FOUND);
#else
 return 0;
#endif
 }char **value=!strcmp(key,a->scope)?&a->record:&a->pending_records;wipe(*value);free(*value);*value=NULL;return 1;
}
/* 0=missing, 1=owned, 2=newer/other session, -1=unreadable. */
static int app_active_owner(launch_application *a,const char *token){char *raw=NULL;int found=app_load_record(a,&raw);if(found<=0)return found;cJSON *record=ln_json(raw);wipe(raw);free(raw);const char *stored=text(record,"access_token"),*scope=text(record,"scope");int owner=stored&&scope&&!strcmp(scope,a->scope)?(token&&!strcmp(token,stored)?1:2):-1;cJSON_Delete(record);return owner;}
static int app_delete_owned(launch_application *a,const char *token){int owner=app_active_owner(a,token);return owner<0?0:owner!=1||app_key_delete(a,a->scope);}
static cJSON *app_pending(launch_application *a){char *key=app_queue_key(a),*raw=NULL;if(!key)return NULL;int found=app_key_load(a,key,&raw);free(key);if(found==0)return cJSON_CreateArray();if(found<0)return NULL;cJSON *queue=ln_json(raw);wipe(raw);free(raw);cJSON *version=cJSON_GetObjectItemCaseSensitive(queue,"version"),*records=cJSON_GetObjectItemCaseSensitive(queue,"records");if(!cJSON_IsNumber(version)||version->valuedouble!=1||!cJSON_IsArray(records)||cJSON_GetArraySize(records)>64){cJSON_Delete(queue);return NULL;}cJSON *record=NULL;cJSON_ArrayForEach(record,records){const char *scope=text(record,"scope"),*token=text(record,"access_token");if(!scope||strcmp(scope,a->scope)||!token||!*token||!cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(record,"logout_pending"))||text(cJSON_GetObjectItemCaseSensitive(record,"identity_context"),"identity_token")){cJSON_Delete(queue);return NULL;}}cJSON *result=cJSON_Duplicate(records,1);cJSON_Delete(queue);return result;}
static int app_update_pending(launch_application *a,const cJSON *record,int remove){cJSON *records=app_pending(a);const char *token=text((cJSON*)record,"access_token");if(!records||!token){cJSON_Delete(records);return 0;}for(int i=cJSON_GetArraySize(records)-1;i>=0;i--){const char *other=text(cJSON_GetArrayItem(records,i),"access_token");if(other&&!strcmp(other,token))cJSON_DeleteItemFromArray(records,i);}if(!remove){cJSON *entry=cJSON_Duplicate(record,1);if(!entry||!cJSON_AddItemToArray(records,entry)){cJSON_Delete(entry);cJSON_Delete(records);return 0;}}char *key=app_queue_key(a);if(!key||cJSON_GetArraySize(records)>64){free(key);cJSON_Delete(records);return 0;}if(cJSON_GetArraySize(records)==0){int ok=app_key_delete(a,key);free(key);cJSON_Delete(records);return ok;}cJSON *queue=cJSON_CreateObject();cJSON_AddNumberToObject(queue,"version",1);cJSON_AddItemToObject(queue,"records",records);char *raw=cJSON_PrintUnformatted(queue);cJSON_Delete(queue);int ok=raw&&strlen(raw)<60000&&app_key_save(a,key,raw);wipe(raw);free(raw);free(key);return ok;}
static int app_finish_account(launch_application *a,const cJSON *record){const char *token=text((cJSON*)record,"access_token");return app_delete_owned(a,token)&&app_update_pending(a,record,1);}
static int app_retry_others(launch_application *a,const char *skip){cJSON *records=app_pending(a);if(!records)return 0;int ok=1;cJSON *record=NULL;cJSON_ArrayForEach(record,records){const char *token=text(record,"access_token");if(skip&&!strcmp(skip,token))continue;launch_account account=a->account;account.token=(char*)token;cJSON *input=cJSON_CreateObject(),*result=request(&account,"/auth/logout",input);int confirmed=result!=NULL||account.http_status==401;cJSON_Delete(input);cJSON_Delete(result);if(!confirmed||!app_finish_account(a,record))ok=0;}cJSON_Delete(records);return ok;}
typedef struct app_guard {
#ifdef _WIN32
 HANDLE handle;
#else
 int fd;
#endif
} app_guard;
static int app_account_lock(launch_application *a,app_guard *guard){char hash[65],root[2300],path[2400];if(!hash_scope(a->scope,hash))return 0;
#ifdef _WIN32
 const char *base=getenv("LOCALAPPDATA");if(!base||snprintf(root,sizeof(root),"%s\\LicentivoLaunch",base)>=(int)sizeof(root)-100)return 0;MKDIR(root);strcat(root,"\\account-locks");MKDIR(root);strcat(root,"\\native");MKDIR(root);if(snprintf(path,sizeof(path),"%s\\%s.lock",root,hash)>=(int)sizeof(path))return 0;ULONGLONG end=GetTickCount64()+5000;do{guard->handle=CreateFileA(path,GENERIC_READ|GENERIC_WRITE,0,NULL,OPEN_ALWAYS,FILE_ATTRIBUTE_NORMAL,NULL);if(guard->handle!=INVALID_HANDLE_VALUE)return 1;if(GetLastError()!=ERROR_SHARING_VIOLATION&&GetLastError()!=ERROR_LOCK_VIOLATION)return 0;Sleep(20);}while(GetTickCount64()<end);return 0;
#else
 const char *base=getenv("HOME");if(!base||snprintf(root,sizeof(root),"%s/.cache",base)>=(int)sizeof(root)-100)return 0;MKDIR(root);strcat(root,"/LicentivoLaunch");MKDIR(root);strcat(root,"/account-locks");MKDIR(root);strcat(root,"/native");MKDIR(root);if(snprintf(path,sizeof(path),"%s/%s.lock",root,hash)>=(int)sizeof(path))return 0;guard->fd=open(path,O_CREAT|O_RDWR,0600);if(guard->fd<0)return 0;for(int attempt=0;attempt<250;attempt++){if(flock(guard->fd,LOCK_EX|LOCK_NB)==0)return 1;usleep(20000);}close(guard->fd);return 0;
#endif
}
static void app_account_unlock(app_guard *guard){
#ifdef _WIN32
 CloseHandle(guard->handle);
#else
 flock(guard->fd,LOCK_UN);close(guard->fd);
#endif
}
static int app_forget(launch_application *a){
 if(a->runtime){ln_destroy(a->runtime);a->runtime=NULL;}int ok=a->account.token?app_delete_owned(a,a->account.token):1;
 /* SDK-owned files, including pending logout tickets, must survive. */
 wipe(a->record);free(a->record);a->record=NULL;wipe(a->account.token);free(a->account.token);a->account.token=NULL;wipe(a->account.key);free(a->account.key);a->account.key=NULL;free(a->identity_context);a->identity_context=NULL;free(a->user);a->user=NULL;a->logout_pending=0;a->account_cleared=1;return ok;
}
static int app_attach(launch_application *a,cJSON *record){
 const char *scope=text(record,"scope"),*user=text(record,"user_id"),*token=text(record,"access_token");cJSON *version=cJSON_GetObjectItemCaseSensitive(record,"version"),*context=cJSON_GetObjectItemCaseSensitive(record,"identity_context");const char *connection=text(context,"launch_connection_id");
 int pending=cJSON_IsTrue(cJSON_GetObjectItemCaseSensitive(record,"logout_pending"));
 if(!cJSON_IsNumber(version)||(version->valuedouble!=2&&!(pending&&version->valuedouble==3))||!scope||strcmp(scope,a->scope)||!token||!*token||(!pending&&(!user||!*user||!connection||strlen(connection)!=36||text(context,"identity_token"))))return app_fail(a,"INVALID_SAVED_SESSION");
 a->account_cleared=0;free(a->user);a->user=copy(user?user:"");wipe(a->account.token);free(a->account.token);a->account.token=copy(token);wipe(a->account.key);free(a->account.key);a->account.key=copy("");free(a->identity_context);a->identity_context=cJSON_IsObject(context)?cJSON_PrintUnformatted(context):NULL;a->logout_pending=pending;return a->user&&a->account.token&&a->account.key&&(pending?(!connection||!*a->user||app_cache(a)):(a->identity_context&&app_cache(a)));
}
static cJSON *app_logout_record(launch_application *a){
 cJSON *record=cJSON_CreateObject();if(!record)return NULL;
 cJSON_AddNumberToObject(record,"version",3);cJSON_AddStringToObject(record,"scope",a->scope);cJSON_AddBoolToObject(record,"logout_pending",1);cJSON_AddStringToObject(record,"access_token",a->account.token);cJSON_AddStringToObject(record,"user_id",a->user?a->user:"");
 cJSON *context=a->identity_context?ln_json(a->identity_context):NULL;if(context){cJSON_DeleteItemFromObjectCaseSensitive(context,"identity_token");cJSON_AddItemToObject(record,"identity_context",context);}
 return record;
}
static int app_mark_logout(launch_application *a){
 a->logout_pending=1;cJSON *record=app_logout_record(a);if(!record)return 0;int queued=app_update_pending(a,record,0),active=0;char *raw=cJSON_PrintUnformatted(record);cJSON_Delete(record);if(app_active_owner(a,a->account.token)==1){active=raw&&app_save_record(a,raw);if(!active)app_delete_owned(a,a->account.token);}wipe(raw);free(raw);return queued||active;
}
static char *app_runtime_config(launch_application *a){
 cJSON *config=ln_json(a->config_json),*context=a->identity_context?ln_json(a->identity_context):NULL;if(!config||!context){cJSON_Delete(config);cJSON_Delete(context);return NULL;}const char *connection=text(context,"launch_connection_id");if(!connection){cJSON_Delete(config);cJSON_Delete(context);return NULL;}
 const char *names[]={"license_key","identity_token","launch_connection_id","environment","cache_path"};for(int i=0;i<5;i++)cJSON_DeleteItemFromObjectCaseSensitive(config,names[i]);cJSON_AddStringToObject(config,"license_key","");cJSON_AddStringToObject(config,"identity_token",a->account.key?a->account.key:"");cJSON_AddStringToObject(config,"launch_connection_id",connection);cJSON_AddStringToObject(config,"environment",a->environment);cJSON_AddStringToObject(config,"cache_path",a->account.cache_path);char *raw=cJSON_PrintUnformatted(config);cJSON_Delete(config);cJSON_Delete(context);return raw;
}
static int app_start(launch_application *a,int offline){
 (void)offline;if(a->runtime){ln_destroy(a->runtime);a->runtime=NULL;}char *raw=app_runtime_config(a);if(!raw)return app_fail(a,"INVALID_CONFIG");ln_client *sdk=ln_init(raw);wipe(raw);free(raw);if(!sdk)return app_fail(a,"SDK_INIT_FAILED");if(!ln_start(sdk)){ln_error_info info={0};ln_get_error_info(sdk,&info);app_fail(a,info.code[0]?info.code:"SDK_AUTHORIZATION_FAILED");ln_destroy(sdk);return 0;}a->runtime=sdk;a->error[0]=0;return 1;
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
 char scope[2048];int n=snprintf(scope,sizeof(scope),"launch-account-v2\n%s\n%s\n%s\n%s",a->account.origin?a->account.origin:"",product,environment,sdk_product);cJSON_Delete(config);if(n<0||n>=(int)sizeof(scope)){launch_application_close(a);return NULL;}a->scope=copy(scope);
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
static int app_logout(launch_application *a){
 if(!a)return 0;if(!a->account.token){char *raw=NULL;int found=app_load_record(a,&raw);if(found<0){app_forget(a);return app_fail(a,"SECURE_STORE_FAILED");}if(found){cJSON *saved=ln_json(raw);int valid=saved&&app_attach(a,saved);cJSON_Delete(saved);wipe(raw);free(raw);if(!valid){app_forget(a);return app_fail(a,"INVALID_SAVED_SESSION");}}}
 if(!a->account.token){cJSON *records=app_pending(a);if(!records){app_forget(a);return app_fail(a,"SECURE_STORE_FAILED");}cJSON *record=cJSON_GetArrayItem(records,0);int ok=!record||app_attach(a,record);cJSON_Delete(records);if(!ok)return 0;}
 int others=app_retry_others(a,a->account.token);int persisted=!a->account.token||app_mark_logout(a);
 int released=1,pending=0,owner=app_active_owner(a,a->account.token);if(!a->runtime&&a->account.token&&a->identity_context&&owner>=0&&owner<=1){char *raw=app_runtime_config(a);if(raw){a->runtime=ln_init(raw);wipe(raw);free(raw);}if(!a->runtime)released=0;}
 if(a->runtime){released=ln_logout(a->runtime);pending=ln_logout_pending(a->runtime);}
 if(a->runtime){ln_destroy(a->runtime);a->runtime=NULL;}wipe(a->account.key);free(a->account.key);a->account.key=NULL;
 int account=1;if(a->account.token){cJSON *input=cJSON_CreateObject(),*result=request(&a->account,"/auth/logout",input);account=result!=NULL||a->account.http_status==401;cJSON_Delete(input);cJSON_Delete(result);}
 int ok=1;if(account){if(a->account.token){cJSON *record=app_logout_record(a);ok=record&&app_finish_account(a,record);cJSON_Delete(record);}if(!app_forget(a))ok=0;}
 if(!ok)return app_fail(a,"SECURE_STORE_FAILED");if(!account&&!persisted)return app_fail(a,"ACCOUNT_LOGOUT_STORAGE_FAILED");if(!released)return app_fail(a,"SDK_LOGOUT_FAILED");if(pending)return app_fail(a,"DEVICE_REVOCATION_PENDING");if(!account||!others)return app_fail(a,"ACCOUNT_LOGOUT_PENDING");a->error[0]=0;return 1;
}
static int app_login(launch_application *a,const char *email,const char *password){
 if(!a||!email||!password)return 0;if(!app_logout(a))return 0;cJSON *input=cJSON_CreateObject();cJSON_AddStringToObject(input,"email",email);cJSON_AddStringToObject(input,"password",password);cJSON_AddStringToObject(input,"kind","desktop");cJSON *grant=request(&a->account,"/auth/login",input);cJSON_Delete(input);if(!grant)return app_fail(a,a->account.error_code);
 const char *token=text(grant,"access_token"),*expires=text(grant,"expires_at"),*user=text(cJSON_GetObjectItemCaseSensitive(grant,"user"),"id");if(!token||!expires||!user){cJSON_Delete(grant);return app_fail(a,"INVALID_RESPONSE");}a->account.token=copy(token);cJSON *own=app_credential(a);if(!own){char failure[80];snprintf(failure,sizeof(failure),"%s",a->error);cJSON_Delete(grant);app_logout(a);app_fail(a,failure);return 0;}
 char *identity=copy(text(own,"identity_token"));cJSON *public_identity=app_public_identity(own);cJSON_Delete(own);own=public_identity;cJSON *record=cJSON_CreateObject();cJSON_AddNumberToObject(record,"version",2);cJSON_AddStringToObject(record,"scope",a->scope);cJSON_AddStringToObject(record,"user_id",user);cJSON_AddStringToObject(record,"access_token",token);cJSON_AddStringToObject(record,"expires_at",expires);cJSON_AddItemToObject(record,"identity_context",own);cJSON_Delete(grant);
 int ok=app_attach(a,record);if(ok){wipe(a->account.key);free(a->account.key);a->account.key=identity;identity=NULL;ok=app_start(a,0);}wipe(identity);free(identity);char *raw=cJSON_PrintUnformatted(record);cJSON_Delete(record);if(ok)ok=raw&&app_save_record(a,raw);wipe(raw);free(raw);if(!ok){char failure[80];strcpy(failure,a->error);app_logout(a);app_fail(a,failure);}return ok;
}
static int app_restore(launch_application *a){
 if(a&&a->logout_pending)return app_logout(a)?0:-1;
 if(!a)return -1;if(a->runtime){ln_destroy(a->runtime);a->runtime=NULL;}char *raw=NULL;int found=app_load_record(a,&raw);if(!found){cJSON *records=app_pending(a);if(!records){app_fail(a,"SECURE_STORE_FAILED");return -1;}cJSON *pending=cJSON_GetArrayItem(records,0);int attached=pending&&app_attach(a,pending);cJSON_Delete(records);return attached?(app_logout(a)?0:-1):0;}if(found<0){app_fail(a,"SECURE_STORE_FAILED");return -1;}cJSON *record=ln_json(raw);wipe(raw);free(raw);if(!record||!app_attach(a,record)){cJSON_Delete(record);app_forget(a);return -1;}
 cJSON *records=app_pending(a),*task=NULL;if(!records){cJSON_Delete(record);app_fail(a,"SECURE_STORE_FAILED");return -1;}cJSON_ArrayForEach(task,records){const char *token=text(task,"access_token");if(token&&!strcmp(token,a->account.token)){app_attach(a,task);break;}}cJSON_Delete(records);
 if(a->logout_pending){cJSON_Delete(record);return app_logout(a)?0:-1;}
 if(!app_retry_others(a,a->account.token)){cJSON_Delete(record);app_fail(a,"ACCOUNT_LOGOUT_PENDING");return -1;}
 if(app_expiry(text(record,"expires_at"))<=time(NULL)){cJSON_Delete(record);app_forget(a);app_fail(a,"SESSION_EXPIRED");return -1;}cJSON *current=request_method(&a->account,"/me",NULL,1);if(!current){char failure[80];snprintf(failure,sizeof(failure),"%s",a->account.error_code);cJSON_Delete(record);if(a->account.http_status==401||a->account.http_status==403||a->account.http_status==404)app_forget(a);app_fail(a,failure);return -1;}const char *user=text(cJSON_GetObjectItemCaseSensitive(current,"user"),"id");int match=user&&!strcmp(user,a->user);cJSON_Delete(current);if(!match){cJSON_Delete(record);app_forget(a);app_fail(a,"SESSION_REJECTED");return -1;}
 cJSON *own=app_credential(a);if(!own){char failure[80];snprintf(failure,sizeof(failure),"%s",a->error);cJSON_Delete(record);if(strcmp(failure,"ENTITLEMENT_PENDING")&&(a->account.http_status==401||a->account.http_status==403||a->account.http_status==404))app_forget(a);app_fail(a,failure);return -1;}wipe(a->account.key);free(a->account.key);a->account.key=copy(text(own,"identity_token"));cJSON *public_identity=app_public_identity(own);cJSON_Delete(own);own=public_identity;free(a->identity_context);a->identity_context=cJSON_PrintUnformatted(own);cJSON_DeleteItemFromObjectCaseSensitive(record,"identity_context");cJSON_AddItemToObject(record,"identity_context",own);int ok=app_start(a,0);if(ok){raw=cJSON_PrintUnformatted(record);ok=raw&&app_save_record(a,raw);wipe(raw);free(raw);}cJSON_Delete(record);return ok?1:-1;
}
int launch_application_run_feature(launch_application *a,const char *feature,ln_feature_action action,void *context){if(!a||!a->runtime)return a?app_fail(a,"LOGIN_REQUIRED"):0;return ln_run_feature(a->runtime,feature,action,context)?1:app_fail(a,"SDK_FEATURE_DENIED");}
int launch_application_login(launch_application *a,const char *email,const char *password){if(!a)return 0;app_guard guard;if(!app_account_lock(a,&guard))return app_fail(a,"ACCOUNT_LOCK_BUSY");int ok=app_login(a,email,password);app_account_unlock(&guard);return ok;}
int launch_application_restore(launch_application *a){if(!a)return -1;app_guard guard;if(!app_account_lock(a,&guard)){app_fail(a,"ACCOUNT_LOCK_BUSY");return -1;}int result=app_restore(a);app_account_unlock(&guard);return result;}
int launch_application_logout(launch_application *a){if(!a)return 0;app_guard guard;if(!app_account_lock(a,&guard)){if(a->runtime){ln_destroy(a->runtime);a->runtime=NULL;}if(a->account.token)a->logout_pending=1;return app_fail(a,"ACCOUNT_LOCK_BUSY");}int ok=a->account_cleared&&!a->account.token?app_retry_others(a,NULL):app_logout(a);if(!ok&&a->account_cleared&&!a->account.token)app_fail(a,"ACCOUNT_LOGOUT_PENDING");if(a->runtime){ln_destroy(a->runtime);a->runtime=NULL;}if(!ok&&a->account.token)a->logout_pending=1;app_account_unlock(&guard);return ok;}
int launch_application_release_device(launch_application *a){return launch_application_logout(a);}
void launch_application_close(launch_application *a){if(!a)return;if(a->runtime)ln_destroy(a->runtime);wipe(a->record);free(a->record);wipe(a->pending_records);free(a->pending_records);wipe(a->account.token);free(a->account.token);wipe(a->account.key);free(a->account.key);free(a->account.origin);free(a->account.cache_path);free(a->product);free(a->environment);free(a->config_json);free(a->scope);free(a->user);free(a->identity_context);free(a);}
launch_application *launch_application_cli(int argc,char **argv){
 if(argc<4){fputs("--config LAUNCH_CONFIG PRODUCT_JSON [--remember] [--sign-out] [--release-device]\n",stderr);return NULL;}int remember=0;for(int i=1;i<argc;i++)if(!strcmp(argv[i],"--remember"))remember=1;
 launch_application *a=!strcmp(argv[1],"--config")?launch_application_from_files(argv[2],argv[3],remember,NULL):launch_application_open(argv[1],argv[2],argv[3],argc>4&&strncmp(argv[4],"--",2)?argv[4]:"live",remember,NULL);
 if(!a){fputs("Invalid configuration or secure store unavailable\n",stderr);return NULL;}for(int i=1;i<argc;i++)if(!strcmp(argv[i],"--sign-out"))return a;int restored=launch_application_restore(a);if(restored<0){fprintf(stderr,"Launch: %s\n",a->error);launch_application_close(a);return NULL;}if(restored)return a;
 char email[255],password[129]={0};printf("Email: ");if(!fgets(email,sizeof(email),stdin)){launch_application_close(a);return NULL;}email[strcspn(email,"\r\n")]=0;printf("Password: ");fflush(stdout);if(!read_password(password)){launch_application_close(a);return NULL;}int ok=launch_application_login(a,email,password);wipe(password);if(!ok){fprintf(stderr,"Launch: %s\n",a->error);launch_application_close(a);return NULL;}return a;
}
