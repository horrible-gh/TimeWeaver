import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { schedulePayload, taskPayload, formBool, cronDefaults, scheduleError } from '../src/dashboard/utils/schedulePayload.mjs';

test('schedule writes strip display fields and actor claims and use typed IDs/booleans',()=>{
 const f={...cronDefaults(),name:'Test',group_id:'7',target_device:'8',schedule_id:12,status:'active',is_error_stop:'0',creator:'forged',modifier:'forged',device_name:'hidden',can_edit:true};
 const add=schedulePayload(f,false);assert.equal(add.group_id,7);assert.equal(add.target_device,8);assert.equal(add.is_error_stop,false);
 for(const key of ['creator','modifier','can_edit','device_name','schedule_id'])assert.ok(!(key in add));
 assert.equal(schedulePayload(f,true).schedule_id,12);
 for(const value of [null,undefined,'0',0,false,'false','',{}])assert.equal(formBool(value),false);
});
test('task updates preserve retry/order and normalize nullable fields without copying read metadata',()=>{
 const t=taskPayload({schedule_id:2,task_name:'one',task_type:'command',command:'echo ok',status:'active',detail_id:'id',sequence:'2',retry_count:'3',house_keep_days:'',house_keep_recursive:true,error_on_missing_source:'0',created_at:'ignored'},true);
 assert.equal(t.sequence,2);assert.equal(t.retry_count,3);assert.equal(t.house_keep_days,null);assert.equal(t.house_keep_recursive,false);assert.equal(t.error_on_missing_source,false);assert.equal(t.detail_id,'id');assert.equal(t.year,'*');assert.ok(!('created_at' in t));
});
test('all new schedule errors have translations and server errors are not echoed',async()=>{
 const source=await readFile(new URL('../src/dashboard/i18n/schedulesAccess.js',import.meta.url),'utf8');
 const {default:m}=await import('data:text/javascript;base64,'+Buffer.from(source).toString('base64'));
 assert.deepEqual(Object.keys(m.ko),Object.keys(m.en));assert.deepEqual(Object.keys(m.ja),Object.keys(m.en));
 assert.equal(scheduleError({response:{data:{detail:{code:'sql secret'}}}}),'access_failed');
 assert.equal(scheduleError({response:{data:{detail:{code:'task_in_use'}}}}),'schedule_error_task_in_use');
});
