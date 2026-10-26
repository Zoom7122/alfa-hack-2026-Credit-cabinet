import { delay, HttpResponse, http } from 'msw';

import type { CollectorStatus } from '../../api/collector/types';
import { registry } from '../data';
import { authenticate, iso, notFound, problem, scoredAt, statusOf, timelineOf, toApplication, toTask, unauthorized, url, validation } from './handlers';
import { offerOf, questionsOf, scoredOutcome } from './outcome';
import { scenarioByInn } from './scenarios';
import { mutateCollector, readCollector, type StoredCollectorApp, type StoredDecision, type StoredReview } from './store';

/* Мок рабочего места андеррайтера data-collector (/api/v1/backoffice/*): поведение — как у Go
   (internal/service/backoffice.go): очередь, досье, взять в работу, решение с If-Match и Idempotency-Key. */

type Field = { field: string; code: string; message: string };

/** Что «посчитал движок» по сценарию mock-api — для досье. */
type Engine = {
    grade?: string;
    total?: number;
    reasons: { code: string; message: string; fields: string[] }[];
    proposed?: { amount: number; term_months: number; rate_annual: number };
    findings: { code: string; message: string; material: boolean; severity: string }[];
};

const DEFAULT_ENGINE: Engine = {
    grade: 'C',
    total: 640,
    reasons: [{ code: 'NEEDS_REVIEW', message: 'Клиент прислал документы — нужна проверка специалистом.', fields: [] }],
    proposed: { amount: 3_000_000, term_months: 36, rate_annual: 22.5 },
    findings: [],
};

const ENGINES: Record<string, Engine> = {
    '7799000012': { grade: 'A', total: 873, reasons: [], proposed: { amount: 2_030_000, term_months: 36, rate_annual: 20.75 }, findings: [] },
    '7799000020': {
        grade: 'B',
        total: 738,
        reasons: [{ code: 'MANY_CREDIT_APPLICATIONS', message: 'За 90 дней было не менее восьми кредитных обращений.', fields: ['scoring.legal_entity.applications_90d'] }],
        findings: [
            { code: 'UNDECLARED_DEBT', message: 'В выписке есть платежи по долгам, которых нет в кредитной истории: ООО "ЛИЗИНГ-ПРО"', material: true, severity: 'WARN' },
        ],
    },
    '7799000037': {
        reasons: [
            { code: 'BANKRUPT', message: 'Подтверждено банкротство заёмщика.', fields: [] },
            { code: 'CURRENT_DPD_30_PLUS', message: 'Текущая просрочка составляет не менее 30 дней.', fields: [] },
        ],
        findings: [],
    },
};

const engineOf = (inn: string) => ENGINES[inn] ?? DEFAULT_ENGINE;

const FACTORS = [
    ['LE_Q01', 'Сколько месяцев работает действующий бизнес организации', 'QUESTIONNAIRE', 'business_age_months=67', 60, 60],
    ['LE_Q03', 'Какова худшая погашенная просрочка организации за 24 месяца, дней', 'QUESTIONNAIRE', 'worst_repaid_dpd_days_24m=12', 50, 100],
    ['LE_Q05', 'Сколько фактических кредитных обращений организации было за 90 дней', 'QUESTIONNAIRE', 'applications_90d=9', 0, 30],
    ['LE_E01', 'Текущая ликвидность', 'ECONOMICS', undefined, 15, 20],
    ['LE_E11', 'Рентабельность продаж', 'ECONOMICS', undefined, 7.5, 15],
] as const;

function underwriter(request: Request) {
    const { user, error } = authenticate(request);
    if (error) return { error };
    if (!user) return { error: unauthorized('нужен токен: Authorization: Bearer mock.<user_id>') };
    if (!user.roles.includes('UNDERWRITER')) return { error: problem(403, 'FORBIDDEN', 'Forbidden', 'рабочее место банка — только для андеррайтера') };
    return { user };
}

const review = (app: StoredCollectorApp): StoredReview => app.review ?? { version: 1, decisions: [], events: [], keys: [] };

function slaOf(app: StoredCollectorApp, status: CollectorStatus) {
    if (status !== 'MANUAL_REVIEW') return null;
    return app.review?.deadline ?? iso(scoredAt(app) + 4 * 3_600_000);
}

function listItem(app: StoredCollectorApp) {
    const status = statusOf(app);
    const scenario = scenarioByInn(app.company.inn);
    const engine = engineOf(app.company.inn);
    const task = toTask(app);
    const q = app.questionnaire;
    return {
        id: app.id,
        number: app.number,
        status,
        version: review(app).version,
        applicant: { id: app.company.id, short_name: scenario?.egrulName ?? app.company.name ?? `ИНН ${app.company.inn}`, inn: app.company.inn,
            type: app.company.inn.length === 12 ? 'SOLE_PROPRIETOR' : 'LEGAL_ENTITY' },
        product_code: q?.product_code,
        requested: q?.requested,
        grade: engine.grade,
        score: engine.total,
        manual_review_reasons: status === 'MANUAL_REVIEW' ? engine.reasons.map((r) => r.message) : [],
        degraded_sources: (task.sources ?? []).filter((s) => s.state === 'FAILED' || s.state === 'PARTIAL').map((s) => s.source),
        assignee: app.review?.assignee ?? null,
        submitted_at: app.submitted_ms === undefined ? app.created_at : iso(app.submitted_ms),
        sla_due_at: slaOf(app, status),
    };
}

function dossier(app: StoredCollectorApp) {
    const status = statusOf(app);
    const scenario = scenarioByInn(app.company.inn);
    const prefill = registry.find((r) => r.inn === app.company.inn);
    const engine = engineOf(app.company.inn);
    const outcome = scoredOutcome(app.company.inn);
    const task = toTask(app);
    const r = review(app);
    const decided = !['DRAFT', 'SUBMITTED', 'ENRICHING', 'VERIFYING'].includes(status);
    const latest = r.decisions.at(-1);
    const offers = latest?.action === 'APPROVE'
        ? (latest.offers ?? []).map((o, i) => ({ id: `ofr_${app.id.slice(4)}_${i}`, state: 'ACTIVE', recommended: i === 0, collateral_required: [], conditions: [], ...o }))
        : status === 'OFFERED' ? [offerOf(app, scoredAt(app))] : [];
    const staff = r.events.map((e) => ({ id: e.id, at: e.at, type: e.type, message: e.message, actor_kind: e.actor ? 'EMPLOYEE' : 'SYSTEM', actor: e.actor ?? null, internal: e.internal }));
    const client = timelineOf(app).filter((e) => !r.events.some((x) => x.id === e.id)).map((e) => ({ ...e, actor: null, internal: false }));
    return {
        application: toApplication(app),
        version: r.version,
        assignee: r.assignee ?? null,
        sla_due_at: slaOf(app, status),
        applicant: {
            id: app.company.id,
            inn: app.company.inn,
            type: app.company.inn.length === 12 ? 'SOLE_PROPRIETOR' : 'LEGAL_ENTITY',
            short_name: scenario?.egrulName ?? app.company.name ?? `ИНН ${app.company.inn}`,
            full_name: prefill?.full_name,
            ogrn: scenario?.ogrn ?? prefill?.ogrn,
            registration_date: prefill?.registration_date,
            director_name: prefill?.director_name,
            region: prefill?.legal_address?.full,
            okved: prefill?.okved_main?.code,
            financial_statements: [
                { year: 2024, revenue: 46_310_000, net_profit: 1_980_000, total_assets: 17_260_000, equity: 4_920_000 },
                { year: 2025, revenue: 58_420_000, net_profit: 2_730_000, total_assets: 21_430_000, equity: 7_650_000 },
            ],
            credit_history: { has_history: true, active_accounts: 3, total_outstanding: 3_690_000, inquiries_90d: app.company.inn === '7799000020' ? 9 : 4 },
            enforcement: { active_amount: 48_250 },
            litigation: { defendant_active_amount: 128_400, bankruptcy_as_debtor: app.company.inn === '7799000037' },
            bank_relationship: { client_since: '2022-03-14' },
            risk_flags: app.company.inn === '7799000037' ? ['Дело о банкротстве', 'Картотека, блокировка или арест счёта'] : [],
        },
        sources: (task.sources ?? []).map((s) => ({
            code: s.source,
            state: s.state,
            criticality: ['CORE_BANKING', 'FNS', 'BKI', 'COMPLIANCE', 'FEDRESURS'].includes(s.source ?? '') ? 'CRITICAL' : 'NORMAL',
            reason: s.reason ?? null,
            calls: s.calls,
            failed_calls: s.failed_calls,
            duration_ms: s.duration_ms,
            fetched_at: task.finished_at ?? null,
        })),
        findings: decided ? engine.findings : [],
        score: decided && engine.total
            ? {
                  model_version: app.company.inn.length === 12 ? 'ip-v2' : 'legal-v2',
                  engine: 'SCORECARD',
                  total: engine.total,
                  raw_total: engine.total,
                  grade: engine.grade,
                  computed_at: iso(scoredAt(app)),
                  factors: FACTORS.map(([code, title, part, answer, points, max]) => ({ code, title, part, answer, value: answer ? undefined : 1.4, points, max_points: max })),
              }
            : null,
        engine_decision: decided
            ? {
                  outcome: outcome === 'OFFERED' ? 'APPROVED' : outcome === 'REJECTED' ? 'REJECTED' : outcome === 'WAITING_FOR_INFO' ? 'NEED_INFO' : 'MANUAL_REVIEW',
                  engine_status: outcome === 'OFFERED' ? 'APPROVED' : outcome === 'REJECTED' ? 'REJECTED' : outcome === 'WAITING_FOR_INFO' ? 'NEED_INFO' : 'UNDER_REVIEW',
                  review_reason: outcome === 'MANUAL_REVIEW' ? 'BORROWER' : undefined,
                  reasons: engine.reasons,
                  proposed_terms: engine.proposed ? { ...engine.proposed, monthly_payment: annuity(engine.proposed.amount, engine.proposed.rate_annual, engine.proposed.term_months), confirmed: outcome === 'OFFERED' } : null,
              }
            : null,
        underwriter_decision: latest ? { outcome: latest.outcome, decided_by: latest.user, comment: latest.comment, decided_at: latest.at } : null,
        offers,
        info_requests: questionsOf(app, scoredAt(app)),
        other_applications: readCollector()
            .apps.filter((a) => a.id !== app.id && a.company.inn === app.company.inn && a.submitted_ms !== undefined)
            .map(listItem),
        timeline: [...client, ...staff].sort((a, b) => a.at.localeCompare(b.at)),
    };
}

function annuity(amount: number, rate: number, months: number) {
    const r = rate / 100 / 12;
    if (!r) return Math.round(amount / months);
    const k = (r * (1 + r) ** months) / ((1 + r) ** months - 1);
    return Math.round(amount * k);
}

const STATUSES = ['DRAFT', 'SUBMITTED', 'ENRICHING', 'VERIFYING', 'SCORING', 'MANUAL_REVIEW', 'WAITING_FOR_INFO', 'OFFERED', 'REJECTED'];

const conflict = (code: string, detail: string) => problem(409, code, 'Conflict', detail);

type DecisionBody = {
    action?: string;
    offers?: { kind?: string; product_code?: string; amount?: number; term_months?: number; rate_annual?: number; conditions?: string[]; guarantee_required?: boolean; valid_days?: number }[];
    client_reasons?: { code: string; title: string; description?: string }[];
    info_requests?: { title?: string; description?: string; target_field?: string; required?: boolean; input?: unknown; documents?: unknown }[];
    expert_adjustments?: { code?: string; points?: number; comment?: string }[];
    internal_comment?: string;
};

export const collectorBackofficeHandlers = [
    http.get(url('/backoffice/applications'), async ({ request }) => {
        await delay(200);
        const { user, error } = underwriter(request);
        if (error) return error;
        const params = new URL(request.url).searchParams;
        const statuses = (params.get('status') ?? '').split(',').map((x) => x.trim().toUpperCase()).filter(Boolean);
        if (statuses.some((x) => !STATUSES.includes(x))) {
            return validation(422, 'неизвестный статус', [{ field: 'status', code: 'invalid_value', message: `допустимые значения: ${STATUSES.join(', ')}` }]);
        }
        const assignee = params.get('assignee') === 'me' ? user.id : (params.get('assignee') ?? '');
        const limit = Math.min(Number(params.get('limit')) || 50, 100);
        const offset = Math.max(Number(params.get('cursor')) || 0, 0);
        const all = readCollector().apps.filter((a) => a.submitted_ms !== undefined).map(listItem);
        const filtered = all
            .filter((i) => !statuses.length || statuses.includes(i.status))
            .filter((i) => !assignee || (assignee === 'none' ? !i.assignee : i.assignee?.id === assignee))
            .sort((a, b) => {
                const manual = Number(b.status === 'MANUAL_REVIEW') - Number(a.status === 'MANUAL_REVIEW');
                return manual || (a.sla_due_at ?? '~').localeCompare(b.sla_due_at ?? '~') || b.submitted_at.localeCompare(a.submitted_at);
            });
        const page = filtered.slice(offset, offset + limit);
        const manual = all.filter((i) => i.status === 'MANUAL_REVIEW');
        const endOfDay = new Date();
        endOfDay.setHours(23, 59, 59, 999);
        return HttpResponse.json({
            items: page,
            next_cursor: offset + limit < filtered.length ? String(offset + limit) : null,
            counters: {
                manual_review: manual.length,
                mine: manual.filter((i) => i.assignee?.id === user.id).length,
                due_today: manual.filter((i) => i.sla_due_at && new Date(i.sla_due_at) <= endOfDay).length,
                waiting_for_info: all.filter((i) => i.status === 'WAITING_FOR_INFO').length,
            },
        });
    }),

    http.get(url('/backoffice/applications/:id'), async ({ request, params }) => {
        await delay(200);
        const { error } = underwriter(request);
        if (error) return error;
        const app = readCollector().apps.find((a) => a.id === params.id);
        if (!app) return notFound('заявка не найдена');
        return HttpResponse.json(dossier(app), { headers: { ETag: `"${review(app).version}"` } });
    }),

    http.post(url('/backoffice/applications/:id/assign'), async ({ request, params }) => {
        await delay(250);
        const { user, error } = underwriter(request);
        if (error) return error;
        const app = readCollector().apps.find((a) => a.id === params.id);
        if (!app) return notFound('заявка не найдена');
        if (statusOf(app) !== 'MANUAL_REVIEW') return conflict('INVALID_TRANSITION', 'заявка не на ручном рассмотрении');
        const current = app.review?.assignee;
        if (current && current.id !== user.id) return conflict('CONFLICT', 'заявку уже взял другой андеррайтер');
        const updated = mutateCollector((d) => {
            const stored = d.apps.find((a) => a.id === app.id) ?? app;
            if (!current) {
                const r = review(stored);
                r.assignee = { id: user.id, full_name: user.full_name };
                r.version += 1;
                r.events.push({ id: `evt_uw_${Date.now()}`, at: iso(Date.now()), type: 'uw.assigned', message: 'Взял(а) заявку в работу', internal: true, actor: r.assignee });
                stored.review = r;
            }
            return stored;
        });
        return HttpResponse.json(listItem(updated));
    }),

    http.post(url('/backoffice/applications/:id/decision'), async ({ request, params }) => {
        await delay(400);
        const { user, error } = underwriter(request);
        if (error) return error;
        const app = readCollector().apps.find((a) => a.id === params.id);
        if (!app) return notFound('заявка не найдена');
        const key = request.headers.get('Idempotency-Key')?.trim() ?? '';
        if (key && review(app).keys.includes(key)) return HttpResponse.json(dossier(app));
        let body: DecisionBody;
        try {
            body = (await request.json()) as DecisionBody;
        } catch {
            return validation(400, 'тело запроса — не JSON', []);
        }
        if (!body.action) return validation(400, 'не заполнены обязательные поля', [{ field: 'action', code: 'required', message: 'обязательное поле' }]);
        const r = review(app);
        if (statusOf(app) !== 'MANUAL_REVIEW') return conflict('INVALID_TRANSITION', 'заявка не на ручном рассмотрении');
        if (r.assignee && r.assignee.id !== user.id) return conflict('CONFLICT', 'заявку уже взял другой андеррайтер');
        const ifMatch = request.headers.get('If-Match')?.replace(/^W\//, '').replace(/"/g, '').trim();
        if (ifMatch && Number(ifMatch) !== r.version) return problem(412, 'VERSION_CONFLICT', 'Precondition failed', 'заявка изменилась — обновите досье');

        const errors: Field[] = [];
        const add = (field: string, code: string, message: string) => errors.push({ field, code, message });
        const now = Date.now();
        const requested = app.questionnaire?.requested;
        const engine = engineOf(app.company.inn);
        const decision: StoredDecision = { action: body.action.toUpperCase() as StoredDecision['action'], outcome: '', user: { id: user.id, full_name: user.full_name }, at: iso(now), comment: body.internal_comment?.trim() || undefined };
        (body.expert_adjustments ?? []).forEach((a, i) => {
            if (!a.code?.trim()) add(`expert_adjustments[${i}].code`, 'required', 'обязательное поле');
            if (!a.points || Math.abs(a.points) > 100) add(`expert_adjustments[${i}].points`, 'out_of_range', 'от −100 до 100, не ноль');
        });
        switch (decision.action) {
            case 'APPROVE': {
                const offers = body.offers?.length ? body.offers : engine.proposed ? [engine.proposed] : [];
                if (!offers.length) add('offers', 'required', 'у движка нет предложенных условий — укажите их');
                offers.forEach((o, i) => {
                    if (!o.amount || o.amount <= 0) add(`offers[${i}].amount`, 'out_of_range', 'сумма больше нуля');
                    if (!o.term_months || o.term_months <= 0 || o.term_months > 600) add(`offers[${i}].term_months`, 'out_of_range', 'срок от 1 до 600 месяцев');
                    if (!o.rate_annual || o.rate_annual <= 0 || o.rate_annual > 100) add(`offers[${i}].rate_annual`, 'out_of_range', 'ставка от 0 до 100 % годовых');
                });
                decision.offers = offers.map((o) => {
                    const days = 'valid_days' in o && o.valid_days ? o.valid_days : 14;
                    return {
                        kind: ('kind' in o && o.kind) || 'REQUESTED',
                        product_code: ('product_code' in o && o.product_code) || app.questionnaire?.product_code || '',
                        amount: o.amount ?? 0,
                        term_months: o.term_months ?? 0,
                        rate_annual: o.rate_annual ?? 0,
                        monthly_payment: annuity(o.amount ?? 0, o.rate_annual ?? 0, o.term_months ?? 1),
                        conditions: 'conditions' in o ? o.conditions : [],
                        guarantee_required: 'guarantee_required' in o ? o.guarantee_required : false,
                        valid_until: iso(now + days * 86_400_000),
                    };
                });
                const first = decision.offers[0];
                decision.outcome = first && requested && first.amount === requested.amount && first.term_months === requested.term_months ? 'APPROVED' : 'APPROVED_WITH_CHANGES';
                break;
            }
            case 'REJECT':
                if (!body.client_reasons?.length) add('client_reasons', 'required', 'укажите причину отказа для клиента');
                body.client_reasons?.forEach((x, i) => !x.title?.trim() && add(`client_reasons[${i}].title`, 'required', 'обязательное поле'));
                decision.outcome = 'REJECTED';
                decision.reasons = body.client_reasons;
                decision.reapply_after = iso(now + 30 * 86_400_000).slice(0, 10);
                break;
            case 'REQUEST_INFO':
                if (!body.info_requests?.length) add('info_requests', 'required', 'задайте хотя бы один вопрос');
                body.info_requests?.forEach((x, i) => !x.title?.trim() && add(`info_requests[${i}].title`, 'required', 'обязательное поле'));
                decision.outcome = 'NEED_INFO';
                decision.questions = (body.info_requests ?? []).map((q, i) => ({
                    id: `ir_${app.id.slice(4)}_uw${r.decisions.length}_${i}`,
                    title: q.title ?? '',
                    description: q.description,
                    required: Boolean(q.required),
                    input: q.input ?? { type: 'TEXT' },
                    documents: q.documents,
                    created_at: iso(now),
                }));
                break;
            default:
                add('action', 'invalid_value', 'допустимые значения: APPROVE, REJECT, REQUEST_INFO');
        }
        if (errors.length) return validation(422, 'решение не прошло проверку', errors);

        const messages = { APPROVE: 'Банк одобрил кредит — посмотрите предложение', REJECT: 'Банк отказал в кредите', REQUEST_INFO: 'Банку нужны дополнительные сведения' };
        const updated = mutateCollector((d) => {
            const stored = d.apps.find((a) => a.id === app.id) ?? app;
            const rv = review(stored);
            const actor = { id: user.id, full_name: user.full_name };
            rv.assignee ??= actor;
            rv.decisions.push(decision);
            rv.version += 1;
            rv.deadline = undefined;
            if (key) rv.keys.push(key);
            for (const a of body.expert_adjustments ?? []) {
                rv.events.push({ id: `evt_uw_${now}_adj_${a.code}`, at: iso(now), type: 'uw.adjustment', internal: true, actor,
                    message: `Поправка к скорингу: ${a.code} ${a.points && a.points > 0 ? '+' : ''}${a.points}${a.comment ? ` — ${a.comment}` : ''}` });
            }
            if (decision.comment) rv.events.push({ id: `evt_uw_${now}_c`, at: iso(now), type: 'uw.comment', internal: true, actor, message: `Внутренний комментарий: ${decision.comment}` });
            rv.events.push({ id: `evt_uw_${now}_d`, at: iso(now + 1), type: 'uw.decision', internal: false, actor, message: messages[decision.action] });
            stored.review = rv;
            return stored;
        });
        return HttpResponse.json(dossier(updated), { headers: { ETag: `"${review(updated).version}"` } });
    }),
];

