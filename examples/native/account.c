#include "account.h"
#include <curl/curl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#ifdef _WIN32
#include <conio.h>
#include <direct.h>
#include <windows.h>
#include <bcrypt.h>
#define MKDIR(path) _mkdir(path)
#define SEP "\\"
#else
#include <sys/stat.h>
#include <termios.h>
#include <unistd.h>
#include <openssl/sha.h>
#define MKDIR(path) mkdir(path,0700)
#define SEP "/"
#endif

typedef struct buffer {char *data;size_t length;} buffer;
static char *copy(const char *s){size_t n=strlen(s)+1;char *v=(char*)malloc(n);if(v)memcpy(v,s,n);return v;}
static void wipe(char *v){if(v){volatile char *p=v;while(*p){*p++=0;}}}
static const char *text(cJSON *object,const char *field){cJSON *v=cJSON_GetObjectItemCaseSensitive(object,field);return cJSON_IsString(v)?v->valuestring:NULL;}
static size_t receive(char *data,size_t size,size_t count,void *context){buffer *b=(buffer*)context;if(size && count>1048576/size)return 0;size_t n=size*count;if(n>1048576-b->length)return 0;char *next=(char*)realloc(b->data,b->length+n+1);if(!next)return 0;b->data=next;memcpy(b->data+b->length,data,n);b->length+=n;b->data[b->length]=0;return n;}
static cJSON *request(launch_account *a,const char *path,cJSON *input){
 CURL *curl=curl_easy_init();if(!curl)return NULL;char *body=cJSON_PrintUnformatted(input);size_t length=strlen(a->origin)+strlen(path)+9;char *url=(char*)malloc(length);char auth[256];buffer out={NULL,0};cJSON *data=NULL;struct curl_slist *headers=NULL;
 if(!body || !url)goto finish;snprintf(url,length,"%s/api/v1%s",a->origin,path);headers=curl_slist_append(headers,"Content-Type: application/json");
 if(a->token){if(strlen(a->token)>200)goto finish;snprintf(auth,sizeof(auth),"Authorization: Bearer %s",a->token);headers=curl_slist_append(headers,auth);}
 curl_easy_setopt(curl,CURLOPT_URL,url);curl_easy_setopt(curl,CURLOPT_POSTFIELDS,body);curl_easy_setopt(curl,CURLOPT_HTTPHEADER,headers);curl_easy_setopt(curl,CURLOPT_TIMEOUT,15L);curl_easy_setopt(curl,CURLOPT_FOLLOWLOCATION,0L);curl_easy_setopt(curl,CURLOPT_WRITEFUNCTION,receive);curl_easy_setopt(curl,CURLOPT_WRITEDATA,&out);
 if(curl_easy_perform(curl)!=CURLE_OK)goto finish;long status=0;curl_easy_getinfo(curl,CURLINFO_RESPONSE_CODE,&status);cJSON *root=out.data?ln_json(out.data):NULL;
 if(status>=200 && status<300 && root)data=cJSON_Duplicate(cJSON_GetObjectItemCaseSensitive(root,"data"),1);
 else{const char *code=root?text(cJSON_GetObjectItemCaseSensitive(root,"error"),"code"):NULL;if(code && strspn(code,"ABCDEFGHIJKLMNOPQRSTUVWXYZ_0123456789")==strlen(code))fprintf(stderr,"Launch: %s\n",code);else fputs("Merchant request unavailable\n",stderr);}cJSON_Delete(root);
 finish:curl_slist_free_all(headers);curl_easy_cleanup(curl);wipe(body);free(body);free(url);wipe(out.data);free(out.data);return data;
}
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
int launch_sign_in(launch_account *a,int argc,char **argv){
 memset(a,0,sizeof(*a));if(argc<4){fputs("MERCHANT_ORIGIN LAUNCH_PRODUCT_ID PRODUCT_JSON [sandbox|live] [--sign-out]\n",stderr);return 0;}const char *environment=argc>4?argv[4]:"live";
 if(strcmp(environment,"sandbox") && strcmp(environment,"live"))return 0;if(strlen(argv[2])!=36 || strspn(argv[2],"0123456789abcdef-")!=36 || !origin_ok(argv[1],!strcmp(environment,"sandbox")))return 0;
 a->origin=copy(argv[1]);if(!a->origin)return 0;size_t n=strlen(a->origin);if(n && a->origin[n-1]=='/')a->origin[n-1]=0;
 char email[255],password[129]={0};printf("Email: ");if(!fgets(email,sizeof(email),stdin))return 0;email[strcspn(email,"\r\n")]=0;printf("Password: ");fflush(stdout);if(!read_password(password))return 0;
 cJSON *login=cJSON_CreateObject();cJSON_AddStringToObject(login,"email",email);cJSON_AddStringToObject(login,"password",password);cJSON_AddStringToObject(login,"kind","desktop");cJSON *grant=request(a,"/auth/login",login);cJSON_Delete(login);wipe(password);if(!grant)return 0;
 const char *token=text(grant,"access_token"),*user=text(cJSON_GetObjectItemCaseSensitive(grant,"user"),"id");if(!token || !user){cJSON_Delete(grant);return 0;}a->token=copy(token);
 char scope_input[1024],scope[65],cache_root[2048],dir[2300],path[2400];if(snprintf(scope_input,sizeof(scope_input),"%s\n%s\n%s\n%s",a->origin,user,argv[2],environment)>=(int)sizeof(scope_input) || !hash_scope(scope_input,scope)){cJSON_Delete(grant);return 0;}cJSON_Delete(grant);
 cJSON *input=cJSON_CreateObject();cJSON_AddStringToObject(input,"environment",environment);char endpoint[160];snprintf(endpoint,sizeof(endpoint),"/software/products/%s/credential",argv[2]);cJSON *own=request(a,endpoint,input);cJSON_Delete(input);if(!own)return 0;const char *key=text(own,"license_key"),*product=text(own,"product_id");if(!key || !product || strcmp(product,argv[2])){cJSON_Delete(own);return 0;}a->key=copy(key);cJSON_Delete(own);
#ifdef _WIN32
 const char *root=getenv("LOCALAPPDATA");if(!root || snprintf(cache_root,sizeof(cache_root),"%s" SEP "LicentivoLaunch",root)>=(int)sizeof(cache_root))return 0;
#else
 const char *root=getenv("XDG_CACHE_HOME");if(root){if(snprintf(cache_root,sizeof(cache_root),"%s" SEP "LicentivoLaunch",root)>=(int)sizeof(cache_root))return 0;}else{root=getenv("HOME");if(!root || snprintf(cache_root,sizeof(cache_root),"%s/.cache",root)>=(int)sizeof(cache_root))return 0;MKDIR(cache_root);if(snprintf(cache_root,sizeof(cache_root),"%s/.cache/LicentivoLaunch",root)>=(int)sizeof(cache_root))return 0;}
#endif
 MKDIR(cache_root);if(snprintf(dir,sizeof(dir),"%s" SEP "%s",cache_root,scope)>=(int)sizeof(dir))return 0;MKDIR(dir);snprintf(path,sizeof(path),"%s" SEP "lease.json",dir);a->cache_path=copy(path);
 char *raw=file_text(argv[3]);cJSON *config=raw?ln_json(raw):NULL;free(raw);if(!config)return 0;cJSON_DeleteItemFromObjectCaseSensitive(config,"license_key");cJSON_AddStringToObject(config,"license_key","");cJSON_DeleteItemFromObjectCaseSensitive(config,"cache_path");cJSON_AddStringToObject(config,"cache_path",a->cache_path);char *public_config=cJSON_PrintUnformatted(config);cJSON_Delete(config);if(!public_config)return 0;
 snprintf(path,sizeof(path),"%s" SEP "account-product.json",dir);FILE *f=fopen(path,"wb");if(!f){free(public_config);return 0;}int ok=fputs(public_config,f)>=0;int closed=fclose(f)==0;free(public_config);if(!ok || !closed)return 0;a->public_config_path=copy(path);return a->token && a->key && a->cache_path && a->public_config_path;
}
void launch_sign_out(launch_account *a,int clear_cache){
 if(a->token){cJSON *input=cJSON_CreateObject();cJSON *result=request(a,"/auth/logout",input);cJSON_Delete(input);cJSON_Delete(result);}if(clear_cache && a->cache_path){remove(a->cache_path);size_t n=strlen(a->cache_path)+32;char *credential=(char*)malloc(n);if(credential){snprintf(credential,n,"%s.credential.json",a->cache_path);remove(credential);free(credential);}}
 wipe(a->token);wipe(a->key);free(a->token);free(a->key);free(a->origin);free(a->cache_path);free(a->public_config_path);memset(a,0,sizeof(*a));
}
