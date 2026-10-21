#include "account.h"
#include "licentivo.hpp"
#include <fstream>
#include <iostream>
#include <cstring>
int main(int argc,char **argv){
 launch_account account{};bool clearCache=false,release=false;for(int i=5;i<argc;i++){if(!std::strcmp(argv[i],"--sign-out"))clearCache=true;if(!std::strcmp(argv[i],"--release-device"))release=true;}
 int exitCode=1;
 try{if(!launch_sign_in(&account,argc,argv))throw std::runtime_error("Sign in failed; view merchant account center");
  {licentivo::Client sdk(account.public_config_path,account.key);sdk.Start();sdk.RunFeature("export",[]{std::ofstream file("licensed-report.txt");file<<"Protected report\n";file.close();if(!file)throw std::runtime_error("Export write failed");});if(release)sdk.Deactivate();}
  std::cout<<"Protected export completed\n";exitCode=0;
 }catch(const std::exception&){std::cerr<<"Sign in, SDK authorization or protected export failed\n";}
 launch_sign_out(&account,clearCache);return exitCode;
}
