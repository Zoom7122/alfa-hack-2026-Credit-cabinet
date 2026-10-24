import type { ClientDecision, InfoRequest, Offer, Product, TimelineEvent } from '../api/types';
import { products, type StoredApplication } from './data';

/* Стартовые история, вопросы, предложения и решения для сидов. Без доступа к хранилищу — чистые функции. */

export type StoredTimelineEvent = TimelineEvent & { application_id: string };
export type StoredInfoRequest = InfoRequest & { application_id: string };
export type StoredOffer = Offer & { application_id: string };

const DAY = 86_400_000;

/** Аннуитетный платёж, округлённый до рубля. */
export function annuity(amount: number, months: number, rateAnnual: number) {
    const r = rateAnnual / 100 / 12;
    return Math.round((amount * r) / (1 - (1 + r) ** -months));
}

const round = (n: number, step: number) => Math.round(n / step) * step;
const productOf = (app: StoredApplication) => products.find((p) => p.code === app.product_code);

function offer(
    app: StoredApplication,
    product: Product | undefined,
    id: string,
    fields: Pick<Offer, 'kind' | 'amount' | 'term_months' | 'rate_annual'> & Partial<Offer>,
    validUntil: string,
): StoredOffer {
    const monthly = annuity(fields.amount, fields.term_months, fields.rate_annual);
    return {
        id,
        application_id: app.id,
        state: 'ACTIVE',
        product_code: app.product_code,
        product_name: product?.name,
        monthly_payment: monthly,
        total_payment: monthly * fields.term_months,
        full_cost_rate: Math.round((fields.rate_annual + 0.8) * 10) / 10,
        collateral_required: [],
        guarantee_required: false,
        conditions: [],
        difference_from_requested: null,
        valid_until: validUntil,
        ...fields,
    };
}

/** 1–3 предложения: как запросили, дольше и дешевле в месяц, меньше и без поручителя. */
export function buildOffers(app: StoredApplication, now: number): StoredOffer[] {
    const product = productOf(app);
    const amount = app.requested?.amount ?? product?.amount.min ?? 1_000_000;
    const term = app.requested?.term_months ?? product?.term_months.min ?? 12;
    const base = product?.rate_annual_from ?? 18;
    const until = new Date(now + 5 * DAY).toISOString();
    const hasGuarantor = Boolean(app.guarantors?.length);
    const offers = [
        offer(
            app,
            product,
            `ofr_${app.id}_1`,
            {
                kind: 'REQUESTED',
                recommended: true,
                amount,
                term_months: term,
                rate_annual: Math.round((base + 0.4) * 10) / 10,
                guarantee_required: hasGuarantor,
                conditions: ['Обороты в банк — не менее 30% выручки'],
            },
            until,
        ),
    ];
    const longer = Math.min(term + 12, product?.term_months.max ?? term + 12);
    if (longer > term) {
        offers.push(
            offer(
                app,
                product,
                `ofr_${app.id}_2`,
                {
                    kind: 'ALTERNATIVE',
                    amount,
                    term_months: longer,
                    rate_annual: Math.round((base + 0.9) * 10) / 10,
                    guarantee_required: hasGuarantor,
                    difference_from_requested: `Срок больше запрошенного на ${longer - term} мес.`,
                },
                until,
            ),
        );
    }
    const smaller = round(amount * 0.7, 100_000);
    if (smaller >= (product?.amount.min ?? 0) && smaller < amount) {
        offers.push(
            offer(
                app,
                product,
                `ofr_${app.id}_3`,
                {
                    kind: 'ALTERNATIVE',
                    amount: smaller,
                    term_months: term,
                    rate_annual: Math.round((base + 2) * 10) / 10,
                    guarantee_required: false,
                    difference_from_requested: `Сумма меньше запрошенной на ${(amount - smaller).toLocaleString('ru-RU')} ₽`,
                },
                until,
            ),
        );
    }
    return offers;
}

/** Удерживающее предложение после первого отказа: та же сумма, ставка ниже — при условии по оборотам. */
export function buildRetentionOffer(app: StoredApplication, from: StoredOffer, now: number): StoredOffer {
    return offer(
        app,
        productOf(app),
        `ofr_${app.id}_retention`,
        {
            kind: 'RETENTION',
            amount: from.amount,
            term_months: from.term_months,
            rate_annual: Math.round((from.rate_annual - 1.5) * 10) / 10,
            guarantee_required: from.guarantee_required,
            conditions: ['Обороты в банк от 50% выручки'],
        },
        new Date(now + DAY).toISOString(),
    );
}

export function approvedDecision(at: string): ClientDecision {
    return { outcome: 'APPROVED', decided_at: at, decided_by: 'AUTO', reasons: [], recommendations: [] };
}

/** Вопросы банка, если заявку подал не директор: нужна доверенность. */
export function powerOfAttorneyRequest(app: StoredApplication, now: number, index: number): StoredInfoRequest {
    return {
        id: `ir_${app.id}_${index}`,
        application_id: app.id,
        state: 'OPEN',
        required: true,
        title: 'Загрузите доверенность на подачу заявки',
        description: 'Вы подаёте заявку не как директор. Подойдёт машиночитаемая доверенность (МЧД) или скан с подписью директора.',
        target_field: null,
        input: { type: 'NONE' },
        documents: {
            required: true,
            accepted_types: ['POWER_OF_ATTORNEY'],
            hint: 'XML + SIG или PDF · до 20 МБ',
        },
        created_at: new Date(now).toISOString(),
        due_at: new Date(now + 5 * DAY).toISOString(),
        answer: null,
    };
}

const at = (iso: string, minutes: number) => new Date(new Date(iso).getTime() + minutes * 60_000).toISOString();

type Seeded = {
    timeline: StoredTimelineEvent[];
    infoRequests: StoredInfoRequest[];
    offers: StoredOffer[];
    decisions: Record<string, ClientDecision>;
};

/** История и сопутствующие данные для сидовых заявок — чтобы экраны статуса были не пустыми. */
/** now — от него считаются сроки ответов и предложений, чтобы демо не «протухало» с календарём. */
export function seedWorkflow(apps: StoredApplication[], now = Date.now()): Seeded {
    const timeline: StoredTimelineEvent[] = [];
    const infoRequests: StoredInfoRequest[] = [];
    const offers: StoredOffer[] = [];
    const decisions: Record<string, ClientDecision> = {};
    let n = 0;
    const event = (app: StoredApplication, minutes: number, fields: Omit<TimelineEvent, 'id' | 'at'>) =>
        timeline.push({ id: `tle_seed_${++n}`, application_id: app.id, at: at(app.created_at, minutes), ...fields });

    for (const app of apps) {
        if (app.status === 'DRAFT') continue;
        event(app, 0, {
            type: 'STATUS_CHANGED',
            from_status: 'DRAFT',
            to_status: 'SUBMITTED',
            actor_kind: 'CLIENT',
            actor: app.created_by,
            message: 'Заявка отправлена',
        });
        event(app, 1, { type: 'STATUS_CHANGED', from_status: 'SUBMITTED', to_status: 'ENRICHING', actor_kind: 'SYSTEM', message: 'Заявка принята' });
        event(app, 2, { type: 'STATUS_CHANGED', from_status: 'ENRICHING', to_status: 'VERIFYING', actor_kind: 'SYSTEM', message: 'Данные о компании собраны' });

        switch (app.status) {
            case 'WAITING_FOR_INFO': {
                event(app, 3, { type: 'INFO_REQUESTED', actor_kind: 'SYSTEM', message: 'Банк задал вопросы' });
                const created = new Date(at(app.created_at, 3)).getTime();
                const due = new Date(now + 5 * DAY).toISOString();
                infoRequests.push(
                    {
                        id: `ir_${app.id}_1`,
                        application_id: app.id,
                        state: 'OPEN',
                        required: true,
                        title: 'Уточните выручку за 2025 год',
                        description: 'Это поможет подобрать для вас максимальную сумму',
                        target_field: 'financials.annual_revenue',
                        input: { type: 'MONEY', min: 0 },
                        documents: {
                            required: false,
                            accepted_types: ['FINANCIAL_STATEMENTS'],
                            hint: 'Бухгалтерская отчётность за 2025 год с отметкой ФНС',
                        },
                        created_at: new Date(created).toISOString(),
                        due_at: due,
                        answer: null,
                    },
                    { ...powerOfAttorneyRequest(app, created, 2), due_at: due },
                );
                break;
            }
            case 'MANUAL_REVIEW':
                event(app, 3, { type: 'STATUS_CHANGED', from_status: 'VERIFYING', to_status: 'SCORING', actor_kind: 'SYSTEM', message: 'Данные о компании проверены' });
                event(app, 4, { type: 'STATUS_CHANGED', from_status: 'SCORING', to_status: 'DECIDING', actor_kind: 'SYSTEM', message: 'Финансы оценены' });
                event(app, 5, { type: 'STATUS_CHANGED', from_status: 'DECIDING', to_status: 'MANUAL_REVIEW', actor_kind: 'SYSTEM', message: 'Заявку смотрит специалист банка' });
                break;
            case 'REJECTED':
                event(app, 3, { type: 'STATUS_CHANGED', from_status: 'VERIFYING', to_status: 'SCORING', actor_kind: 'SYSTEM', message: 'Данные о компании проверены' });
                event(app, 4, { type: 'STATUS_CHANGED', from_status: 'SCORING', to_status: 'DECIDING', actor_kind: 'SYSTEM', message: 'Финансы оценены' });
                event(app, 11, { type: 'STATUS_CHANGED', from_status: 'DECIDING', to_status: 'REJECTED', actor_kind: 'SYSTEM', message: 'Банк отказал' });
                decisions[app.id] = {
                    outcome: 'REJECTED',
                    decided_at: app.updated_at,
                    decided_by: 'AUTO',
                    reasons: [
                        { code: 'HIGH_DEBT_LOAD', title: 'Высокая текущая долговая нагрузка' },
                        { code: 'AMOUNT_ABOVE_LIMIT', title: 'Запрошенная сумма больше, чем позволяют обороты' },
                    ],
                    recommendations: ['Попробуйте запросить меньшую сумму или добавить залог'],
                    reapply_after: new Date(now + 30 * DAY).toISOString().slice(0, 10),
                };
                break;
            case 'OFFERED':
            case 'SIGNING':
            case 'COMPLETED':
            case 'DECLINED': {
                event(app, 3, { type: 'STATUS_CHANGED', from_status: 'VERIFYING', to_status: 'SCORING', actor_kind: 'SYSTEM', message: 'Данные о компании проверены' });
                event(app, 4, { type: 'STATUS_CHANGED', from_status: 'SCORING', to_status: 'DECIDING', actor_kind: 'SYSTEM', message: 'Финансы оценены' });
                event(app, 5, { type: 'OFFERS_ISSUED', from_status: 'DECIDING', to_status: 'OFFERED', actor_kind: 'SYSTEM', message: 'Банк подготовил предложения' });
                decisions[app.id] = approvedDecision(at(app.created_at, 5));
                const appOffers = buildOffers(app, now);
                if (app.status === 'OFFERED') {
                    offers.push(...appOffers);
                    break;
                }
                const [first, ...rest] = appOffers;
                if (first) offers.push({ ...first, state: app.status === 'DECLINED' ? 'WITHDRAWN' : 'ACCEPTED' });
                offers.push(...rest.map((o) => ({ ...o, state: 'WITHDRAWN' as const })));
                if (app.status === 'DECLINED') {
                    event(app, 60, { type: 'OFFERS_DECLINED', from_status: 'OFFERED', to_status: 'DECLINED', actor_kind: 'CLIENT', actor: app.created_by, message: 'Вы отказались от предложений' });
                } else {
                    event(app, 30, { type: 'OFFER_ACCEPTED', from_status: 'OFFERED', to_status: 'SIGNING', actor_kind: 'CLIENT', actor: app.created_by, message: 'Вы выбрали предложение' });
                    event(app, 31, { type: 'SIGNING_REQUESTED', actor_kind: 'SYSTEM', message: 'Договор готов к подписанию' });
                }
                if (app.status === 'COMPLETED') {
                    event(app, 60, { type: 'SIGNED', actor_kind: 'CLIENT', message: 'Договор подписан' });
                    event(app, 61, { type: 'DISBURSED', from_status: 'SIGNING', to_status: 'COMPLETED', actor_kind: 'SYSTEM', message: 'Кредит выдан' });
                }
                break;
            }
            default:
                break;
        }
    }
    return { timeline, infoRequests, offers, decisions };
}
