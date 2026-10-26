import { sourceNote } from '../../shared/lib/collector';
import type { components } from './schema';
import type { CollectorSystem } from './types';
import type { ApplicantFull, Application, BoApplicationListItem, BoDossier, Document, InfoRequest, Offer, SourceCode, TimelineEvent, UserRef } from '../types';

/*
 * Рабочее место банка на API data-collector (/collector/api/v1/backoffice/*): ответы приводятся к типам контракта,
 * по которым написаны экраны 16–18. Отличия data-collector: источники — его 19 систем, у досье есть исполнитель и срок.
 */

type S = components['schemas'];

/** Досье с тем, чего нет в контракте: исполнитель и срок ручного рассмотрения. */
export type BoDossierView = BoDossier & { assignee?: UserRef | null; sla_due_at?: string | null };

const user = (u?: S['UserRef'] | null): UserRef | undefined => (u?.id ? { id: u.id, full_name: u.full_name ?? '' } : undefined);

export function toBoListItem(c: S['BoApplicationListItem']): BoApplicationListItem {
    return {
        id: c.id ?? '',
        number: c.number ?? '',
        status: (c.status ?? 'SUBMITTED') as BoApplicationListItem['status'],
        applicant: { id: c.applicant?.id, short_name: c.applicant?.short_name, inn: c.applicant?.inn ?? '', type: c.applicant?.type },
        product_code: c.product_code,
        requested: { amount: c.requested?.amount ?? 0, term_months: c.requested?.term_months ?? 0 },
        grade: c.grade ?? null,
        manual_review_reasons: c.manual_review_reasons ?? [],
        degraded_sources: (c.degraded_sources ?? []) as SourceCode[],
        assignee: user(c.assignee) ?? null,
        submitted_at: c.submitted_at ?? '',
        sla_due_at: c.sla_due_at ?? null,
    };
}

const REGISTRY_KEYS: [keyof S['BoApplicant'], string][] = [
    ['registration_date', 'registration_date'],
    ['okved', 'okved'],
    ['director_name', 'director'],
    ['region', 'address'],
    ['kpp', 'kpp'],
    ['terminated_on', 'terminated_on'],
];

function toApplicant(a: S['BoApplicant'] | undefined): ApplicantFull {
    const registry: Record<string, unknown> = {};
    for (const [from, to] of REGISTRY_KEYS) if (a?.[from]) registry[to] = a[from];
    return {
        id: a?.id ?? '',
        inn: a?.inn ?? '',
        applicant_type: (a?.type ?? 'LEGAL_ENTITY') as ApplicantFull['applicant_type'],
        short_name: a?.short_name,
        full_name: a?.full_name ?? a?.short_name ?? '',
        ogrn: a?.ogrn,
        registry,
        financial_statements: (a?.financial_statements ?? []).map((y) => ({
            year: y.year ?? 0,
            revenue: y.revenue,
            net_profit: y.net_profit,
            total_assets: y.total_assets,
            equity: y.equity,
        })),
        credit_history: a?.credit_history,
        enforcement: a?.enforcement,
        litigation: a?.litigation,
        bank_relationship: a?.bank_relationship,
        risk_flags: a?.risk_flags ?? [],
    } as ApplicantFull;
}

function toOffer(o: S['Offer'], productCode: string): Offer {
    return {
        id: o.id ?? '',
        kind: o.kind ?? 'REQUESTED',
        state: o.state ?? 'ACTIVE',
        recommended: o.recommended,
        product_code: o.product_code || productCode,
        amount: o.amount ?? 0,
        term_months: o.term_months ?? 0,
        rate_annual: o.rate_annual ?? 0,
        monthly_payment: o.monthly_payment ?? 0,
        total_payment: (o.monthly_payment ?? 0) * (o.term_months ?? 0),
        collateral_required: (o.collateral_required ?? []) as Offer['collateral_required'],
        guarantee_required: o.guarantee_required,
        conditions: o.conditions,
        valid_until: o.valid_until ?? '',
    };
}

function toInfoRequest(q: S['InfoRequest']): InfoRequest {
    const input = (q.input ?? {}) as unknown as Partial<InfoRequest['input']>;
    return {
        id: q.id ?? '',
        state: (q.state ?? 'OPEN') as InfoRequest['state'],
        required: Boolean(q.required),
        title: q.title ?? '',
        description: q.description,
        target_field: q.target_field || null,
        input: { type: input.type ?? 'TEXT', options: input.options },
        documents: q.documents as InfoRequest['documents'],
        created_at: q.created_at ?? '',
        due_at: q.due_at ?? null,
        answer: (q.answer as InfoRequest['answer']) ?? null,
    };
}

const ANSWER_WORDS: Record<string, string> = { true: 'да', false: 'нет', CONFIRMED: 'подтверждено', NOT_REQUIRED: 'не требуется', VERIFIED_ESTIMATE: 'подтверждённая оценка' };
const decimal = new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 2 });

/** Значение критерия для глаз: ответ без имени поля («business_age_months=89» → «89»), числа — до сотых. */
function factorBucket(answer?: string, value?: unknown): string | undefined {
    const raw = answer ? answer.replace(/^[a-z0-9_]+=/, '') : value;
    if (raw === undefined || raw === null || raw === '') return undefined;
    const n = typeof raw === 'number' ? raw : typeof raw === 'string' && /^-?\d+(\.\d+)?$/.test(raw) ? Number(raw) : NaN;
    if (!Number.isNaN(n)) return decimal.format(n);
    return ANSWER_WORDS[String(raw)] ?? String(raw);
}

/** Тип события data-collector → тип журнала кабинета (для значка). */
function eventType(type: string): TimelineEvent['type'] {
    if (type.startsWith('document.')) return 'DOCUMENT_UPLOADED';
    if (type === 'info.provided' || type === 'info.answered') return 'INFO_PROVIDED';
    return 'STATUS_CHANGED';
}

export function toBoDossier(c: S['BoDossier']): BoDossierView {
    const a = c.application ?? {};
    const productCode = a.product_code ?? '';
    const engine = c.engine_decision;
    const proposed = engine?.proposed_terms;
    const score = c.score;
    const application = {
        id: a.id ?? '',
        number: a.number ?? '',
        status: a.status ?? 'SUBMITTED',
        version: c.version ?? 0,
        applicant: { id: a.applicant?.id ?? '', short_name: a.applicant?.name ?? '', inn: a.applicant?.inn ?? '' },
        product_code: productCode,
        requested: a.requested?.amount ? { amount: a.requested.amount, term_months: a.requested.term_months ?? 0 } : undefined,
        purpose: a.purpose?.code,
        created_at: a.created_at ?? '',
        updated_at: a.updated_at ?? a.created_at ?? '',
        submitted_at: a.submitted_at,
    } as unknown as Application;
    return {
        application,
        applicant: toApplicant(c.applicant),
        sources: (c.sources ?? []).map((s) => ({
            code: (s.code ?? '') as SourceCode,
            // частичный ответ — данные есть, но не все: показываем как полученный с пометкой
            state: s.state === 'PARTIAL' ? 'DONE' : ((s.state ?? 'PENDING') as BoDossier['sources'][number]['state']),
            criticality: s.criticality ?? 'NORMAL',
            fetched_at: s.fetched_at ?? null,
            error: s.state === 'PARTIAL' ? `частично${s.reason ? `: ${s.reason}` : ''}` : s.state === 'FAILED' ? (s.reason ?? null) : null,
            skip_reason: s.state === 'SKIPPED' ? (sourceNote({ source: s.code as CollectorSystem, state: 'SKIPPED', reason: s.reason ?? undefined }) ?? null) : null,
        })),
        findings: (c.findings ?? []).map((f) => ({
            code: f.code ?? '',
            severity: (f.severity ?? 'INFO') as BoDossier['findings'][number]['severity'],
            message: f.message ?? '',
            declared_value: f.declared_value,
            found_value: f.found_value,
            source: (f.source || undefined) as SourceCode | undefined,
            material: f.material,
        })),
        score: score
            ? {
                  model_version: score.model_version ?? '',
                  engine: 'SCORECARD',
                  total: score.total ?? 0,
                  grade: (score.grade ?? 'D') as NonNullable<BoDossier['score']>['grade'],
                  computed_at: score.computed_at ?? '',
                  factors: (score.factors ?? []).map((f) => ({
                      code: f.code ?? '',
                      title: f.title ?? f.code ?? '',
                      bucket: factorBucket(f.answer, f.value),
                      points: f.points ?? 0,
                      source: f.part === 'ECONOMICS' ? 'DERIVED' : 'DECLARED',
                  })),
                  max_amount: proposed ? { value: proposed.amount } : undefined,
              }
            : null,
        engine_decision: engine
            ? {
                  outcome: (engine.outcome ?? 'MANUAL_REVIEW') as NonNullable<BoDossier['engine_decision']>['outcome'],
                  rules_fired: (engine.reasons ?? []).map((r) => ({ code: r.code ?? '', effect: engine.outcome ?? '', detail: r.message })),
                  manual_review_reasons: (engine.reasons ?? []).map((r) => r.message ?? ''),
                  proposed_offers: proposed
                      ? [
                            toOffer(
                                {
                                    id: 'proposed',
                                    kind: 'REQUESTED',
                                    state: 'ACTIVE',
                                    amount: proposed.amount,
                                    term_months: proposed.term_months,
                                    rate_annual: proposed.rate_annual,
                                    monthly_payment: proposed.monthly_payment,
                                },
                                productCode,
                            ),
                        ]
                      : [],
              }
            : null,
        offers: (c.offers ?? []).map((o) => toOffer(o, productCode)),
        documents: (a.documents ?? []).map(
            (d) =>
                ({
                    id: d.id ?? '',
                    type: (d.type ?? 'OTHER') as Document['type'],
                    file_name: d.file_name ?? '',
                    mime_type: d.mime_type ?? '',
                    size_bytes: d.size_bytes ?? 0,
                    is_test: d.is_test,
                    uploaded_at: d.uploaded_at ?? '',
                    signature: { state: d.signature_state === 'DONE' ? 'VALID' : d.signature_state === 'PENDING' ? 'PENDING' : 'NOT_SIGNED' },
                }) as unknown as Document,
        ),
        info_requests: (c.info_requests ?? []).map(toInfoRequest),
        other_applications: (c.other_applications ?? []).map((o) => {
            const item = toBoListItem(o);
            return { id: item.id, number: item.number, status: item.status, requested: item.requested } as never;
        }),
        timeline: (c.timeline ?? []).map((e) => ({
            id: e.id ?? '',
            at: e.at ?? '',
            type: eventType(e.type ?? ''),
            actor_kind: (e.actor_kind ?? 'SYSTEM') as TimelineEvent['actor_kind'],
            actor: user(e.actor),
            message: e.message ?? '',
        })),
        assignee: user(c.assignee) ?? null,
        sla_due_at: c.sla_due_at ?? null,
    };
}
