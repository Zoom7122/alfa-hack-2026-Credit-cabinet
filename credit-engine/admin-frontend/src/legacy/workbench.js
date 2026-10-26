import {api, message} from "./api.js";
import {el, button, field, table, empty} from "./dom.js";
import {text, area, select, check, requiredText} from "./forms.js";
import {parseJSON, stringifyJSON} from "./numbers.js";

export async function allRows(path) {
  const result = [];
  for (let offset = 0; offset < 20000; offset += 200) {
    const rows = await api(path + "?limit=200&offset=" + offset); result.push(...rows);
    if (rows.length < 200) return result;
  }
  throw new Error("Каталог слишком велик для одного экрана. Используйте списки с пагинацией.");
}
export function problemView(error) {
  return el("div", {class: "notice danger", role: "alert"}, el("p", {}, message(error)), error.problem?.fields?.length ? el("p", {}, error.problem.fields.join(", ")) : null, error.problem?.validation?.issues?.length ? el("ul", {}, error.problem.validation.issues.map(issue => el("li", {}, issue.message, el("code", {class: "block"}, issue.path)))) : null);
}
const raw = (label, value) => el("details", {class: "subpanel"}, el("summary", {}, label), el("pre", {class: "code"}, stringifyJSON(value, true)));
const shown = value => value === null || value === undefined ? "—" : typeof value === "object" ? stringifyJSON(value) : String(value);
const valueText = value => !value?.known ? "Неизвестно (null)" : value.exact_value || shown(value.value);
function simulationView(result) {
  return el("section", {class: "panel simulation-result"}, el("h2", {}, "Результат: " + result.model_version), el("p", {}, "Маршрут: " + result.route + " · класс: " + (result.score?.grade || "—") + " · итог: " + shown(result.score?.total)), el("code", {}, result.definition_sha256), el("p", {}, "Дата оценки: " + result.evaluation_date), result.score ? null : el("p", {class: "notice warning"}, "Итогового балла нет. Изолированные значения критериев не являются итоговой оценкой."), table(["Причина", "Сообщение", "Поля"], result.reasons.map(r => [r.code, r.message, r.fields.join(", ")])), result.missing.length ? el("p", {class: "notice warning"}, "Нужны данные: " + result.missing.join(", ")) : null,
    el("h3", {}, "Критерии"), table(["Критерий", "Значение / ответ", "Баллы", "В итоговом балле", "Вариант"], result.criteria.map(c => [c.id + " · " + c.title, c.answer || valueText(c.value), shown(c.points) + " / " + c.max_points, c.contributes_to_score ? "Да" : "Нет", c.case_id + (c.special ? " · особый" : "") + (c.unknown ? " · неизвестно" : "") + (c.skipped ? " · пропущен" : "")])),
    el("details", {class: "subpanel"}, el("summary", {}, "Исходные и эффективные параметры"), table(["Параметр", "Исходное", "После проверки", "Источник"], result.parameters.map(p => [p.id, valueText(p.raw), valueText(p.effective), p.source.kind + ": " + p.source.path]))),
    el("details", {class: "subpanel"}, el("summary", {}, "Проверки и правила"), table(["Проверка", "Статус", "Пройдена"], result.checks.map(c => [c.id, c.status, c.passed ? "Да" : c.skipped ? "Пропущена" : "Нет"])), table(["Правило", "Условие", "Подтверждено", "Применено"], result.rules.map(r => [r.id, r.status, String(r.confirmed), String(r.applied)]))), raw("Полный результат JSON", result));
}
function comparisonView(result) {
  return el("div", {}, el("section", {class: "panel"}, el("h2", {}, "Сравнение расчётов"), el("p", {}, result.left.route + " → " + result.right.route + "; изменение итога: " + shown(result.total_delta)), table(["Критерий", "Слева", "Справа", "Разница баллов"], result.criteria.map(c => [c.id, c.left ? shown(c.left.points) : "—", c.right ? shown(c.right.points) : "—", shown(c.points_delta)])), raw("Изменения определения", result.definition)), simulationView(result.left), simulationView(result.right));
}

export async function workbenchPage(query) {
  const [models, drafts, releases] = await Promise.all([allRows("/api/admin/v1/models"), allRows("/api/admin/v1/drafts"), allRows("/api/admin/v1/releases")]);
  const entries = [
    ...models.map(m => ({label: m.definition.title + " · " + m.version, definition: m.definition, ref: {kind: "PUBLISHED", id: m.version, revision: null}})),
    ...drafts.map(d => ({label: d.definition.title + " · " + d.definition.version + " · ревизия " + d.revision, definition: d.definition, ref: {kind: "REVISION", id: d.id, revision: d.revision}}))
  ];
  if (query.get("id") && query.get("kind") === "REVISION" && query.get("revision")) {
    const id = query.get("id"), revision = query.get("revision");
    const row = await api("/api/admin/v1/drafts/" + encodeURIComponent(id) + "/revisions/" + encodeURIComponent(revision));
    if (!entries.some(e => e.ref.kind === "REVISION" && e.ref.id === id && String(e.ref.revision) === revision)) entries.push({label: row.definition.version + " · историческая ревизия " + row.revision, definition: row.definition, ref: {kind: "REVISION", id, revision: row.revision}});
  }
  if (!entries.length) return empty("Моделей пока нет", "Создайте черновик для проверки.");
  const preferred = entries.findIndex(e => e.ref.kind === query.get("kind") && e.ref.id === query.get("id") && (e.ref.kind === "PUBLISHED" || String(e.ref.revision) === query.get("revision")));
  const options = entries.map((e, i) => [String(i), e.label]);
  const target = select(options, String(Math.max(0, preferred))), left = select(options, "0");
  const selected = () => entries[Number(target.value)], baseline = () => entries[Number(left.value)];
  const sample = area("", 14), upload = el("input", {type: "file", accept: ".json,application/json"}), output = el("div", {"aria-live": "polite"}), errors = el("div", {"aria-live": "polite"});
  let busy = false;
  const controls = [];
  const run = async action => {
    if (busy) return; busy = true; controls.forEach(b => { b.disabled = true; }); errors.replaceChildren();
    try { await action(); } catch (error) { errors.replaceChildren(problemView(error)); } finally { busy = false; controls.forEach(b => { b.disabled = false; }); updatePrepare(); }
  };
  upload.addEventListener("change", () => run(async () => {
    const file = upload.files[0]; if (!file) return;
    if (file.size > 4 * 1024 * 1024) throw new Error("Набор должен быть не больше 4 MiB.");
    const data = await file.text(); parseJSON(data); sample.value = data; output.replaceChildren();
  }));
  const readSample = () => {
    if (new TextEncoder().encode(sample.value).length > 4 * 1024 * 1024) throw new Error("Набор должен быть не больше 4 MiB.");
    return parseJSON(sample.value);
  };
  const single = button("Выполнить расчёт", () => run(async () => { output.replaceChildren(simulationView(await api("/api/admin/v1/simulations", {method: "POST", body: {model: selected().ref, sample: readSample()}}))); }), "primary");
  const compare = button("Сравнить расчёты", () => run(async () => { output.replaceChildren(comparisonView(await api("/api/admin/v1/simulations/compare", {method: "POST", body: {left: baseline().ref, right: selected().ref, sample: readSample()}}))); }));
  const diff = button("Показать изменения модели", () => run(async () => {
    const result = await api("/api/admin/v1/compare", {method: "POST", body: {left: baseline().ref, right: selected().ref}});
    output.replaceChildren(el("section", {class: "panel"}, el("h2", {}, "Изменения определения"), el("p", {}, result.comparison.changed_paths.length ? result.comparison.changed_paths.join("\n") : "Определения совпадают."), raw("Контрольные суммы и пути", result), raw("Исходное определение", baseline().definition), raw("Новое определение", selected().definition)));
  }));
  const version = text("", {placeholder: "Например: 3.0.0"}), base = select(releases.map(r => [r.version, r.version]), releases[0]?.version), notes = area(""), reason = area("");
  const publishedChoices = models.map(row => ({row, input: check(true)}));
  const draftChoices = drafts.map(row => ({row, input: check(selected().ref.kind === "REVISION" && selected().ref.id === row.id)}));
  const releaseOutput = el("div", {"aria-live": "polite"});
  let previewKey = "";
  const request = () => ({version: requiredText(version), base_version: requiredText(base), published_versions: publishedChoices.filter(c => c.input.checked).map(c => c.row.version), drafts: draftChoices.filter(c => c.input.checked).map(c => ({id: c.row.id, revision: c.row.revision})), notes: notes.value.trim()});
  const updatePrepare = () => { try { prepare.disabled = busy || !previewKey || stringifyJSON(request()) !== previewKey; } catch { prepare.disabled = true; } };
  const preview = button("Проверить и показать документы", () => run(async () => {
    previewKey = ""; const body = request();
    const result = await api("/api/admin/v1/releases/preview", {method: "POST", body: {release: body}});
    previewKey = stringifyJSON(body);
    releaseOutput.replaceChildren(el("p", {class: "notice success"}, "Комплект прошёл проверку. Подготовка ещё не выполнена."), raw("Состав и SHA-256", result.manifest), ...result.documents.map(doc => el("details", {class: "panel"}, el("summary", {}, doc.path), el("pre", {class: "code"}, doc.content))));
  }));
  const prepare = button("Подготовить релиз", () => run(async () => {
    if (!reason.value.trim()) throw new Error("Укажите основание подготовки релиза.");
    const body = request(); if (stringifyJSON(body) !== previewKey) throw new Error("После изменения состава повторите проверку комплекта.");
    const result = await api("/api/admin/v1/releases", {method: "POST", body: {release: body, reason: reason.value.trim()}});
    previewKey = "";
    releaseOutput.replaceChildren(el("p", {class: "notice success"}, "Релиз " + result.version + " подготовлен. Опубликованные модели можно применить в разделе «Модели» без перезапуска."), el("a", {class: "button primary", href: "/api/admin/v1/releases/" + encodeURIComponent(result.version) + "/archive", download: "credit-engine-models-" + result.version + ".zip"}, "Скачать комплект"), el("p", {class: "code"}, "SHA-256 ZIP: " + result.archive_sha256), raw("Подготовленный релиз", result));
  }), "primary");
  controls.push(single, compare, diff, preview, prepare);
  const releaseForm = el("section", {class: "panel"}, el("h2", {}, "Подготовка релиза"), el("p", {class: "notice info"}, "Выберите версии для экспортируемого комплекта. Все ревизии закреплены на момент открытия экрана. Подготовка не активирует модель."), el("div", {class: "form-grid"}, field("Новая версия релиза", version), field("Базовый подготовленный релиз", base)), field("Описание изменений", notes), el("h3", {}, "Опубликованные версии в составе"), publishedChoices.map(c => field(c.row.version, c.input)), el("h3", {}, "Ревизии черновиков в составе"), draftChoices.map(c => field(c.row.definition.version + " · ревизия " + c.row.revision, c.input)), field("Основание подготовки релиза", reason), el("div", {class: "actions"}, preview, prepare), releaseOutput);
  releaseForm.addEventListener("input", updatePrepare); releaseForm.addEventListener("change", updatePrepare); updatePrepare();
  return el("div", {}, el("h1", {}, "Проверка и выпуск"), el("p", {class: "muted"}, "Расчёт сохранённых версий и ревизий без операций банка"), errors, el("section", {class: "panel"}, el("div", {class: "form-grid"}, field("Модель для расчёта (справа)", target), field("Модель для сравнения (слева)", left)), field("Загрузить тестовый набор JSON", upload), field("Тестовый набор", sample, "Все пять ключей: evaluation_date, application, statements, facts_date, additional_facts. Дату задайте явно. Набор не сохраняется сервером."), el("div", {class: "actions"}, single, compare, diff)), output, releaseForm);
}
