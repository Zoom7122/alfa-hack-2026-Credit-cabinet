// Network fixture provider and transparent bank proxy; never a production API.
import {createServer} from "node:http";
import {parseJSON,stringifyJSON} from "../admin-frontend/src/legacy/numbers.js";
if(process.env.CREDIT_ENGINE_UI_TEST!=="1")throw Error("Isolated acceptance required");
let statement=null, calls={statements:[],bank:[],facts:[]};
const send=(res,status,value)=>{res.writeHead(status,{"Content-Type":"application/json"});res.end(stringifyJSON(value));};
createServer(async(req,res)=>{
  try{
    const url=new URL(req.url,"http://fixture");
    if(req.method==="GET"&&url.pathname==="/healthz")return send(res,200,{status:"ok"});
    if(req.method==="GET"&&url.pathname==="/__test/calls")return send(res,200,calls);
    let body="";if(req.method==="POST")for await(const chunk of req){body+=chunk;if(body.length>5*1024*1024)return send(res,413,{error:"too large"});}
    if(req.method==="POST"&&url.pathname==="/__test/reset"){
      const data=parseJSON(body);statement=data.statements??null;calls={statements:[],bank:[],facts:[]};return send(res,200,{ok:true});
    }
    if(req.method==="GET"&&/^\/api\/v1\/borrowers\/[^/]+\/statements$/.test(url.pathname)){
      calls.statements.push({path:url.pathname,years:url.searchParams.get("years")});
      return send(res,statement?200:404,statement||{});
    }
    if(req.method==="POST"&&url.pathname.startsWith("/bank/api/v1/bank/")){
      calls.bank.push({path:url.pathname.slice(5),body:parseJSON(body)});
      const response=await fetch("http://bank-state-service:8083"+url.pathname.slice(5),{method:"POST",headers:{"Content-Type":"application/json"},body,signal:AbortSignal.timeout(15000)});
      res.writeHead(response.status,{"Content-Type":response.headers.get("content-type")||"application/json"});res.end(await response.text());return;
    }
    if(req.method==="POST"&&url.pathname==="/api/v1/scoring-facts"){
      const data=parseJSON(body);calls.facts.push(body);
      return send(res,200,{application_id:data.application_id,borrower_id:data.borrower.id,borrower_type:data.borrower.type,model_version:data.model_version,facts_schema_version:data.facts_schema_version,facts_date:"2026-10-23",additional_facts:{supplier_flag:false,supplier_metric:9}});
    }
    send(res,404,{error:"unknown fixture route"});
  }catch(error){send(res,502,{error:"fixture request failed",detail:error.message});}
}).listen(18090,"0.0.0.0");
