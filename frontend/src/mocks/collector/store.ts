import type { CollectorDocument, CollectorRole, CollectorSigningSession, QuestionnaireInput } from '../../api/collector/types';

/* Состояние мока data-collector: заявки, документы, задачи на сбор. Переживает перезагрузку, как и остальные моки. */

export type StoredCollectorApp = {
    id: string;
    number: string;
    company: { id: string; inn: string; name?: string };
    representative: { id: string; full_name: string; inn?: string; role: CollectorRole; can_sign: boolean };
    already_known: boolean;
    created_at: string;
    updated_at: string;
    /** Время отправки анкеты (мс) — от него считаются SUBMITTED → ENRICHING → VERIFYING. */
    submitted_ms?: number;
    questionnaire?: Omit<QuestionnaireInput, 'marketplace_tokens' | 'ofd_token'> & {
        id: string;
        created_at: string;
        has_marketplace_tokens: boolean;
        has_ofd_token: boolean;
    };
    documents: CollectorDocument[];
    task_id?: string;
    /** Ответы на вопросы банка: id вопроса → ответ. */
    answers?: Record<string, { value?: unknown; comment?: string; document_ids: string[]; at: string }>;
    /** Ручное рассмотрение: исполнитель, версия заявки, решения андеррайтера, служебный журнал. */
    review?: StoredReview;
    signing?: CollectorSigningSession;
    signing_contract?: string;
    signing_events?: { id: string; at: string; type: string; message: string; actor_kind: 'CLIENT' | 'SYSTEM' }[];
};

export type StoredQuestion = { id: string; title: string; description?: string; required: boolean; input: unknown; documents?: unknown; created_at: string };

export type StoredDecision = {
    action: 'APPROVE' | 'REJECT' | 'REQUEST_INFO';
    outcome: string;
    user: { id: string; full_name: string };
    at: string;
    comment?: string;
    reasons?: { code: string; title: string; description?: string }[];
    offers?: { kind: string; product_code: string; amount: number; term_months: number; rate_annual: number; monthly_payment: number; conditions?: string[]; guarantee_required?: boolean; valid_until: string }[];
    questions?: StoredQuestion[];
    reapply_after?: string;
};

export type StoredReview = {
    assignee?: { id: string; full_name: string };
    version: number;
    deadline?: string;
    decisions: StoredDecision[];
    events: { id: string; at: string; type: string; message: string; internal: boolean; actor?: { id: string; full_name: string } }[];
    keys: string[];
};

type CollectorDb = { seq: number; apps: StoredCollectorApp[] };

const KEY = 'credit-cabinet:collector-mock:v1';

function load(): CollectorDb {
    try {
        const raw = localStorage.getItem(KEY);
        if (raw) return JSON.parse(raw) as CollectorDb;
    } catch {
        /* битые данные — начинаем заново */
    }
    return { seq: 0, apps: [] };
}

let db = load();

window.addEventListener('storage', (e) => {
    if (e.key === KEY) db = e.newValue ? (JSON.parse(e.newValue) as CollectorDb) : { seq: 0, apps: [] };
});

export const readCollector = () => db;

export function mutateCollector<T>(fn: (d: CollectorDb) => T): T {
    const result = fn(db);
    try {
        localStorage.setItem(KEY, JSON.stringify(db));
    } catch {
        /* без сохранения — до перезагрузки */
    }
    return result;
}

/** id в духе data-collector: префикс и ULID-подобный хвост. */
export function newId(prefix: string) {
    const tail = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`.toUpperCase().padEnd(26, '0').slice(0, 26);
    return `${prefix}_${tail}`;
}
