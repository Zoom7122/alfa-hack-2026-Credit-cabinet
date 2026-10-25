export function el(tag, attributes = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attributes)) {
    if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else if (key === "class") node.className = value;
    else if (key === "text") node.textContent = value;
    else if (key === "value") node.value = String(value);
    else if (key === "checked" || key === "disabled" || key === "hidden" || key === "required") node[key] = Boolean(value);
    else if (value !== null && value !== undefined) node.setAttribute(key, String(value));
  }
  const append = value => { if (Array.isArray(value)) value.forEach(append); else if (value !== null && value !== undefined && value !== false) node.append(value instanceof Node ? value : document.createTextNode(String(value))); };
  children.forEach(append);
  return node;
}
export const button = (label, action, style = "secondary") => el("button", {type: "button", class: "button " + style, onclick: action}, label);
let fieldID = 0;
export function field(label, input, help = "") {
  const id = input.id || "field-" + (++fieldID);
  input.id = id;
  if (help) input.setAttribute("aria-describedby", id + "-help");
  return el("div", {class: "field"}, el("label", {class: "field-label", for: id}, label), input, help ? el("small", {id: id + "-help"}, help) : null);
}
export const badge = (label, style = "neutral") => el("span", {class: "badge " + style}, label);
export function table(headers, rows) {
  return el("div", {class: "table-scroll"}, el("table", {}, el("thead", {}, el("tr", {}, headers.map(header => el("th", {scope: "col"}, header)))), el("tbody", {}, rows.map(row => el("tr", {}, row.map(cell => el("td", {}, cell)))))));
}
export const empty = (title, description) => el("div", {class: "empty"}, el("h2", {}, title), el("p", {class: "muted"}, description));
export const date = value => value ? new Intl.DateTimeFormat("ru-RU", {dateStyle: "medium", timeStyle: "short"}).format(new Date(value)) : "—";
export const borrower = value => value === "SOLE_PROPRIETOR" ? "ИП" : value === "LEGAL_ENTITY" ? "ЮЛ" : String(value);
