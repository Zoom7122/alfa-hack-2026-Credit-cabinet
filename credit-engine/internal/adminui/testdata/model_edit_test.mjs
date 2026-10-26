import assert from "node:assert/strict";
import {maxima, dependencies, replaceItem, classIssues, conditionReferences, rulePhase} from "../../../admin-frontend/src/legacy/model-edit.js";
import {parseJSON, stringifyJSON, ExactNumber} from "../../../admin-frontend/src/legacy/numbers.js";
let passed = 0;
function test(name, action) { action(); passed++; }
const d = parseJSON('{"parameters":[{"id":"flag","enabled":true,"source":{"kind":"SUPPLIER","path":"flag"}},{"id":"derived","enabled":true,"source":{"kind":"COMPUTED","path":"derived"},"depends_on":["flag"]}],"criteria":[{"id":"q","enabled":true,"block":"QUESTIONNAIRE","max_points":750,"answer":{"template":"${flag}"}},{"id":"e","enabled":true,"block":"ADDITIONAL","max_points":250,"value_parameter":"flag"}],"data_checks":[],"stops":[{"id":"stop","enabled":true,"when":{"op":"EQ","parameter":"flag","value":{"boolean":false}}}],"manual_reviews":[]}');
test("exact maxima and block deficits",()=>{ assert.equal(maxima(d).valid,true); const changed=replaceItem(d,"criteria",0,{...d.criteria[0],max_points:new ExactNumber("751")}); assert.equal(maxima(changed).valid,false); assert.equal(maxima(changed).total,1001n); assert.equal(maxima(d).total,1000n); });
test("all dependencies including templates",()=>assert.deepEqual(dependencies(d,"flag").map(r=>r.id),["derived","q","e","stop"]));
test("no value-string false positive",()=>assert.equal(dependencies(d,"false").length,0));
test("duplicate ID refused",()=>assert.throws(()=>replaceItem(d,"criteria",-1,d.criteria[0])));
test("deep copy and exact tokens",()=>{ const changed=replaceItem(d,"criteria",0,{...d.criteria[0],threshold:new ExactNumber("9007199254740993")}); changed.parameters[0].enabled=false; assert.equal(d.parameters[0].enabled,true); assert.match(stringifyJSON(changed),/9007199254740993/); });
const bands=parseJSON('[{"grade":"D","min_total":0,"max_total":549},{"grade":"C","min_total":550,"max_total":649},{"grade":"B","min_total":650,"max_total":799},{"grade":"A","min_total":800,"max_total":1000}]');
test("class complete scale",()=>assert.deepEqual(classIssues(bands),[]));
test("class gaps and overlaps",()=>{ for(const min of ["549","551"])assert.ok(classIssues(bands.map(c=>c.grade==="C"?{...c,min_total:new ExactNumber(min)}:c)).length); });
test("duplicate grade",()=>assert.ok(classIssues([...bands.slice(0,3),bands[0]]).length));
test("nested condition references",()=>assert.deepEqual(conditionReferences({op:"ALL",conditions:[{op:"EQ",parameter:"flag",other_parameter:"derived"},{op:"KNOWN",parameter:"flag"}]}),["flag","derived"]));
test("phase dependencies",()=>assert.match(rulePhase(d,{when:{op:"KNOWN",parameter:"derived"},requires_checks:["complete"],after_criterion:"e"},true),/После скоринга.*ответ поставщика.*complete.*e/));
console.log(JSON.stringify({passed}));
export const result={passed};
