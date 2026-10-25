import {parseJSON, stringifyJSON} from "./numbers.js";

let csrf = "";
let authEpoch = 0;
export function session(token = "") { csrf = token; authEpoch++; }
export class APIError extends Error {
  constructor(status, problem) {
    super(problem?.detail || "Не удалось выполнить запрос");
    this.status = status; this.code = problem?.code || "REQUEST_FAILED"; this.problem = problem;
  }
}
export async function api(path, options = {}) {
  if (!path.startsWith("/api/admin/v1/")) throw new TypeError("Недопустимый адрес API");
  const method = options.method || "GET";
  const headers = {Accept: "application/json"};
  if (options.body !== undefined) headers["Content-Type"] = "application/json";
  if (!["GET", "HEAD"].includes(method) && csrf) headers["X-CSRF-Token"] = csrf;
  const startedEpoch = authEpoch;
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 50000);
  let response;
  try {
    response = await fetch(path, {method, headers, credentials: "same-origin", mode: "same-origin", redirect: "error", cache: "no-store", body: options.body === undefined ? undefined : stringifyJSON(options.body), signal: controller.signal});
    const text = await response.text();
    const data = text ? parseJSON(text) : null;
    if (!response.ok) {
      if (response.status === 401 && !options.public && startedEpoch === authEpoch) { session(); window.dispatchEvent(new CustomEvent("admin-session-expired")); }
      throw new APIError(response.status, data);
    }
    return data;
  } catch (error) {
    if (error instanceof APIError) throw error;
    throw new APIError(0, {code: error.name === "AbortError" ? "CLIENT_TIMEOUT" : "NETWORK_ERROR", detail: error.name === "AbortError" ? "Запрос занял слишком много времени. Попробуйте ещё раз." : "Не удалось связаться с сервером. Проверьте соединение и повторите запрос."});
  } finally { clearTimeout(timeout); }
}
export function message(error) {
  const messages = {INVALID_CREDENTIALS: "Неверный логин или пароль.", LOGIN_RATE_LIMIT: "Слишком много попыток входа. Повторите через минуту.", INVALID_ORIGIN: "Адрес страницы не совпадает с настроенным адресом кабинета.", INVALID_CSRF_TOKEN: "Защита сессии устарела. Обновите страницу.", REVISION_CONFLICT: "Черновик уже изменён. Откройте актуальную ревизию.", IMMUTABLE_VERSION: "Эту версию уже нельзя изменить. Укажите новую версию.", ADMIN_OBJECT_NOT_FOUND: "Объект не найден.", INVALID_ADMIN_INPUT: "Проверьте заполненные поля и основание изменения.", ADMIN_DATABASE_UNAVAILABLE: "Хранилище временно недоступно. Повторите запрос.", SESSION_STORE_UNAVAILABLE: "Не удалось проверить сессию. Повторите запрос.", MODEL_VALIDATION_FAILED: "Модель не прошла проверку. Исправьте указанные ошибки."};
  return Object.hasOwn(messages, error.code) ? messages[error.code] : error.message || "Не удалось выполнить запрос.";
}
