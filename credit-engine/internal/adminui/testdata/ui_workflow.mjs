// Runs only against an explicitly isolated local Docker admin instance.
import assert from "node:assert/strict";
import {writeFile} from "node:fs/promises";
import {pathToFileURL} from "node:url";

if (process.env.CREDIT_ENGINE_UI_TEST !== "1") throw new Error("An explicitly isolated UI test instance is required");
const {chromium} = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href);
const origin = process.env.CREDIT_ENGINE_UI_ORIGIN || "http://localhost:18084";
const output = process.env.CREDIT_ENGINE_UI_OUTPUT;
let ready = false;
for (let attempt = 0; attempt < 120; attempt++) {
  try { const response = await fetch(origin + "/healthz"); if (response.ok) { ready = true; break; } } catch {}
  await new Promise(resolve => setTimeout(resolve, 250));
}
assert.ok(ready, "The isolated admin service did not become ready");
const browser = await chromium.launch({headless: true});
const context = await browser.newContext({viewport: {width: 1440, height: 1000}, acceptDownloads: false});
const page = await context.newPage();
const errors = [];
page.on("pageerror", error => errors.push(error.message));
page.on("console", event => { if (/Content Security Policy|Refused to/.test(event.text())) errors.push(event.text()); });
const checks = [];
const visible = async (locator) => { await locator.waitFor({state: "visible", timeout: 15000}); };
const login = async password => {
  await page.getByLabel("Логин", {exact: true}).fill("editor");
  await page.getByLabel("Пароль", {exact: true}).fill(password);
  await page.getByRole("button", {name: "Войти", exact: true}).click();
};
try {
  await page.goto(origin, {waitUntil: "networkidle"});
  await visible(page.getByRole("heading", {name: "Конструктор скоринга", exact: true})); checks.push("login page");
  await login("wrong-fictional-password"); await visible(page.getByRole("alert").getByText("Неверный логин или пароль.")); checks.push("invalid credentials");
  await login("fictional-admin-password");
  await visible(page.getByRole("heading", {name: "Модели скоринга", exact: true}));
  await visible(page.locator('a[href="#model/ip-v2"]')); await visible(page.locator('a[href="#model/legal-v2"]')); checks.push("IP and LE model list");
  await page.screenshot({path: output + "/ui-desktop.png", fullPage: true});

  await page.locator('a[href="#model/ip-v2"]').click();
  await visible(page.locator(".page-heading").getByRole("button", {name: "Копировать в черновик", exact: true}));
  await page.locator(".page-heading").getByRole("button", {name: "Копировать в черновик", exact: true}).click();
  await visible(page.getByRole("dialog"));
  const suffix = Date.now().toString(36);
  await page.getByLabel("Новая версия", {exact: true}).fill("ip-ui-" + suffix);
  await page.getByLabel("Основание", {exact: true}).fill("UI copy verification");
  await page.getByRole("button", {name: "Создать черновик", exact: true}).click();
  await visible(page.getByText("Модель прошла проверку.", {exact: true})); checks.push("copy draft");
  const copiedID = page.url().split("#draft/")[1].split("/")[0];
  const copiedResponse = await context.request.get(origin + "/api/admin/v1/drafts/" + copiedID);
  assert.equal(copiedResponse.status(), 200); assert.equal((await copiedResponse.json()).created_by, "editor");
  await page.getByRole("button",{name:"Ревизии и JSON",exact:true}).click();
  await page.getByRole("link",{name:"Ревизия 1",exact:true}).click();
  await visible(page.getByText(/Историческая ревизия/)); checks.push("saved revision");

  await page.getByRole("navigation",{name:"Основная навигация"}).getByRole("link", {name: "Модели", exact: true}).click();
  await visible(page.getByRole("heading", {name: "Модели скоринга", exact: true}));
  await page.getByRole("button", {name: "Создать черновик", exact: true}).click();
  const title = "Проверка <img src=x onerror=alert(1)>";
  await page.getByLabel("Название модели", {exact: true}).fill(title);
  await page.getByLabel("Тип заёмщика", {exact: true}).selectOption("LEGAL_ENTITY");
  await page.getByLabel("Идентификатор модели", {exact: true}).fill("legal-ui-" + suffix);
  await page.getByLabel("Новая версия", {exact: true}).fill("legal-ui-" + suffix);
  await page.getByLabel("Основание", {exact: true}).fill("UI empty model verification");
  await page.getByRole("dialog").getByRole("button", {name: "Создать черновик", exact: true}).click();
  await visible(page.getByRole("heading", {name: title, exact: true}));
  await visible(page.locator("summary").filter({hasText:"Модель не прошла проверку"}));
  assert.equal(await page.locator('img[src="x"]').count(), 0); checks.push("blank draft and safe text rendering");

  await page.getByRole("navigation").getByRole("link", {name: "Релизы", exact: true}).click();
  await visible(page.getByRole("heading", {name: "Релизы и запущенные версии", exact: true})); await visible(page.getByRole("cell").filter({hasText: "2.0.0"}).first()); checks.push("release navigation");
  await page.getByRole("navigation").getByRole("link", {name: "Сообщения", exact: true}).click();
  await visible(page.getByRole("heading", {name: "Сообщения банка", exact: true})); await visible(page.getByText("Сообщений не найдено", {exact: true})); checks.push("message navigation and empty state");

  await page.route("**/api/admin/v1/models?*", route => route.fulfill({status: 503, contentType: "application/problem+json", body: JSON.stringify({code: "ADMIN_DATABASE_UNAVAILABLE", detail: "Service Unavailable", fields: []})}));
  await page.getByRole("navigation",{name:"Основная навигация"}).getByRole("link", {name: "Модели", exact: true}).click();
  await visible(page.getByRole("alert").getByText("Хранилище временно недоступно. Повторите запрос."));
  await page.unroute("**/api/admin/v1/models?*"); await page.getByRole("button", {name: "Обновить", exact: true}).click();
  await visible(page.getByRole("heading", {name: "Модели скоринга", exact: true})); checks.push("API error and retry");

  // Revoke the session through the real server, then check the next UI request.
  const sessionResponse = await context.request.get(origin + "/api/admin/v1/auth/session");
  const activeSession = await sessionResponse.json();
  const logoutResponse = await context.request.post(origin + "/api/admin/v1/auth/logout", {headers: {Origin: origin, "X-CSRF-Token": activeSession.csrf_token}});
  assert.equal(logoutResponse.status(), 204);
  await page.getByRole("navigation").getByRole("link", {name: "Сообщения", exact: true}).click();
  await visible(page.getByText("Сессия завершена. Войдите снова.", {exact: true})); checks.push("revoked session returns to login");
  await login("fictional-admin-password"); await visible(page.getByRole("heading", {name: "Сообщения банка", exact: true}));
  await page.getByRole("button", {name: "Выйти", exact: true}).click();
  await visible(page.getByRole("heading", {name: "Конструктор скоринга", exact: true})); checks.push("logout");
  assert.deepEqual(errors, []);
  await writeFile(output + "/browser-workflow.json", JSON.stringify({passed: checks.length, checks, javascriptErrors: errors}, null, 2));
  console.log(JSON.stringify({passed: checks.length, checks}));
} catch (error) {
  await page.screenshot({path: output + "/ui-failure.png", fullPage: true});
  await writeFile(output + "/ui-failure.txt", await page.locator("body").innerText());
  throw error;
} finally { await browser.close(); }
