import assert from "node:assert/strict";
import {readFile,writeFile} from "node:fs/promises";
import {parseJSON,stringifyJSON} from "../../../admin-frontend/src/legacy/numbers.js";
if(process.env.CREDIT_ENGINE_UI_TEST!=="1")throw Error("Isolated Docker instance required");
const scoring=process.env.CREDIT_ENGINE_SCORING_URL, bank=process.env.CREDIT_ENGINE_BANK_URL, admin="http://localhost:18084", output=process.env.CREDIT_ENGINE_UI_OUTPUT;
async function call(base,path,body,headers={}) {
  const response=await fetch(base+path,{method:body===undefined?"GET":"POST",headers:{"Content-Type":"application/json",...headers},body:body===undefined?undefined:stringifyJSON(body),signal:AbortSignal.timeout(30000)});
  const raw=await response.text(); assert.ok(response.ok,response.status+" "+raw.slice(0,1000));
  return {response,data:raw?parseJSON(raw):null};
}
async function ready(base) {
  for(let i=0;i<80;i++) {
    try {await call(base,"/healthz");return;} catch {}
    await new Promise(resolve=>setTimeout(resolve,250));
  }
  throw Error("Service not ready: "+base);
}
await Promise.all([ready(scoring),ready(bank),ready(admin)]);
const fixture=parseJSON(await readFile(new URL("../../../testdata/decisions/uc04_ip_approved.json",import.meta.url),"utf8"));
const input=fixture.request;
const rebuilt=process.env.CREDIT_ENGINE_REBUILT==="1";
const facts=process.env.CREDIT_ENGINE_FACTS_URL;
const beforeFacts=rebuilt&&facts?(await call(facts,"/__test/calls")).data.facts.length:null;
if(rebuilt) {
  const sample=parseJSON(await readFile(output+"/accepted-sample.json","utf8"));
  const definition=parseJSON(await readFile(output+"/accepted-model.json","utf8"));
  Object.assign(input,sample.application); input.model_version=definition.version;
}
input.application_id=(rebuilt?"rebuilt-":"runtime-")+Date.now();
const metadata=(await call(scoring,"/api/v1/models")).data;
assert.ok(metadata.models.some(m=>m.version===(input.model_version||"ip-v2")));
const before=(await call(bank,"/api/v1/bank/state")).data;
const first=(await call(scoring,"/api/v1/decisions",input)).data;
await writeFile(output+"/"+(rebuilt?"rebuilt-":"")+"decision.json",stringifyJSON(first,true));
assert.equal(first.status,"APPROVED"); assert.equal(first.terms_confirmed,true);
const after=(await call(bank,"/api/v1/bank/state")).data;
assert.equal(Number(String(before.state.Cash))-Number(String(after.state.Cash)),Number(String(first.terms.amount_rub)));
const second=(await call(scoring,"/api/v1/decisions",input)).data;
if(beforeFacts!==null){
  const requests=(await call(facts,"/__test/calls")).data.facts;
  assert.equal(requests.length,beforeFacts+2,"exactly one aggregate request per evaluation");
  assert.equal(requests.at(-1),requests.at(-2),"identical repeat wire body");
}
assert.equal(stringifyJSON(first),stringifyJSON(second));
assert.equal(stringifyJSON(after),stringifyJSON((await call(bank,"/api/v1/bank/state")).data));
const login=await call(admin,"/api/admin/v1/auth/login",{username:"editor",password:"fictional-admin-password"},{Origin:admin});
const cookie=login.response.headers.getSetCookie().map(value=>value.split(";")[0]).join("; ");
const rows=(await call(admin,"/api/admin/v1/events?application_id="+encodeURIComponent(input.application_id),undefined,{Cookie:cookie})).data;
assert.equal(rows.length,1); assert.equal(rows[0].kind,"LOAN_ISSUED");
const event=(await call(admin,"/api/admin/v1/events/"+encodeURIComponent(rows[0].event_id),undefined,{Cookie:cookie})).data;
assert.equal(event.payload.loan_id,first.loan_id); assert.equal(event.payload.score.model,input.model_version||"ip-v2");
if(rebuilt) {
  assert.equal(event.payload.score.economics.criteria.find(c=>c.id==="ACCEPT_E").value.toString(),"9");
  assert.equal(event.payload.score.questionnaire.criteria.find(c=>c.id==="ACCEPT_Q").answer,"false");
  const sample=parseJSON(await readFile(output+"/accepted-sample.json","utf8")); sample.application=input;
  const preview=(await call(admin,"/api/admin/v1/simulations",{model:{kind:"PUBLISHED",id:input.model_version,revision:null},sample},{Cookie:cookie,Origin:admin,"X-CSRF-Token":login.data.csrf_token})).data;
  assert.equal(stringifyJSON(first.score),stringifyJSON(preview.score));
}
const name=rebuilt?"rebuilt-runtime-smoke":"runtime-smoke";
await writeFile(output+"/"+name+".json",JSON.stringify({passed:true,checks:["actual compiled catalog","MS1 -> MS2 -> MS3","one issuance on identical repeat","persisted event after ACK",...(rebuilt?["new named facts in real MS1","compiled score equals administrative preview"]:[])],application_id:input.application_id},null,2));
console.log(name+": passed");
