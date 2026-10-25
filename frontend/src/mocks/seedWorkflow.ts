import type { ClientDecision, Document, InfoRequest, Offer, Product, SigningSession, TimelineEvent } from '../api/types';
import { products, type StoredApplication, userRef } from './data';

/* Стартовые история, вопросы, предложения и решения для сидов. Без доступа к хранилищу — чистые функции. */

export type StoredTimelineEvent = TimelineEvent & { application_id: string };
export type StoredInfoRequest = InfoRequest & { application_id: string };
export type StoredOffer = Offer & { application_id: string };
/** Документ + когда мок «распознает» его и проверит подпись (в контракт не попадает). */
export type StoredDocument = Document & {
    application_id: string;
    recognize_at?: number;
    verify_at?: number;
    /** Для демо: подпись внутри файла (PAdES) и её вердикт. */
    mock_signature?: { verdict: 'VALID' | 'VALID_WITH_WARNINGS' };
    /** Для демо: распознавание не удалось. */
    mock_recognition?: 'FAILED';
};
/** Сессия подписания без вычисляемых полей (подписант, awaiting_me, документы целиком). */
export type StoredSigning = Omit<SigningSession, 'required_signer' | 'awaiting_me' | 'documents'> & {
    document_ids: string[];
    /** Какие предложения отозвал выбор — их вернём, если подписать не удастся (sign.failed). */
    withdrawn_offer_ids?: string[];
};

export const SIGNING_ATTEMPTS = 3;

/** Счёт зачисления в моке: «40702•••••••••••» + последние цифры ИНН. */
export const accountMasked = (inn: string) => `40702•••••••••••${inn.slice(-4)}`;

const pdfSize = (pages: number) => 18_000 * pages;

/** Договор, график и (если нужен поручитель) договор поручительства — их формирует банк при подписании. */
export function buildSigning(app: StoredApplication, offer: Offer, now: number): { documents: StoredDocument[]; session: StoredSigning } {
    const at = new Date(now).toISOString();
    const base = { application_id: app.id, mime_type: 'application/pdf', origin: 'GENERATED' as const, uploaded_at: at, info_request_id: null, signed_document_id: null };
    const contractNumber = `КД-${app.number.slice(2)}`;
    // новый комплект на каждое формирование: подпись к прошлому договору к новому не подходит
    const v = now.toString(36);
    const documents: StoredDocument[] = [
        { ...base, id: `doc_${app.id}_contract_${v}`, type: 'LOAN_AGREEMENT_DRAFT', file_name: `Кредитный договор № ${contractNumber}.pdf`, size_bytes: pdfSize(14) },
        { ...base, id: `doc_${app.id}_schedule_${v}`, type: 'OTHER', file_name: 'График платежей.pdf', size_bytes: pdfSize(2) },
    ];
    const guarantor = app.guarantors?.[0];
    if (offer.guarantee_required && guarantor) {
        documents.push({
            ...base,
            id: `doc_${app.id}_guarantee_${v}`,
            type: 'OTHER',
            file_name: `Договор поручительства — ${guarantor.name}.pdf`,
            size_bytes: pdfSize(4),
        });
    }
    return {
        documents,
        session: {
            state: 'AWAITING_SIGNER',
            offer_id: offer.id,
            demo_code: null,
            attempts_left: SIGNING_ATTEMPTS,
            signature_report: null,
            signed_at: null,
            disbursement: null,
            document_ids: documents.map((d) => d.id),
        },
    };
}

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

const PETROV = userRef('usr_director_romashka');
const IVANOVA = userRef('usr_accountant_romashka');

/** Документы «Ромашки» на вкладке «Документы» (макет 15): распознанные, подписанные КЭП, с замечаниями. */
function demoDocuments(app: StoredApplication): StoredDocument[] {
    const base = { application_id: app.id, origin: 'CLIENT_UPLOAD' as const, info_request_id: null, signed_document_id: null };
    const time = (minutes: number) => at(app.created_at, minutes);
    return [
        { ...base, id: `doc_${app.id}_fin`, type: 'FINANCIAL_STATEMENTS', file_name: 'Отчётность_2025.pdf', mime_type: 'application/pdf', size_bytes: 1_258_291, uploaded_by: PETROV, uploaded_at: time(10) },
        { ...base, id: `doc_${app.id}_bank`, type: 'BANK_STATEMENT', file_name: 'Выписка_по_счёту_6мес.xlsx', mime_type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', size_bytes: 348_160, uploaded_by: PETROV, uploaded_at: time(12) },
        { ...base, id: `doc_${app.id}_charter`, type: 'CHARTER', file_name: 'Устав_Ромашка.pdf', mime_type: 'application/pdf', size_bytes: 2_100_000, uploaded_by: PETROV, uploaded_at: time(14) },
        { ...base, id: `doc_${app.id}_charter_sig`, type: 'SIGNATURE', file_name: 'Устав_Ромашка.pdf.sig', mime_type: 'application/pkcs7-signature', size_bytes: 8_192, uploaded_by: PETROV, uploaded_at: time(14), signed_document_id: `doc_${app.id}_charter` },
        {
            ...base,
            id: `doc_${app.id}_contract_metro`,
            type: 'CONTRACT_WITH_CUSTOMER',
            file_name: 'Договор_поставки_Метро.pdf',
            mime_type: 'application/pdf',
            size_bytes: 2_516_582,
            uploaded_by: IVANOVA,
            uploaded_at: time(20),
            mock_signature: { verdict: 'VALID_WITH_WARNINGS' },
            mock_recognition: 'FAILED',
        },
    ];
}

const at = (iso: string, minutes: number) => new Date(new Date(iso).getTime() + minutes * 60_000).toISOString();

type Seeded = {
    timeline: StoredTimelineEvent[];
    infoRequests: StoredInfoRequest[];
    offers: StoredOffer[];
    decisions: Record<string, ClientDecision>;
    documents: StoredDocument[];
    signing: Record<string, StoredSigning>;
};

/** История и сопутствующие данные для сидовых заявок — чтобы экраны статуса были не пустыми. */
/** now — от него считаются сроки ответов и предложений, чтобы демо не «протухало» с календарём. */
export function seedWorkflow(apps: StoredApplication[], now = Date.now()): Seeded {
    const timeline: StoredTimelineEvent[] = [];
    const infoRequests: StoredInfoRequest[] = [];
    const offers: StoredOffer[] = [];
    const decisions: Record<string, ClientDecision> = {};
    const documents: StoredDocument[] = [];
    const signing: Record<string, StoredSigning> = {};
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
        if (app.status === 'ENRICHING') continue;
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
                for (const doc of demoDocuments(app)) {
                    documents.push(doc);
                    timeline.push({
                        id: `tle_seed_doc_${doc.id}`,
                        application_id: app.id,
                        at: doc.uploaded_at,
                        type: 'DOCUMENT_UPLOADED',
                        actor_kind: 'CLIENT',
                        actor: doc.uploaded_by,
                        message: `Загружен документ: ${doc.file_name}`,
                    });
                }
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
                        {
                            code: 'HIGH_DEBT_LOAD',
                            title: 'Высокая текущая долговая нагрузка',
                            description: 'Платежи по действующим кредитам и лизингу занимают больше половины поступлений на счета',
                        },
                        {
                            code: 'AMOUNT_ABOVE_LIMIT',
                            title: 'Запрошенная сумма больше, чем позволяет выручка',
                            description: 'Сумма кредита превышает годовую выручку компании',
                        },
                    ],
                    recommendations: ['Попробуйте запросить меньшую сумму или добавить залог', 'Погасите часть действующих кредитов'],
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
                if (first && (app.status === 'SIGNING' || app.status === 'COMPLETED')) {
                    const built = buildSigning(app, first, new Date(at(app.created_at, 30)).getTime());
                    documents.push(...built.documents);
                    signing[app.id] =
                        app.status === 'COMPLETED'
                            ? {
                                  ...built.session,
                                  state: 'SIGNED',
                                  method: 'SIMPLE_CODE',
                                  signed_at: at(app.created_at, 60),
                                  disbursement: { amount: first.amount, account_masked: accountMasked(app.applicant.inn), executed_at: at(app.created_at, 61) },
                              }
                            : built.session;
                }
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
    return { timeline, infoRequests, offers, decisions, documents, signing };
}
