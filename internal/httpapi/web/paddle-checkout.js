"use strict";
const paymentStatus=document.getElementById("payment-status");
const config=document.body.dataset;
function openPayment(){
 if(!window.Paddle){paymentStatus.textContent="The payment form could not load. Check your connection and reload this page.";return}
 try{window.Paddle.Checkout.open({transactionId:config.transaction})}catch{paymentStatus.textContent="The payment form could not open. Please return to your order and try again."}
}
document.getElementById("open-checkout").addEventListener("click",openPayment);
if(window.Paddle){
 try{
  if(config.environment==="sandbox")window.Paddle.Environment.set("sandbox");
  window.Paddle.Initialize({token:config.token,checkout:{settings:{displayMode:"overlay",theme:"light",successUrl:config.return,showAddDiscounts:false}},eventCallback:event=>{
   if(event.name==="checkout.completed")location.assign(config.return);
   if(event.name==="checkout.closed")paymentStatus.textContent="The payment form is closed. You can reopen it or return to your order.";
   if(event.name==="checkout.error")paymentStatus.textContent="Paddle could not complete the payment. Please check the payment form or try another method.";
  }});
  // Paddle automatically opens the existing transaction from _ptxn.
  paymentStatus.textContent="Complete the payment in the secure form. If it does not open, use the button below.";
 }catch{paymentStatus.textContent="The payment service is unavailable. Please return to your order and contact the store."}
}else{paymentStatus.textContent="The payment form could not load. Check your connection and reload this page."}
