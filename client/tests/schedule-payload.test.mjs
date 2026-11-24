import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { schedulePayload, taskPayload, taskTypePayload, resetTaskType, formBool, cronDefaults, scheduleError } from '../src/dashboard/utils/schedulePayload.mjs';

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

test('task writes contain only the selected type fields and retain relevant date formats',()=>{
 const form={command:'echo {date}',archive_type:'zip',source_path:'/src/{date}',destination_path:'/dst/{date}',error_on_missing_source:'1',house_keep_days:'7',house_keep_recursive:true,date_format:'%Y%m%d',target_date_format:'%Y',destination_date_format:'%m'};
 for (const type of ['command','copy','archive','housekeep']) {
  const p=taskTypePayload({...form,task_type:type});
  const file=type==='copy'||type==='archive', hk=type==='housekeep';
  assert.equal(p.command,type==='command'?form.command:null);
  assert.equal(p.archive_type,type==='archive'?'zip':null);
  assert.equal(p.source_path,file?form.source_path:null);
  assert.equal(p.destination_path,file||hk?form.destination_path:null);
  assert.equal(p.error_on_missing_source,file);
  assert.equal(p.house_keep_days,hk?7:null);
  assert.equal(p.house_keep_recursive,hk);
  assert.equal(p.date_format,form.date_format);
  assert.equal(p.target_date_format,file?form.target_date_format:null);
  assert.equal(p.destination_date_format,file||hk?form.destination_date_format:null);
 }
 assert.equal(taskTypePayload({...form,task_type:'copy',error_on_missing_source:'0'}).error_on_missing_source,false);
});
test('switching task type resets execution-specific fields without a destructive default',()=>{
 const common={schedule_id:3,task_name:'task',sequence:20,retry_count:2,status:'inactive',detail_id:'keep'};
 const old={...common,task_type:'copy',source_path:'/src',destination_path:'/dst',date_format:'%Y',house_keep_days:7,house_keep_recursive:true};
 const form={...old,...resetTaskType('housekeep')};
 for(const [k,v] of Object.entries(common))assert.equal(form[k],v);
 const p=taskPayload(form,true);
 assert.equal(p.destination_path,null);assert.equal(p.house_keep_days,null);assert.equal(p.house_keep_recursive,false);
 assert.equal(p.source_path,null);assert.equal(p.date_format,null);
 assert.equal(taskTypePayload(resetTaskType('archive')).archive_type,'zip');
});
