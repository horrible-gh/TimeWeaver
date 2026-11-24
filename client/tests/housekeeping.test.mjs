import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

async function loadSource(relative) {
  const source=await readFile(new URL(relative,import.meta.url),"utf8");
  return import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);
}
const {recursiveEnabled,housekeepOptions}=await loadSource("../src/dashboard/utils/housekeeping.js");
const {default:messages}=await loadSource("../src/dashboard/i18n/housekeeping.js");

test("old values default off and false-like strings never enable recursion",()=>{
  for (const value of [undefined,null,false,0,"0","false","",{},"no"]) assert.equal(recursiveEnabled(value),false);
  for (const value of [true,1,"1","true"," TRUE "]) assert.equal(recursiveEnabled(value),true);
});
test("only housekeep tasks send the recursive option",()=>{
  assert.equal(housekeepOptions({task_type:"housekeep",house_keep_recursive:true}).house_keep_recursive,true);
  for (const type of ["command","archive","copy",undefined]) assert.equal(housekeepOptions({task_type:type,house_keep_recursive:true}).house_keep_recursive,false);
  assert.equal(housekeepOptions({task_type:"housekeep"}).house_keep_recursive,false);
});
test("compact checkbox is conditional and new/edit/save paths normalize its state",async()=>{
  const source=await readFile(new URL("../src/dashboard/components/contents/tasks/TaskList.vue",import.meta.url),"utf8");
  assert.match(source,/v-if="formControl.task_type === 'housekeep'"/);
  assert.match(source,/type="checkbox" v-model="formControl.house_keep_recursive"/);
  assert.match(source,/for="task-recursive" class="housekeep-recursive"/);
  assert.match(source,/accent-color: var\(--cyan\)/);
  assert.match(source,/height: var\(--field-h\)/);
  assert.match(source,/task_type: "command",\s+house_keep_recursive: false/);
  assert.match(source,/housekeepOptions\(schedule\)/);
  assert.match(source,/housekeepOptions\(formControl.value\)/);
  assert.match(source,/Object\.assign\(formControl.value, resetTaskType\(type\)\)/);
});
test("all supported locales provide a short checkbox label",()=>{
  for(const locale of ["ko","en","ja"]) assert.equal(typeof messages[locale].task_house_keep_recursive,"string");
  assert.equal(messages.ko.task_house_keep_recursive,"하위 폴더 포함");
});

test("empty-directory deletion is opt-in and only applicable to housekeeping", async () => {
  assert.equal(housekeepOptions({task_type:"housekeep"}).house_keep_delete_empty_dirs,false);
  for (const value of [false,0,"0","false",null,undefined]) assert.equal(housekeepOptions({task_type:"housekeep",house_keep_delete_empty_dirs:value}).house_keep_delete_empty_dirs,false);
  assert.equal(housekeepOptions({task_type:"housekeep",house_keep_delete_empty_dirs:"1"}).house_keep_delete_empty_dirs,true);
  assert.equal(housekeepOptions({task_type:"copy",house_keep_delete_empty_dirs:true}).house_keep_delete_empty_dirs,false);
  const source=await readFile(new URL("../src/dashboard/components/contents/tasks/TaskList.vue",import.meta.url),"utf8");
  assert.match(source,/for="task-delete-empty" class="housekeep-recursive"/);
  assert.match(source,/type="checkbox" v-model="formControl.house_keep_delete_empty_dirs"/);
  for (const locale of ["ko","en","ja"]) assert.equal(typeof messages[locale].task_house_keep_delete_empty_dirs,"string");
});
