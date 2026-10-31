//! Joint acceptance driver for the embedded official runtime and its adapter.
use licentivo_launch::{Failure,OfficialRuntimeFactory,RuntimeFactory};
use std::{env,fs::{self,OpenOptions},io::Write,thread,time::Duration};
fn main(){
    let args:Vec<String>=env::args().collect();let path=&args[1];let phase=&args[2];let seconds:f64=args[3].parse().unwrap();
    let settings=serde_json::from_str(&fs::read_to_string(path).unwrap()).unwrap();
    let mut client=OfficialRuntimeFactory.open(&settings,false).unwrap();let mut calls=0;
    let mut action=||->Result<(),Failure>{
        calls+=1;fs::write(format!("{path}.started"),"started").unwrap();thread::sleep(Duration::from_secs_f64(seconds));
        let mut file=OpenOptions::new().create(true).append(true).open(format!("{path}.result")).unwrap();
        file.write_all(if phase=="cancel_pending"{b"failure:0\n"}else{b"success:3\n"}).unwrap();file.sync_all().unwrap();
        if phase=="crash_success"{std::process::exit(23);}
        if phase=="cancel_pending"{return Err(Failure::new("EXPECTED_BUSINESS_FAILURE"));}Ok(())
    };
    let failed=client.run_metered_feature("export",3,"renewal-job",&mut action).is_err();
    if phase.starts_with("recover_")||phase=="replay"{
        if phase!="replay"{for _ in 0..2{client.resolve_metered_feature("renewal-job",phase=="recover_success").unwrap();}}
        let failed=client.run_metered_feature("export",3,"renewal-job",&mut action).is_err();
        assert_eq!(failed,phase=="recover_failure");assert_eq!(calls,0,"business repeated during recovery");
    }else{assert_eq!(failed,phase=="commit_pending"||phase=="cancel_pending");assert_eq!(calls,1,"unexpected business outcome");}
    client.close();println!("Launch Rust: phase={phase} calls={calls} passed");
}
