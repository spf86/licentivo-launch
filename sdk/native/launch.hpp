#ifndef LICENTIVO_LAUNCH_HPP
#define LICENTIVO_LAUNCH_HPP
#include "account.h"
#include <exception>
#include <stdexcept>
#include <string>
#include <utility>

namespace licentivo_launch {
class Application {
    launch_application *app_;
    void Require(int success) const { if (!success) throw std::runtime_error(launch_application_error(app_)); }
public:
    explicit Application(launch_application *app) : app_(app) { if (!app_) throw std::runtime_error("INVALID_CONFIG"); }
    Application(const char *origin,const char *product,const char *config,const char *environment="live",bool remember=false,const launch_secret_store *store=nullptr)
        : Application(launch_application_open(origin,product,config,environment,remember,store)) {}
    static Application FromFiles(const char *launch,const char *product,bool remember=false,const launch_secret_store *store=nullptr) { return Application(launch_application_from_files(launch,product,remember,store)); }
    static Application Connect(const char *origin,const char *product,const char *licentivo_product,const char *trusted_keys_json,const char *environment="live",bool remember=false,const launch_secret_store *store=nullptr) { return Application(launch_application_connect(origin,product,licentivo_product,trusted_keys_json,environment,remember,store)); }
    static Application ConnectHTTP(const char *origin,const char *product,const char *licentivo_product,const char *trusted_keys_json,const char *environment="sandbox",bool remember=false,const launch_secret_store *store=nullptr) { return Application(launch_application_connect_http(origin,product,licentivo_product,trusted_keys_json,environment,1,remember,store)); }
    ~Application() { launch_application_close(app_); }
    Application(const Application&) = delete;
    Application& operator=(const Application&) = delete;
    Application(Application&& other) noexcept : app_(std::exchange(other.app_,nullptr)) {}
    Application& operator=(Application&& other) noexcept { if(this!=&other){launch_application_close(app_);app_=std::exchange(other.app_,nullptr);}return *this; }
    bool Restore() { int result=launch_application_restore(app_);if(result<0)Require(0);return result==1; }
    void Login(const char *email,const char *password) { Require(launch_application_login(app_,email,password)); }
    template<class Action> void RunFeature(const char *feature,Action action) {
        struct Context { Action *action; std::exception_ptr failure; } context{&action,{}};
        int ok=launch_application_run_feature(app_,feature,[](void *raw)->int {auto *ctx=static_cast<Context*>(raw);try{(*ctx->action)();return 1;}catch(...){ctx->failure=std::current_exception();return 0;}},&context);
        if(context.failure)std::rethrow_exception(context.failure);Require(ok);
    }
    std::string Status() { char *value=launch_application_status(app_);std::string result=value?value:"";ln_free_string(value);return result; }
    void Logout() { Require(launch_application_logout(app_)); }
    void ReleaseDevice() { Require(launch_application_release_device(app_)); }
};
}
#endif
