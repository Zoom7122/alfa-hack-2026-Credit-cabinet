import type { CollectorInfoRequest, CollectorOffer, CollectorStatus } from '../../api/collector/types';
import type { StoredCollectorApp } from './store';

/* Итог оценки в моке data-collector — как у credit-engine на сценариях mock-api (data-collector/README.md):
   A — предложение, C — отказ, D — вопросы банка, остальные — специалисту. */

type Outcome = Extract<CollectorStatus, 'OFFERED' | 'REJECTED' | 'WAITING_FOR_INFO' | 'MANUAL_REVIEW'>;

const OUTCOMES: Record<string, Outcome> = {
    '7799000012': 'OFFERED',
    '7799000037': 'REJECTED',
    '779900010202': 'WAITING_FOR_INFO',
};

export const scoredOutcome = (inn: string): Outcome => OUTCOMES[inn] ?? 'MANUAL_REVIEW';

const suffix = (app: StoredCollectorApp) => app.id.slice(4);

/** Вопросы банка по заявке (сценарий D): свободные деньги — сумма, выписка — документ. */
export function questionsOf(app: StoredCollectorApp, createdMs: number): CollectorInfoRequest[] {
    const fromUnderwriter = (app.review?.decisions ?? []).flatMap((d) =>
        (d.questions ?? []).map((q) => {
            const answer = app.answers?.[q.id];
            return {
                ...q,
                source: 'underwriter',
                state: answer ? 'ANSWERED' : 'OPEN',
                due_at: new Date(new Date(q.created_at).getTime() + 5 * 86_400_000).toISOString(),
                answer: answer ? { value: answer.value, comment: answer.comment, document_ids: answer.document_ids } : null,
                answered_at: answer?.at ?? null,
            } as unknown as CollectorInfoRequest;
        }),
    );
    return [...engineQuestions(app, createdMs), ...fromUnderwriter];
}

function engineQuestions(app: StoredCollectorApp, createdMs: number): CollectorInfoRequest[] {
    if (scoredOutcome(app.company.inn) !== 'WAITING_FOR_INFO') return [];
    const created = new Date(createdMs).toISOString();
    const due = new Date(createdMs + 5 * 86_400_000).toISOString();
    const base = [
        {
            id: `ir_${suffix(app)}_1`,
            title: 'Свободные деньги',
            description: 'Остаток на счетах меньше указанных свободных денег — уточните сумму.',
            input: { type: 'MONEY', scoring_field: 'free_cash' },
        },
        {
            id: `ir_${suffix(app)}_2`,
            title: 'Выписка по счетам за 12 месяцев',
            description: 'Загрузите выписку по расчётным счетам за последние 12 полных месяцев.',
            input: { type: 'NONE' },
            documents: { required: true, accepted_types: ['BANK_STATEMENT'] },
        },
    ];
    return base.map((q) => {
        const answer = app.answers?.[q.id];
        return {
            ...q,
            required: true,
            source: 'credit-engine',
            state: answer ? 'ANSWERED' : 'OPEN',
            created_at: created,
            due_at: due,
            answer: answer ? { value: answer.value, comment: answer.comment, document_ids: answer.document_ids } : null,
            answered_at: answer?.at ?? null,
        } as unknown as CollectorInfoRequest; // answer и input в swagger — произвольные объекты
    });
}

export const allAnswered = (app: StoredCollectorApp, createdMs: number) => {
    const qs = engineQuestions(app, createdMs);
    return qs.length > 0 && qs.every((q) => q.state === 'ANSWERED');
};

/** Предложение сценария A: подобрано движком под денежный поток. */
export function offerOf(app: StoredCollectorApp, createdMs: number): CollectorOffer {
    return {
        id: `ofr_${suffix(app)}`,
        kind: 'REQUESTED',
        state: 'ACTIVE',
        recommended: true,
        product_code: app.questionnaire?.product_code,
        amount: 2_030_000,
        term_months: 36,
        rate_annual: 20.75,
        monthly_payment: 76_220,
        collateral_required: [],
        guarantee_required: false,
        conditions: [],
        valid_until: new Date(createdMs + 14 * 86_400_000).toISOString(),
    };
}

export function offersOf(app: StoredCollectorApp, createdMs: number): CollectorOffer[] {
    const approved = app.review?.decisions.filter((d) => d.action === 'APPROVE').at(-1);
    const offers: CollectorOffer[] = approved
        ? (approved.offers ?? []).map((o, i) => ({ ...o, kind: o.kind === 'ALTERNATIVE' ? 'ALTERNATIVE' : 'REQUESTED', id: `ofr_${app.id.slice(4)}_${i}`, state: 'ACTIVE', recommended: i === 0, collateral_required: [] }))
        : [offerOf(app, createdMs)];
    return offers.map((o) => ({ ...o, state: !app.signing ? 'ACTIVE' : o.id !== app.signing.offer_id ? 'WITHDRAWN' : app.signing.state === 'FAILED' ? 'ACTIVE' : 'ACCEPTED' }));
}

export const REJECT_REASONS = [
    { code: 'BANKRUPT', title: 'Подтверждено банкротство заёмщика.' },
    { code: 'CURRENT_DPD_30_PLUS', title: 'Текущая просрочка составляет не менее 30 дней.' },
];
