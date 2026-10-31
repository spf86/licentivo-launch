import test from 'node:test';
import assert from 'node:assert/strict';
import {orderStatusBadge,reconcileSelection} from '../internal/httpapi/web/order-management.js';

test('order badges use distinct semantic colors and readable escaped text',()=>{
 for(const [status,color] of Object.entries({paid:'green',pending:'amber',refunding:'amber',refunded:'blue',reversed:'red',closed:'gray'}))assert.match(orderStatusBadge(status),new RegExp('order-status-'+color));
 assert.ok(!orderStatusBadge('unrecognized','<img src=x>').includes('<img'));
 assert.match(orderStatusBadge('unrecognized'),/order-status-gray/);
});

test('selected reconciliation deduplicates, reports completed batches, and retains per-order failures',async()=>{
 const calls=[],progress=[],ids=Array.from({length:12},(_,i)=>'order-'+i);
 const result=await reconcileSelection(async(path,method,input)=>{
  calls.push(input);assert.equal(path,'/admin/orders/reconcile');assert.equal(method,'POST');
  return input.order_ids.map(order_id=>({order_id,checked:order_id!=='order-2',message:'test'}));
 },[...ids,ids[0]],'用户要求核对',items=>progress.push(items.length));
 assert.deepEqual(calls.map(call=>call.order_ids.length),[5,5,2]);
 assert.deepEqual(progress,[5,10,12]);assert.equal(result.filter(item=>item.checked).length,11);
 assert.ok(calls.every(call=>call.reason==='用户要求核对'));
});

test('lost response stops later batches without retrying or claiming success',async()=>{
 let calls=0;
 const results=await reconcileSelection(async()=>{calls++;throw new Error('连接中断，结果未确认')},Array.from({length:12},(_,i)=>String(i)),'核对原因');
 assert.equal(calls,1);assert.equal(results.length,5);
 assert.ok(results.every(item=>!item.checked&&item.code==='RESULT_UNKNOWN'));
});

test('selection must be bounded before any request',async()=>{
 let calls=0;const api=async()=>{calls++;return []};
 await assert.rejects(reconcileSelection(api,[],'reason'));
 await assert.rejects(reconcileSelection(api,Array.from({length:26},(_,i)=>String(i)),'reason'));
 assert.equal(calls,0);
});
