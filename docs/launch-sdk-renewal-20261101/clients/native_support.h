#ifndef JOINT_NATIVE_SUPPORT_H
#define JOINT_NATIVE_SUPPORT_H
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "account.h"
#include "vendor/cJSON.h"
typedef struct {char *keys[16],*values[16];} memory_store;
static char *duplicate(const char *value){size_t n=strlen(value)+1;char *out=(char*)malloc(n);if(out)memcpy(out,value,n);return out;}
static int load_slot(void *raw,const char *key,char **out){memory_store *m=(memory_store*)raw;*out=NULL;for(int i=0;i<16;i++)if(m->keys[i]&&!strcmp(m->keys[i],key)){*out=duplicate(m->values[i]);return *out?1:-1;}return 0;}
static int save_slot(void *raw,const char *key,const char *value){memory_store *m=(memory_store*)raw;int at=-1;for(int i=0;i<16;i++)if(m->keys[i]&&!strcmp(m->keys[i],key)){at=i;break;}if(at<0)for(int i=0;i<16;i++)if(!m->keys[i]){at=i;break;}if(at<0)return 0;if(m->values[at]){memset(m->values[at],0,strlen(m->values[at]));free(m->values[at]);}if(!m->keys[at])m->keys[at]=duplicate(key);m->values[at]=duplicate(value);return m->keys[at]&&m->values[at];}
static int remove_slot(void *raw,const char *key){memory_store *m=(memory_store*)raw;for(int i=0;i<16;i++)if(m->keys[i]&&!strcmp(m->keys[i],key)){free(m->keys[i]);memset(m->values[i],0,strlen(m->values[i]));free(m->values[i]);m->keys[i]=m->values[i]=NULL;}return 1;}
static cJSON *read_json(const char *path){FILE *f=fopen(path,"rb");if(!f)return NULL;fseek(f,0,SEEK_END);long n=ftell(f);rewind(f);if(n<0||n>1048576){fclose(f);return NULL;}char *raw=(char*)malloc((size_t)n+1);if(!raw){fclose(f);return NULL;}size_t got=fread(raw,1,(size_t)n,f);raw[got]=0;fclose(f);cJSON *value=cJSON_Parse(raw);memset(raw,0,got);free(raw);return value;}
static const char *json_text(cJSON *object,const char *key){cJSON *v=cJSON_GetObjectItemCaseSensitive(object,key);return cJSON_IsString(v)?v->valuestring:"";}
static int calls=0;
static int metered_calls=0;
static char meter_path[512];
static int meter_business(void *unused){(void)unused;metered_calls++;FILE *f=fopen(meter_path,"wb");if(!f)return 0;fputs(getenv("NFIX_RUN"),f);return fclose(f)==0;}

static char output_path[512];
static int business(void *unused){(void)unused;calls++;FILE *file=fopen(output_path,"wb");if(!file)return 0;fputs("Verified Launch SDK business\n",file);return fclose(file)==0;}
static void emit(const char *language,const char *product,int ok,const char *code,int blocked){cJSON *out=cJSON_CreateObject();cJSON_AddBoolToObject(out,"ok",ok);cJSON_AddStringToObject(out,"language",language);cJSON_AddStringToObject(out,"product",product);cJSON_AddNumberToObject(out,"callbacks",calls);cJSON_AddStringToObject(out,"startup_denial",code);cJSON_AddBoolToObject(out,"logout_rejects_restore",blocked);char *raw=cJSON_PrintUnformatted(out);puts(raw);free(raw);cJSON_Delete(out);}
#endif
