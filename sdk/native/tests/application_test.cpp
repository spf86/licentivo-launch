// Keep regression assertions active in optimized Release test builds.
#undef NDEBUG
#include "launch.hpp"
#include <cassert>
#include <string>
int main(int argc,char **argv){
 assert(argc==3);licentivo_launch::Application app(argv[1],"11111111-1111-4111-8111-111111111111",argv[2],"sandbox");
 app.Login("account@example.test","secret");int calls=0;
 try{app.RunFeature("export",[&]{calls++;throw std::runtime_error("callback failed");});assert(false);}catch(const std::runtime_error& error){assert(std::string(error.what())=="callback failed");}
 assert(calls==1);try{app.RunFeature("denied",[&]{calls++;});assert(false);}catch(const std::runtime_error&){}assert(calls==1);
 auto moved=std::move(app);moved.RunFeature("export",[&]{calls++;});assert(calls==2);moved.Logout();return 0;
}
