const escape=value=>String(value??"").replace(/[&<>"']/g,c=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));

export function deviceTitle(device,unnamed){
 return device.device_alias||device.device_name||unnamed;
}

export function deviceDetails(device,t){
 const rows=[];
 const add=(key,label,value)=>{if(value)rows.push(`<p><strong>${escape(t("devices."+key,label))}</strong> ${escape(value)}</p>`)};
 add("name","设备名称",device.device_alias?device.device_name:"");
 add("code","设备识别码",device.device_code);
 add("system","系统",[device.os_version||device.platform,device.system_type].filter(Boolean).join(" · "));
 add("cpu","处理器",device.cpu);
 add("memory","内存",Number.isFinite(device.memory_mb)&&device.memory_mb>0?`${Number((device.memory_mb/1024).toFixed(1))} GB`:"");
 add("gpu","显卡",device.gpu);
 add("storage","存储",Number.isFinite(device.storage_gb)&&device.storage_gb>0?`${device.storage_gb} GB`:"");
 add("app_version","软件版本",device.app_version);
 return rows.length?`<div class="device-details">${rows.join("")}</div>`:"";
}
