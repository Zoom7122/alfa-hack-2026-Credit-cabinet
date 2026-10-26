import { useEffect, useRef, useState, type ReactNode } from "react";
import { Button } from "@alfalab/core-components/button";
import { Input } from "@alfalab/core-components/input";
import { Switch } from "@alfalab/core-components/switch";
import { AlfaBankLogoMIcon } from "@alfalab/icons-logotype/AlfaBankLogoMIcon";
import { Spinner } from "@alfalab/core-components/spinner";
import { api as legacyApi, message, session } from "./legacy/api.js";
import {
  parseJSON,
  stringifyJSON,
  copyJSON,
  ExactNumber,
} from "./legacy/numbers.js";
import { maxima } from "./legacy/model-edit.js";
import {
  editCriterion,
  editParameter,
  downloadDefinition,
} from "./legacy/editor.js";
import { rulesSection } from "./legacy/rules-editor.js";
import { allRows, workbenchPage } from "./legacy/workbench.js";
import { eventsPage, eventPage, releasesWithRuntime } from "./legacy/audit.js";
import { borrower, conditionText, criterionFields, sourceName } from "./model";
import { ReadonlyParameters, ReadonlyRules } from "./ModelDetails";
import type {
  Active,
  Catalog,
  Criterion,
  Definition,
  Draft,
  Numeric,
  Published,
  Revision,
  Validation,
} from "./types";

type Options = { method?: string; body?: unknown; public?: boolean };
const request = <T,>(path: string, options?: Options) =>
  legacyApi(path, options) as Promise<T>;
let canLeave = () => true;
const routeNow = () => location.hash.slice(1) || "models";
function go(route: string) {
  location.hash = route;
}
function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null;
  const p = (
    error as {
      problem?: {
        validation?: { issues: { path: string; message: string }[] };
        current_revision?: Numeric;
      };
    }
  ).problem;
  return (
    <div className="notice danger" role="alert">
      <strong>{message(error)}</strong>
      {p?.current_revision && (
        <p>
          Серверная ревизия: {String(p.current_revision)}. Ваши изменения
          остаются в редакторе.
        </p>
      )}
      {p?.validation?.issues && (
        <ul>
          {p.validation.issues.map((i) => (
            <li key={i.path + i.message}>
              {i.message}
              <code>{i.path}</code>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
const Loading = () => (
  <div className="loading" role="status">
    <Spinner visible preset={48} />
    <span>Загружаем данные…</span>
  </div>
);
function Heading({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children?: ReactNode;
}) {
  return (
    <div className="page-heading">
      <div>
        <h1>{title}</h1>
        {description && <p className="muted">{description}</p>}
      </div>
      <div className="actions">{children}</div>
    </div>
  );
}
function RouteLink({
  to,
  children,
  ...props
}: {
  to: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <a
      href={"#" + to}
      {...props}
      onClick={(e) => {
        e.preventDefault();
        go(to);
      }}
    >
      {children}
    </a>
  );
}
function Modal({
  title,
  children,
  onClose,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    ref.current?.showModal();
    return () => ref.current?.close();
  }, []);
  return (
    <dialog ref={ref} className="dialog" aria-label={title} onCancel={onClose}>
      <div className="modal-heading">
        <h2>{title}</h2>
        <Button
          view="transparent"
          size={32}
          onClick={onClose}
          aria-label="Закрыть"
        >
          ×
        </Button>
      </div>
      {children}
    </dialog>
  );
}

function Login({
  onLogin,
}: {
  onLogin: (value: { username: string; csrf_token: string }) => void;
}) {
  const [name, setName] = useState(""),
    [password, setPassword] = useState(""),
    [error, setError] = useState<unknown>(null),
    [busy, setBusy] = useState(false);
  return (
    <main className="login-page">
      <section className="login-card">
        <div className="brand">
          <AlfaBankLogoMIcon className="alpha-logo" />
          <strong>Альфа Бизнес</strong>
        </div>
        <p className="eyebrow">Управление кредитными моделями</p>
        <h1>Конструктор скоринга</h1>
        <p className="muted">
          Баллы, проверки и версии моделей в одном кабинете
        </p>
        <form
          onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            setError(null);
            try {
              onLogin(
                await request("/api/admin/v1/auth/login", {
                  method: "POST",
                  public: true,
                  body: { username: name, password },
                }),
              );
            } catch (e) {
              setError(e);
            } finally {
              setPassword("");
              setBusy(false);
            }
          }}
        >
          <Input
            label="Логин"
            name="username"
            autoComplete="username"
            value={name}
            onChange={(e) => setName(e.target.value)}
            block
            required
          />
          <Input
            label="Пароль"
            name="password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            block
            required
          />
          <ErrorBox error={error} />
          <Button view="primary" type="submit" block loading={busy}>
            Войти
          </Button>
        </form>
      </section>
    </main>
  );
}

function NewDraft({
  source,
  onClose,
}: {
  source: Published | Definition | null;
  onClose: () => void;
}) {
  const original =
    source && ("definition" in source ? source.definition : source);
  const [title, setTitle] = useState(original?.title || ""),
    [version, setVersion] = useState(
      original ? original.version + "-next" : "",
    ),
    [id, setId] = useState(""),
    [type, setType] = useState("SOLE_PROPRIETOR"),
    [reason, setReason] = useState(""),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<unknown>(null);
  return (
    <Modal
      title={original ? "Копировать модель в черновик" : "Новая модель"}
      onClose={onClose}
    >
      <form
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setError(null);
          try {
            const definition: Definition = original
              ? {
                  ...copyJSON(original),
                  version: version.trim(),
                  title: title.trim(),
                }
              : {
                  format_version: new ExactNumber("1"),
                  id: id.trim(),
                  title: title.trim(),
                  version: version.trim(),
                  borrower_type: type,
                  parameters: [],
                  criteria: [],
                  data_checks: [],
                  stops: [],
                  manual_reviews: [],
                  classes: [],
                };
            const result = await request<Draft>("/api/admin/v1/drafts", {
              method: "POST",
              body: { definition, reason: reason.trim() },
            });
            onClose();
            go("draft/" + result.id);
          } catch (e) {
            setError(e);
          } finally {
            setBusy(false);
          }
        }}
      >
        <Input
          label="Название модели"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          required
          block
        />
        {!original && (
          <>
            <Input
              label="Идентификатор модели"
              value={id}
              onChange={(e) => setId(e.target.value)}
              required
              block
            />
            <label className="field">
              Тип заёмщика
              <select value={type} onChange={(e) => setType(e.target.value)}>
                <option value="SOLE_PROPRIETOR">ИП</option>
                <option value="LEGAL_ENTITY">Юридическое лицо</option>
              </select>
            </label>
          </>
        )}
        <Input
          label="Новая версия"
          value={version}
          onChange={(e) => setVersion(e.target.value)}
          required
          block
        />
        <label className="field">
          Основание
          <textarea
            required
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </label>
        <ErrorBox error={error} />
        <div className="actions">
          <Button onClick={onClose}>Отмена</Button>
          <Button view="primary" type="submit" loading={busy}>
            Создать черновик
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function ModelsPage({ drafts }: { drafts: boolean }) {
  const [rows, setRows] = useState<(Draft | Published)[] | null>(null),
    [active, setActive] = useState<Active[]>([]),
    [search, setSearch] = useState(""),
    [type, setType] = useState(""),
    [error, setError] = useState<unknown>(null),
    [dialog, setDialog] = useState<{
      source: Published | Definition | null;
    } | null>(null),
    [reload, setReload] = useState(0);
  useEffect(() => {
    let mounted = true;
    setRows(null);
    setError(null);
    Promise.all([
      allRows("/api/admin/v1/" + (drafts ? "drafts" : "models")),
      request<Active[]>("/api/admin/v1/active-models"),
    ])
      .then(([rows, active]) => {
        if (mounted) {
          setRows(rows);
          setActive(active);
        }
      })
      .catch((e) => mounted && setError(e));
    return () => {
      mounted = false;
    };
  }, [drafts, reload]);
  const filtered = rows?.filter(
    (row) =>
      (!type || row.definition.borrower_type === type) &&
      [
        row.definition.title,
        row.definition.version,
        borrower(row.definition.borrower_type),
      ].some((s) =>
        s.toLocaleLowerCase("ru").includes(search.toLocaleLowerCase("ru")),
      ),
  );
  return (
    <>
      <Heading
        title={drafts ? "Черновики моделей" : "Модели скоринга"}
        description={
          drafts
            ? "Настройте баллы и проверки, затем примените сохранённую ревизию"
            : "Активные модели для оценки заявок ИП и юридических лиц"
        }
      >
        <Button size={40} onClick={() => setReload((x) => x + 1)}>
          Обновить
        </Button>
        <label className="button import-button">
          Импорт JSON
          <input
            type="file"
            accept=".json,application/json"
            onChange={async (e) => {
              const file = e.target.files?.[0];
              e.target.value = "";
              if (!file) return;
              try {
                if (file.size > 4 * 1024 * 1024)
                  throw new Error("Модель должна быть не больше 4 MiB.");
                const definition = parseJSON(await file.text());
                if (!definition?.parameters || !definition?.criteria)
                  throw new Error("Выберите JSON-определение модели.");
                setDialog({ source: definition });
              } catch (e) {
                setError(e);
              }
            }}
          />
        </label>
        <Button
          view="primary"
          size={40}
          onClick={() => setDialog({ source: null })}
        >
          Создать черновик
        </Button>
      </Heading>
      <div className="toolbar">
        <input
          type="search"
          aria-label="Поиск моделей"
          placeholder="Найти по названию или версии"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select
          aria-label="Тип заёмщика"
          value={type}
          onChange={(e) => setType(e.target.value)}
        >
          <option value="">Все заёмщики</option>
          <option value="SOLE_PROPRIETOR">ИП</option>
          <option value="LEGAL_ENTITY">Юридические лица</option>
        </select>
      </div>
      <ErrorBox error={error} />
      {!rows && !error ? (
        <Loading />
      ) : (
        <div className="model-grid">
          {filtered?.map((row) => {
            const d = row.definition;
            const isActive = active.some(
              (a) =>
                a.version === d.version &&
                a.definition_sha256 === row.definition_sha256,
            );
            const target = drafts
              ? "draft/" + (row as Draft).id
              : "model/" + d.version;
            const totals = maxima(d);
            return (
              <article className="model-card" key={target}>
                <div className="card-top">
                  <span className="badge">{borrower(d.borrower_type)}</span>
                  <span
                    className={
                      "badge " +
                      (drafts ? "warning" : isActive ? "success" : "neutral")
                    }
                  >
                    {drafts
                      ? "Черновик"
                      : isActive
                        ? "Активна"
                        : "Опубликована"}
                  </span>
                </div>
                <h2>
                  <RouteLink to={target}>{d.title}</RouteLink>
                </h2>
                <p className="muted">{d.version}</p>
                <div className="model-metrics">
                  <div>
                    <strong>
                      {
                        d.criteria.filter(
                          (c) => c.enabled && Number(c.max_points) > 0,
                        ).length
                      }
                    </strong>
                    <span>балльных критериев</span>
                  </div>
                  <div>
                    <strong>{String(totals.total)}</strong>
                    <span>максимум баллов</span>
                  </div>
                </div>
                <p className="muted small">
                  Анкета {String(totals.questionnaire)} / 750 · Показатели{" "}
                  {String(totals.additional)} / 250
                </p>
                <div className="actions">
                  <Button
                    size={40}
                    view={isActive ? "primary" : "secondary"}
                    onClick={() => go(target)}
                  >
                    {drafts ? "Продолжить настройку" : "Открыть модель"}
                  </Button>
                  {!drafts && (
                    <Button
                      size={40}
                      view="transparent"
                      onClick={() => setDialog({ source: row as Published })}
                    >
                      Копировать
                    </Button>
                  )}
                </div>
              </article>
            );
          })}
          {filtered?.length === 0 && (
            <div className="empty">
              <h2>Модели не найдены</h2>
              <p>Измените фильтры или создайте черновик.</p>
            </div>
          )}
        </div>
      )}
      {dialog && (
        <NewDraft source={dialog.source} onClose={() => setDialog(null)} />
      )}
    </>
  );
}

function ScoreSummary({ definition }: { definition: Definition }) {
  const m = maxima(definition);
  return (
    <div className="score-summary" aria-live="polite">
      {[
        ["Анкета", m.questionnaire, 750n],
        ["Дополнительные показатели", m.additional, 250n],
        ["Общий максимум", m.total, 1000n],
      ].map(([label, value, target]) => (
        <div
          key={String(label)}
          className={"score-tile " + (value === target ? "" : "invalid")}
        >
          <span>{String(label)}</span>
          <div>
            <strong>{String(value)}</strong>
            <span> / {String(target)}</span>
          </div>
          {value !== target && (
            <small>
              Отклонение: {BigInt(value) > BigInt(target) ? "+" : ""}
              {String(BigInt(value) - BigInt(target))}
            </small>
          )}
        </div>
      ))}
    </div>
  );
}
function DomView({ render }: { render: () => HTMLElement }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const node = render();
    ref.current?.replaceChildren(node);
    return () => {
      node.remove();
    };
  }, [render]);
  return <div className="legacy" ref={ref} />;
}

function Criteria({
  definition,
  onChange,
  editable,
  onError,
}: {
  definition: Definition;
  onChange: (d: Definition) => void;
  editable: boolean;
  onError: (e: unknown) => void;
}) {
  const [search, setSearch] = useState(""),
    [block, setBlock] = useState(""),
    [status, setStatus] = useState("");
  const state = { definition, change: onChange };
  const change = (index: number, fn: (c: Criterion) => void) => {
    const next = copyJSON(definition);
    fn(next.criteria[index]);
    onChange(next);
  };
  const points = (
    index: number,
    group: "cases" | "special_cases",
    row: number,
    text: string,
  ) => {
    try {
      const n = new ExactNumber(text.replace(",", "."));
      change(index, (c) => {
        c[group][row].points = n;
      });
    } catch (e) {
      onError(e);
    }
  };
  return (
    <>
      <div className="toolbar">
        <input
          aria-label="Поиск критериев"
          type="search"
          placeholder="Найти критерий, поле или источник"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select
          aria-label="Блок критериев"
          value={block}
          onChange={(e) => setBlock(e.target.value)}
        >
          <option value="">Все блоки</option>
          <option value="QUESTIONNAIRE">Анкета</option>
          <option value="ADDITIONAL">Дополнительные показатели</option>
        </select>
        <select
          aria-label="Участие в скоринге"
          value={status}
          onChange={(e) => setStatus(e.target.value)}
        >
          <option value="">Все критерии</option>
          <option value="enabled">Включённые</option>
          <option value="disabled">Выключенные</option>
        </select>
        {editable && (
          <Button size={40} onClick={() => editCriterion(state, -1)}>
            Добавить критерий
          </Button>
        )}
      </div>
      <div className="criteria-list">
        {definition.criteria.map((c, index) => {
          const fields = criterionFields(c, definition);
          if (
            (block && c.block !== block) ||
            (status === "enabled" && !c.enabled) ||
            (status === "disabled" && c.enabled) ||
            ![
              c.title,
              c.id,
              ...fields.map((p) => p.title + " " + p.source.path),
            ]
              .join(" ")
              .toLocaleLowerCase("ru")
              .includes(search.toLocaleLowerCase("ru"))
          )
            return null;
          return (
            <article
              key={c.id}
              className={
                "criterion-card " + (!c.enabled ? "criterion-disabled" : "")
              }
            >
              <div className="criterion-heading">
                <div>
                  <span className="eyebrow">
                    {c.block === "QUESTIONNAIRE"
                      ? "Анкета"
                      : "Дополнительные показатели"}{" "}
                    · {c.id}
                  </span>
                  <h2>{c.title}</h2>
                  <div className="field-chips">
                    {fields.map((p) => (
                      <span
                        className="field-chip"
                        key={p.id}
                        title={p.source.path}
                      >
                        {p.title}
                      </span>
                    ))}
                  </div>
                  <p className="muted small">
                    {[
                      ...new Set(fields.map((p) => sourceName(p.source.kind))),
                    ].join(" · ")}
                    {fields.length > 1
                      ? " · Составной критерий — общий максимум для группы полей"
                      : ""}
                  </p>
                </div>
                <div className="criterion-controls">
                  <label className="point-maximum">
                    Максимум
                    {editable ? (
                      <input
                        aria-label={"Максимум " + c.id}
                        inputMode="numeric"
                        value={String(c.max_points)}
                        onChange={(e) => {
                          try {
                            const n = new ExactNumber(e.target.value);
                            change(index, (c) => {
                              c.max_points = n;
                            });
                          } catch (e) {
                            onError(e);
                          }
                        }}
                      />
                    ) : (
                      <strong>{String(c.max_points)}</strong>
                    )}
                  </label>
                  <Switch
                    size={20}
                    checked={c.enabled}
                    disabled={!editable}
                    label="Учитывать в баллах"
                    aria-label={"Учитывать в баллах " + c.id}
                    onChange={(_, p) =>
                      change(index, (c) => {
                        c.enabled = p.checked;
                      })
                    }
                  />
                  {editable && (
                    <div className="actions">
                      <Button
                        view="transparent"
                        size={32}
                        aria-label={"Изменить критерий " + c.id}
                        onClick={() => editCriterion(state, index)}
                      >
                        Изменить
                      </Button>
                      <Button
                        view="transparent"
                        size={32}
                        aria-label={"Удалить критерий " + c.id}
                        onClick={() => {
                          if (
                            window.confirm(
                              "Удалить критерий «" +
                                c.title +
                                "» из черновика? Связанные проверки останутся отдельными.",
                            )
                          ) {
                            const next = copyJSON(definition);
                            next.criteria.splice(index, 1);
                            onChange(next);
                          }
                        }}
                      >
                        Удалить
                      </Button>
                    </div>
                  )}
                </div>
              </div>
              {Number(c.max_points) === 0 && (
                <p className="notice info small">
                  Контрольный критерий: баллы не начисляются. Связанные проверки
                  и стоп-факторы настраиваются отдельно.
                </p>
              )}
              <details className="scale">
                <summary>
                  Шкала начисления · {c.cases.length + c.special_cases.length}{" "}
                  условий
                </summary>
                <div className="scale-table">
                  <div className="scale-table-head">
                    <span>Условие</span>
                    <span>Баллы</span>
                  </div>
                  {(["special_cases", "cases"] as const).flatMap((group) =>
                    c[group].map((row, i) => (
                      <div className="scale-row" key={group + row.id}>
                        <div>
                          {group === "special_cases" && (
                            <span className="badge warning">Особый случай</span>
                          )}
                          <span>
                            {conditionText(row.when, definition.parameters)}
                          </span>
                          {row.description && <small>{row.description}</small>}
                          {row.review_reason && (
                            <small>
                              Рассмотрение: {row.review_reason.message}
                            </small>
                          )}
                        </div>
                        {editable ? (
                          <input
                            inputMode="decimal"
                            aria-label={"Баллы " + c.id + " " + row.id}
                            value={String(row.points)}
                            onChange={(e) =>
                              points(index, group, i, e.target.value)
                            }
                          />
                        ) : (
                          <strong>{String(row.points)}</strong>
                        )}
                      </div>
                    )),
                  )}
                </div>
                <p className="muted small">
                  При отсутствии данных:{" "}
                  {
                    (
                      {
                        NEED_INFO: "запросить сведения",
                        ZERO: "начислить 0 баллов",
                        SKIP: "пропустить критерий",
                      } as Record<string, string>
                    )[c.unknown_action]
                  }
                  .{" "}
                  {c.applies_when
                    ? "Применимость: " +
                      conditionText(c.applies_when, definition.parameters)
                    : ""}
                </p>
                {editable && (
                  <Button size={40} onClick={() => editCriterion(state, index)}>
                    Редактировать условия и особые случаи
                  </Button>
                )}
              </details>
            </article>
          );
        })}
      </div>
    </>
  );
}

const roleNames: Record<string, string> = {
  SCORE: "Баллы",
  CONTROL: "Контроль без баллов",
  DISABLED_SCORE: "Баллы выключены",
  COMPLETENESS: "Обязательность",
  DATA_CHECK: "Проверка данных",
  STOP: "Стоп-фактор",
  MANUAL_REVIEW: "Ручное рассмотрение",
};
function UnusedFields({ definition }: { definition: Definition }) {
  const [catalog, setCatalog] = useState<Catalog | null>(null),
    [error, setError] = useState<unknown>(null),
    [search, setSearch] = useState(""),
    [system, setSystem] = useState(""),
    [filter, setFilter] = useState("unused"),
    [page, setPage] = useState(0);
  useEffect(() => {
    let alive = true;
    const timer = setTimeout(() => {
      request<Catalog>("/api/admin/v1/field-catalog/usage", {
        method: "POST",
        body: { definition },
      })
        .then((c) => {
          if (alive) {
            setCatalog(c);
            setError(null);
          }
        })
        .catch((e) => alive && setError(e));
    }, 250);
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [definition]);
  useEffect(() => {
    setPage(0);
  }, [search, system, filter]);
  const rows = catalog?.fields.filter(
    (f) =>
      (!system || f.system === system) &&
      (filter === "all" ||
        (filter === "unused" && !f.used) ||
        (filter === "controls" && f.used && !f.roles.includes("SCORE"))) &&
      [f.title, f.path, catalog.systems.find((s) => s.id === f.system)?.title]
        .join(" ")
        .toLocaleLowerCase("ru")
        .includes(search.toLocaleLowerCase("ru")),
  );
  return (
    <section>
      <h2>Данные data-collector · {borrower(definition.borrower_type)}</h2>
      <p className="muted">
        Участие в модели «{definition.title}», версия {definition.version}.
        Показаны описания полей, включая условно собираемые источники.
      </p>
      <div className="toolbar">
        <input
          aria-label="Поиск полей data-collector"
          type="search"
          placeholder="Название или путь поля"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <select
          aria-label="Источник данных"
          value={system}
          onChange={(e) => setSystem(e.target.value)}
        >
          <option value="">Все источники</option>
          {catalog?.systems.map((s) => (
            <option key={s.id} value={s.id}>
              {s.title}
            </option>
          ))}
        </select>
        <select
          aria-label="Использование данных"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        >
          <option value="unused">Не используются в модели</option>
          <option value="controls">Используются без начисления баллов</option>
          <option value="all">Все собираемые поля</option>
        </select>
      </div>
      <ErrorBox error={error} />
      {!catalog && !error ? (
        <Loading />
      ) : (
        <>
          <p className="muted small">
            Найдено: {rows?.length ?? 0} · Всего путей:{" "}
            {catalog?.fields.length ?? 0} · Источников:{" "}
            {catalog?.systems.length ?? 0}
          </p>
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>Поле / путь</th>
                  <th>Источник и условия сбора</th>
                  <th>Использование</th>
                </tr>
              </thead>
              <tbody>
                {rows?.slice(page * 50, (page + 1) * 50).map((f) => {
                  const source = catalog?.systems.find(
                    (s) => s.id === f.system,
                  );
                  return (
                    <tr key={f.id}>
                      <td>
                        <strong>{f.title}</strong>
                        <code>{f.path}</code>
                        <small className="muted">{f.type}</small>
                      </td>
                      <td>
                        {source?.title}
                        <small className="block muted">
                          {source?.collection_condition}
                        </small>
                      </td>
                      <td>
                        {f.used ? (
                          <div className="field-chips">
                            {f.roles.map((r) => (
                              <span className="badge" key={r}>
                                {roleNames[r] || r}
                              </span>
                            ))}
                          </div>
                        ) : (
                          f.reason
                        )}
                        {f.derived && (
                          <small className="block muted">
                            Через расчётный показатель
                          </small>
                        )}
                        {f.criteria.length > 0 && (
                          <small className="block muted">
                            Критерии: {f.criteria.join(", ")}
                          </small>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <div className="pager">
            <Button
              size={32}
              disabled={page === 0}
              onClick={() => setPage((x) => x - 1)}
            >
              Назад
            </Button>
            <span>Страница {page + 1}</span>
            <Button
              size={32}
              disabled={(page + 1) * 50 >= (rows?.length ?? 0)}
              onClick={() => setPage((x) => x + 1)}
            >
              Далее
            </Button>
          </div>
        </>
      )}
    </section>
  );
}

function UnusedDataPage() {
  const [type, setType] = useState("SOLE_PROPRIETOR");
  const [definition, setDefinition] = useState<Definition | null>(null);
  const [error, setError] = useState<unknown>(null);
  useEffect(() => {
    let alive = true;
    setDefinition(null);
    setError(null);
    request<Active[]>("/api/admin/v1/active-models")
      .then((rows) => {
        const selected = rows.find((r) => r.borrower_type === type);
        if (!selected) throw new Error("Активная модель не выбрана.");
        return request<Published>(
          "/api/admin/v1/models/" + encodeURIComponent(selected.version),
        );
      })
      .then((model) => {
        if (alive) setDefinition(model.definition);
      })
      .catch((e) => {
        if (alive) setError(e);
      });
    return () => {
      alive = false;
    };
  }, [type]);
  return (
    <>
      <Heading
        title="Неиспользуемые данные"
        description="Каталог полей из 19 систем и их участие в активной модели скоринга"
      />
      <div className="toolbar">
        <label className="field">
          Тип заёмщика
          <select
            aria-label="Тип заёмщика данных"
            value={type}
            onChange={(e) => setType(e.target.value)}
          >
            <option value="SOLE_PROPRIETOR">ИП</option>
            <option value="LEGAL_ENTITY">Юридическое лицо</option>
          </select>
        </label>
        {definition && (
          <RouteLink to={"model/" + encodeURIComponent(definition.version)}>
            Открыть модель {definition.version} →
          </RouteLink>
        )}
      </div>
      <ErrorBox error={error} />
      {definition ? (
        <div className="panel">
          <UnusedFields
            key={type + definition.version}
            definition={definition}
          />
        </div>
      ) : (
        !error && <Loading />
      )}
    </>
  );
}

function ModelPage({ route }: { route: string }) {
  const [loaded, setLoaded] = useState<{
      draft?: Draft;
      published?: Published;
      validation?: Validation;
      active: Active[];
      revisions: Revision[];
    } | null>(null),
    [error, setError] = useState<unknown>(null),
    [refresh, setRefresh] = useState(0),
    [copy, setCopy] = useState(false);
  const [path] = route.split("?"),
    parts = path.split("/"),
    draft = parts[0] === "draft",
    id = decodeURIComponent(parts[1]),
    revision = parts[2] === "revision" ? parts[3] : null;
  useEffect(() => {
    let alive = true;
    setLoaded(null);
    setError(null);
    const base =
      "/api/admin/v1/" +
      (draft ? "drafts/" : "models/") +
      encodeURIComponent(id);
    Promise.all([
      request<Draft | Published>(
        base + (revision ? "/revisions/" + encodeURIComponent(revision) : ""),
      ),
      request<Active[]>("/api/admin/v1/active-models"),
      draft
        ? (allRows(base + "/revisions") as Promise<Revision[]>)
        : Promise.resolve([]),
      draft && !revision
        ? request<Validation>(base + "/validation")
        : Promise.resolve(undefined),
    ])
      .then(([row, active, revisions, validation]) => {
        if (alive)
          setLoaded({
            ...(draft
              ? { draft: row as Draft }
              : { published: row as Published }),
            active,
            revisions,
            validation,
          });
      })
      .catch((e) => alive && setError(e));
    return () => {
      alive = false;
    };
  }, [draft, id, revision, refresh]);
  if (!loaded)
    return (
      <>
        <ErrorBox error={error} />
        {!error && <Loading />}
      </>
    );
  const row = loaded.draft || loaded.published!,
    definition = row.definition;
  return (
    <>
      <RouteLink to={draft ? "drafts" : "models"} className="back-link">
        ← {draft ? "Все черновики" : "Все модели"}
      </RouteLink>
      <Heading
        title={definition.title}
        description={
          borrower(definition.borrower_type) + " · " + definition.version
        }
      >
        {(!draft || revision) && (
          <Button view="primary" size={40} onClick={() => setCopy(true)}>
            Копировать в черновик
          </Button>
        )}
        <Button size={40} onClick={() => downloadDefinition(definition)}>
          Скачать JSON
        </Button>
      </Heading>
      {loaded.draft && !revision ? (
        <Editor
          draft={loaded.draft}
          validation={loaded.validation!}
          active={loaded.active}
          revisions={loaded.revisions}
          onReload={() => setRefresh((x) => x + 1)}
        />
      ) : (
        <ReadModel
          definition={definition}
          active={loaded.active}
          historical={Boolean(revision)}
          onError={setError}
        />
      )}
      <ErrorBox error={error} />
      {copy && <NewDraft source={definition} onClose={() => setCopy(false)} />}
    </>
  );
}
const tabs = [
  ["criteria", "Критерии и баллы"],
  ["rules", "Проверки и стоп-факторы"],
  ["unused", "Неиспользуемые данные"],
  ["parameters", "Параметры"],
  ["history", "Ревизии и JSON"],
];
function Tabs({ tab, setTab }: { tab: string; setTab: (t: string) => void }) {
  return (
    <nav className="editor-tabs" aria-label="Разделы модели">
      {tabs.map(([id, label]) => (
        <button
          key={id}
          aria-current={tab === id ? "page" : undefined}
          onClick={() => setTab(id)}
        >
          {label}
        </button>
      ))}
    </nav>
  );
}
function ReadModel({
  definition,
  active,
  historical,
  onError,
}: {
  definition: Definition;
  active: Active[];
  historical: boolean;
  onError: (e: unknown) => void;
}) {
  const [tab, setTab] = useState("criteria"),
    [busy, setBusy] = useState(false),
    [applied, setApplied] = useState(false);
  const a = active.find((a) => a.borrower_type === definition.borrower_type);
  return (
    <>
      <div className="notice info">
        {historical ? "Историческая ревизия" : "Опубликованная модель"}. Для
        редактирования создайте черновик.
      </div>
      <div className="model-status">
        <span
          className={
            "badge " +
            (a?.version === definition.version || applied
              ? "success"
              : "neutral")
          }
        >
          {a?.version === definition.version || applied
            ? "Активная версия"
            : "Не активна"}
        </span>
        {!historical && a && a.version !== definition.version && !applied && (
          <Button
            size={40}
            loading={busy}
            onClick={async () => {
              setBusy(true);
              try {
                await request(
                  "/api/admin/v1/active-models/" + definition.borrower_type,
                  {
                    method: "PUT",
                    body: {
                      version: definition.version,
                      expected_activation_revision: a.revision,
                      reason:
                        "Применение опубликованной модели " +
                        definition.version,
                    },
                  },
                );
                setApplied(true);
              } catch (e) {
                onError(e);
              } finally {
                setBusy(false);
              }
            }}
          >
            Применить эту версию
          </Button>
        )}
      </div>
      <ScoreSummary definition={definition} />
      <Tabs tab={tab} setTab={setTab} />
      {tab === "criteria" ? (
        <Criteria
          definition={definition}
          onChange={() => {}}
          editable={false}
          onError={onError}
        />
      ) : tab === "unused" ? (
        <div className="panel">
          <UnusedFields definition={definition} />
        </div>
      ) : tab === "rules" ? (
        <ReadonlyRules definition={definition} />
      ) : tab === "parameters" ? (
        <ReadonlyParameters definition={definition} />
      ) : (
        <div className="panel">
          <pre className="code">{stringifyJSON(definition, true)}</pre>
        </div>
      )}
    </>
  );
}

function Editor({
  draft,
  validation,
  active,
  revisions,
  onReload,
}: {
  draft: Draft;
  validation: Validation;
  active: Active[];
  revisions: Revision[];
  onReload: () => void;
}) {
  const [definition, setDefinition] = useState<Definition>(() =>
      copyJSON(draft.definition),
    ),
    [dirty, setDirty] = useState(false),
    [tab, setTab] = useState("criteria"),
    [reason, setReason] = useState(""),
    [error, setError] = useState<unknown>(null),
    [pending, setPending] = useState(false),
    [jsonText, setJsonText] = useState(""),
    [success, setSuccess] = useState("");
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;
  useEffect(() => {
    const guard = () =>
      !dirtyRef.current ||
      window.confirm("Есть несохранённые изменения. Покинуть редактор?");
    canLeave = guard;
    const unload = (e: BeforeUnloadEvent) => {
      if (dirtyRef.current) {
        e.preventDefault();
        e.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", unload);
    return () => {
      canLeave = () => true;
      window.removeEventListener("beforeunload", unload);
      document.querySelectorAll<HTMLDialogElement>("dialog.editor-dialog").forEach((d) => {
        d.close();
        d.remove();
      });
    };
  }, []);
  const change = (next: Definition) => {
    if (pending) throw new Error("Дождитесь завершения сохранения.");
    setDefinition(next);
    setDirty(true);
    setSuccess("");
    setError(null);
  };
  const valid =
    !dirty &&
    String(validation?.revision) === String(draft.revision) &&
    validation?.validation.valid;
  const a = active.find((a) => a.borrower_type === definition.borrower_type);
  const save = async () => {
    if (!reason.trim()) {
      setError(new Error("Укажите основание изменения."));
      return;
    }
    setPending(true);
    setError(null);
    try {
      await request("/api/admin/v1/drafts/" + draft.id, {
        method: "PUT",
        body: {
          expected_revision: draft.revision,
          definition,
          reason: reason.trim(),
        },
      });
      dirtyRef.current = false;
      setDirty(false);
      onReload();
    } catch (e) {
      setError(e);
    } finally {
      setPending(false);
    }
  };
  const apply = async () => {
    if (!a) return;
    if (!reason.trim()) {
      setError(new Error("Укажите основание применения."));
      return;
    }
    setPending(true);
    setError(null);
    try {
      const result = await request<Active>(
        "/api/admin/v1/drafts/" + draft.id + "/apply",
        {
          method: "POST",
          body: {
            expected_revision: draft.revision,
            expected_activation_revision: a.revision,
            reason: reason.trim(),
          },
        },
      );
      setSuccess(
        "Модель " +
          result.version +
          " применяется к новым попыткам оценки. Перезапуск не требуется.",
      );
    } catch (e) {
      setError(e);
    } finally {
      setPending(false);
    }
  };
  const state = { definition, change };
  return (
    <>
      <div className="model-status">
        <span className="badge warning">
          Черновик · ревизия {String(draft.revision)}
        </span>
        <span className="muted">
          Активна: {success ? definition.version : a?.version || "не выбрана"}
        </span>
        <span className="save-status">
          {dirty ? "Есть несохранённые изменения" : "Ревизия сохранена"}
        </span>
        <RouteLink
          to={
            "workbench?kind=REVISION&id=" +
            draft.id +
            "&revision=" +
            draft.revision
          }
        >
          Проверить расчёт →
        </RouteLink>
      </div>
      <ScoreSummary definition={definition} />
      {!maxima(definition).valid && (
        <p className="notice warning">
          Применение заблокировано. Перераспределите баллы вручную: анкета —
          750, дополнительные показатели — 250.
        </p>
      )}
      {!dirty && validation && !validation.validation.valid && (
        <details className="notice warning">
          <summary>
            Модель не прошла проверку · {validation.validation.issues.length}{" "}
            замечаний
          </summary>
          <ul>
            {validation.validation.issues.map((i) => (
              <li key={i.path + i.code}>
                {i.message}
                <code>{i.path}</code>
              </li>
            ))}
          </ul>
        </details>
      )}
      {valid && !success && (
        <p className="notice success small">Модель прошла проверку.</p>
      )}
      <ErrorBox error={error} />
      {success && (
        <p className="notice success" role="status">
          {success}
        </p>
      )}
      <Tabs tab={tab} setTab={setTab} />
      <fieldset disabled={pending} className="editor-fieldset">
        {tab === "criteria" && (
          <Criteria
            definition={definition}
            onChange={change}
            editable
            onError={setError}
          />
        )}{" "}
        {tab === "rules" && <DomView render={() => rulesSection(state)} />}{" "}
        {tab === "unused" && (
          <div className="panel">
            <UnusedFields definition={definition} />
          </div>
        )}{" "}
        {tab === "parameters" && (
          <section className="panel">
            <div className="page-heading">
              <h2>Параметры и источники</h2>
              <Button size={40} onClick={() => editParameter(state, -1)}>
                Добавить параметр
              </Button>
            </div>
            <div className="table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>Поле</th>
                    <th>Источник</th>
                    <th>Обязательность</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {definition.parameters.map((p, i) => (
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
                        {p.required ? "Обязательное" : "Необязательное"}
                        {!p.enabled ? " · выключено" : ""}
                      </td>
                      <td>
                        <Button
                          size={32}
                          aria-label={"Изменить параметр " + p.id}
                          onClick={() => editParameter(state, i)}
                        >
                          Изменить
                        </Button>
                        <Button
                          size={32}
                          view="transparent"
                          aria-label={"Удалить параметр " + p.id}
                          onClick={() => {
                            if (
                              window.confirm(
                                "Удалить параметр «" +
                                  p.title +
                                  "»? Зависимости будут проверены перед применением.",
                              )
                            ) {
                              const next = copyJSON(definition);
                              next.parameters.splice(i, 1);
                              change(next);
                            }
                          }}
                        >
                          Удалить
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        )}{" "}
        {tab === "history" && (
          <>
            <section className="panel">
              <h2>Ревизии</h2>
              <div className="table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>Ревизия</th>
                      <th>Автор / время</th>
                      <th>Основание</th>
                    </tr>
                  </thead>
                  <tbody>
                    {revisions.map((r) => (
                      <tr key={String(r.revision)}>
                        <td>
                          <RouteLink
                            to={"draft/" + draft.id + "/revision/" + r.revision}
                          >
                            Ревизия {String(r.revision)}
                          </RouteLink>
                        </td>
                        <td>
                          {r.author}
                          <small className="block muted">
                            {new Date(r.created_at).toLocaleString("ru-RU")}
                          </small>
                        </td>
                        <td>{r.reason}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
            <section className="panel">
              <h2>JSON-определение модели</h2>
              <p className="muted">
                Изменения JSON сначала попадают в локальный черновик. Сохраните
                ревизию перед применением.
              </p>
              <div className="actions">
                <Button
                  size={40}
                  onClick={() => setJsonText(stringifyJSON(definition, true))}
                >
                  Открыть JSON для редактирования
                </Button>
                <Button
                  size={40}
                  onClick={() => downloadDefinition(definition)}
                >
                  Скачать текущий JSON
                </Button>
              </div>
              {jsonText && (
                <>
                  <textarea
                    className="json-editor"
                    aria-label="JSON модели"
                    value={jsonText}
                    onChange={(e) => setJsonText(e.target.value)}
                  />
                  <Button
                    size={40}
                    onClick={async () => {
                      try {
                        const value = parseJSON(jsonText);
                        if (
                          !value.parameters ||
                          !value.criteria ||
                          value.borrower_type !== definition.borrower_type ||
                          value.id !== definition.id
                        )
                          throw new Error(
                            "Сохраните структуру модели, тип заёмщика и идентификатор.",
                          );
                        await request("/api/admin/v1/field-catalog/usage", {
                          method: "POST",
                          body: { definition: value },
                        });
                        change(value);
                        setJsonText("");
                      } catch (e) {
                        setError(e);
                      }
                    }}
                  >
                    Обновить локальный черновик из JSON
                  </Button>
                </>
              )}
            </section>
          </>
        )}
      </fieldset>
      <section className="save-bar">
        <div>
          <label className="field">
            Основание сохранения / применения
            <input
              aria-label="Основание сохранения / применения"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Кратко опишите изменение"
            />
          </label>
        </div>
        <Button loading={pending} disabled={!dirty || pending} onClick={save}>
          Сохранить ревизию
        </Button>
        <Button
          view="primary"
          loading={pending}
          disabled={!valid || pending || !a || Boolean(success)}
          onClick={apply}
        >
          Применить
        </Button>
        <Button
          size={40}
          view="transparent"
          disabled={pending}
          onClick={async () => {
            if (!reason.trim()) {
              setError(new Error("Укажите основание удаления черновика."));
              return;
            }
            if (!window.confirm("Удалить черновик «" + definition.title + "»?"))
              return;
            setPending(true);
            setError(null);
            try {
              await request("/api/admin/v1/drafts/" + draft.id, {
                method: "DELETE",
                body: {
                  expected_revision: draft.revision,
                  reason: reason.trim(),
                },
              });
              dirtyRef.current = false;
              go("drafts");
            } catch (e) {
              setError(e);
            } finally {
              setPending(false);
            }
          }}
        >
          Удалить черновик
        </Button>
      </section>
    </>
  );
}

function LegacyPage({ route }: { route: string }) {
  const [content, setContent] = useState<HTMLElement | null>(null),
    [error, setError] = useState<unknown>(null);
  useEffect(() => {
    let alive = true;
    setContent(null);
    setError(null);
    const [path, query] = route.split("?"),
      params = new URLSearchParams(query);
    const page = Math.max(0, Number(params.get("page")) || 0);
    const pending =
      path === "workbench"
        ? workbenchPage(params)
        : path === "releases"
          ? releasesWithRuntime(page, go)
          : path.startsWith("event/")
            ? eventPage(decodeURIComponent(path.slice(6)))
            : eventsPage(params, page, go);
    pending
      .then((node) => {
        if (alive) setContent(node);
      })
      .catch((e) => alive && setError(e));
    return () => {
      alive = false;
    };
  }, [route]);
  return (
    <>
      <ErrorBox error={error} />
      {content ? <DomView render={() => content} /> : !error && <Loading />}
    </>
  );
}

export function App() {
  const [user, setUser] = useState<{
      username: string;
      csrf_token: string;
    } | null>(null),
    [ready, setReady] = useState(false),
    [route, setRoute] = useState(routeNow),
    [error, setError] = useState<unknown>(null);
  const previous = useRef(location.hash),
    restoring = useRef(false);
  const login = (value: { username: string; csrf_token: string }) => {
    session(value.csrf_token);
    setUser(value);
    setError(null);
  };
  useEffect(() => {
    let alive = true;
    request<{ username: string; csrf_token: string }>(
      "/api/admin/v1/auth/session",
      { public: true },
    )
      .then((u) => {
        if (alive) login(u);
      })
      .catch((e) => {
        if (alive && (e as { status: number }).status !== 401) setError(e);
      })
      .finally(() => alive && setReady(true));
    const expired = () => {
      setUser(null);
      session();
      setError(new Error("Сессия завершена. Войдите снова."));
    };
    window.addEventListener("admin-session-expired", expired);
    return () => {
      alive = false;
      window.removeEventListener("admin-session-expired", expired);
    };
  }, []);
  useEffect(() => {
    const changed = () => {
      if (restoring.current) {
        restoring.current = false;
        return;
      }
      if (!canLeave()) {
        restoring.current = true;
        location.hash = previous.current;
        return;
      }
      previous.current = location.hash;
      document.querySelectorAll<HTMLDialogElement>("dialog.editor-dialog").forEach((d) => {
        d.close();
        d.remove();
      });
      setRoute(routeNow());
      document.getElementById("main")?.focus();
    };
    window.addEventListener("hashchange", changed);
    return () => window.removeEventListener("hashchange", changed);
  }, []);
  if (!ready) return <Loading />;
  if (!user)
    return (
      <>
        <ErrorBox error={error} />
        <Login onLogin={login} />
      </>
    );
  const section = route.startsWith("draft")
    ? "drafts"
    : route.startsWith("model")
      ? "models"
      : route.startsWith("event")
        ? "messages"
        : route.split("?")[0];
  return (
    <div className="app">
      <a
        className="skip-link"
        href="#main"
        onClick={(e) => {
          e.preventDefault();
          document.getElementById("main")?.focus();
        }}
      >
        Перейти к содержимому
      </a>
      <header className="app-header">
        <RouteLink to="models" className="brand">
          <AlfaBankLogoMIcon className="alpha-logo" />
          <strong>Альфа Бизнес</strong>
          <span className="brand-divider" />
          <span>Конструктор скоринга</span>
        </RouteLink>
        <div className="account">
          <span className="avatar">
            {user.username.slice(0, 2).toUpperCase()}
          </span>
          <span>{user.username}</span>
          <Button
            view="transparent"
            size={40}
            onClick={async () => {
              if (!canLeave()) return;
              try {
                await request("/api/admin/v1/auth/logout", { method: "POST" });
                setUser(null);
                session();
              } catch (e) {
                setError(e);
              }
            }}
          >
            Выйти
          </Button>
        </div>
      </header>
      <div className="app-body">
        <aside className="sidebar">
          <p className="eyebrow">Управление моделями</p>
          <nav aria-label="Основная навигация">
            {[
              ["models", "Модели"],
              ["drafts", "Черновики"],
              ["data", "Неиспользуемые данные"],
              ["workbench", "Проверка и выпуск"],
              ["releases", "Релизы"],
              ["messages", "Сообщения"],
            ].map(([id, title]) => (
              <a
                key={id}
                aria-current={section === id ? "page" : undefined}
                href={"#" + id}
                onClick={(e) => {
                  e.preventDefault();
                  go(id);
                }}
              >
                {title}
              </a>
            ))}
          </nav>
          <div className="sidebar-help">
            <span className="badge success">JSON-модели</span>
            <p>
              Изменения вступают в силу после применения проверенной ревизии.
            </p>
          </div>
        </aside>
        <main id="main" tabIndex={-1} className="content">
          <ErrorBox error={error} />
          {route === "models" || route === "drafts" ? (
            <ModelsPage key={route} drafts={route === "drafts"} />
          ) : route === "data" ? (
            <UnusedDataPage />
          ) : route.startsWith("model/") || route.startsWith("draft/") ? (
            <ModelPage key={route} route={route} />
          ) : ["workbench", "releases", "messages", "event"].includes(
              section,
            ) ? (
            <LegacyPage route={route} />
          ) : (
            <Heading
              title="Страница не найдена"
              description="Выберите раздел в меню."
            />
          )}
        </main>
      </div>
    </div>
  );
}
