//go:build ignore

package main
import("context";"encoding/json";"fmt";"os";"os/exec";"path/filepath";launch "licentivo-launch/pkg/launchclient")
func main(){if err:=run();err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}}
func run()error{
 const root="/home/spf86/launch-sdk-renewal-20261101"
 app,err:=launch.NewApplicationFromFiles(filepath.Join(root,"meter-launch.json"),filepath.Join(root,"meter-product.json"),launch.Config{CacheDir:filepath.Join(root,"runtime/go"),Store:&launch.MemoryStore{}});if err!=nil{return err};defer app.Close()
 raw,err:=os.ReadFile(filepath.Join(root,"deployment-private.json"));if err!=nil{return err};var secret map[string]string;if err=json.Unmarshal(raw,&secret);err!=nil{return err}
 ctx:=context.Background();if _,err=app.Login(ctx,os.Getenv("LAUNCH_EMAIL"),secret["customer_password"]);err!=nil{return err};calls:=0
 if err=app.RunMeteredFeature(ctx,"export",3,os.Getenv("LAUNCH_OPERATION"),func()error{calls++;cmd:=exec.Command("python3",filepath.Join(root,"long_business.py"));cmd.Stdout=os.Stdout;cmd.Stderr=os.Stderr;return cmd.Run()});err!=nil{return err}
 phase:=os.Getenv("LAUNCH_PHASE");expected:=1;if phase=="replay"{expected=0;if err=app.ResolveMeteredFeature(ctx,os.Getenv("LAUNCH_OPERATION"),true);err!=nil{return err}}
 if calls!=expected{return fmt.Errorf("unexpected callback count %d",calls)}
 return json.NewEncoder(os.Stdout).Encode(map[string]any{"ok":true,"language":"go","phase":phase,"callbacks":calls})
}
