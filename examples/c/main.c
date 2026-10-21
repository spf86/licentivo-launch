#include "account.h"
#include <stdio.h>
#include <string.h>
static int export_report(void *unused){(void)unused;FILE *file=fopen("licensed-report.txt","w");if(!file)return 0;int ok=fputs("Protected report\n",file)>=0;int closed=fclose(file)==0;return ok && closed;}
int main(int argc,char **argv){
 launch_account account={0};int clear_cache=0,release=0;for(int i=5;i<argc;i++){if(!strcmp(argv[i],"--sign-out"))clear_cache=1;if(!strcmp(argv[i],"--release-device"))release=1;}
 if(!launch_sign_in(&account,argc,argv)){fputs("Sign in or authorization failed. View the merchant account center.\n",stderr);launch_sign_out(&account,clear_cache);return 1;}
 ln_client *sdk=ln_init_file_with_license(account.public_config_path,account.key);int ok=sdk && ln_start(sdk) && ln_run_feature(sdk,"export",export_report,NULL);
 if(ok)puts("Protected export completed");else fputs("SDK authorization or protected export failed\n",stderr);
 if(release && sdk && !ln_deactivate(sdk))ok=0;if(sdk)ln_destroy(sdk);launch_sign_out(&account,clear_cache);return ok?0:1;
}
