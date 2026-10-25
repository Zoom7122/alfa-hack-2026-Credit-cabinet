// Full desktop acceptance against explicitly isolated Linux/Docker services.
import assert from "node:assert/strict";
import {readFile, writeFile} from "node:fs/promises";
import {pathToFileURL} from "node:url";
import {createHash} from "node:crypto";
import {parseJSON, stringifyJSON} from "../web/numbers.js";
if (process.env.CREDIT_ENGINE_UI_TEST !== "1") throw Error("Isolated Docker instance required");
const {chromium}=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href);
const origin="http://localhost:18084", output=process.env.CREDIT_ENGINE_UI_OUTPUT;
const browser=await chromium.launch({headless:true});
const context=await browser.newContext({viewport:{width:1440,height:1000}});
const page=await context.newPage(), errors=[], checks=[];
page.on("pageerror",error=>errors.push(error.message));
page.on("console",event=>{if(/Content Security Policy|Refused to/.test(event.text()))errors.push(event.text());});
page.on("dialog",dialog=>dialog.accept());
const visible=async locator=>locator.waitFor({state:"visible",timeout:25000});
const click=async(name,scope=page)=>scope.getByRole("button",{name,exact:true}).click();
const dialog=()=>page.getByRole("dialog");
let csrf="";
async function request(method,path,body,token=csrf) {
  const response=await context.request.fetch(origin+path,{method,headers:{Origin:origin,"X-CSRF-Token":token,"Content-Type":"application/json"},...(body===undefined?{}:{data:stringifyJSON(body)})});
  const raw=await response.text();
  assert.ok(response.ok(),method+" "+path+" "+response.status()+" "+raw.slice(0,1200));
  return raw?parseJSON(raw):null;
}
async function closeModal() { await click("Применить к черновику",dialog()); await dialog().waitFor({state:"detached"}); }
async function addParameter(id,type) {
  await click("Добавить параметр");
  await dialog().getByLabel("Идентификатор параметра",{exact:true}).fill(id);
  await dialog().getByLabel("Название параметра",{exact:true}).fill("Приёмка "+id);
  await dialog().getByLabel("JSON-имя",{exact:true}).fill(id);
  await dialog().getByLabel("Тип значения",{exact:true}).selectOption(type);
  await dialog().getByLabel("Версия схемы поставщика",{exact:true}).fill("acceptance-facts-v1");
  if(type==="NUMBER") { await dialog().getByLabel("Нижняя граница",{exact:true}).fill("0"); await dialog().getByLabel("Верхняя граница",{exact:true}).fill("10"); }
  await closeModal();
}
async function redistribute(c,delta) {
  await click("Изменить критерий "+c.id);
  const max=Number(String(c.max_points))-delta;
  await dialog().getByLabel("Максимум баллов",{exact:true}).fill(String(max));
  for(const points of await dialog().getByLabel("Баллы",{exact:true}).all()) {
    const value=Number(await points.inputValue()); if(value>max)await points.fill(String(max));
  }
  await closeModal();
}
async function addCriterion(id,parameter,block,cases) {
  await click("Добавить критерий");
  await dialog().getByLabel("Идентификатор критерия",{exact:true}).fill(id);
  await dialog().getByLabel("Название критерия",{exact:true}).fill("Приёмка "+id);
  await dialog().getByLabel("Блок",{exact:true}).selectOption(block);
  await dialog().getByLabel("Максимум баллов",{exact:true}).fill("10");
  await dialog().getByLabel("Параметр результата",{exact:true}).selectOption(parameter);
  if(block==="QUESTIONNAIRE") { await dialog().getByLabel("Текстовый ответ анкеты",{exact:true}).check(); await dialog().getByLabel("Шаблон ответа",{exact:true}).fill("${"+parameter+"}"); }
  for(let i=0;i<cases.length;i++) {
    await click("Добавить вариант: Балльная шкала",dialog());
    const scale=dialog().locator("section").filter({has:page.getByRole("heading",{name:"Балльная шкала",exact:true})});
    const row=scale.locator("details").last();
    await row.getByLabel("ID варианта",{exact:true}).fill(id+"_"+i);
    await row.getByLabel("Баллы",{exact:true}).fill(String(cases[i].points));
    await row.getByLabel("Оператор",{exact:true}).selectOption(cases[i].op);
    await row.getByLabel("Параметр",{exact:true}).selectOption(parameter);
    const value=row.getByLabel("Значение",{exact:true});
    if(typeof cases[i].value==="boolean")await value.selectOption(String(cases[i].value)); else await value.fill(String(cases[i].value));
  }
  await closeModal();
}
try {
  await page.goto(origin);
  await page.getByLabel("Логин",{exact:true}).fill("editor");
  await page.getByLabel("Пароль",{exact:true}).fill("fictional-admin-password");
  await click("Войти"); await visible(page.getByRole("heading",{name:"Модели",exact:true}));
  csrf=(await request("GET","/api/admin/v1/auth/session")).csrf_token;
  const source=await request("GET","/api/admin/v1/models/ip-v2");
  await page.locator('a[href="#model/ip-v2"]').click();
  await click("Копировать в черновик",page.locator(".page-heading"));
  const suffix=Date.now().toString(36), version="ip-accept-"+suffix, releaseVersion="accept-"+suffix;
  await dialog().getByLabel("Новая версия",{exact:true}).fill(version);
  await dialog().getByLabel("Основание",{exact:true}).fill("Fictional acceptance workflow");
  await click("Создать черновик",dialog()); await visible(page.getByText("Модель прошла проверку.",{exact:true}));
  const id=page.url().split("#draft/")[1].split("/")[0];
  await addParameter("supplier_flag","BOOLEAN"); await addParameter("supplier_metric","NUMBER"); checks.push("named boolean and numeric supplier fields");
  const q=source.definition.criteria.find(c=>c.block==="QUESTIONNAIRE"&&Number(String(c.max_points))>=10);
  const e=source.definition.criteria.find(c=>c.block==="ADDITIONAL"&&Number(String(c.max_points))>=10);
  await redistribute(q,10); await redistribute(e,10);
  await addCriterion("ACCEPT_Q","supplier_flag","QUESTIONNAIRE",[{op:"EQ",value:false,points:0},{op:"EQ",value:true,points:10}]);
  await addCriterion("ACCEPT_E","supplier_metric","ADDITIONAL",[{op:"LE",value:5,points:0},{op:"GT",value:5,points:10}]);
  checks.push("redistribution across both 750/250 blocks and typed scales");
  await click("Изменить параметр supplier_flag");
  await visible(dialog().getByText(/ACCEPT_Q/)); await click("Отмена",dialog()); checks.push("dependency warning");
  await click("Добавить: Стоп-факторы");
  await dialog().getByLabel("ID правила",{exact:true}).fill("ACCEPT_STOP");
  await dialog().getByLabel("Название правила",{exact:true}).fill("Fictional supplier stop");
  await dialog().getByLabel("Оператор",{exact:true}).selectOption("EQ");
  await dialog().getByLabel("Параметр",{exact:true}).selectOption("supplier_flag");
  await dialog().getByLabel("Значение",{exact:true}).selectOption("true");
  await dialog().getByLabel("Код причины",{exact:true}).fill("ACCEPTANCE_STOP");
  await dialog().getByLabel("Текст причины",{exact:true}).fill("Fictional acceptance reason");
  await closeModal();
  await click("Изменить классы");
  await dialog().getByLabel("Максимум D",{exact:true}).fill("548");
  await dialog().getByLabel("Минимум C",{exact:true}).fill("549");
  await closeModal(); checks.push("stop and class boundary editing");
  await page.getByLabel("Основание изменения",{exact:true}).fill("Fictional complete editor change");
  await click("Сохранить новую ревизию");
  await visible(page.getByText("Ревизия 2",{exact:true})); await visible(page.getByText("Модель прошла проверку.",{exact:true}));
  const saved=await request("GET","/api/admin/v1/drafts/"+id);
  assert.equal(String(saved.revision),"2");
  const conflict=await context.request.put(origin+"/api/admin/v1/drafts/"+id,{headers:{Origin:origin,"X-CSRF-Token":csrf,"Content-Type":"application/json"},data:stringifyJSON({expected_revision:1,definition:saved.definition,reason:"stale test"})});
  assert.equal(conflict.status(),409); checks.push("saved revision and optimistic conflict");
  await page.getByRole("link",{name:"Проверить сохранённую ревизию и выпустить",exact:true}).click();
  await visible(page.getByRole("heading",{name:"Проверка и выпуск",exact:true}));
  const sample=parseJSON(await readFile(new URL("../../../testdata/simulation/ip-v2.json",import.meta.url),"utf8"));
  sample.additional_facts={supplier_flag:false,supplier_metric:9}; sample.facts_date=sample.evaluation_date;
  await page.getByLabel("Тестовый набор",{exact:true}).fill(stringifyJSON(sample,true));
  await click("Выполнить расчёт");
  await visible(page.getByRole("heading",{name:"Результат: "+version,exact:true}));
  const falseResult=await request("POST","/api/admin/v1/simulations",{model:{kind:"REVISION",id,revision:2},sample});
  assert.notEqual(falseResult.route,"REJECTED");
  sample.additional_facts.supplier_flag=true;
  const trueResult=await request("POST","/api/admin/v1/simulations",{model:{kind:"REVISION",id,revision:2},sample});
  assert.equal(trueResult.route,"REJECTED"); assert.ok(trueResult.reasons.some(r=>r.code==="ACCEPTANCE_STOP"));
  sample.additional_facts.supplier_flag=null;
  const nullResult=await request("POST","/api/admin/v1/simulations",{model:{kind:"REVISION",id,revision:2},sample});
  assert.equal(nullResult.route,"NEED_INFO"); sample.additional_facts.supplier_flag=false;
  await page.getByLabel("Модель для сравнения (слева)",{exact:true}).selectOption({label:source.definition.title+" · ip-v2"});
  await click("Сравнить расчёты"); await visible(page.getByRole("heading",{name:"Сравнение расчётов",exact:true}));
  await click("Показать изменения модели"); await visible(page.getByRole("heading",{name:"Изменения определения",exact:true}));
  checks.push("simulation, comparison, explicit false/null and changed stop");
  await page.getByLabel("Новая версия релиза",{exact:true}).fill(releaseVersion);
  await page.getByLabel("Основание подготовки релиза",{exact:true}).fill("Fictional reproducible release");
  const badDefinition=parseJSON(stringifyJSON(saved.definition)); badDefinition.version="invalid-"+suffix; badDefinition.criteria[0].max_points=Number(String(badDefinition.criteria[0].max_points))+1;
  const bad=await request("POST","/api/admin/v1/drafts",{definition:badDefinition,reason:"invalid acceptance weights"});
  const rejected=await context.request.post(origin+"/api/admin/v1/releases/preview",{headers:{Origin:origin,"X-CSRF-Token":csrf,"Content-Type":"application/json"},data:stringifyJSON({release:{version:"invalid-release-"+suffix,base_version:"2.0.0",published_versions:["ip-v2","legal-v2"],drafts:[{id:bad.id,revision:1}],notes:"invalid"}})});
  assert.equal(rejected.status(),422); assert.match(await rejected.text(),/MODEL_VALIDATION_FAILED/); checks.push("invalid weights prevent release");
  await click("Проверить и показать документы");
  await visible(page.getByText("Комплект прошёл проверку. Подготовка ещё не выполнена.",{exact:true}));
  await visible(page.locator("summary").filter({hasText:"docs/02_inputs_and_scoring.md"}));
  await click("Подготовить релиз");
  await visible(page.getByText("Релиз "+releaseVersion+" подготовлен. Для применения нужны сборка и развёртывание МС1.",{exact:true}));
  const prepared=await request("GET","/api/admin/v1/releases/"+releaseVersion);
  const zip=await context.request.get(origin+"/api/admin/v1/releases/"+releaseVersion+"/archive");
  const zipData=await zip.body(); assert.equal(createHash("sha256").update(zipData).digest("hex"),prepared.archive_sha256);
  await writeFile(output+"/accepted-release.zip",zipData);
  await writeFile(output+"/accepted-model.json",stringifyJSON(saved.definition,true));
  await writeFile(output+"/accepted-sample.json",stringifyJSON(sample,true)); checks.push("preview documents, immutable release and exact ZIP checksum");
  const fixture=parseJSON(await readFile(new URL("../../../testdata/decisions/uc04_ip_approved.json",import.meta.url),"utf8"));
  const event=fixture.expected.notification; event.application_id="accept/opaque+%?"+suffix; event.event_id=event.application_id+":"+event.kind; event.score.model=version; event.score.economics.criteria[0].value=false;
  const post=()=>context.request.post(origin+"/api/v1/credit-events",{headers:{"Content-Type":"application/json",Authorization:"Bearer fictional-ms2-service-token-1234567890"},data:stringifyJSON(event)});
  assert.equal((await post()).status(),202); assert.equal((await post()).status(),202);
  await page.getByRole("navigation").getByRole("link",{name:"Сообщения",exact:true}).click();
  await page.getByLabel("ID заёмщика",{exact:true}).fill(event.borrower.id);
  await page.getByLabel("ID заявки",{exact:true}).fill(event.application_id);
  const filtering=page.waitForResponse(response=>{
    const url=new URL(response.url());
    return response.request().method()==="GET"&&url.pathname==="/api/admin/v1/events"&&url.searchParams.get("application_id")===event.application_id&&url.searchParams.get("borrower_id")===event.borrower.id;
  });
  await click("Применить фильтры");
  assert.equal((await filtering).status(),200);
  await page.waitForFunction(()=>document.querySelectorAll('a[href^="#event/"]').length===1);
  const eventLink=page.locator('a[href="#event/'+encodeURIComponent(event.event_id)+'"]');
  await visible(eventLink);
  await eventLink.click();
  await visible(page.getByRole("heading",{name:"Выдача",exact:true}));
  assert.ok(await page.getByRole("cell",{name:"false",exact:true}).count()>0);
  await page.screenshot({path:output+"/event-desktop.png",fullPage:true}); checks.push("idempotent message, opaque ID, borrower filter and boolean display");
  await page.getByRole("navigation").getByRole("link",{name:"Релизы",exact:true}).click();
  await visible(page.getByText("Версии, сообщённые запущенным МС1",{exact:true}));
  await visible(page.locator(".runtime-models").getByText(/Получен ответ МС1/));
  assert.ok(!(await page.locator(".runtime-models").innerText()).includes(version));
  await page.screenshot({path:output+"/releases-desktop.png",fullPage:true}); checks.push("prepared release does not activate running catalog");
  assert.deepEqual(errors,[]);
  await writeFile(output+"/admin-acceptance.json",JSON.stringify({passed:checks.length,checks,javascriptErrors:errors,version,releaseVersion},null,2));
  console.log(JSON.stringify({passed:checks.length,checks,version,releaseVersion}));
} catch(error) {
  await page.screenshot({path:output+"/acceptance-failure.png",fullPage:true});
  await writeFile(output+"/acceptance-failure.txt",await page.locator("body").innerText()); throw error;
} finally { await browser.close(); }
