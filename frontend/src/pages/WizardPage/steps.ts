import {
    borrowerErrors,
    collateralErrors,
    type Errors,
    filesErrors,
    type QuestionnaireDraft,
    reviewErrors,
    scoringErrors,
    type Secrets,
    sourcesErrors,
    termsErrors,
} from './draft';

export type StepId = 'company' | 'terms' | 'borrower' | 'scoring' | 'sources' | 'collateral' | 'documents' | 'review';

type StepConfig = {
    id: StepId;
    title: string;
    /** Поля анкеты data-collector (Problem.errors[].field), которые правятся на этом шаге. */
    owns?: (field: string) => boolean;
};

export const STEPS: StepConfig[] = [
    { id: 'company', title: 'Компания', owns: (f) => f === 'inn' || f.startsWith('company.') || f.startsWith('representative.') },
    { id: 'terms', title: 'Сумма, срок и цель', owns: (f) => /^(requested|purpose|product_code)(\.|$)/.test(f) },
    { id: 'borrower', title: 'Руководитель и анкета', owns: (f) => /^(age|marital_status|children_count|director)(\.|$)/.test(f) },
    { id: 'scoring', title: 'Для оценки', owns: (f) => f.startsWith('scoring.') },
    { id: 'sources', title: 'Счета и площадки', owns: (f) => /^(other_bank_biks|marketplace_tokens|ofd_token)(\.|\[|$)/.test(f) },
    { id: 'collateral', title: 'Залог', owns: (f) => f.startsWith('collateral') },
    { id: 'documents', title: 'Документы', owns: (f) => /^(files|documents)(\[|\.|$)/.test(f) },
    { id: 'review', title: 'Согласия и отправка', owns: (f) => /^(consents|poa_number|representative_inn)(\.|$)/.test(f) },
];

export const isStepId = (value: string | undefined): value is StepId => STEPS.some((s) => s.id === value);
export const stepIndex = (id: StepId) => STEPS.findIndex((s) => s.id === id);
export const nextStep = (id: StepId): StepId | undefined => STEPS[stepIndex(id) + 1]?.id;
export const prevStep = (id: StepId): StepId | undefined => STEPS[stepIndex(id) - 1]?.id;
export const stepOfField = (field: string): StepId => STEPS.find((s) => s.owns?.(field))?.id ?? 'review';

export type StepState = 'done' | 'todo' | 'warning';

export function errorsOf(step: StepId, d: QuestionnaireDraft, s: Secrets, needsPoa: boolean): Errors {
    switch (step) {
        case 'terms':
            return termsErrors(d);
        case 'borrower':
            return borrowerErrors(d);
        case 'scoring':
            return scoringErrors(d);
        case 'sources':
            return sourcesErrors(d, s);
        case 'collateral':
            return collateralErrors(d);
        case 'documents':
            return filesErrors(d);
        case 'review':
            return reviewErrors(d, needsPoa);
        default:
            return {};
    }
}

/** Шаги, которые нельзя оставить пустыми: без них data-collector анкету не примет или в заявке не будет суммы. */
const REQUIRED: StepId[] = ['terms', 'borrower'];

export function stepState(step: StepId, d: QuestionnaireDraft, s: Secrets, needsPoa: boolean): StepState {
    if (step === 'company') return 'done';
    const errors = Object.keys(errorsOf(step, d, s, needsPoa)).length > 0;
    if (errors) return REQUIRED.includes(step) ? 'todo' : 'warning';
    if (step === 'documents') return d.files.length ? 'done' : 'warning';
    if (step === 'scoring') return d.scoring?.max_annual_rate !== undefined && d.scoring.need_cost !== undefined ? 'done' : 'warning';
    if (step === 'review') return d.consents.bki_company || d.consents.tax ? 'done' : 'todo';
    return 'done';
}

export const firstIncompleteStep = (d: QuestionnaireDraft, s: Secrets, needsPoa: boolean): StepId =>
    REQUIRED.find((step) => Object.keys(errorsOf(step, d, s, needsPoa)).length) ?? 'review';

export function completionPercent(d: QuestionnaireDraft, s: Secrets, needsPoa: boolean) {
    const done = STEPS.filter((step) => stepState(step.id, d, s, needsPoa) === 'done').length;
    return Math.round((done / STEPS.length) * 100);
}
