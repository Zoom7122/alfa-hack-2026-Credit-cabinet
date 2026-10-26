import {api} from "./api.js";
import {el, button, field, table, empty, date, borrower} from "./dom.js";
import {text, select} from "./forms.js";
import {stringifyJSON} from "./numbers.js";
import {problemView} from "./workbench.js";

const names = {LOAN_ISSUED: "Выдача", BORROWER_REVIEW: "Рассмотрение заёмщика", BANK_REVIEW: "Банковская проверка"};
const show = value => value === null || value === undefined ? "Неизвестно (null)" : String(value);
const json = (title, value) => el("details", {class: "panel"}, el("summary", {}, title), el("pre", {class: "code"}, stringifyJSON(value, true)));
function paging(section, page, size, query, go) {
  const route = index => { const params = new URLSearchParams(query); params.set("page", String(index)); go(section + "?" + params); };
  const back = button("Назад", () => route(page - 1)), next = button("Далее", () => route(page + 1));
  back.disabled = page === 0; next.disabled = size < 20;
  return el("div", {class: "pager"}, back, el("span", {}, "Страница " + (page + 1)), next);
}
export async function eventsPage(query, page, go) {
  const controls = {
    kind: select([["", "Все события"], ...Object.entries(names)], query.get("kind") || ""),
    borrower_type: select([["", "Все заёмщики"], ["SOLE_PROPRIETOR", "ИП"], ["LEGAL_ENTITY", "ЮЛ"]], query.get("borrower_type") || ""),
    application_id: text(query.get("application_id") || ""), borrower_id: text(query.get("borrower_id") || ""),
    model_version: text(query.get("model_version") || ""), from: text(query.get("from") || "", {placeholder: "2026-10-25T00:00:00Z"}), to: text(query.get("to") || "", {placeholder: "2026-10-25T23:59:59Z"})
  };
  const labels = {kind: "Тип сообщения", borrower_type: "Тип заёмщика", application_id: "ID заявки", borrower_id: "ID заёмщика", model_version: "Версия модели", from: "С (RFC3339, включительно)", to: "По (RFC3339, включительно)"};
  const filters = el("form", {class: "panel", onsubmit: event => {
    event.preventDefault(); const next = new URLSearchParams();
    for (const [key, input] of Object.entries(controls)) if (input.value.trim()) next.set(key, input.value.trim());
    go("messages?" + next);
  }}, el("h2", {}, "Фильтры"), el("div", {class: "form-grid"}, Object.entries(controls).map(([key, input]) => field(labels[key], input))), el("div", {class: "actions"}, el("button", {type: "submit", class: "button primary"}, "Применить фильтры"), button("Сбросить фильтры", () => go("messages"))));
  const params = new URLSearchParams(query); params.delete("page"); params.set("limit", "20"); params.set("offset", String(page * 20));
  const rows = await api("/api/admin/v1/events?" + params);
  return el("div", {}, el("h1", {}, "Сообщения банка"), el("p", {class: "muted"}, "Сохранённые события МС2. Доступен только просмотр."), filters, rows.length ? table(["Событие / заявка", "Заёмщик", "Модель / класс / балл", "Время"], rows.map(row => [
    el("span", {}, el("a", {href: "#event/" + encodeURIComponent(row.event_id)}, names[row.kind] || row.kind), el("code", {class: "block"}, row.application_id)),
    el("span", {}, row.borrower_name, el("small", {class: "block muted"}, borrower(row.borrower_type) + " · " + row.borrower_id)),
    row.model_version + " · " + row.grade + " · " + row.score_total, date(row.occurred_at)
  ])) : empty("Сообщений не найдено", "Измените фильтры или дождитесь события банка."), paging("messages", page, rows.length, query, go));
}
export async function eventPage(id) {
  const stored = await api("/api/admin/v1/events/" + encodeURIComponent(id)), event = stored.payload, summary = stored.summary;
  const terms = event.terms;
  return el("div", {}, el("a", {href: "#messages", class: "text-link"}, "К сообщениям"), el("h1", {}, names[event.kind] || event.kind),
    el("section", {class: "panel"}, el("h2", {}, event.borrower.name), el("p", {}, borrower(event.borrower.type) + " · ID " + event.borrower.id + " · ИНН " + show(event.borrower.inn)), el("p", {}, "Заявка: " + event.application_id), el("p", {}, "Событие: " + event.event_id), el("p", {}, "Возникло: " + date(event.occurred_at) + " · принято: " + date(summary.received_at)), el("code", {}, "SHA-256: " + summary.payload_sha256)),
    el("section", {class: "panel"}, el("h2", {}, "Условия"), table(["", "Сумма, руб.", "Срок, месяцев", "Ставка, %"], [["Запрошено", show(event.requested_terms.amount_rub), show(event.requested_terms.term_months), show(event.requested_terms.max_annual_rate)], ["Выбрано", show(terms?.amount_rub), show(terms?.term_months), show(terms?.annual_rate)]]), el("p", {}, "ID кредита: " + show(event.loan_id)), event.selection ? json("Подбор и экономика", event.selection) : null),
    el("section", {class: "panel"}, el("h2", {}, "Скоринг"), el("p", {}, "Модель: " + event.score.model + " · класс " + event.score.grade + " · итог " + event.score.total + " · до округления " + event.score.raw_total), el("h3", {}, "Анкета"), table(["Критерий", "Ответ", "Баллы / максимум"], event.score.questionnaire.criteria.map(c => [c.id, c.answer, show(c.points) + " / " + c.max_points])), el("h3", {}, "Дополнительные параметры"), table(["Критерий", "Значение", "Баллы / максимум"], event.score.economics.criteria.map(c => [c.id, show(c.value), show(c.points) + " / " + c.max_points]))),
    el("section", {class: "panel"}, el("h2", {}, "Причины"), event.reasons.length ? table(["Код", "Сообщение", "Поля"], event.reasons.map(r => [r.code, r.message, r.fields.join(", ")])) : el("p", {}, "Причин нет."), el("h2", {}, "Банковские проверки"), event.problems.length ? table(["Код", "До", "После", "Условие / предел", "Результат", "Описание"], event.problems.map(p => [p.code, show(p.before), show(p.after), show(p.operator) + " " + show(p.limit), p.passed ? "Пройдена" : "Не пройдена", p.detail])) : el("p", {}, "Банковских проблем нет.")), json("Полное сохранённое событие", event));
}
export async function releasesWithRuntime(page, go) {
  const rows = await api("/api/admin/v1/releases?limit=20&offset=" + page * 20);
  const runtime = el("section", {class: "panel runtime-models"}), status = el("div", {"aria-live": "polite"});
  const refresh = button("Обновить версии МС1", async () => {
    refresh.disabled = true; status.replaceChildren(el("p", {class: "loading"}, "Запрашиваем запущенный МС1…"));
    try {
      const result = await api("/api/admin/v1/runtime/models");
      status.replaceChildren(el("p", {class: "notice success"}, "Получен ответ МС1. Время запроса: " + date(new Date().toISOString())), table(["Версия / ID", "Применение", "Заёмщик", "SHA-256 определения", "Схема фактов / SHA-256"], result.models.map(m => [m.version + " · " + m.id, m.active ? "Активная модель" : "Опубликована", borrower(m.borrower_type), el("code", {}, m.definition_sha256), el("span", {}, (m.uses_facts ? m.facts_schema_version : "Поставщик не используется"), el("code", {class: "block"}, m.facts_schema_sha256))])));
    } catch (error) { status.replaceChildren(el("p", {class: "notice warning"}, "Сведения о запущенных версиях недоступны. Подготовленные релизы ниже не подтверждают их применение."), problemView(error)); }
    finally { refresh.disabled = false; }
  });
  runtime.append(el("div", {class: "page-heading"}, el("h2", {}, "Версии, сообщённые запущенным МС1"), refresh), status);
  refresh.click();
  return el("div", {}, el("h1", {}, "Релизы и запущенные версии"), runtime, el("section", {class: "panel"}, el("h2", {}, "Подготовленные комплекты"), el("p", {class: "notice info"}, "Экспорт комплекта не переключает активные версии. Применение выполняется в разделе «Модели»."), rows.length ? table(["Релиз / дата", "Модели / SHA-256", "Архив"], rows.map(row => [
    el("span", {}, row.version, el("small", {class: "block muted"}, date(row.created_at)), row.created_by),
    el("div", {}, row.manifest.models.map(m => el("p", {}, m.version, el("code", {class: "block"}, m.definition_sha256)))),
    el("span", {}, el("a", {href: "/api/admin/v1/releases/" + encodeURIComponent(row.version) + "/archive", class: "button", download: "credit-engine-models-" + row.version + ".zip"}, "Скачать ZIP"), el("code", {class: "block"}, row.archive_sha256))
  ])) : empty("Релизов пока нет", "Подготовьте комплект на экране проверки и выпуска.")), paging("releases", page, rows.length, new URLSearchParams(), go));
}
