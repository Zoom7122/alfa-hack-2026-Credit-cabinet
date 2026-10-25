import {api, session, message} from "./api.js";
import {el, button, field, badge, table, empty, date, borrower} from "./dom.js";
import {ExactNumber, integer, stringifyJSON} from "./numbers.js";
import {createDraftEditor} from "./editor.js";
import {rulesSection} from "./rules-editor.js";
import {workbenchPage} from "./workbench.js";
import {eventsPage, eventPage, releasesWithRuntime} from "./audit.js";

const root = document.getElementById("app");
document.querySelector(".skip-link").addEventListener("click", event => { event.preventDefault(); document.getElementById("main")?.focus(); });
const limit = 20;
let user = null;
let epoch = 0;
let content = null;
let activeEditor = null;
let shownRoute = "";
let restoringRoute = false;

const link = (label, route, style = "text-link") => el("a", {href: "#" + route, class: style}, label);
const heading = (title, description, actions = []) => el("div", {class: "page-heading"}, el("div", {}, el("h1", {}, title), el("p", {class: "muted"}, description)), el("div", {class: "actions"}, actions));
function errorBox(error, retry = null) { return el("div", {class: "notice danger", role: "alert"}, el("strong", {}, message(error)), error.problem?.fields?.length ? el("p", {}, error.problem.fields.join(", ")) : null, retry ? button("Повторить", retry) : null); }
function go(route) { const next = "#" + route; if (location.hash === next) renderRoute(); else location.hash = next; }
function closeDialogs() { document.querySelectorAll("dialog").forEach(dialog => { dialog.close(); dialog.remove(); }); }

function login(expired = false) {
  activeEditor?.dispose(); activeEditor = null;
  epoch++; user = null; session(); closeDialogs();
  const name = el("input", {name: "username", autocomplete: "username", required: true, maxlength: 256});
  const password = el("input", {name: "password", type: "password", autocomplete: "current-password", required: true});
  const errors = el("div", {"aria-live": "polite"});
  const submit = el("button", {type: "submit", class: "button primary"}, "Войти");
  const form = el("form", {onsubmit: async event => {
    event.preventDefault(); errors.replaceChildren(); submit.disabled = true; submit.textContent = "Выполняем вход…";
    try {
      const result = await api("/api/admin/v1/auth/login", {method: "POST", public: true, body: {username: name.value, password: password.value}});
      user = result; session(result.csrf_token); shell(); await renderRoute();
    } catch (error) { errors.replaceChildren(errorBox(error)); }
    finally { password.value = ""; submit.disabled = false; submit.textContent = "Войти"; }
  }}, field("Логин", name), field("Пароль", password), errors, submit);
  root.replaceChildren(el("main", {id: "main", class: "login-page", tabindex: "-1"}, el("section", {class: "login-card"}, el("div", {class: "brand-mark", "aria-hidden": "true"}, "С"), el("p", {class: "eyebrow"}, "Административный кабинет"), el("h1", {}, "Модели скоринга"), el("p", {class: "muted"}, "Настройка моделей и просмотр сообщений банка"), expired ? el("p", {class: "notice warning", role: "status"}, "Сессия завершена. Войдите снова.") : null, form)));
  name.focus();
}

function shell() {
  const nav = el("nav", {"aria-label": "Основная навигация"}, [["models", "Модели"], ["drafts", "Черновики"], ["workbench", "Проверка и выпуск"], ["releases", "Релизы"], ["messages", "Сообщения"]].map(([route, label]) => el("a", {href: "#" + route, "data-section": route}, label)));
  content = el("main", {id: "main", class: "content", tabindex: "-1"});
  root.replaceChildren(el("div", {class: "layout"}, el("aside", {class: "sidebar"}, link(el("span", {class: "brand"}, el("span", {class: "brand-mark", "aria-hidden": "true"}, "С"), "Скоринг"), "models", "brand-link"), el("p", {class: "sidebar-caption"}, "Управление моделями"), nav, el("div", {class: "account"}, el("span", {}, user.username), button("Выйти", async () => {
    try { await api("/api/admin/v1/auth/logout", {method: "POST"}); login(); }
    catch (error) { if (user) content.prepend(errorBox(error)); }
  }, "quiet"))), content));
}

function pager(section, page, size) {
  const previous = button("Назад", () => go(section + "?page=" + (page - 1))); previous.disabled = page === 0;
  const next = button("Далее", () => go(section + "?page=" + (page + 1))); next.disabled = size < limit;
  return el("div", {class: "pager"}, previous, el("span", {class: "muted"}, "Страница " + (page + 1)), next);
}
function modelCard(model, draft = false) {
  const definition = model.definition;
  return el("article", {class: "model-card"}, el("div", {class: "card-top"}, badge(borrower(definition.borrower_type), "borrower"), badge(draft ? "Черновик" : "Опубликовано", draft ? "warning" : "success")), el("h2", {}, link(definition.title || "Без названия", draft ? "draft/" + encodeURIComponent(model.id) : "model/" + encodeURIComponent(model.version))), el("code", {}, definition.version), el("dl", {class: "compact-facts"}, el("dt", {}, "Параметры"), el("dd", {}, definition.parameters?.filter(item => item.enabled).length ?? 0), el("dt", {}, "Критерии"), el("dd", {}, definition.criteria?.filter(item => item.enabled).length ?? 0), el("dt", {}, draft ? "Ревизия" : "Опубликовано"), el("dd", {}, draft ? String(model.revision) : date(model.created_at))), draft ? el("p", {class: "muted small"}, "Обновлено: " + date(model.updated_at)) : button("Копировать в черновик", () => draftDialog(model)));
}

async function modelsPage(draft, page, current) {
  const section = draft ? "drafts" : "models";
  const rows = await api("/api/admin/v1/" + section + "?limit=" + limit + "&offset=" + page * limit);
  if (current !== epoch || !user) return;
  const search = el("input", {type: "search", placeholder: "Название или версия", "aria-label": "Поиск по текущей странице"});
  const grid = el("div", {class: "cards"});
  const filter = () => {
    const query = search.value.trim().toLocaleLowerCase("ru-RU");
    const selected = rows.filter(row => [row.definition.title, row.definition.version, borrower(row.definition.borrower_type)].some(value => String(value || "").toLocaleLowerCase("ru-RU").includes(query)));
    grid.replaceChildren(...selected.map(row => modelCard(row, draft)));
    if (!selected.length) grid.append(empty(rows.length ? "Ничего не найдено" : draft ? "Черновиков пока нет" : "Моделей пока нет", rows.length ? "Попробуйте другое название или версию." : "Создайте черновик или скопируйте опубликованную модель."));
  };
  search.addEventListener("input", filter); filter();
  content.replaceChildren(heading(draft ? "Черновики" : "Модели", draft ? "Сохранённые изменения до подготовки релиза" : "Версии моделей для ИП и юридических лиц", [button("Обновить", renderRoute), button("Создать черновик", () => draftDialog(), "primary")]), el("div", {class: "toolbar"}, search, el("span", {class: "muted"}, "Поиск по текущей странице")), grid, pager(section, page, rows.length));
}

function draftDialog(source = null) {
  const id = el("input", {name: "model_id", required: true, maxlength: 128, pattern: "[A-Za-z0-9][A-Za-z0-9_.-]*", placeholder: "Например: ip-business"});
  const title = el("input", {name: "title", required: true, maxlength: 256, placeholder: "Название модели"});
  const version = el("input", {name: "version", required: true, maxlength: 128, pattern: "[A-Za-z0-9][A-Za-z0-9_.-]*", placeholder: "Например: ip-v3"});
  const type = el("select", {name: "borrower_type"}, el("option", {value: "SOLE_PROPRIETOR"}, "Индивидуальный предприниматель"), el("option", {value: "LEGAL_ENTITY"}, "Юридическое лицо"));
  const reason = el("textarea", {name: "reason", required: true, rows: 3, maxlength: 16384, placeholder: "Для чего создаётся черновик"});
  const errors = el("div", {"aria-live": "polite"});
  const submit = el("button", {type: "submit", class: "button primary"}, "Создать черновик");
  const dialog = el("dialog", {class: "dialog", "aria-labelledby": "draft-dialog-title"});
  const form = el("form", {onsubmit: async event => {
    event.preventDefault(); if (!reason.value.trim()) { reason.setCustomValidity("Укажите основание."); reason.reportValidity(); reason.setCustomValidity(""); return; }
    errors.replaceChildren(); submit.disabled = true; submit.textContent = "Сохраняем…";
    try {
      let result;
      if (source) result = await api("/api/admin/v1/models/" + encodeURIComponent(source.version) + "/drafts", {method: "POST", body: {new_version: version.value.trim(), reason: reason.value.trim()}});
      else {
        const definition = {format_version: new ExactNumber("1"), id: id.value.trim(), version: version.value.trim(), borrower_type: type.value, title: title.value.trim(), parameters: [], criteria: [], data_checks: [], stops: [], manual_reviews: [], classes: []};
        result = await api("/api/admin/v1/drafts", {method: "POST", body: {definition, reason: reason.value.trim()}});
      }
      dialog.close(); dialog.remove(); go("draft/" + encodeURIComponent(result.id));
    } catch (error) { errors.replaceChildren(errorBox(error)); }
    finally { submit.disabled = false; submit.textContent = "Создать черновик"; }
  }}, source ? el("p", {class: "notice info"}, "Копия модели «" + source.definition.title + "», версия " + source.version) : el("div", {class: "form-grid"}, field("Название", title), field("Тип заёмщика", type), field("Идентификатор модели", id, "Постоянное имя из латинских букв, цифр, точки, дефиса и подчёркивания.")), field("Новая версия", version, "Укажите новое имя версии для последующей публикации."), field("Основание", reason), source ? null : el("p", {class: "muted small"}, "Новый черновик сохраняется без правил. Готовность к выпуску проверяется отдельно."), errors, el("div", {class: "actions"}, button("Отмена", () => { dialog.close(); dialog.remove(); }), submit));
  dialog.append(el("h2", {id: "draft-dialog-title"}, source ? "Копировать модель" : "Новая модель"), form);
  dialog.addEventListener("cancel", () => dialog.remove()); document.body.append(dialog); dialog.showModal();
}

function weights(definition) {
  let questionnaire = 0n, additional = 0n;
  for (const criterion of definition.criteria || []) { if (!criterion.enabled) continue; try { if (criterion.block === "QUESTIONNAIRE") questionnaire += integer(criterion.max_points); else if (criterion.block === "ADDITIONAL") additional += integer(criterion.max_points); } catch { return el("div", {class: "notice danger"}, "Максимумы критериев должны быть целыми числами."); } }
  const valid = questionnaire === 750n && additional === 250n;
  return el("div", {class: "score-summary"}, [["Анкета", questionnaire, 750n], ["Дополнительно", additional, 250n], ["Всего", questionnaire + additional, 1000n]].map(([label, value, expected]) => el("div", {class: "score-tile " + (value === expected ? "" : "invalid")}, el("span", {class: "muted"}, label), el("strong", {}, String(value)), el("small", {}, "из " + expected))), valid ? badge("750 + 250 = 1000", "success") : badge("Выпуск требует 750 / 250 / 1000", "warning"));
}
function definitionView(definition) {
  return el("div", {}, weights(definition), el("section", {class: "panel"}, el("h2", {}, "Критерии"), definition.criteria.length ? table(["Критерий", "Блок", "Максимум", "Состояние"], definition.criteria.map(item => [el("span", {}, el("strong", {}, item.title), el("small", {class: "muted block"}, item.id)), item.block === "QUESTIONNAIRE" ? "Анкета" : "Дополнительно", String(item.max_points), item.enabled ? "Включён" : "Выключен"])) : el("p", {class: "muted"}, "Критерии ещё не добавлены.")), el("section", {class: "panel"}, el("h2", {}, "Параметры"), definition.parameters.length ? table(["Параметр", "Тип", "Источник", "Обязательность"], definition.parameters.map(item => [el("span", {}, item.title, el("small", {class: "muted block"}, item.id)), item.type === "BOOLEAN" ? "Логический" : item.type === "NUMBER" ? "Число" : "Категория / дата", item.source.kind + ": " + item.source.path, item.required ? "Обязательный" : "Необязательный"])) : el("p", {class: "muted"}, "Параметры ещё не добавлены.")), el("details", {class: "panel"}, el("summary", {}, "Описание модели в JSON"), el("pre", {class: "code"}, stringifyJSON(definition, true))));
}

async function modelPage(version, current) {
  const model = await api("/api/admin/v1/models/" + encodeURIComponent(version)); if (current !== epoch || !user) return;
  content.replaceChildren(heading(model.definition.title, borrower(model.borrower_type) + " · " + model.version, [button("Копировать в черновик", () => draftDialog(model), "primary"), link("Проверить модель", "workbench?kind=PUBLISHED&id=" + encodeURIComponent(model.version), "button")]), el("div", {class: "notice info"}, "Опубликованное описание сохраняется неизменным. Для изменения создайте черновик."), el("div", {class: "metadata"}, badge("Опубликовано", "success"), el("span", {}, "Автор: " + model.created_by), el("span", {}, date(model.created_at))), definitionView(model.definition));
}
async function draftPage(id, revision, current) {
  const base = "/api/admin/v1/drafts/" + encodeURIComponent(id);
  const [draft, revisions] = await Promise.all([api(base + (revision ? "/revisions/" + encodeURIComponent(revision) : "")), api(base + "/revisions?limit=50")]);
  let validation = null;
  if (!revision) validation = await api(base + "/validation");
  if (current !== epoch || !user) return;
  const selector = el("select", {"aria-label": "Сохранённая ревизия", onchange: event => go("draft/" + encodeURIComponent(id) + "/revision/" + encodeURIComponent(event.target.value))}, revisions.map(item => el("option", {value: String(item.revision)}, "Ревизия " + item.revision + " · " + date(item.created_at) + " · " + item.author))); selector.value = String(draft.revision);
  const shown = new Set(revisions.map(item => String(item.revision)));
  if (!shown.has(String(draft.revision))) { selector.append(el("option", {value: String(draft.revision)}, "Выбранная ревизия " + draft.revision)); selector.value = String(draft.revision); }
  let revisionOffset = revisions.length;
  const revisionErrors = el("div", {"aria-live": "polite"});
  const earlier = button("Показать более ранние ревизии", async () => {
    earlier.disabled = true; revisionErrors.replaceChildren();
    try {
      const rows = await api(base + "/revisions?limit=50&offset=" + revisionOffset);
      if (current !== epoch || !user) return;
      for (const item of rows) if (!shown.has(String(item.revision))) { selector.append(el("option", {value: String(item.revision)}, "Ревизия " + item.revision + " · " + date(item.created_at) + " · " + item.author)); shown.add(String(item.revision)); }
      revisionOffset += rows.length; earlier.hidden = rows.length < 50;
    } catch (error) { revisionErrors.replaceChildren(errorBox(error)); }
    finally { earlier.disabled = false; }
  }); earlier.hidden = revisions.length < 50;
  const details = [];
  if (validation && String(validation.revision) === String(draft.revision)) {
    details.push(validation.validation.valid ? el("p", {class: "notice success"}, "Модель прошла проверку.") : el("section", {class: "notice warning"}, el("strong", {}, "Перед выпуском нужно исправить ошибки"), el("ul", {}, validation.validation.issues.map(issue => el("li", {}, issue.message, el("code", {class: "block"}, issue.path))))));
  } else if (validation) details.push(el("p", {class: "notice warning"}, "Черновик изменился во время загрузки. Обновите страницу."));
  if (!revision) {
    activeEditor = createDraftEditor(draft, validation, () => go("draft/" + encodeURIComponent(id)));
    activeEditor.addSection(rulesSection);
    activeEditor.node.prepend(link("Проверить сохранённую ревизию и выпустить", "workbench?kind=REVISION&id=" + encodeURIComponent(id) + "&revision=" + draft.revision, "button"));
  }
  content.replaceChildren(heading(draft.definition.title, borrower(draft.definition.borrower_type) + " · " + draft.definition.version, [button("Открыть текущую ревизию", () => go("draft/" + encodeURIComponent(id)))]), el("div", {class: "metadata"}, badge(revision ? "Историческая ревизия" : "Черновик", "warning"), el("span", {}, "Ревизия " + draft.revision), el("span", {}, "Автор: " + (draft.author || draft.created_by))), el("div", {class: "toolbar"}, field("Сохранённые ревизии", selector), earlier), revisionErrors, ...details, activeEditor?.node || definitionView(draft.definition));
}

async function renderRoute() {
  if (!user || !content) return;
  if (restoringRoute) { restoringRoute = false; return; }
  if (activeEditor && !activeEditor.canLeave()) {
    if (location.hash !== shownRoute) { restoringRoute = true; location.hash = shownRoute; }
    return;
  }
  activeEditor?.dispose(); activeEditor = null; shownRoute = location.hash;
  const current = ++epoch;
  const [path, query] = (location.hash.slice(1) || "models").split("?");
  const parts = path.split("/");
  const section = parts[0] === "model" ? "models" : parts[0] === "draft" ? "drafts" : parts[0] === "event" ? "messages" : parts[0];
  document.querySelectorAll("nav a").forEach(item => { if (item.dataset.section === section) item.setAttribute("aria-current", "page"); else item.removeAttribute("aria-current"); });
  content.setAttribute("aria-busy", "true"); content.replaceChildren(el("p", {class: "loading", role: "status"}, "Загружаем данные…"));
  try {
    const requestedPage = new URLSearchParams(query).get("page") || "0";
    if (!/^\d{1,6}$/.test(requestedPage)) throw new Error("Некорректный номер страницы.");
    const page = Number(requestedPage);
    if (parts[0] === "models") await modelsPage(false, page, current);
    else if (parts[0] === "drafts") await modelsPage(true, page, current);
    else if (parts[0] === "model" && parts[1]) await modelPage(decodeURIComponent(parts[1]), current);
    else if (parts[0] === "draft" && parts[1]) await draftPage(decodeURIComponent(parts[1]), parts[2] === "revision" ? parts[3] : null, current);
    else if (parts[0] === "releases") { const view = await releasesWithRuntime(page, go); if (current === epoch && user) content.replaceChildren(view); }
    else if (parts[0] === "messages") { const view = await eventsPage(new URLSearchParams(query), page, go); if (current === epoch && user) content.replaceChildren(view); }
    else if (parts[0] === "event" && parts[1]) { const view = await eventPage(decodeURIComponent(parts[1])); if (current === epoch && user) content.replaceChildren(view); }
    else if (parts[0] === "workbench") { const view = await workbenchPage(new URLSearchParams(query)); if (current === epoch && user) content.replaceChildren(view); }
    else content.replaceChildren(empty("Страница не найдена", "Выберите раздел в меню."));
  } catch (error) { if (current === epoch && user) content.replaceChildren(heading("Не удалось загрузить данные", "Изменения не отправлялись."), errorBox(error, renderRoute)); }
  finally { if (current === epoch && user) content.setAttribute("aria-busy", "false"); }
}

window.addEventListener("hashchange", renderRoute);
window.addEventListener("admin-session-expired", () => login(true));
async function start() {
  try { user = await api("/api/admin/v1/auth/session", {public: true}); session(user.csrf_token); shell(); await renderRoute(); }
  catch (error) { login(); if (error.status !== 401) root.querySelector("form").prepend(errorBox(error, start)); }
}
start();
