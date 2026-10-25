import type { CollectorDocument, CollectorRole, QuestionnaireInput } from '../../api/collector/types';

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
