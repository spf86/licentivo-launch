//go:build ignore

package main
import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "licentivo-launch/pkg/launchclient"
)
type report struct {Config struct{Merchant string `json:"merchant_origin"`;Launch string `json:"launch_product_id"`} `json:"config"`; Product launchclient.ProductConfig `json:"product_config"`}
func main(){if err:=run();err!=nil{fmt.Println(`{"ok":false,"code":"CLIENT_FAILED"}`);os.Exit(1)}}
func run()error{
 const base="/home/spf86/n-fixes-20261101"
 name:=os.Args[1];var r report
 raw,e:=os.ReadFile(filepath.Join(base,name+"-public.json"));if e!=nil{return e};if e=json.Unmarshal(raw,&r);e!=nil{return e}
 var secrets map[string]string;raw,e=os.ReadFile(filepath.Join(base,"deployment-private.json"));if e!=nil{return e};json.Unmarshal(raw,&secrets)
 r.Product.AllowHTTP=true
 cfg:=launchclient.Config{Origin:r.Config.Merchant,ProductID:r.Config.Launch,Environment:"sandbox",AllowHTTP:true,Product:r.Product,Store:&launchclient.MemoryStore{},CacheDir:filepath.Join(base,"go-"+name)}
 app,e:=launchclient.NewApplication(cfg);if e!=nil{return e};ctx:=context.Background();calls:=0
 if _,e=app.Login(ctx,"alice@launch-joint.example",secrets["customer_password"]);e!=nil{return e}
 if name=="fixed" {op:="nfix-go-"+os.Getenv("NFIX_RUN");meterCalls:=0;job:=func()error{meterCalls++;return os.WriteFile(filepath.Join(base,"metered-go.txt"),[]byte(op),0600)};for i:=0;i<2;i++{if e=app.RunMeteredFeature(ctx,"export",1,op,job);e!=nil{return e}};if e=app.ResolveMeteredFeature(ctx,op,true);e!=nil{return e};if meterCalls!=1{return errors.New("METERING_REPEATED")}}

 action:=func()error{calls++;return os.WriteFile(filepath.Join(base,"go-"+name+"-business.txt"),[]byte("Verified Launch SDK business\n"),0600)}
 if e=app.RunFeature("export",action);e!=nil{return e}
 denied:=app.RunFeature("not_entitled",action);if denied==nil||calls!=1{return errors.New("feature gate failed")};app.Close()
 app,e=launchclient.NewApplication(cfg);if e!=nil{return e}
 if _,e=app.Restore(ctx);e!=nil{return e}
 if e=app.RunFeature("export",action);e!=nil{return e}
 if e=app.Logout(ctx);e!=nil{return e};app.Close()
 app,e=launchclient.NewApplication(cfg);if e!=nil{return e}
 _,e=app.Restore(ctx);loggedOut:=errors.Is(e,launchclient.ErrNoSession);app.Close();if !loggedOut{return errors.New("logout restored access")}
 app,e=launchclient.NewApplication(cfg);if e!=nil{return e}
 _,e=app.Login(ctx,"bob@launch-joint.example",secrets["customer_password"])
 var denial *launchclient.RuntimeAPIError
 correct:=errors.As(e,&denial)&&denial.Code=="LAUNCH_ENTITLEMENT_REQUIRED"
 app.Close()
 return json.NewEncoder(os.Stdout).Encode(map[string]any{"ok":calls==2&&correct,"language":"Go","product":name,"callbacks":calls,"startup_denial_preserved":correct,"logout_rejects_restore":loggedOut})
}
