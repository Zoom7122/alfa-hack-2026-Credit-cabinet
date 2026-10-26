import {api, message} from "./api.js";
import {el, button, field, table} from "./dom.js";
import {copyJSON, ExactNumber, stringifyJSON} from "./numbers.js";
import {maxima, dependencies, replaceItem} from "./model-edit.js";
import {text, area, select, check, requiredText, numeric, lines, optional, modal, conditionControl, optionalCondition, reasonControl, literalControl} from "./forms.js";

const names = {parameters: "Параметр", criteria: "Критерий", data_checks: "Проверка данных", stops: "Стоп", manual_reviews: "Рассмотрение"};
export function weightView(definition) {
  try {
    const value = maxima(definition);
    return el("div", {class: "score-summary", "aria-live": "polite"}, [["Анкета", value.questionnaire, 750n], ["Дополнительно", value.additional, 250n], ["Всего", value.total, 1000n]].map(([label, actual, expected]) => el("div", {class: "score-tile " + (actual === expected ? "" : "invalid")}, el("span", {}, label), el("strong", {}, String(actual)), el("small", {}, "из " + expected + (actual === expected ? "" : " · отклонение " + (actual > expected ? "+" : "") + (actual - expected))))), value.valid ? null : el("p", {class: "notice warning"}, "Выпуск заблокирован до восстановления максимумов 750 / 250 / 1000."));
  } catch { return el("p", {class: "notice danger"}, "Максимумы критериев должны быть целыми числами."); }
}

function dependencyNotice(definition, id) {
  const rows = dependencies(definition, id);
  return el("div", {class: "notice warning"}, el("p", {}, "Отключение или удаление не изменяет связанные правила автоматически. Перед выпуском нужно устранить недопустимые ссылки."), rows.length ? el("ul", {}, rows.map(row => el("li", {}, names[row.section] + ": " + row.title + " (" + row.id + ")" + (row.enabled ? "" : " · выключен")))) : el("p", {}, "Явных зависимостей в определении не найдено. Доверенные расчётные зависимости дополнительно проверит сервер."));
}
export function editParameter(state, index) {
  const d = state.definition, existing = d.parameters[index];
  const p = copyJSON(existing || {id: "", enabled: true, title: "", source: {kind: "SUPPLIER", path: ""}, json_name: "", type: "NUMBER", unit: "COUNT", number: {integer: true}, required: true});
  const supplier = p.source.kind === "SUPPLIER";
  const id = text(p.id, {required: true, pattern: "[a-z][a-z0-9_]*", disabled: Boolean(existing)});
  const title = text(p.title, {required: true}), description = area(p.description);
  const enabled = check(p.enabled), required = check(p.required);
  const jsonName = text(p.json_name, {required: true, disabled: !supplier, pattern: "[a-z][a-z0-9]*(_[a-z0-9]+)*"});
  const goName = text(p.go_name || "", {disabled: !supplier});
  const type = select(supplier ? [["NUMBER", "Число"], ["BOOLEAN", "true / false"]] : [p.type], p.type); type.disabled = !supplier;
  const unit = select(["RUB", "RATIO", "PERCENT", "COUNT", "MONTHS", "YEARS", "DAYS", "BOOLEAN", "CATEGORY", "DATE"], p.unit); unit.disabled = !supplier;
  const whole = check(p.number?.integer), lower = text(p.number?.lower?.value ?? ""), upper = text(p.number?.upper?.value ?? ""), lowerInc = check(p.number?.lower?.inclusive ?? true), upperInc = check(p.number?.upper?.inclusive ?? true);
  const numberBox = el("div", {class: "form-grid"}, field("Только целые", whole), field("Нижняя граница", lower), field("Включить нижнюю границу", lowerInc), field("Верхняя граница", upper), field("Включить верхнюю границу", upperInc));
  const updateType = () => { numberBox.hidden = type.value !== "NUMBER"; if (type.value === "BOOLEAN") unit.value = "BOOLEAN"; else if (unit.value === "BOOLEAN") unit.value = "COUNT"; };
  type.addEventListener("change", updateType); updateType();
  const requiredWhen = optionalCondition("Условие обязательности", p.required_when, d.parameters);
  const depends = area(p.depends_on?.join("\n"));
  const schema = text(d.facts_schema_version || "", {placeholder: "Например: ip-facts-v3"});
  modal(existing ? "Изменить параметр" : "Добавить параметр поставщика", el("div", {},
    el("div", {class: "form-grid"}, field("Идентификатор параметра", id, "Сохранённый ID постоянный; для другого ID создайте новый параметр."), field("Название параметра", title), field("Включён", enabled), field("Обязательный", required), field("JSON-имя", jsonName), field("Имя Go-поля (необязательно)", goName), field("Тип значения", type), field("Единица", unit)),
    supplier ? field("Версия схемы поставщика", schema, "При изменении активной wire-структуры задайте новую версию схемы.") : el("p", {class: "notice info"}, "Источник " + p.source.kind + ": " + p.source.path + ". Тип, путь и JSON-имя существующего контракта сохраняются."),
    field("Описание", description), numberBox, field("Зависит от параметров", depends, "Идентификаторы через перевод строки."), requiredWhen.node,
    existing ? dependencyNotice(d, p.id) : null),
  () => {
    p.id = requiredText(id); p.title = requiredText(title); p.enabled = enabled.checked; p.required = required.checked;
    optional(p, "description", description.value.trim());
    if (supplier) { p.type = type.value; p.unit = unit.value; p.json_name = requiredText(jsonName); p.source.path = p.json_name; optional(p, "go_name", goName.value.trim()); }
    if (p.type === "NUMBER") {
      p.number = {integer: whole.checked};
      if (lower.value.trim()) p.number.lower = {value: numeric(lower), inclusive: lowerInc.checked};
      if (upper.value.trim()) p.number.upper = {value: numeric(upper), inclusive: upperInc.checked};
    } else delete p.number;
    optional(p, "required_when", requiredWhen.read()); optional(p, "depends_on", depends.value.trim() ? lines(depends.value) : undefined);
    const next = replaceItem(d, "parameters", index, p);
    if (supplier) optional(next, "facts_schema_version", schema.value.trim());
    state.change(next);
  });
}

function scoreCases(initial, parameters, label) {
  const controls = [], list = el("div");
  function draw() { list.replaceChildren(...controls.map(c => el("details", {class: "subpanel", open: ""}, el("summary", {}, c.label), c.node, button("Удалить вариант", () => { controls.splice(controls.indexOf(c), 1); draw(); })))); }
  function add(item = {id: "", when: {op: "ALWAYS"}, points: new ExactNumber("0")}) {
    const id = text(item.id, {required: true}), points = text(item.points, {required: true}), description = text(item.description);
    const condition = conditionControl(item.when, parameters);
    const result = select([["", "Исходное значение"], ["NULL", "Неизвестное значение (null)"], ["VALUE", "Заданное значение"]], item.result?.mode || "");
    const literalType = select(["NUMBER", "BOOLEAN", "STRING"], item.result?.value?.boolean !== undefined ? "BOOLEAN" : item.result?.value?.string !== undefined ? "STRING" : "NUMBER");
    const literalBox = el("div"); let literal;
    const renderLiteral = () => { literal = literalControl(item.result?.value, literalType.value); literalBox.replaceChildren(field("Тип результата", literalType), field("Результат", literal.node)); literalBox.hidden = result.value !== "VALUE"; };
    result.addEventListener("change", renderLiteral); literalType.addEventListener("change", renderLiteral); renderLiteral();
    const useReason = check(Boolean(item.review_reason)), reason = reasonControl(item.review_reason); reason.node.hidden = !useReason.checked;
    useReason.addEventListener("change", () => { reason.node.hidden = !useReason.checked; });
    controls.push({label: item.id || "Новый вариант", node: el("div", {}, el("div", {class: "form-grid"}, field("ID варианта", id), field("Баллы", points)), condition.node, field("Описание варианта", description), field("Значение в результате", result), literalBox, field("Добавить основание рассмотрения", useReason), reason.node), read: () => {
      const row = {...copyJSON(item), id: requiredText(id), when: condition.read(), points: numeric(points)};
      optional(row, "description", description.value.trim());
      optional(row, "result", result.value ? {mode: result.value, ...(result.value === "VALUE" ? {value: literal.read()} : {})} : undefined);
      optional(row, "review_reason", useReason.checked ? reason.read() : undefined); return row;
    }}); draw();
  }
  initial.forEach(add);
  return {node: el("section", {}, el("h3", {}, label), list, button("Добавить вариант: " + label, () => add())), read: () => controls.map(c => c.read())};
}
export function editCriterion(state, index) {
  const d = state.definition, existing = d.criteria[index];
  const c = copyJSON(existing || {id: "", enabled: true, title: "", block: "QUESTIONNAIRE", max_points: new ExactNumber("0"), unknown_action: "NEED_INFO", special_cases: [], cases: []});
  const id = text(c.id, {required: true, disabled: Boolean(existing)}), title = text(c.title, {required: true}), enabled = check(c.enabled), description = area(c.description);
  const block = select([["QUESTIONNAIRE", "Анкета · 750"], ["ADDITIONAL", "Дополнительно · 250"]], c.block), max = text(c.max_points, {required: true});
  const value = select([["", "Без отдельного значения"], ...d.parameters.map(p => [p.id, p.title + " · " + p.id])], c.value_parameter || "");
  const action = select([["NEED_INFO", "Запросить данные"], ["ZERO", "Начислить ноль"], ["SKIP", "Пропустить"]], c.unknown_action);
  const when = optionalCondition("Условие применения критерия", c.applies_when, d.parameters);
  const cases = scoreCases(c.cases, d.parameters, "Балльная шкала"), special = scoreCases(c.special_cases, d.parameters, "Особые случаи");
  const answerEnabled = check(Boolean(c.answer)), answerTemplate = text(c.answer?.template || ""), unknownText = text(c.answer?.unknown_text || "");
  const answers = [], answerList = el("div");
  const drawAnswers = () => answerList.replaceChildren(...answers.map(a => el("div", {class: "subpanel"}, a.when.node, field("Текст ответа", a.template), button("Удалить текст ответа", () => { answers.splice(answers.indexOf(a), 1); drawAnswers(); }))));
  const addAnswer = (a = {}) => { answers.push({when: conditionControl(a.when, d.parameters), template: text(a.template || "")}); drawAnswers(); };
  (c.answer?.cases || []).forEach(addAnswer);
  const answerBox = el("div", {}, field("Шаблон ответа", answerTemplate, "Подстановка параметра: ${parameter_id}."), field("Ответ при неизвестности", unknownText), answerList, button("Добавить условный текст", () => addAnswer()));
  answerBox.hidden = !answerEnabled.checked; answerEnabled.addEventListener("change", () => { answerBox.hidden = !answerEnabled.checked; });
  const totals = el("div");
  const preview = () => { try { totals.replaceChildren(weightView(replaceItem(d, "criteria", index, {...c, max_points: numeric(max, true), block: block.value, enabled: enabled.checked}))); } catch { totals.replaceChildren(el("p", {class: "notice warning"}, "Введите целый максимум критерия.")); } };
  [max, block, enabled].forEach(input => input.addEventListener("input", preview)); preview();
  modal(existing ? "Изменить критерий" : "Добавить критерий", el("div", {}, totals, el("div", {class: "form-grid"}, field("Идентификатор критерия", id), field("Название критерия", title), field("Включён", enabled), field("Блок", block), field("Максимум баллов", max), field("Параметр результата", value), field("При неизвестных данных", action)), field("Описание", description), when.node, field("Текстовый ответ анкеты", answerEnabled), answerBox, special.node, cases.node), () => {
    Object.assign(c, {id: requiredText(id), title: requiredText(title), enabled: enabled.checked, block: block.value, max_points: numeric(max, true), unknown_action: action.value, cases: cases.read(), special_cases: special.read()});
    optional(c, "description", description.value.trim()); optional(c, "value_parameter", value.value); optional(c, "applies_when", when.read());
    optional(c, "answer", answerEnabled.checked ? {template: answerTemplate.value, ...(unknownText.value ? {unknown_text: unknownText.value} : {}), ...(answers.length ? {cases: answers.map(a => ({when: a.when.read(), template: a.template.value}))} : {})} : undefined);
    state.change(replaceItem(d, "criteria", index, c));
  });
}

export function createDraftEditor(draft, validation, onSaved) {
  const node = el("section", {class: "draft-editor"}), summary = el("div"), parameters = el("div"), criteria = el("div"), extra = el("div"), errors = el("div", {"aria-live": "polite"});
  const reason = area("", 2), status = el("p", {class: "muted"});
  let dirty = false, saving = false, active = true;
  const leave = event => { if (dirty) { event.preventDefault(); event.returnValue = ""; } };
  window.addEventListener("beforeunload", leave);
  const state = {definition: copyJSON(draft.definition), node, extra, change(next) { if (saving) throw new Error("Дождитесь завершения сохранения."); state.definition = next; dirty = true; errors.replaceChildren(); draw(); }, get dirty() { return dirty; }, dispose() { active = false; window.removeEventListener("beforeunload", leave); }, canLeave: () => !dirty || window.confirm("Есть несохранённые изменения черновика. Покинуть редактор?")};
  function remove(section, index) {
    const item = state.definition[section][index];
    modal("Удалить " + (names[section] || "объект"), el("div", {}, el("p", {}, item.title + " (" + item.id + ")"), section === "parameters" ? dependencyNotice(state.definition, item.id) : el("p", {}, "Связанные ссылки будут проверены перед выпуском.")), () => { const next = copyJSON(state.definition); next[section].splice(index, 1); state.change(next); }, "Удалить из черновика");
  }
  const save = button("Сохранить новую ревизию", async () => {
    if (saving) return;
    errors.replaceChildren();
    if (!reason.value.trim()) { errors.append(el("p", {class: "notice danger"}, "Укажите основание изменения.")); return; }
    saving = true; save.disabled = true;
    try {
      const result = await api("/api/admin/v1/drafts/" + encodeURIComponent(draft.id), {method: "PUT", body: {expected_revision: draft.revision, definition: state.definition, reason: reason.value.trim()}});
      dirty = false; if (active) onSaved(result);
    } catch (error) {
      if (active) errors.replaceChildren(el("div", {class: "notice danger", role: "alert"}, el("p", {}, message(error)), error.problem?.current_revision ? el("p", {}, "Текущая серверная ревизия: " + error.problem.current_revision + ". Ваши изменения сохранены в этом окне; скачайте их перед обновлением.") : null, button("Скачать локальное определение", () => downloadDefinition(state.definition))));
    } finally { saving = false; save.disabled = false; }
  }, "primary");
  const extensions = [];
  state.addSection = renderer => { extensions.push(renderer); draw(); };
  function draw() {
    summary.replaceChildren(weightView(state.definition));
    status.textContent = dirty ? "Есть несохранённые изменения. Серверная проверка относится к сохранённой ревизии." : "Открыта сохранённая ревизия " + draft.revision + ".";
    parameters.replaceChildren(el("div", {class: "page-heading"}, el("h2", {}, "Параметры"), button("Добавить параметр", () => editParameter(state, -1))), table(["Название / ID", "Тип / источник", "Состояние", "Действия"], state.definition.parameters.map((p, index) => [el("span", {}, p.title, el("code", {class: "block"}, p.id)), p.type + " · " + p.source.kind + " · " + p.json_name, p.enabled ? "Включён" : "Выключен", el("div", {class: "actions"}, button("Изменить параметр " + p.id, () => editParameter(state, index)), button("Удалить параметр " + p.id, () => remove("parameters", index)))])));
    criteria.replaceChildren(el("div", {class: "page-heading"}, el("h2", {}, "Критерии и баллы"), button("Добавить критерий", () => editCriterion(state, -1))), table(["Название / ID", "Блок", "Максимум", "Действия"], state.definition.criteria.map((c, index) => [el("span", {}, c.title, el("code", {class: "block"}, c.id), c.enabled ? "" : " · выключен"), c.block === "QUESTIONNAIRE" ? "Анкета" : "Дополнительно", String(c.max_points), el("div", {class: "actions"}, button("Изменить критерий " + c.id, () => editCriterion(state, index)), button("Удалить критерий " + c.id, () => remove("criteria", index)))])));
    extra.replaceChildren(...extensions.map(renderer => renderer(state)));
  }
  node.append(el("p", {class: "notice info"}, "Изменения сохраняются в черновике. Для включения изменений примените проверенную сохранённую ревизию."), summary, el("section", {class: "panel"}, status, field("Основание изменения", reason), errors, el("div", {class: "actions"}, save, button("Скачать определение", () => downloadDefinition(state.definition)))), el("section", {class: "panel"}, parameters), el("section", {class: "panel"}, criteria), extra);
  draw(); return state;
}
export function downloadDefinition(definition) {
  const url = URL.createObjectURL(new Blob([stringifyJSON(definition, true)], {type: "application/json"}));
  const a = el("a", {href: url, download: definition.version + ".json"}); a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
}
