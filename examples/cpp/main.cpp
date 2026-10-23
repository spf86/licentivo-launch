#include "launch.hpp"
#include <fstream>
#include <iostream>
#include <cstring>
int main(int argc,char **argv){
 bool clear=false,release=false;for(int i=1;i<argc;i++){if(!std::strcmp(argv[i],"--sign-out"))clear=true;if(!std::strcmp(argv[i],"--release-device"))release=true;}
 try{
  if(clear&&release)throw std::runtime_error("Choose one account action");
  licentivo_launch::Application app(launch_application_cli(argc,argv));
  if(clear){app.Logout();std::cout<<"Signed out locally\n";return 0;}
  if(release){app.ReleaseDevice();std::cout<<"Device released and signed out\n";return 0;}
  app.RunFeature("export",[]{std::ofstream file("licensed-report.txt");file<<"Protected report\n";file.close();if(!file)throw std::runtime_error("Export write failed");});
  std::cout<<"Protected export completed\n";return 0;
 }catch(const std::exception& e){std::cerr<<e.what()<<"\nCheck your merchant account center.\n";return 1;}
}
