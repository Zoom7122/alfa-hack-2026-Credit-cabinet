import {copyJSON, integer} from "./numbers.js";

export function maxima(definition) {
  let questionnaire = 0n, additional = 0n;
  for (const item of definition.criteria) if (item.enabled) {
    const points = integer(item.max_points);
    if (item.block === "QUESTIONNAIRE") questionnaire += points;
    if (item.block === "ADDITIONAL") additional += points;
  }
  return {questionnaire, additional, total: questionnaire + additional, valid: questionnaire === 750n && additional === 250n};
}

// Include disabled objects too: removing a source must not silently lose its links.
export function dependencies(definition, id) {
  const result = [];
  const matches = (value, key) => {
    if (Array.isArray(value)) return value.some(item => matches(item, key));
    if (value && typeof value === "object") return Object.entries(value).some(([k, v]) => matches(v, k));
    return (["parameter", "other_parameter", "value_parameter", "depends_on"].includes(key) && value === id) ||
      (typeof value === "string" && ["template", "unknown_text"].includes(key) && value.includes("${" + id + "}"));
  };
  for (const section of ["parameters", "criteria", "data_checks", "stops", "manual_reviews"]) for (const item of definition[section]) {
    if (section === "parameters" && item.id === id) continue;
    if (matches(item, "")) result.push({section, id: item.id, title: item.title, enabled: item.enabled});
  }
  return result;
}

export function replaceItem(definition, section, index, item) {
  const result = copyJSON(definition);
  if (result[section].some((row, i) => i !== index && row.id === item.id)) throw new Error("Идентификатор уже используется в этом разделе.");
  if (index < 0) result[section].push(copyJSON(item)); else result[section][index] = copyJSON(item);
  return result;
}

export function conditionReferences(value) {
  const ids = new Set();
  function walk(item) {
    if (!item || typeof item !== "object") return;
    if (item.parameter) ids.add(item.parameter);
    if (item.other_parameter) ids.add(item.other_parameter);
    for (const child of item.conditions || []) walk(child);
  }
  walk(value); return [...ids];
}
export function rulePhase(definition, rule, manual = false) {
  const seen = new Set(), sources = new Set();
  function visit(id) {
    if (seen.has(id)) return; seen.add(id);
    const p = definition.parameters.find(item => item.id === id);
    if (!p) { sources.add("неизвестная ссылка " + id); return; }
    sources.add(({APPLICATION: "заявка А", SUPPLIER: "ответ поставщика", STATEMENTS: "отчётность Б", COMPUTED: "подготовленные показатели"})[p.source.kind] || p.source.kind);
    (p.depends_on || []).forEach(visit);
  }
  conditionReferences(rule.when || rule.satisfied_when).forEach(visit);
  return (manual ? "После скоринга; " : "По мере доступности данных; ") + "источники: " + ([...sources].join(", ") || "нет") +
    (rule.requires_checks?.length ? "; после проверок: " + rule.requires_checks.join(", ") : "") +
    (rule.after_criterion ? "; после критерия " + rule.after_criterion : "");
}
export function classIssues(classes) {
  const errors = [], order = ["D", "C", "B", "A"];
  if (classes.length !== 4) return ["Нужны ровно четыре класса D, C, B, A."];
  let next = 0n;
  for (const grade of order) {
    const rows = classes.filter(row => row.grade === grade);
    if (rows.length !== 1) { errors.push("Класс " + grade + " должен встречаться один раз."); continue; }
    try {
      const min = integer(rows[0].min_total), max = integer(rows[0].max_total);
      if (min !== next || max < min || min < 0n || max > 1000n) errors.push("Класс " + grade + ": разрыв, пересечение или неверные границы.");
      next = max + 1n;
    } catch { errors.push("Класс " + grade + ": нужны целые границы."); }
  }
  if (next !== 1001n) errors.push("Шкала должна покрывать 0–1000 полностью.");
  return errors;
}
