use licentivo_launch::{Application,Config,Failure};
use serde_json::{json,Value};
use std::{fs,path::PathBuf};
fn run(name:&str)->Result<Value,Failure>{
 let root=PathBuf::from("/home/spf86/n-fixes-20261101");
 let report:Value=serde_json::from_slice(&fs::read(root.join(format!("{name}-public.json")))?).map_err(|_|Failure::new("FIXTURE_JSON"))?;
 let secrets:Value=serde_json::from_slice(&fs::read(root.join("deployment-private.json"))?).map_err(|_|Failure::new("FIXTURE_JSON"))?;
 let c=&report["config"];let mut product=report["product_config"].clone();product["allow_http"]=json!(true);
 let cfg=Config::new_with_http(c["merchant_origin"].as_str().unwrap(),c["launch_product_id"].as_str().unwrap(),c["licentivo_product_id"].as_str().unwrap(),"sandbox",true)?;
 let mut app=Application::new(cfg,product,root.join(format!("rust-{name}")))?;
 let password=secrets["customer_password"].as_str().unwrap();let mut calls=0;
 app.login("alice@launch-joint.example",password)?;
 if name=="fixed"{let op=format!("nfix-rust-{}",std::env::var("NFIX_RUN").unwrap());let mut count=0;for _ in 0..2{let _=app.run_metered_feature("export",1,&op,||{count+=1;fs::write(root.join("metered-rust.txt"),&op)?;Ok(())})?;}app.resolve_metered_feature(&op,true)?;if count!=1{return Err(Failure::new("METERING_REPEATED"));}}

 app.run_feature("export",||{calls+=1;fs::write(root.join(format!("rust-{name}-business.txt")),"Verified Launch SDK business\n")?;Ok(())})?;
 let denied=app.run_feature("not_entitled",||{calls+=1;Ok(())}).is_err();if !denied||calls!=1{return Err(Failure::new("FEATURE_GATE_FAILED"));}
 app.close();app.restore()?;app.run_feature("export",||{calls+=1;Ok(())})?;app.logout()?;
 let blocked=matches!(app.restore(),Err(e) if e.code=="NO_SESSION");
 let code=match app.login("bob@launch-joint.example",password){Ok(_)=>String::new(),Err(e)=>e.code};app.close();
 Ok(json!({"ok":calls==2&&blocked&&code=="LAUNCH_ENTITLEMENT_REQUIRED","language":"Rust","product":name,"callbacks":calls,"startup_denial":code,"logout_rejects_restore":blocked}))
}
fn main(){let name=std::env::args().nth(1).unwrap();match run(&name){Ok(value)=>println!("{value}"),Err(error)=>{println!("{}",json!({"ok":false,"language":"Rust","product":name,"code":error.code}));std::process::exit(1)}}}
