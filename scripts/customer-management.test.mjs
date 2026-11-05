import test from 'node:test';
import assert from 'node:assert/strict';
import {customerPlanState,customerExpiry,renderCustomerProduct,renderCustomerOrder} from '../internal/httpapi/web/customer-management.js';

const now=Date.parse('2026-11-02T00:00:00Z');
test('plan state separates account state, future activation, expiry boundary and seven-day warning',()=>{
 const p={status:'active',dates_known:true,valid_until:'2026-11-09T00:00:00Z'};
 assert.deepEqual(customerPlanState(p,now),['amber','即将到期']);
 assert.deepEqual(customerPlanState({...p,valid_until:'2026-11-02T00:00:00Z'},now),['red','已到期']);
 assert.deepEqual(customerPlanState({...p,valid_until:'2026-12-01T00:00:00Z'},now),['green','有效']);
 assert.deepEqual(customerPlanState({...p,status:'scheduled'},now),['blue','待生效']);
 assert.deepEqual(customerPlanState({...p,status:'revoked'},now),['gray','已撤销']);
 assert.deepEqual(customerPlanState({...p,status:'suspended'},now),['amber','已暂停']);
});
test('missing dates are never interpreted as a permanent purchase',()=>{
 const time=v=>v||'—';
 assert.equal(customerExpiry({dates_known:false,valid_until:null,permanent:false},time),'待确认');
 assert.equal(customerExpiry({dates_known:true,valid_until:null,permanent:true},time),'永久');
 assert.deepEqual(customerPlanState({dates_known:false,status:'active'},now),['gray','期限待确认']);
});
test('real product and plan text is escaped while renewal and synchronization stay visible',()=>{
 const html=renderCustomerProduct({product_name:'<img src=x onerror=alert(1)>',plan_name:'<script>x</script>',status:'active',dates_known:true,valid_until:'2026-12-01',purchased_until:'2027-01-01',paid_at:'2026-11-01',valid_from:'2026-11-01',synchronized:false},v=>v||'—');
 assert.ok(!html.includes('<img src=x'));assert.ok(!html.includes('<script>'));
 assert.match(html,/&lt;img/);assert.match(html,/已购买至 2027-01-01/);assert.match(html,/授权未同步/);
});

test('purchase history uses the shared object-based currency formatter',()=>{
 const order={id:'order',product_name:'Editor',plan_name:'Pro',plan_version:1,action:'purchase',amount_minor:1000,currency:'USD',status:'paid',coverage_recorded:true,valid_from:'2026-11-01',valid_until:'2026-12-01'};
 const html=renderCustomerOrder(order,{time:v=>v||'—',money:value=>{assert.equal(value,order);return new Intl.NumberFormat('en-US',{style:'currency',currency:value.currency}).format(value.amount_minor/100)}});
 assert.match(html,/\$10\.00/);assert.match(html,/2026-12-01/);
});
