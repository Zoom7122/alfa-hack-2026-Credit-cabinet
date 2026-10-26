import type {
  Condition,
  Criterion,
  Definition,
  Literal,
  Parameter,
} from "./types";
export const borrower = (type: string) =>
  type === "SOLE_PROPRIETOR" ? "ИП" : "Юридическое лицо";
export const sourceName = (kind: string) =>
  ({
    APPLICATION: "Анкета и собранные данные",
    STATEMENTS: "Бухгалтерская отчётность",
    COMPUTED: "Расчётный показатель",
    SUPPLIER: "Дополнительные факты",
  })[kind] || kind;
const values: Record<string, string> = {
  MARRIED: "В браке",
  NEVER_MARRIED: "Никогда не состоял(а) в браке",
  DIVORCED_ONCE: "Один развод",
  DIVORCED_MULTIPLE: "Несколько разводов",
  WIDOWED: "Вдовец / вдова",
  NONE: "Нет",
  VERIFIED: "Подтверждено",
  NOT_REQUIRED: "Не требуется",
  BUSINESS_DEVELOPMENT: "Развитие бизнеса",
  OTHER_OR_UNKNOWN: "Другое / неизвестно",
};
export const literal = (v?: Literal): string =>
  v?.number !== undefined
    ? String(v.number)
    : v?.boolean !== undefined
      ? v.boolean
        ? "Да"
        : "Нет"
      : values[v?.string || ""] || v?.string || "—";

function range(c: Condition, p: Parameter | undefined): string | null {
  if (!p?.number) return null;
  let lower = p.number.lower ? Number(String(p.number.lower.value)) : -Infinity,
    upper = p.number.upper ? Number(String(p.number.upper.value)) : Infinity;
  let lowerInclusive = p.number.lower?.inclusive ?? false,
    upperInclusive = p.number.upper?.inclusive ?? false;
  const excluded: number[] = [];
  const walk = (x: Condition, negative = false): boolean => {
    if (x.op === "NOT")
      return (
        (x.conditions?.length === 1 && walk(x.conditions[0], !negative)) ||
        false
      );
    if ((x.op === "ALL" && !negative) || (x.op === "ANY" && negative))
      return x.conditions?.every((child) => walk(child, negative)) ?? false;
    if (
      x.parameter !== p.id ||
      x.other_parameter ||
      x.value?.number === undefined
    )
      return false;
    let op = x.op;
    const token = String(x.value.number);
    const n = Number(token);
    if (!Number.isFinite(n) || token.replace(/[^0-9]/g, "").length > 15)
      return false;
    if (negative)
      op =
        (
          {
            LT: "GE",
            LE: "GT",
            GT: "LE",
            GE: "LT",
            EQ: "NE",
            NE: "EQ",
          } as Record<string, string>
        )[op] || "";
    if (op === "EQ") {
      if (n > lower) {
        lower = n;
        lowerInclusive = true;
      }
      if (n < upper) {
        upper = n;
        upperInclusive = true;
      }
      return true;
    }
    if (op === "NE") {
      excluded.push(n);
      return true;
    }
    if (op === "GT" || op === "GE") {
      if (n > lower) {
        lower = n;
        lowerInclusive = op === "GE";
      } else if (n === lower) lowerInclusive = lowerInclusive && op === "GE";
      return true;
    }
    if (op === "LT" || op === "LE") {
      if (n < upper) {
        upper = n;
        upperInclusive = op === "LE";
      } else if (n === upper) upperInclusive = upperInclusive && op === "LE";
      return true;
    }
    return false;
  };
  if (!walk(c)) return null;
  if (p.number.integer) {
    if (Number.isFinite(lower))
      lower = lowerInclusive ? Math.ceil(lower) : Math.floor(lower) + 1;
    if (Number.isFinite(upper))
      upper = upperInclusive ? Math.floor(upper) : Math.ceil(upper) - 1;
    lowerInclusive = upperInclusive = true;
    while (excluded.includes(lower)) lower++;
    while (excluded.includes(upper)) upper--;
  }
  if (excluded.some((n) => n > lower && n < upper) || lower > upper)
    return null;
  if (lower === upper) return String(lower);
  if (Number.isFinite(lower) && Number.isFinite(upper))
    return `${lowerInclusive ? "от" : "больше"} ${lower} ${upperInclusive ? "до" : "и меньше"} ${upper}`;
  if (Number.isFinite(lower))
    return `${lowerInclusive ? "от" : "больше"} ${lower}`;
  if (Number.isFinite(upper))
    return `${upperInclusive ? "до" : "меньше"} ${upper}`;
  return "Любое допустимое значение";
}
export function conditionText(
  c: Condition | undefined,
  parameters: Parameter[],
): string {
  if (!c) return "Всегда";
  const refs = references(c);
  const p =
    refs.length === 1 ? parameters.find((p) => p.id === refs[0]) : undefined;
  const numeric = range(c, p);
  if (numeric !== null) return numeric;
  const name = (id?: string) =>
    parameters.find((p) => p.id === id)?.title || id || "Поле";
  if (c.op === "ALWAYS") return "Всегда";
  if (c.op === "NEVER") return "Никогда";
  if (c.op === "ALL" || c.op === "ANY")
    return (c.conditions || [])
      .map((x) => `(${conditionText(x, parameters)})`)
      .join(c.op === "ALL" ? " и " : " или ");
  if (c.op === "NOT")
    return `Не (${conditionText(c.conditions?.[0], parameters)})`;
  if (c.op === "KNOWN" || c.op === "UNKNOWN")
    return `${name(c.parameter)}: ${c.op === "KNOWN" ? "данные известны" : "данных нет"}`;
  if (c.op === "IN" || c.op === "NOT_IN")
    return `${name(c.parameter)} ${c.op === "IN" ? "в списке" : "вне списка"}: ${(c.values || []).map(literal).join(", ")}`;
  const op =
    (
      { EQ: "=", NE: "≠", LT: "<", LE: "≤", GT: ">", GE: "≥" } as Record<
        string,
        string
      >
    )[c.op] || c.op;
  return `${name(c.parameter)} ${op} ${c.other_parameter ? name(c.other_parameter) : literal(c.value)}`;
}
export function references(value: unknown): string[] {
  const found = new Set<string>();
  const walk = (x: unknown, key = "") => {
    if (Array.isArray(x)) {
      x.forEach((child) => walk(child, key));
      return;
    }
    if (x && typeof x === "object") {
      for (const [k, v] of Object.entries(x)) walk(v, k);
      return;
    }
    if (typeof x === "string") {
      if (
        [
          "parameter",
          "other_parameter",
          "value_parameter",
          "depends_on",
        ].includes(key)
      )
        found.add(x);
      if (key === "template" || key === "unknown_text")
        for (const match of x.matchAll(/\$\{([a-z][a-z0-9_]*)\}/g))
          found.add(match[1]);
    }
  };
  walk(value);
  return [...found];
}
export function criterionFields(c: Criterion, d: Definition): Parameter[] {
  const ids = new Set(references(c));
  return d.parameters.filter((p) => ids.has(p.id));
}
