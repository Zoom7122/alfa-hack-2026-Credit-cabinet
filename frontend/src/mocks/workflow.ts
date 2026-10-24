import type { ApplicationStatus, ApplicationStatusView, ClientStage, TimelineEvent, UserRef } from '../api/types';
import { directorOf, type StoredApplication, testUsers } from './data';
import { ACTIONS, stageOf } from './fsm';
import { approvedDecision, buildOffers, powerOfAttorneyRequest } from './seedWorkflow';
import { mutate, read, type StoredWsEvent } from './store';

/* Конвейер рассмотрения в моке: заявка сама идёт по статусам FSM, о каждом шаге — событие для WebSocket. */

const PIPELINE_KEY = 'credit-cabinet:mock-pipeline-ms';
const DEFAULT_STEP_MS = 2500;
const MAX_EVENTS = 300;

/** Длительность шага конвейера; e2e ускоряют его флагом в localStorage. */
function stepMs() {
    try {
        const value = Number(localStorage.getItem(PIPELINE_KEY));
        return value > 0 ? value : DEFAULT_STEP_MS;
    } catch {
        return DEFAULT_STEP_MS;
    }
}

export function emit(applicantId: string, type: string, data: Record<string, unknown>) {
    mutate((d) => {
        d.seq += 1;
        d.events.push({ id: `evt_${String(d.seq).padStart(10, '0')}`, type, at: new Date().toISOString(), data, applicant_id: applicantId });
        if (d.events.length > MAX_EVENTS) d.events.splice(0, d.events.length - MAX_EVENTS);
    });
}

export function addTimeline(app: StoredApplication, fields: Omit<TimelineEvent, 'id' | 'at'>) {
    mutate((d) => {
        d.seq += 1;
        d.timeline.push({ id: `tle_${d.seq}`, application_id: app.id, at: new Date().toISOString(), ...fields });
    });
}

export function schedule(appId: string, steps = 1) {
    mutate((d) => {
        d.pipeline[appId] = Date.now() + stepMs() * steps;
    });
}

type TransitionOptions = {
    message: string;
    type?: TimelineEvent['type'];
    actor?: UserRef;
    next?: boolean;
};

/** Переход FSM: статус, версия, запись в историю, событие application.status_changed. */
export function transition(app: StoredApplication, to: ApplicationStatus, { message, type = 'STATUS_CHANGED', actor, next }: TransitionOptions) {
    const from = app.status;
    const updated = mutate((d) => {
        const index = d.applications.findIndex((a) => a.id === app.id);
        const current = d.applications[index] ?? app;
        const result: StoredApplication = {
            ...current,
            status: to,
            client_stage: stageOf(to),
            version: current.version + 1,
            updated_at: new Date().toISOString(),
        };
        d.applications[index] = result;
        delete d.pipeline[app.id];
        return result;
    });
    addTimeline(updated, { type, from_status: from, to_status: to, actor_kind: actor ? 'CLIENT' : 'SYSTEM', actor, message });
    emit(app.applicant.id, 'application.status_changed', {
        application_id: app.id,
        from,
        to,
        version: updated.version,
        client_stage: updated.client_stage,
    });
    if (next) schedule(app.id);
    return updated;
}

/** Заявку подал не директор, а доверенности ещё нет — банк её запросит (verify.missing). */
function needsPowerOfAttorney(app: StoredApplication) {
    const creatorId = app.created_by?.id;
    const creator = testUsers.find((u) => u.id === creatorId);
    const role = [...(creator?.companies ?? []), ...(read().links[creatorId ?? ''] ?? [])].find(
        (c) => c.applicant_id === app.applicant.id,
    )?.role;
    if (!role || role === 'DIRECTOR') return false;
    const hasDocument = read().documents.some((doc) => doc.application_id === app.id && doc.type === 'POWER_OF_ATTORNEY');
    const asked = read().infoRequests.some((r) => r.application_id === app.id && r.documents?.accepted_types?.includes('POWER_OF_ATTORNEY'));
    return !hasDocument && !asked;
}

/** Один шаг конвейера по заявке. */
function advance(app: StoredApplication) {
    switch (app.status) {
        case 'SUBMITTED':
            return transition(app, 'ENRICHING', { message: 'Заявка принята', next: true });
        case 'ENRICHING':
            return transition(app, 'VERIFYING', { message: 'Данные о компании собраны', next: true });
        case 'VERIFYING': {
            if (needsPowerOfAttorney(app)) {
                const index = read().infoRequests.filter((r) => r.application_id === app.id).length + 1;
                const request = powerOfAttorneyRequest(app, Date.now(), index);
                mutate((d) => d.infoRequests.push(request));
                const updated = transition(app, 'WAITING_FOR_INFO', { message: 'Банк задал вопросы', type: 'INFO_REQUESTED' });
                emit(app.applicant.id, 'info_request.created', { application_id: app.id, info_request_id: request.id });
                return updated;
            }
            return transition(app, 'SCORING', { message: 'Данные о компании проверены', next: true });
        }
        case 'SCORING':
            return transition(app, 'DECIDING', { message: 'Финансы оценены', next: true });
        case 'DECIDING': {
            // персонаж «ИП Смирнов»: серая зона скоринга, решение за андеррайтером
            if (app.applicant.id === 'apl_smirnov') {
                return transition(app, 'MANUAL_REVIEW', { message: 'Заявку смотрит специалист банка' });
            }
            const offers = buildOffers(app, Date.now());
            mutate((d) => {
                d.offers = d.offers.filter((o) => o.application_id !== app.id).concat(offers);
                d.decisions[app.id] = approvedDecision(new Date().toISOString());
            });
            const updated = transition(app, 'OFFERED', { message: 'Банк подготовил предложения', type: 'OFFERS_ISSUED' });
            emit(app.applicant.id, 'offers.issued', { application_id: app.id, count: offers.length, kind: 'REQUESTED' });
            return updated;
        }
        default:
            return app;
    }
}

/** Таймеры FSM: истёк срок предложений (OFFERED → EXPIRED) или ответа на вопросы (WAITING_FOR_INFO → EXPIRED). */
function expire(now: number) {
    const iso = new Date(now).toISOString();
    const expiredOffers = read().offers.filter((o) => o.state === 'ACTIVE' && o.valid_until < iso);
    if (expiredOffers.length) {
        const ids = new Set(expiredOffers.map((o) => o.id));
        mutate((d) => {
            d.offers = d.offers.map((o) => (ids.has(o.id) ? { ...o, state: 'EXPIRED' } : o));
        });
    }
    for (const app of read().applications) {
        if (app.status === 'OFFERED' && !read().offers.some((o) => o.application_id === app.id && o.state === 'ACTIVE')) {
            transition(app, 'EXPIRED', { message: 'Срок действия предложений истёк' });
        }
        if (app.status === 'WAITING_FOR_INFO') {
            const open = read().infoRequests.filter((r) => r.application_id === app.id && r.state === 'OPEN' && r.required);
            if (open.length && open.every((r) => r.due_at && r.due_at < iso)) {
                transition(app, 'EXPIRED', { message: 'Истёк срок ответа на вопросы банка' });
            }
        }
    }
}

/** Сделать все шаги, время которых пришло. Вызывается на каждом запросе к моку и по таймеру сокета. */
export function tick(now = Date.now()) {
    expire(now);
    for (const [id, at] of Object.entries(read().pipeline)) {
        if (at > now) continue;
        const app = read().applications.find((a) => a.id === id);
        if (app) advance(app);
        else mutate((d) => delete d.pipeline[id]);
    }
}

/** События после lastId по компаниям пользователя. */
export function eventsSince(lastId: string, applicantIds: Set<string>): StoredWsEvent[] {
    return read().events.filter((e) => e.id > lastId && applicantIds.has(e.applicant_id));
}

export const lastEventId = () => read().events.at(-1)?.id ?? '';

// ---------------------------------------------------------------- вид статуса для клиента

/** Тексты этапов — docs/fsm.md, раздел «Что видит клиент». */
const STAGE_TEXT: Record<ClientStage, { title: string; subtitle?: string }> = {
    FILLING: { title: 'Заполнение заявки', subtitle: 'Черновик сохраняется автоматически' },
    REVIEW: { title: 'Рассматриваем заявку', subtitle: 'Обычно это несколько минут. Можно закрыть страницу — статус обновится сам.' },
    ACTION_REQUIRED: { title: 'Нужно уточнить пару моментов', subtitle: 'Ответьте на вопросы банка' },
    OFFER: { title: 'Решение готово', subtitle: 'Выберите подходящее предложение' },
    SIGNING: { title: 'Подписание договора' },
    DONE: { title: 'Кредит выдан' },
    CLOSED: { title: 'Заявка закрыта' },
};

const STAGE_ORDER: ClientStage[] = ['FILLING', 'REVIEW', 'OFFER', 'SIGNING', 'DONE'];
const STAGE_TITLES = ['Заявка', 'Рассмотрение', 'Решение', 'Подписание', 'Выдача'];

/** До какого этапа заявка дошла перед закрытием — по статусу, из которого её закрыли (журнал). */
function reachedBeforeClose(app: StoredApplication): number {
    if (app.status === 'DECLINED') return 2;
    if (app.status === 'REJECTED') return 1;
    const from = read()
        .timeline.filter((e) => e.application_id === app.id && e.to_status === app.status)
        .at(-1)?.from_status;
    if (!from) return 0;
    const stage = stageOf(from);
    return Math.max(0, STAGE_ORDER.indexOf(stage === 'ACTION_REQUIRED' ? 'REVIEW' : stage));
}

const earliest = (dates: (string | null | undefined)[]) =>
    dates.filter((d): d is string => Boolean(d)).sort()[0] ?? null;

export function signerOf(app: StoredApplication): UserRef {
    const applicant = read().applicants.find((a) => a.id === app.applicant.id);
    return directorOf(app.applicant.id) ?? { id: 'unknown', full_name: applicant?.director_name ?? 'Директор компании' };
}

/** ApplicationStatusView для конкретного пользователя (awaiting_me зависит от того, кто смотрит). */
export function statusViewFor(app: StoredApplication, viewerId: string): ApplicationStatusView {
    const closed = app.client_stage === 'CLOSED';
    const current = app.client_stage === 'ACTION_REQUIRED' ? 'REVIEW' : app.client_stage;
    const idx = closed ? reachedBeforeClose(app) : STAGE_ORDER.indexOf(current);
    const text = STAGE_TEXT[app.client_stage];
    const isSigning = app.status === 'SIGNING';
    const signer = signerOf(app);
    const awaitingMe = isSigning && viewerId === signer.id;
    const openRequests = read().infoRequests.filter((r) => r.application_id === app.id && r.state === 'OPEN');
    const activeOffers = read().offers.filter((o) => o.application_id === app.id && o.state === 'ACTIVE');

    const actions = [...(ACTIONS[app.status] ?? [])];
    if (isSigning) actions.push(awaitingMe ? 'SIGN' : 'START_SIGNING');

    const subtitle =
        app.status === 'MANUAL_REVIEW'
            ? 'Заявку смотрит специалист банка — обычно это занимает до одного рабочего дня'
            : isSigning && !awaitingMe
              ? 'Ждём подписи директора'
              : text.subtitle;

    return {
        id: app.id,
        version: app.version,
        status: app.status,
        client_stage: app.client_stage,
        title: text.title,
        subtitle,
        stages: STAGE_ORDER.map((code, i) => ({
            code,
            title: STAGE_TITLES[i] ?? code,
            // кредит выдан — пройдены все этапы, текущего нет
            state: closed ? (i <= idx ? 'DONE' : 'SKIPPED') : i < idx || app.client_stage === 'DONE' ? 'DONE' : i === idx ? 'CURRENT' : 'UPCOMING',
        })),
        allowed_actions: actions,
        open_info_requests: openRequests.length,
        offers_available: activeOffers.length,
        decision_outcome: read().decisions[app.id]?.outcome ?? null,
        signing: isSigning ? { required_signer: signer, awaiting_me: awaitingMe } : null,
        deadline_at:
            app.status === 'WAITING_FOR_INFO'
                ? earliest(openRequests.map((r) => r.due_at))
                : app.status === 'OFFERED'
                  ? earliest(activeOffers.map((o) => o.valid_until))
                  : null,
        updated_at: app.updated_at,
    };
}
