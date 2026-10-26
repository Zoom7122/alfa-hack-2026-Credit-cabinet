// Deterministic test-only aggregator for the model released by admin_acceptance.
import {createServer} from "node:http";
if(process.env.CREDIT_ENGINE_UI_TEST!=="1")throw Error("Isolated Docker instance required");
const requests=[];
createServer(async(req,res)=>{
  if(req.method==="GET"&&req.url==="/__requests"){res.setHeader("Content-Type","application/json");res.end(JSON.stringify(requests));return;}
  if(req.method==="GET"&&req.url==="/__test/calls"){res.setHeader("Content-Type","application/json");res.end(JSON.stringify({facts:requests}));return;}
  if(req.method!=="POST"||req.url!=="/api/v1/scoring-facts"){res.writeHead(404);res.end();return;}
  let raw="";
  for await(const chunk of req){raw+=chunk;if(raw.length>1048576){res.writeHead(413);res.end();return;}}
  try{
    const input=JSON.parse(raw); requests.push(raw);
    const payload={application_id:input.application_id,borrower_id:input.borrower.id,borrower_type:input.borrower.type,model_version:input.model_version,facts_schema_version:input.facts_schema_version,facts_date:"2026-10-23",additional_facts:{supplier_flag:false,supplier_metric:9}};
    res.writeHead(200,{"Content-Type":"application/json"});res.end(JSON.stringify(payload));
  }catch{res.writeHead(400);res.end();}
}).listen(18090,"0.0.0.0");
