import type { CollectionSource, CollectorApplication, CollectorStatus, CollectorSystem } from '../../api/collector/types';
import type { ApplicationSummary, ClientStage } from '../../api/types';

/** id заявки data-collector — ULID (app_ + 26 символов); у демо-заявок моков — app_000147. */
export const isCollectorId = (id: string | undefined) => Boolean(id && /^app_[0-9A-Z]{26}$/.test(id));

type StatusColor = 'green' | 'orange' | 'red' | 'blue' | 'grey';

/** Статусы заявки data-collector (FSM docs/fsm.md): сбор данных и оценка credit-engine. */
export const COLLECTOR_STATUS: Record<CollectorStatus, { label: string; color: StatusColor; title: string; subtitle: string; hint: string; stage: ClientStage }> = {
    DRAFT: { label: 'Черновик', color: 'grey', title: 'Анкета не отправлена', subtitle: 'Продолжите заполнение', hint: 'Продолжить заполнение', stage: 'FILLING' },
    SUBMITTED: { label: 'Отправлена', color: 'blue', title: 'Заявка принята', subtitle: 'Сейчас начнём собирать данные о компании', hint: 'Скоро начнём сбор данных', stage: 'REVIEW' },
    ENRICHING: { label: 'Собираем данные', color: 'blue', title: 'Собираем данные о компании', subtitle: 'Опрашиваем реестры, бюро и банки — обычно это пара минут', hint: 'Опрашиваем реестры и бюро', stage: 'REVIEW' },
    VERIFYING: {
        label: 'Данные собраны',
        color: 'green',
        title: 'Данные о компании собраны',
        subtitle: 'Заявка ушла на проверку. Решение банка появится здесь',
        hint: 'Ждёт проверки банком',
        stage: 'REVIEW',
    },
    SCORING: { label: 'На рассмотрении', color: 'blue', title: 'Банк оценивает заявку', subtitle: 'Считаем скоринг и подбираем условия — обычно это минута', hint: 'Банк оценивает заявку', stage: 'REVIEW' },
    MANUAL_REVIEW: {
        label: 'На рассмотрении',
        color: 'blue',
        title: 'Заявку рассматривает кредитный специалист',
        subtitle: 'Решение появится здесь, обычно в течение рабочего дня',
        hint: 'Рассматривает специалист',
        stage: 'REVIEW',
    },
    WAITING_FOR_INFO: { label: 'Нужно уточнить', color: 'orange', title: 'Банку нужны дополнительные сведения', subtitle: 'Ответьте на вопросы банка', hint: 'Ответьте на вопросы банка', stage: 'ACTION_REQUIRED' },
    OFFERED: { label: 'Есть предложение', color: 'green', title: 'Банк одобрил кредит', subtitle: 'Посмотрите предложение', hint: 'Посмотрите предложение', stage: 'OFFER' },
    SIGNING: { label: 'Подписание', color: 'orange', title: 'Договор готов к подписанию', subtitle: 'Ознакомьтесь с договором и подтвердите его кодом', hint: 'Подпишите договор', stage: 'SIGNING' },
    COMPLETED: { label: 'Оформлен', color: 'green', title: 'Кредит оформлен', subtitle: 'Договор подписан, демонстрационная выдача завершена', hint: 'Договор подписан', stage: 'DONE' },
    REJECTED: { label: 'Отказ', color: 'red', title: 'Банк отказал в кредите', subtitle: 'Причины — ниже', hint: 'Банк отказал', stage: 'CLOSED' },
};

/** Сбор не удался: задача FAILED, а заявка у data-collector так и остаётся в ENRICHING. */
export const COLLECTION_FAILED = {
    label: 'Сбор не удался',
    color: 'red' as StatusColor,
    title: 'Не получилось собрать данные о компании',
    subtitle: 'Сервис сбора остановился после нескольких попыток. Заявка сохранена, но сама дальше не пойдёт — обратитесь к менеджеру банка',
    hint: 'Сбор данных не удался',
};

export const SOURCE_STATE: Record<NonNullable<CollectionSource['state']>, { label: string; color: StatusColor }> = {
    DONE: { label: 'получено', color: 'green' },
    PARTIAL: { label: 'частично', color: 'orange' },
    FAILED: { label: 'не ответил', color: 'red' },
    SKIPPED: { label: 'пропущен', color: 'grey' },
};

/** Причина пропуска по-человечески; у data-collector она с именами полей анкеты. */
const SKIP_REASONS: Partial<Record<CollectorSystem, string>> = {
    BKI: 'нет согласия на запрос в БКИ',
    TAX_REPORTS: 'нет согласия на налоговую отчётность',
    EGRN: 'нет недвижимости в залог',
    GOSUSLUGI: 'не указан руководитель',
    OPEN_BANKING: 'не указаны счета в других банках',
    MARKETPLACES: 'нет токенов продавца',
    OFD: 'нет токена ОФД',
};

export const sourceNote = (s: CollectionSource) => (s.state === 'SKIPPED' && s.source ? (SKIP_REASONS[s.source] ?? s.reason) : s.reason);

/** Строка для общего списка «Мои заявки» на главной. */
export function collectorSummary(app: CollectorApplication, draft?: { product_code?: string; requested?: { amount: number; term_months: number } }): ApplicationSummary {
    const status = app.status ?? 'DRAFT';
    return {
        id: app.id ?? '',
        number: app.number ?? '',
        version: 0,
        status,
        client_stage: COLLECTOR_STATUS[status].stage,
        applicant: { id: app.applicant?.id ?? '', short_name: app.applicant?.name ?? '', inn: app.applicant?.inn ?? '' },
        product_code: app.product_code ?? draft?.product_code ?? '',
        requested: app.requested?.amount && app.requested.term_months ? { amount: app.requested.amount, term_months: app.requested.term_months } : draft?.requested,
        created_at: app.created_at ?? '',
        updated_at: app.updated_at ?? app.created_at ?? '',
    };
}
