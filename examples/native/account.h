#ifndef LAUNCH_ACCOUNT_H
#define LAUNCH_ACCOUNT_H
#include "licentivo.h"
#ifdef __cplusplus
extern "C" {
#endif
typedef struct launch_account {char *origin;char *token;char *key;char *public_config_path;char *cache_path;} launch_account;
int launch_sign_in(launch_account *account,int argc,char **argv);
void launch_sign_out(launch_account *account,int clear_cache);
#ifdef __cplusplus
}
#endif
#endif
