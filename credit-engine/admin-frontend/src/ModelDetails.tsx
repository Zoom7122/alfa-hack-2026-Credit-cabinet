import { conditionText, sourceName } from "./model";
import type { Definition } from "./types";

export function ReadonlyParameters({ definition }: { definition: Definition }) {
  return (
    <section className="panel">
      <h2>Параметры и источники</h2>
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Поле</th>
              <th>Источник</th>
              <th>Тип и единица</th>
              <th>Полнота</th>
            </tr>
          </thead>
          <tbody>
            {definition.parameters.map((p) => (
              <tr key={p.id}>
                <td>
                  <strong>{p.title}</strong>
                  <code>{p.id}</code>
                </td>
                <td>
                  {sourceName(p.source.kind)}
                  <code>{p.source.path}</code>
                </td>
                <td>
                  {p.type} · {p.unit}
                </td>
                <td>
                  {p.enabled
                    ? p.required
                      ? "Обязательное"
                      : "Необязательное"
                    : "Параметр выключен"}
                  {Boolean(p.depends_on?.length) && (
                    <small className="block muted">
                      Зависит от: {p.depends_on?.join(", ")}
                    </small>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

export function ReadonlyRules({ definition }: { definition: Definition }) {
  return (
    <>
      <p className="notice info">
        Проверки, стоп-факторы и основания рассмотрения работают независимо от
        переключателя начисления баллов.
      </p>
      {(
        [
          ["data_checks", "Проверки данных"],
          ["stops", "Стоп-факторы"],
          ["manual_reviews", "Основания рассмотрения"],
        ] as const
      ).map(([key, title]) => (
        <section className="panel" key={key}>
          <h2>{title}</h2>
          {definition[key].length ? (
            <div className="table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>Правило</th>
                    <th>Условие</th>
                    <th>Результат и порядок</th>
                  </tr>
                </thead>
                <tbody>
                  {definition[key].map((r) => (
                    <tr key={r.id}>
                      <td>
                        <strong>{r.title}</strong>
                        <code>{r.id}</code>
                        <span
                          className={
                            "badge " + (r.enabled ? "success" : "neutral")
                          }
                        >
                          {r.enabled ? "Включено" : "Выключено"}
                        </span>
                      </td>
                      <td>
                        {conditionText(
                          (r.when || r.satisfied_when)!,
                          definition.parameters,
                        )}
                        {r.applies_when && (
                          <small className="block muted">
                            Применяется:{" "}
                            {conditionText(
                              r.applies_when,
                              definition.parameters,
                            )}
                          </small>
                        )}
                      </td>
                      <td>
                        {r.reason?.message || r.message}
                        {r.reason && <code>{r.reason.code}</code>}
                        {Boolean(r.requires_checks?.length) && (
                          <small className="block muted">
                            После проверок: {r.requires_checks?.join(", ")}
                          </small>
                        )}
                        {r.after_criterion && (
                          <small className="block muted">
                            После критерия {r.after_criterion}. Действует и при
                            выключенном начислении.
                          </small>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="muted">Правил нет</p>
          )}
        </section>
      ))}
      <section className="panel">
        <h2>Классы риска</h2>
        <div className="table-scroll">
          <table>
            <thead>
              <tr>
                <th>Класс</th>
                <th>От (включительно)</th>
                <th>До (включительно)</th>
              </tr>
            </thead>
            <tbody>
              {definition.classes.map((c) => (
                <tr key={c.grade}>
                  <td>{c.grade}</td>
                  <td>{String(c.min_total)}</td>
                  <td>{String(c.max_total)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}
