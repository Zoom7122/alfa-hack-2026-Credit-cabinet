import assert from "node:assert/strict";
import {readFile,writeFile,readdir,appendFile,mkdir} from "node:fs/promises";
import {resolve} from "node:path";
import {parseJSON,stringifyJSON,ExactNumber,integer} from "../admin-frontend/src/legacy/numbers.js";
if(process.env.CREDIT_ENGINE_UI_TEST!=="1")throw Error("Isolated acceptance required");
const root=resolve("."), output=process.env.CREDIT_ENGINE_UI_OUTPUT, runID=process.env.CREDIT_ENGINE_RUN_ID;
const bank=process.env.CREDIT_ENGINE_BANK_URL, scoring=process.env.CREDIT_ENGINE_SCORING_URL, admin=process.env.CREDIT_ENGINE_ADMIN_URL, provider=process.env.CREDIT_ENGINE_FACTS_URL;
const read=async path=>parseJSON(await readFile(path,"utf8"));
const manifest=await read("testdata/scenarios.json");
const banks=(await readdir("testdata/bank")).filter(p=>p.endsWith(".json"));
async function request(base,path,body,headers={}){
  const response=await fetch(base+path,{method:body===undefined?"GET":"POST",headers:{"Content-Type":"application/json",...headers},body:body===undefined?undefined:stringifyJSON(body),signal:AbortSignal.timeout(30000)});
  const raw=await response.text();assert.ok(response.ok,response.status+" "+raw.slice(0,1000));return raw?parseJSON(raw):null;
}
async function ready(base){for(let i=0;i<80;i++){try{await request(base,"/healthz");return;}catch{}await new Promise(r=>setTimeout(r,250));}throw Error("Service not ready: "+base);}
function equal(actual,expected,path="",fixture=false){
  // Match the established fixture contract in app/pipeline_test.go. Explanatory
  // prose is not normative; status messages, reason codes and values are.
  if(fixture&&(path.endsWith("/detail")||/\/reasons\/\d+\/message$/.test(path))){
    assert.ok(typeof actual==="string"&&actual.length>0,"nonempty explanation "+path);return;
  }
  if(expected instanceof ExactNumber){
    assert.ok(actual instanceof ExactNumber,"number type "+path);
    if(/^-?\d+$/.test(String(actual))&&/^-?\d+$/.test(String(expected)))assert.equal(integer(actual),integer(expected),path);
    else{const tolerance=/rub/i.test(path)?0.01:1e-9;assert.ok(Math.abs(Number(String(actual))-Number(String(expected)))<=tolerance,"number mismatch "+path+": "+actual+" != "+expected);}
    return;
  }
  if(expected===null||typeof expected!=="object"){assert.equal(actual,expected,path);return;}
  assert.ok(actual&&typeof actual==="object",path);
  if(Array.isArray(expected)){
    assert.ok(Array.isArray(actual),path);
    if(fixture&&/\/reasons\/\d+\/fields$/.test(path)&&expected.length===0)return;
    assert.equal(actual.length,expected.length,path);
    if(fixture&&path.endsWith("/fields")){actual=[...actual].sort();expected=[...expected].sort();}
    expected.forEach((item,i)=>equal(actual[i],item,path+"/"+i,fixture));
  }
  else{assert.deepEqual(Object.keys(actual).sort(),Object.keys(expected).sort(),"keys "+path);for(const key of Object.keys(expected))equal(actual[key],expected[key],path+"/"+key,fixture);}
}
function identity(fixture){
  const visit=(value,key)=>{
    if(Array.isArray(value))return value.map(item=>visit(item,key));
    if(value&&typeof value==="object"&&!(value instanceof ExactNumber))return Object.fromEntries(Object.entries(value).map(([k,v])=>[k,visit(v,k)]));
    if(typeof value!=="string")return value;
    if(key==="application_id"||key==="issued_application_ids")return value+"-"+runID;
    if(key==="loan_id"&&value.startsWith("loan-"))return value+"-"+runID;
    if(key==="event_id"){const colon=value.lastIndexOf(":");return value.slice(0,colon)+"-"+runID+value.slice(colon);}
    return value;
  };
  return visit(fixture,"");
}
let cookie="";
async function adminCookie(){
  if(cookie)return cookie;
  try{cookie=(await read(output+"/session.json")).cookie;return cookie;}catch{}
  const response=await fetch(admin+"/api/admin/v1/auth/login",{method:"POST",headers:{Origin:"http://localhost:18084","Content-Type":"application/json"},body:JSON.stringify({username:"editor",password:"fictional-admin-password"})});
  assert.equal(response.status,200);cookie=response.headers.getSetCookie().map(s=>s.split(";")[0]).join("; ");
  await writeFile(output+"/session.json",JSON.stringify({cookie}));return cookie;
}
async function eventFor(id){return request(admin,"/api/admin/v1/events?application_id="+encodeURIComponent(id),undefined,{Cookie:await adminCookie()});}
async function checkEvent(expected,id){
  let rows=await eventFor(id);
  if(expected){const deadline=Date.now()+20000;while(rows.length===0&&Date.now()<deadline){await new Promise(resolve=>setTimeout(resolve,100));rows=await eventFor(id);}}
  assert.equal(rows.length,expected?1:0,"event count "+id);
  if(!expected)return;
  const stored=await request(admin,"/api/admin/v1/events/"+encodeURIComponent(rows[0].event_id),undefined,{Cookie:await adminCookie()});
  const actual=stored.payload;
  assert.ok(/^\d{4}-\d\d-\d\dT.*Z$/.test(actual.occurred_at)&&Number.isFinite(Date.parse(actual.occurred_at)),"UTC event timestamp");
  equal({...actual,occurred_at:expected.occurred_at},expected,"event",true);
}
const [mode,id]=process.argv.slice(2);
if(mode==="list"){console.log([...manifest.scenarios.map(s=>"decision:"+s.id),...banks.map(s=>"bank:"+s)].join("\n"));}
else if(mode==="prepare"){
  await mkdir(output,{recursive:true});
  let state;
  if(id==="default")state=await read("config/bank.default.json");
  else if(id.startsWith("bank:"))state=(await read("testdata/bank/"+id.slice(5))).initial_bank;
  else{const scenario=manifest.scenarios.find(s=>"decision:"+s.id===id);assert.ok(scenario,"unknown scenario");state=await read(resolve("testdata",scenario.initial_bank));}
  await writeFile(output+"/bank-state.json",stringifyJSON(state,true));
}
else if(mode==="run"){
  await Promise.all([ready(bank),ready(scoring),ready(admin),ready(provider)]);
  let operations=0;
  if(id.startsWith("bank:")){
    const f=identity(await read("testdata/bank/"+id.slice(5)));
    let result=await request(bank,"/api/v1/bank/check-and-issue",f.request);
    const deadline=Date.now()+20000;
    while(result.notification_status==="PENDING"&&f.expected.response.notification_status==="DELIVERED"&&Date.now()<deadline){await new Promise(resolve=>setTimeout(resolve,100));result=await request(bank,"/api/v1/bank/check-and-issue",f.request);}
    equal(result,f.expected.response,"response",true);
    equal((await request(bank,"/api/v1/bank/state")).state,f.expected.bank_state_saved,"saved state",true);
    await checkEvent(f.expected.notification,f.request.application_id);
    equal(await request(bank,"/api/v1/bank/check-and-issue",f.request),result,"repeat");
    equal((await request(bank,"/api/v1/bank/state")).state,f.expected.bank_state_saved,"repeat state",true);await checkEvent(f.expected.notification,f.request.application_id);operations=2;
  }else{
    const scenario=manifest.scenarios.find(s=>"decision:"+s.id===id);assert.ok(scenario);
    for(const path of scenario.steps){
      const f=identity(await read(resolve("testdata",path)));
      await request(provider,"/__test/reset",{statements:f.service_b_response});
      equal((await request(bank,"/api/v1/bank/state")).state,f.expected.bank_state_before,"before",true);
      const result=await request(scoring,"/api/v1/decisions",f.request);equal(result,f.expected.response,"response",true);
      equal((await request(bank,"/api/v1/bank/state")).state,f.expected.bank_state_saved,"saved state",true);
      const calls=await request(provider,"/__test/calls");
      assert.equal(calls.statements.length,f.expected.statements_called?1:0,"statements calls");assert.equal(calls.bank.length,f.expected.ms2_called?1:0,"bank calls");assert.equal(calls.facts.length,0,"v2 supplier calls");
      if(calls.statements.length){assert.equal(calls.statements[0].years,"2024,2025");assert.ok(calls.statements[0].path.includes(encodeURIComponent(f.request.borrower.id)));}
      if(f.expected.bank_check_request)equal(calls.bank[0].body,f.expected.bank_check_request,"bank request",true);
      if(f.expected.manual_review_request)equal(calls.bank[0].body,f.expected.manual_review_request,"manual request",true);
      await checkEvent(f.expected.notification,f.request.application_id);
      equal(await request(scoring,"/api/v1/decisions",f.request),result,"repeat response");
      equal((await request(bank,"/api/v1/bank/state")).state,f.expected.bank_state_saved,"repeat state",true);await checkEvent(f.expected.notification,f.request.application_id);operations+=2;
    }
  }
  await appendFile(output+"/network-scenarios.jsonl",JSON.stringify({id,passed:true,operations})+"\n");console.log(id+": passed");
}
else if(mode==="pending"||mode==="retry"){
  await ready(bank);
  if(mode==="retry")await ready(admin);
  const f=identity(await read("testdata/bank/bank_fixed_deal.json"));
  f.request.application_id+="-pending";f.expected.notification.application_id=f.request.application_id;f.expected.notification.event_id=f.request.application_id+":"+f.expected.notification.kind;f.expected.notification.loan_id="loan-"+f.request.application_id;
  let result=await request(bank,"/api/v1/bank/check-and-issue",f.request);
  await writeFile(output+"/"+mode+"-response.json",stringifyJSON(result,true));
  if(mode==="pending"){assert.equal(result.notification_status,"PENDING");await writeFile(output+"/pending-state.json",stringifyJSON((await request(bank,"/api/v1/bank/state")).state));}
  else{
    const saved=await read(output+"/pending-state.json"), deadline=Date.now()+20000;
    let attempts=1;
    // Docker DNS/idle connections may still be recovering after a process is
    // healthy. PENDING permits another identical operation; no retry may issue.
    while(result.notification_status==="PENDING"&&Date.now()<deadline){
      equal((await request(bank,"/api/v1/bank/state")).state,saved,"pending retry state");
      await new Promise(resolve=>setTimeout(resolve,250));
      result=await request(bank,"/api/v1/bank/check-and-issue",f.request);attempts++;
    }
    assert.equal(result.notification_status,"DELIVERED");
    equal((await request(bank,"/api/v1/bank/state")).state,saved,"delivery retry state");
    await checkEvent(f.expected.notification,f.request.application_id);
    await writeFile(output+"/retry-response.json",stringifyJSON(result,true));
    await writeFile(output+"/delivery-retry.json",JSON.stringify({passed:true,attempts,checks:["PENDING while admin unavailable","delivered identical retry","no second issuance on every retry"]}));
  }
  console.log(mode+": passed");
}
else throw Error("Unknown test command");
