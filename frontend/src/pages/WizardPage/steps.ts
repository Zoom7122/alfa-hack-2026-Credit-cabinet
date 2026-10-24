import type { Application, Document, Product } from '../../api/types';

export type StepId = 'company' | 'terms' | 'finance' | 'debts' | 'collateral' | 'documents' | 'review';

type StepConfig = {
    id: StepId;
    title: string;
    /** Какие поля из completeness.missing_fields относятся к шагу. */
    owns?: (field: string) => boolean;
};

export const STEPS: StepConfig[] = [
    { id: 'company', title: 'Компания' },
    { id: 'terms', title: 'Сумма, срок и цель', owns: (f) => f.startsWith('requested.') || f.startsWith('purpose.') },
    { id: 'finance', title: 'Финансы', owns: (f) => f.startsWith('financials.') },
    { id: 'debts', title: 'Кредиты и долги', owns: (f) => f === 'debts' || f.startsWith('debts.') || f === 'has_no_debts' },
    { id: 'collateral', title: 'Залог и поручители', owns: (f) => /^(collateral|guarantors)(\.|$)/.test(f) },
    { id: 'documents', title: 'Документы' },
    { id: 'review', title: 'Проверка и отправка', owns: (f) => f.startsWith('consents.') },
];

export const isStepId = (value: string | undefined): value is StepId => STEPS.some((s) => s.id === value);

export const stepIndex = (id: StepId) => STEPS.findIndex((s) => s.id === id);

export const nextStep = (id: StepId): StepId | undefined => STEPS[stepIndex(id) + 1]?.id;
export const prevStep = (id: StepId): StepId | undefined => STEPS[stepIndex(id) - 1]?.id;

export function stepOfField(field: string): StepId {
    return STEPS.find((s) => s.owns?.(field))?.id ?? 'review';
}

export const missingIn = (app: Application, step: StepId) =>
    app.completeness.missing_fields.filter((f) => STEPS.find((s) => s.id === step)?.owns?.(f));

/** Условие из Product.required_documents вида «amount > 10000000». Непонятное условие считаем выполненным. */
function conditionHolds(condition: string | undefined, app: Application) {
    if (!condition) return true;
    const m = /^\s*amount\s*(>=|>|<=|<)\s*(\d+)\s*$/.exec(condition);
    if (!m) return true;
    const amount = app.requested?.amount ?? 0;
    const limit = Number(m[2]);
    switch (m[1]) {
        case '>':
            return amount > limit;
        case '>=':
            return amount >= limit;
        case '<':
            return amount < limit;
        default:
            return amount <= limit;
    }
}

/** Документы, которые продукт просит для этой заявки. На отправку не влияют — без них решение просто медленнее. */
export function expectedDocuments(product: Product | undefined, app: Application) {
    return (product?.required_documents ?? []).filter((d) => conditionHolds(d.condition, app)).map((d) => d.type);
}

export function missingDocuments(product: Product | undefined, app: Application, documents: Document[] | undefined) {
    const uploaded = new Set((documents ?? []).map((d) => d.type));
    return expectedDocuments(product, app).filter((type) => !uploaded.has(type));
}

export type StepState = 'done' | 'todo' | 'warning';

export function stepState(step: StepId, app: Application, product: Product | undefined, documents: Document[] | undefined): StepState {
    if (step === 'company') return 'done';
    if (step === 'documents') return missingDocuments(product, app, documents).length ? 'warning' : 'done';
    if (step === 'review') return 'todo';
    return missingIn(app, step).length ? 'todo' : 'done';
}

/** Куда вести при открытии черновика: первый шаг, где чего-то не хватает. */
export function firstIncompleteStep(app: Application): StepId {
    const field = app.completeness.missing_fields.find((f) => stepOfField(f) !== 'review');
    return field ? stepOfField(field) : 'review';
}

const LAST_FULL_YEAR = new Date().getFullYear() - 1;

/** Год, за который спрашиваем выручку: из анкеты или последний полный. */
export const reportingYear = (app: Application) => app.financials?.annual_revenue_year ?? LAST_FULL_YEAR;
