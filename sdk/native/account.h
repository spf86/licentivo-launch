#ifndef LAUNCH_ACCOUNT_H
#define LAUNCH_ACCOUNT_H
#include "licentivo.h"
#ifdef __cplusplus
extern "C" {
#endif
typedef struct launch_account {char *origin;char *token;char *key;char *public_config_path;char *cache_path;int http_status;int retry_after_seconds;char error_code[80];} launch_account;
int launch_login(launch_account *account,const char *origin,const char *product_id,const char *config_path,const char *environment,const char *email,const char *password);
int launch_sign_in(launch_account *account,int argc,char **argv);
void launch_sign_out(launch_account *account,int clear_cache);
/* A store must protect secrets with an OS vault. load returns malloc-owned text,
   1=found, 0=missing, -1=error. Support multiple opaque scope keys with atomic
   replacement, isolated to the current OS user/host; login and account logout
   tasks use distinct keys. No default plaintext persistence is provided. */
typedef struct launch_secret_store {
 void *context;
 int (*load)(void *context,const char *scope,char **record);
 int (*save)(void *context,const char *scope,const char *record);
 int (*remove)(void *context,const char *scope);
} launch_secret_store;
typedef struct launch_application launch_application;
/* Direct initialization. Merchant origin is mandatory; the licensing origin is
   managed inside Launch. trusted_keys_json is an in-memory JSON object. */
launch_application *launch_application_connect(const char *origin,const char *product_id,const char *licentivo_product_id,const char *trusted_keys_json,const char *environment,int remember,const launch_secret_store *store);
launch_application *launch_application_open(const char *origin,const char *product_id,const char *product_config_path,const char *environment,int remember,const launch_secret_store *store);
launch_application *launch_application_from_files(const char *launch_config_path,const char *product_config_path,int remember,const launch_secret_store *store);
/* Restore: 1=ready, 0=no remembered account, -1=denied/error. */
int launch_application_restore(launch_application *app);
int launch_application_login(launch_application *app,const char *email,const char *password);
int launch_application_run_feature(launch_application *app,const char *feature,ln_feature_action action,void *context);
int launch_application_release_device(launch_application *app);
int launch_application_logout(launch_application *app);
void launch_application_close(launch_application *app);
const char *launch_application_error(const launch_application *app);
/* Free returned status with ln_free_string. */
char *launch_application_status(launch_application *app);
/* CLI helper accepts --config launch-config.json product.json or legacy args. */
launch_application *launch_application_cli(int argc,char **argv);
#ifdef __cplusplus
}
#endif
#endif
