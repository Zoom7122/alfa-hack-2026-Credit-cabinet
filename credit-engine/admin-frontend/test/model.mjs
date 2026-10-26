import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {test} from 'node:test';
import {conditionText,criterionFields} from '../src/model.ts';
import {parseJSON,stringifyJSON} from '../src/legacy/numbers.js';

test('numeric scales render the actual non-overlapping ranges',async()=>{
 const model=parseJSON(await readFile(new URL('../../models/ip-v2.json',import.meta.url),'utf8'));
 const age=model.criteria.find(c=>c.id==='IP_Q01');
 assert.deepEqual(age.cases.map(c=>conditionText(c.when,model.parameters)),['от 0 до 19','от 20 до 25','от 26 до 30','от 31 до 35','от 36 до 49','от 50 до 65','от 66']);
 const dependents=model.criteria.find(c=>c.id==='IP_Q03');
 assert.deepEqual(dependents.cases.map(c=>conditionText(c.when,model.parameters)),['0','1','2','3','от 4']);
 const composite=model.criteria.find(c=>c.id==='IP_Q18');
 assert.ok(criterionFields(composite,model).length>1,'composite fields must share one criterion maximum');
});
test('JSON import/export preserves int64 and decimal tokens',()=>{
 const source='{"integer":9223372036854775807,"decimal":0.1234567890123456789,"flag":false,"missing":null}';
 assert.equal(stringifyJSON(parseJSON(source)),source);
 assert.throws(()=>parseJSON('{"a":1,"a":2}'));
});
