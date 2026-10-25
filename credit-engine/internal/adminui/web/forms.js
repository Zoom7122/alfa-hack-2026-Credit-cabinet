import {el, button, field} from "./dom.js";
import {ExactNumber, copyJSON, parseJSON, stringifyJSON, integer} from "./numbers.js";

export const text = (value = "", attributes = {}) => el("input", {value, ...attributes});
export const area = (value = "", rows = 3) => el("textarea", {rows}, value);
export function select(values, value) {
  const rows = values.map(item => Array.isArray(item) ? item : [item, item]);
  if (value !== undefined && !rows.some(([key]) => key === value)) rows.push([value, value]);
  const input = el("select", {}, rows.map(([key, label]) => el("option", {value: key}, label)));
  input.value = value ?? rows[0]?.[0] ?? "";
  return input;
}
export const check = (value = false) => el("input", {type: "checkbox", checked: value});
export const requiredText = input => { const value = input.value.trim(); if (!value) throw new Error("Заполните обязательные поля."); return value; };
export function numeric(input, whole = false) {
  const result = new ExactNumber(requiredText(input));
  if (!Number.isFinite(Number(String(result)))) throw new Error("Требуется конечное число.");
  if (whole) integer(result);
  return result;
}
export const lines = value => value.split(/[\n,]/).map(item => item.trim()).filter(Boolean);
export function optional(object, key, value) { if (value === undefined || value === "") delete object[key]; else object[key] = value; }
export function jsonControl(label, value, help = "") {
  const input = area(value === undefined ? "" : stringifyJSON(value, true), 5);
  return {node: field(label, input, help), read: () => input.value.trim() ? parseJSON(input.value) : undefined};
}
export function modal(title, body, apply, label = "Применить к черновику") {
  const errors = el("div", {"aria-live": "polite"});
  const dialog = el("dialog", {class: "dialog editor-dialog", "aria-label": title});
  const close = () => { dialog.close(); dialog.remove(); };
  const form = el("form", {onsubmit: async event => {
    event.preventDefault(); errors.replaceChildren();
    try { await apply(); close(); } catch (error) { errors.replaceChildren(el("p", {class: "notice danger", role: "alert"}, error.message)); }
  }}, body, errors, el("div", {class: "actions"}, button("Отмена", close), el("button", {type: "submit", class: "button primary"}, label)));
  dialog.append(el("h2", {}, title), form);
  dialog.addEventListener("cancel", () => dialog.remove());
  document.body.append(dialog); dialog.showModal();
  return dialog;
}
export function reasonControl(value = undefined) {
  const code = text(value?.code), message = area(value?.message), fields = area(value?.fields?.join("\n"));
  return {node: el("div", {class: "form-grid"}, field("Код причины", code), field("Текст причины", message), field("Пути полей причины", fields, "Один путь на строку.")), read: () => ({code: requiredText(code), message: requiredText(message), fields: lines(fields.value)})};
}

export function literalControl(value, type = "NUMBER") {
  const input = type === "BOOLEAN" ? select([["true", "true"], ["false", "false"]], String(value?.boolean ?? true)) : text(type === "STRING" ? value?.string : value?.number ?? "");
  return {node: input, read: () => type === "BOOLEAN" ? {boolean: input.value === "true"} : type === "STRING" ? {string: input.value} : {number: numeric(input)}};
}

// This is a typed condition tree, never executable expression text.
export function conditionControl(value, parameters, depth = 0) {
  if (depth > 24) throw new Error("Условие слишком глубоко для визуального редактора.");
  const initial = copyJSON(value || {op: "ALWAYS"});
  const node = el("div", {class: "condition"});
  const op = select([["ALWAYS", "Всегда"], ["NEVER", "Никогда"], ["ALL", "Все условия (И)"], ["ANY", "Любое условие (ИЛИ)"], ["NOT", "НЕ"], ["KNOWN", "Значение известно"], ["UNKNOWN", "Значение неизвестно"], ["EQ", "="], ["NE", "≠"], ["LT", "<"], ["LE", "≤"], ["GT", ">"], ["GE", "≥"], ["IN", "В списке"], ["NOT_IN", "Не в списке"]], initial.op);
  const body = el("div");
  let readBody = () => ({});
  function render(seed) {
    body.replaceChildren();
    if (["ALL", "ANY", "NOT"].includes(op.value)) {
      const children = [], list = el("div");
      const draw = () => list.replaceChildren(...children.map(child => el("div", {class: "condition-child"}, child.node, op.value !== "NOT" ? button("Удалить условие", () => { children.splice(children.indexOf(child), 1); draw(); }) : null)));
      const add = item => { children.push(conditionControl(item, parameters, depth + 1)); draw(); };
      (seed.conditions?.length ? seed.conditions : [{op: "ALWAYS"}]).forEach(add);
      body.append(list);
      if (op.value !== "NOT") body.append(button("Добавить условие", () => add({op: "ALWAYS"})));
      readBody = () => ({conditions: children.map(child => child.read())});
    } else if (!["ALWAYS", "NEVER"].includes(op.value)) {
      const parameter = select([["", "Выберите параметр"], ...parameters.map(p => [p.id, p.title + " · " + p.id + (p.enabled ? "" : " (выключен)")])], seed.parameter || "");
      const operands = el("div");
      let readOperands = () => ({});
      const redraw = (operandSeed = {}) => {
        operands.replaceChildren();
        if (["KNOWN", "UNKNOWN"].includes(op.value)) { readOperands = () => ({}); return; }
        const type = parameters.find(p => p.id === parameter.value)?.type || "NUMBER";
        if (["IN", "NOT_IN"].includes(op.value)) {
          const controls = [], rows = el("div");
          const draw = () => rows.replaceChildren(...controls.map(control => el("div", {class: "actions"}, control.node, button("Убрать значение", () => { controls.splice(controls.indexOf(control), 1); draw(); }))));
          const add = v => { controls.push(literalControl(v, type)); draw(); };
          (operandSeed.values?.length ? operandSeed.values : [undefined]).forEach(add);
          operands.append(rows, button("Добавить значение", () => add()));
          readOperands = () => { if (!controls.length) throw new Error("Список значений не может быть пустым."); return {values: controls.map(c => c.read())}; };
        } else {
          const mode = select([["literal", "Заданное значение"], ["parameter", "Другой параметр"]], operandSeed.other_parameter ? "parameter" : "literal");
          const slot = el("div"); let read = () => ({});
          const draw = () => {
            if (mode.value === "parameter") {
              const other = select([["", "Выберите параметр"], ...parameters.filter(p => p.type === type).map(p => [p.id, p.title + " · " + p.id])], operandSeed.other_parameter || "");
              slot.replaceChildren(field("Другой параметр", other)); read = () => ({other_parameter: requiredText(other)});
            } else { const literal = literalControl(operandSeed.value, type); slot.replaceChildren(field("Значение", literal.node)); read = () => ({value: literal.read()}); }
          };
          mode.addEventListener("change", draw); draw();
          operands.append(field("Сравнить с", mode), slot); readOperands = () => read();
        }
      };
      parameter.addEventListener("change", () => redraw()); redraw(seed);
      body.append(field("Параметр", parameter), operands);
      readBody = () => ({parameter: requiredText(parameter), ...readOperands()});
    } else readBody = () => ({});
  }
  op.addEventListener("change", () => render({})); render(initial);
  node.append(field("Оператор", op), body);
  return {node, read: () => ({op: op.value, ...readBody()})};
}

export function optionalCondition(label, value, parameters) {
  const enabled = check(Boolean(value)), control = conditionControl(value, parameters);
  control.node.hidden = !enabled.checked;
  enabled.addEventListener("change", () => { control.node.hidden = !enabled.checked; });
  return {node: el("details", {class: "subpanel"}, el("summary", {}, label), field("Использовать условие", enabled), control.node), read: () => enabled.checked ? control.read() : undefined};
}
