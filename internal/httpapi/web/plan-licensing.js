export function licensingContext(connections) {
 const licensing=connections?.licensing?.[0]||null;
 const products=licensing?.catalog?.products?.items||[];
 const policies=licensing?.catalog?.policies?.items||[];
 const selectablePolicies=policies.filter(policy=>products.some(product=>product.id===policy.product_id));
 const mappings=connections?.mappings||[];
 const productName=id=>products.find(product=>product.id===id)?.name||id;
 const policyLabel=policy=>`${productName(policy.product_id)} · ${policy.name} · ${policy.mode==="perpetual"?"永久":policy.duration_days?policy.duration_days+" 天":"限期"} · ${policy.max_devices||"—"} 台/席位 · v${policy.version}`;
 return {licensing,products,policies,selectablePolicies,mappings,productName,policyLabel};
}

export function planLicenseStatus(context,syncError="") {
 if(!context.licensing)return {kind:"disconnected",title:"尚未接入 Licentivo",message:"可以先填写套餐资料和价格，保存为待绑定草稿。接入 Licentivo 并选择授权策略后，才能销售和发放软件授权。"};
 if(syncError)return {kind:"sync-error",title:"Licentivo 策略同步失败",message:context.selectablePolicies.length?"当前显示上次成功同步的策略。可以重试同步；保存时会再次检查所选策略，也可以先保存草稿。":"暂时无法读取可用策略。请重试同步或检查授权连接；已填写的套餐资料仍可保存为草稿。"};
 if(!context.products.length)return {kind:"no-products",title:"已接入 Licentivo，尚无授权产品",message:"请先在 Licentivo 创建产品和授权策略，再回来刷新。现在可以继续填写套餐并保存草稿。"};
 if(!context.selectablePolicies.length)return {kind:"no-policies",title:"已接入 Licentivo，暂无可用策略",message:"请在 Licentivo 中为产品创建授权策略，并确认管理 API Key 有权读取该产品和策略，然后刷新。现在可以先保存草稿。"};
 return {kind:"ready",title:"已接入 Licentivo",message:`已读取 ${context.products.length} 个产品、${context.selectablePolicies.length} 个可用策略。选择策略后，价格类型和授权信息会自动匹配。`};
}

export function planPricingMode(policy,draftType="fixed_term") {
 return (policy?.mode||(draftType==="perpetual"?"perpetual":"fixed_term"))==="perpetual"?"perpetual":"fixed_term";
}

export async function savePlanWithLicensing(api,payload,policy) {
 const plan=await api("/admin/plans","POST",payload);
 if(!policy)return {plan,mapped:false,mappingError:""};
 try {
  await api("/admin/connections/mappings","POST",{version_id:plan.version_id,policy_id:policy.id});
  return {plan,mapped:true,mappingError:""};
 }catch(error){
  return {plan,mapped:false,mappingError:error.message||"授权策略绑定失败"};
 }
}
