import test from 'node:test';
import assert from 'node:assert/strict';
import {deviceTitle,deviceDetails} from '../internal/httpapi/web/device-details.js';

const t=(key,fallback)=>fallback;

test('device cards identify the computer using its alias, code and reported hardware',()=>{
 const device={device_alias:'Work Mac',device_name:'MacBook Air',device_code:'ABCD-1234',platform:'darwin',os_version:'macOS 15.6',cpu:'Apple M2',memory_mb:16384,gpu:'Apple M2 GPU',system_type:'arm64',storage_gb:512,app_version:'1.2.3'};
 assert.equal(deviceTitle(device,'Unnamed'),'Work Mac');
 const html=deviceDetails(device,t);
 for(const value of ['MacBook Air','ABCD-1234','macOS 15.6 · arm64','Apple M2','16 GB','Apple M2 GPU','512 GB','1.2.3'])assert.ok(html.includes(value),value);
 assert.ok(!html.includes('undefined'));
});

test('older SDK devices remain readable without invented details or empty rows',()=>{
 assert.equal(deviceTitle({device_name:'Existing PC'},'Unnamed'),'Existing PC');
 assert.equal(deviceTitle({},'Unnamed'),'Unnamed');
 assert.equal(deviceDetails({device_name:'Existing PC',memory_mb:0,storage_gb:0},t),'');
 assert.ok(!deviceDetails({memory_mb:-1,storage_gb:NaN},t).includes('GB'));
});

test('device metadata and localized labels are escaped',()=>{
 const html=deviceDetails({device_alias:'Alias',device_name:'<script>bad()</script>',cpu:'<img src=x onerror=bad()>',device_code:'"<&'},()=>'<label>');
 assert.ok(html.includes('&lt;script&gt;'));
 assert.ok(html.includes('&lt;img'));
 assert.ok(html.includes('&lt;label&gt;'));
 assert.ok(!html.includes('<script>')&&!html.includes('<img')&&!html.includes('<label>'));
});
