#include "account.h"
#include <stdio.h>
#include <string.h>
static int export_report(void *unused){(void)unused;FILE *file=fopen("licensed-report.txt","w");if(!file)return 0;int ok=fputs("Protected report\n",file)>=0;int closed=fclose(file)==0;return ok && closed;}
int main(int argc,char **argv){
 int clear=0,release=0;for(int i=1;i<argc;i++){if(!strcmp(argv[i],"--sign-out"))clear=1;if(!strcmp(argv[i],"--release-device"))release=1;}
 if(clear&&release){fputs("Choose one account action\n",stderr);return 1;}
 launch_application *app=launch_application_cli(argc,argv);if(!app)return 1;
 int ok=clear?launch_application_logout(app):release?launch_application_release_device(app):launch_application_run_feature(app,"export",export_report,NULL);
 if(ok)puts(clear?"Signed out locally":release?"Device released and signed out":"Protected export completed");else fprintf(stderr,"Launch: %s\n",launch_application_error(app));
 launch_application_close(app);return ok?0:1;
}
