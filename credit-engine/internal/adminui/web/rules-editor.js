import {el, button, field, table} from "./dom.js";
import {copyJSON} from "./numbers.js";
import {text, area, select, check, requiredText, numeric, lines, optional, modal, conditionControl, optionalCondition, reasonControl} from "./forms.js";
import {conditionReferences, rulePhase, classIssues, replaceItem} from "./model-edit.js";

function editRule(state, section, index) {
  const d = state.definition, old = d[section][index];
  const row = copyJSON(old || {id: "", title: "", enabled: true, when: {op: "ALWAYS"}, reason: {code: "", message: "", fields: []}});
  const id = text(row.id, {required: true, disabled: Boolean(old)}), title = text(row.title, {required: true}), enabled = check(row.enabled);
  const condition = conditionControl(row.when, d.parameters), reason = reasonControl(row.reason);
  const checks = area(row.requires_checks?.join("\n")), after = select([["", "Не привязан к критерию"], ...d.criteria.map(c => [c.id, c.title + " · " + c.id])], row.after_criterion || "");
  modal(section === "stops" ? "Стоп-фактор" : "Основание рассмотрения", el("div", {}, field("ID правила", id), field("Название правила", title), field("Правило включено", enabled), el("p", {class: "notice info"}, "Правило подтверждено только при TRUE. UNKNOWN не равен FALSE и не подтверждает стоп. " + rulePhase(d, row, section === "manual_reviews")), condition.node, reason.node, field("Дождаться проверок данных", checks, "ID проверок через перевод строки. Доступны: " + d.data_checks.map(c => c.id).join(", ")), field("После критерия", after)), () => {
    Object.assign(row, {id: requiredText(id), title: requiredText(title), enabled: enabled.checked, when: condition.read(), reason: reason.read()});
    const ids = lines(checks.value);
    if (ids.some(checkID => !d.data_checks.some(c => c.id === checkID))) throw new Error("Указана неизвестная проверка данных.");
    optional(row, "requires_checks", ids.length ? ids : undefined); optional(row, "after_criterion", after.value);
    state.change(replaceItem(d, section, index, row));
  });
}
function editCheck(state, index) {
  const d = state.definition, old = d.data_checks[index];
  const row = copyJSON(old || {id: "", title: "", enabled: true, satisfied_when: {op: "KNOWN", parameter: ""}, fields: [], message: ""});
  const id = text(row.id, {required: true, disabled: Boolean(old)}), title = text(row.title, {required: true}), enabled = check(row.enabled), fields = area(row.fields.join("\n")), message = area(row.message);
  const satisfied = conditionControl(row.satisfied_when, d.parameters), when = optionalCondition("Условие применения проверки", row.applies_when, d.parameters);
  modal("Проверка полноты данных", el("div", {}, field("ID проверки", id), field("Название проверки", title), field("Проверка включена", enabled), when.node, el("h3", {}, "Условие достаточности"), satisfied.node, field("Пути недостающих полей", fields), field("Сообщение о нехватке данных", message)), () => {
    Object.assign(row, {id: requiredText(id), title: requiredText(title), enabled: enabled.checked, fields: lines(fields.value), message: requiredText(message), satisfied_when: satisfied.read()});
    optional(row, "applies_when", when.read()); state.change(replaceItem(d, "data_checks", index, row));
  });
}
function editClasses(state) {
  const controls = ["D", "C", "B", "A"].map(grade => {
    const old = state.definition.classes.find(row => row.grade === grade);
    return {grade, min: text(old?.min_total ?? "", {required: true}), max: text(old?.max_total ?? "", {required: true}), reason: ["C", "D"].includes(grade) ? reasonControl(old?.reason) : null};
  });
  const errors = el("div", {"aria-live": "polite"});
  const read = () => controls.map(c => ({grade: c.grade, min_total: numeric(c.min, true), max_total: numeric(c.max, true), ...(c.reason ? {reason: c.reason.read()} : {})}));
  const preview = () => {
    try { const rows = controls.map(c => ({grade: c.grade, min_total: numeric(c.min, true), max_total: numeric(c.max, true)})); const issues = classIssues(rows); errors.replaceChildren(el("p", {class: "notice " + (issues.length ? "warning" : "success")}, issues.length ? issues.join(" ") : "Шкала непрерывно покрывает 0–1000.")); }
    catch { errors.replaceChildren(el("p", {class: "notice warning"}, "Заполните целые границы всех классов.")); }
  };
  controls.forEach(c => [c.min, c.max].forEach(input => input.addEventListener("input", preview))); preview();
  modal("Границы классов", el("div", {}, el("p", {class: "notice info"}, "Границы включительные. Маршруты фиксированы: D — отказ, C — рассмотрение, A/B — подбор при отсутствии более приоритетных оснований."), errors, controls.map(c => el("section", {class: "subpanel"}, el("h3", {}, "Класс " + c.grade), el("div", {class: "form-grid"}, field("Минимум " + c.grade, c.min), field("Максимум " + c.grade, c.max)), c.reason?.node))), () => {
    const classes = read(), issues = classIssues(classes); if (issues.length) throw new Error(issues.join(" "));
    state.change({...copyJSON(state.definition), classes});
  });
}
function remove(state, section, index) {
  const row = state.definition[section][index];
  const links = section === "data_checks" ? [...state.definition.stops, ...state.definition.manual_reviews].filter(r => r.requires_checks?.includes(row.id)).map(r => r.id) : [];
  modal("Удалить правило", el("p", {}, row.title + (links.length ? ". На проверку ссылаются правила: " + links.join(", ") + ". Перед выпуском устраните ссылки." : ". Связи повторно проверяются сервером перед выпуском.")), () => {
    const next = copyJSON(state.definition); next[section].splice(index, 1); state.change(next);
  }, "Удалить из черновика");
}
export function rulesSection(state) {
  const d = state.definition;
  const sections = [["data_checks", "Проверки данных"], ["stops", "Стоп-факторы"], ["manual_reviews", "Основания рассмотрения"]];
  return el("div", {}, sections.map(([section, title]) => el("section", {class: "panel"}, el("div", {class: "page-heading"}, el("h2", {}, title), button("Добавить: " + title, () => section === "data_checks" ? editCheck(state, -1) : editRule(state, section, -1))), table(["Правило", "Условия / фаза", "Связанные критерии", "Действия"], d[section].map((row, index) => {
    const refs = conditionReferences(row.when || row.satisfied_when);
    const criteria = d.criteria.filter(c => refs.includes(c.value_parameter) || refs.some(id => conditionReferences(c.applies_when).includes(id) || [...c.cases, ...c.special_cases].some(item => conditionReferences(item.when).includes(id)))).map(c => c.id);
    return [el("span", {}, row.title, el("code", {class: "block"}, row.id), row.enabled ? "" : " · выключено"), el("span", {}, rulePhase(d, row, section === "manual_reviews"), el("small", {class: "block muted"}, "Параметры: " + (refs.join(", ") || "нет"))), criteria.join(", ") || "нет", el("div", {class: "actions"}, button("Изменить правило " + row.id, () => section === "data_checks" ? editCheck(state, index) : editRule(state, section, index)), button("Удалить правило " + row.id, () => remove(state, section, index)))];
  })))), el("section", {class: "panel"}, el("div", {class: "page-heading"}, el("h2", {}, "Классы"), button("Изменить классы", () => editClasses(state))), table(["Класс", "От (включительно)", "До (включительно)"], d.classes.map(c => [c.grade, String(c.min_total), String(c.max_total)])), classIssues(d.classes).map(issue => el("p", {class: "notice warning"}, issue))), el("section", {class: "panel"}, el("h2", {}, "Название и описание"), button("Изменить описание модели", () => {
    const title = text(d.title, {required: true}), description = area(d.description), schema = text(d.facts_schema_version || "");
    modal("Описание модели", el("div", {}, field("Название модели", title), field("Описание модели", description), field("Версия схемы фактов", schema)), () => {
      const next = copyJSON(d); next.title = requiredText(title); optional(next, "description", description.value.trim()); optional(next, "facts_schema_version", schema.value.trim()); state.change(next);
    });
  })));
}
