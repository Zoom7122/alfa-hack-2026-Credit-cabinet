// JSON numbers remain tokens throughout model editing, including int64 and
// decimal literals. Never convert a complete model through JSON.parse/stringify.
const numericToken = /^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?$/;

export class ExactNumber {
  constructor(token) {
    if (typeof token !== "string" || token.length > 256 || !numericToken.test(token)) throw new TypeError("Некорректное число JSON");
    this.token = token;
    Object.freeze(this);
  }
  toString() { return this.token; }
  toJSON() { throw new TypeError("Числа модели требуют stringifyJSON"); }
}

export function parseJSON(text) {
  if (typeof text !== "string" || text.length > 128 * 1024 * 1024) throw new SyntaxError("Слишком большой ответ");
  let position = 0;
  const error = () => { throw new SyntaxError("Некорректный JSON"); };
  const space = () => { while (/[\t\n\r ]/.test(text[position] ?? "") && position < text.length) position++; };
  function string() {
    const start = position++;
    while (position < text.length) {
      const char = text[position++];
      if (char === '"') return JSON.parse(text.slice(start, position));
      if (char === "\\") position++;
    }
    return error();
  }
  function value(depth) {
    if (depth > 96) return error();
    space();
    const char = text[position];
    if (char === '"') return string();
    if (char === "{") {
      position++; space(); const result = Object.create(null);
      if (text[position] === "}") { position++; return result; }
      while (true) {
        space(); if (text[position] !== '"') return error();
        const key = string(); space(); if (text[position++] !== ":" || Object.hasOwn(result, key)) return error();
        result[key] = value(depth + 1); space();
        const next = text[position++]; if (next === "}") return result; if (next !== ",") return error();
      }
    }
    if (char === "[") {
      position++; space(); const result = [];
      if (text[position] === "]") { position++; return result; }
      while (true) { result.push(value(depth + 1)); space(); const next = text[position++]; if (next === "]") return result; if (next !== ",") return error(); }
    }
    for (const [token, decoded] of [["true", true], ["false", false], ["null", null]]) {
      if (text.startsWith(token, position)) { position += token.length; return decoded; }
    }
    const start = position;
    while (position < text.length && /[0-9eE+.-]/.test(text[position])) position++;
    if (start === position) return error();
    return new ExactNumber(text.slice(start, position));
  }
  const result = value(0); space(); if (position !== text.length) return error(); return result;
}

export function stringifyJSON(value, pretty = false) {
  const indent = depth => pretty ? "  ".repeat(depth) : "";
  const newline = pretty ? "\n" : "";
  const ancestors = new Set();
  function encode(value, depth) {
    if (depth > 96) throw new TypeError("Слишком глубокий JSON");
    if (value instanceof ExactNumber) return value.token;
    if (value === null) return "null";
    if (typeof value === "string" || typeof value === "boolean") return JSON.stringify(value);
    if (typeof value === "number") { if (!Number.isFinite(value)) throw new TypeError("Неконечное число"); return JSON.stringify(value); }
    if (!value || typeof value !== "object" || ancestors.has(value)) throw new TypeError("Некорректный объект JSON");
    ancestors.add(value);
    const array = Array.isArray(value);
    const entries = array ? value.map(item => encode(item, depth + 1)) : Object.keys(value).map(key => JSON.stringify(key) + (pretty ? ": " : ":") + encode(value[key], depth + 1));
    ancestors.delete(value);
    if (!entries.length) return array ? "[]" : "{}";
    return (array ? "[" : "{") + newline + entries.map(item => indent(depth + 1) + item).join("," + newline) + newline + indent(depth) + (array ? "]" : "}");
  }
  return encode(value, 0);
}

export function integer(value) {
  const text = String(value);
  if (!/^-?\d+$/.test(text)) throw new TypeError("Нужно целое число");
  return BigInt(text);
}
export function number(value) { const result = Number(String(value)); if (!Number.isFinite(result)) throw new TypeError("Некорректное число"); return result; }
export const copyJSON = value => parseJSON(stringifyJSON(value));
