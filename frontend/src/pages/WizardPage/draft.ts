import { useCallback, useState } from 'react';

import type {
    CollectorDocumentType,
    CollectorPurpose,
    MaritalStatus,
    Marketplace,
    QuestionnaireInput,
    TaxScope,
} from '../../api/collector/types';
import { deleteFile, listFileKeys } from './fileStore';

/*
 * Черновик анкеты. PATCH у data-collector нет: всё, что вводит клиент, живёт в браузере до отправки анкеты
 * (POST /applications/{id}/questionnaire — один раз). Токены маркетплейсов и ОФД — отдельно, только во вкладке.
 */

export type FileEntry = {
    /** Ключ содержимого в IndexedDB (fileStore). */
    key: string;
    name: string;
    size: number;
    /** Тип документа; у открепленной подписи (.sig, .p7s) — нет, она всегда SIGNATURE. */
    type?: CollectorDocumentType;
};

export type DirectorDraft = { inn: string; last_name: string; first_name: string; middle_name: string; birth_date: string };

export type QuestionnaireDraft = {
    product_code?: string;
    requested?: { amount: number; term_months: number };
    purpose?: { code?: CollectorPurpose; description?: string };
    age?: number;
    marital_status?: MaritalStatus;
    children_count?: number;
    /** false — руководителя не указываем: data-collector не запросит данные физлица. */
    with_director: boolean;
    director: DirectorDraft;
    other_bank_biks: string[];
    collateral: { cadastral_numbers: string[]; vins: string[]; serials: string[] };
    files: FileEntry[];
    /** Время, когда клиент дал согласие (signed_at); нет — согласия нет. */
    consents: { bki_company?: string; bki_person?: string; tax?: string };
    tax_scope: TaxScope[];
    poa_number: string;
    representative_inn: string;
};

export type Secrets = { marketplace_tokens: Partial<Record<Marketplace, string>>; ofd_token: string };

const EMPTY_DIRECTOR: DirectorDraft = { inn: '', last_name: '', first_name: '', middle_name: '', birth_date: '' };

export const emptyDraft = (init: Partial<QuestionnaireDraft> = {}): QuestionnaireDraft => ({
    with_director: true,
    director: EMPTY_DIRECTOR,
    other_bank_biks: [],
    collateral: { cadastral_numbers: [], vins: [], serials: [] },
    files: [],
    consents: {},
    tax_scope: ['DECLARATIONS', 'ENS_BALANCE'],
    poa_number: '',
    representative_inn: '',
    ...init,
});

/* Ключи — по пользователю кабинета и заявке: в одном браузере могут работать разные пользователи. */
const DRAFT_PREFIX = 'credit-cabinet:questionnaire:';
const SECRETS_PREFIX = 'credit-cabinet:questionnaire-secrets:';
const draftKey = (userId: string, id: string) => `${DRAFT_PREFIX}${userId}:${id}`;
const secretsKey = (userId: string, id: string) => `${SECRETS_PREFIX}${userId}:${id}`;
export const serverErrorsKey = (userId: string, id: string) => `credit-cabinet:questionnaire-errors:${userId}:${id}`;

function readJson<T>(storage: () => Storage, key: string): T | undefined {
    try {
        const raw = storage().getItem(key);
        return raw ? (JSON.parse(raw) as T) : undefined;
    } catch {
        return undefined;
    }
}

function writeJson(storage: () => Storage, key: string, value: unknown) {
    try {
        if (value === undefined) storage().removeItem(key);
        else storage().setItem(key, JSON.stringify(value));
    } catch {
        /* приватный режим — живём без сохранения */
    }
}

export const readDraft = (userId: string, id: string) => {
    const stored = readJson<Partial<QuestionnaireDraft>>(() => localStorage, draftKey(userId, id));
    return stored ? emptyDraft(stored) : undefined;
};
export const writeDraft = (userId: string, id: string, draft: QuestionnaireDraft | undefined) =>
    writeJson(() => localStorage, draftKey(userId, id), draft);
const readSecrets = (userId: string, id: string): Secrets =>
    readJson<Secrets>(() => sessionStorage, secretsKey(userId, id)) ?? { marketplace_tokens: {}, ofd_token: '' };
const writeSecrets = (userId: string, id: string, secrets: Secrets | undefined) => writeJson(() => sessionStorage, secretsKey(userId, id), secrets);

/** Анкета ушла (или ушла раньше, из другой вкладки): черновик, токены, ошибки и файлы в браузере больше не нужны. */
export function discardDraft(userId: string, id: string) {
    const files = readDraft(userId, id)?.files ?? [];
    for (const f of files) void deleteFile(f.key).catch(() => undefined);
    writeDraft(userId, id, undefined);
    writeSecrets(userId, id, undefined);
    writeJson(() => sessionStorage, serverErrorsKey(userId, id), undefined);
}

function keysWith(storage: () => Storage, prefix: string) {
    try {
        const s = storage();
        return Array.from({ length: s.length }, (_, i) => s.key(i) ?? '').filter((k) => k.startsWith(prefix));
    } catch {
        return [];
    }
}

/** При выходе: токены маркетплейсов и ОФД из вкладки — сразу. Черновики остаются, чтобы не терять заполненное. */
export function forgetSecrets() {
    for (const key of keysWith(() => sessionStorage, SECRETS_PREFIX)) writeJson(() => sessionStorage, key, undefined);
}

/** Файлы, на которые не ссылается ни один черновик (брошенные, отправленные), — из IndexedDB. */
export async function collectOrphanFiles() {
    const used = new Set(
        keysWith(() => localStorage, DRAFT_PREFIX).flatMap((key) => readJson<Partial<QuestionnaireDraft>>(() => localStorage, key)?.files?.map((f) => f.key) ?? []),
    );
    const stored = await listFileKeys().catch(() => []);
    await Promise.all(stored.filter((key) => !used.has(key)).map((key) => deleteFile(key).catch(() => undefined)));
}

export type Draft = {
    value: QuestionnaireDraft;
    update: (patch: Partial<QuestionnaireDraft> | ((d: QuestionnaireDraft) => Partial<QuestionnaireDraft>)) => void;
    secrets: Secrets;
    updateSecrets: (patch: Partial<Secrets>) => void;
    savedAt?: string;
};

/** Черновик заявки: правки сразу пишутся в браузер. */
export function useLocalDraft(userId: string, applicationId: string): Draft {
    const [value, setValue] = useState(() => readDraft(userId, applicationId) ?? emptyDraft());
    const [secrets, setSecrets] = useState(() => readSecrets(userId, applicationId));
    const [savedAt, setSavedAt] = useState<string>();

    const update = useCallback<Draft['update']>(
        (patch) =>
            setValue((prev) => {
                const next = { ...prev, ...(typeof patch === 'function' ? patch(prev) : patch) };
                writeDraft(userId, applicationId, next);
                setSavedAt(new Date().toISOString());
                return next;
            }),
        [userId, applicationId],
    );
    const updateSecrets = useCallback(
        (patch: Partial<Secrets>) =>
            setSecrets((prev) => {
                const next = { ...prev, ...patch };
                writeSecrets(userId, applicationId, next);
                return next;
            }),
        [userId, applicationId],
    );
    return { value, update, secrets, updateSecrets, savedAt };
}

// ---------------------------------------------------------------- проверки (как у data-collector)

export type Errors = Record<string, string>;

const PERSON_INN = /^\d{12}$/;
const BIK = /^\d{9}$/;
export const CADASTRAL = /^\d{2}:\d{2}:\d{6,7}:\d+$/;
export const VIN = /^[A-HJ-NPR-Z0-9]{17}$/;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const MAX_FILES = 10;
export const MAX_FILE_BYTES = 20 * 1024 * 1024;
export const MAX_BANKS = 10;
export const MAX_COLLATERAL = 20;
export const ALLOWED_EXTENSIONS = ['pdf', 'docx', 'xlsx', 'jpg', 'jpeg', 'png', 'xml', 'sig', 'p7s'];

export const isSignatureName = (name: string) => /\.(sig|p7s)$/i.test(name);

/**
 * Браузер кодирует «"», CR и LF в имени части multipart как %22/%0D/%0A, а data-collector их не раскодирует —
 * имя в documents[].file_name и пара «документ + .sig» перестают совпадать. Заменяем заранее.
 */
export const safeFileName = (name: string) => name.replace(/["\r\n]/g, '_');
const extOf = (name: string) => name.split('.').pop()?.toLowerCase() ?? '';

function validDate(value: string) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
    const d = new Date(`${value}T00:00:00Z`);
    return !Number.isNaN(d.getTime()) && d.toISOString().startsWith(value);
}

export function termsErrors(d: QuestionnaireDraft): Errors {
    const e: Errors = {};
    if (!d.requested) e.requested = 'Укажите сумму и срок';
    if (!d.purpose?.code) e['purpose.code'] = 'Выберите цель кредита';
    if ((d.purpose?.description?.length ?? 0) > 2000) e['purpose.description'] = 'Не длиннее 2000 символов';
    return e;
}

export function borrowerErrors(d: QuestionnaireDraft): Errors {
    const e: Errors = {};
    if (d.age === undefined) e.age = 'Укажите возраст';
    else if (d.age < 18 || d.age > 100) e.age = 'Возраст — от 18 до 100 лет';
    if (!d.marital_status) e.marital_status = 'Укажите семейное положение';
    if (d.children_count === undefined) e.children_count = 'Укажите число детей и иждивенцев';
    else if (d.children_count < 0 || d.children_count > 20) e.children_count = 'От 0 до 20';
    if (d.with_director) {
        const p = d.director;
        if (!PERSON_INN.test(p.inn)) e['director.inn'] = 'ИНН физлица — 12 цифр';
        if (!p.last_name.trim()) e['director.last_name'] = 'Укажите фамилию';
        if (!p.first_name.trim()) e['director.first_name'] = 'Укажите имя';
        if (!validDate(p.birth_date)) e['director.birth_date'] = 'Укажите дату рождения';
    }
    return e;
}

/** Повторы в списке: data-collector их схлопывает, и номера строк в его ошибках перестают совпадать с нашими. */
function markDuplicates(e: Errors, field: string, values: string[], normalize = (v: string) => v) {
    const seen = new Set<string>();
    values.forEach((v, i) => {
        const key = normalize(v.trim());
        if (key && seen.has(key)) e[`${field}[${i}]`] ??= 'Уже есть в списке';
        seen.add(key);
    });
}

export function sourcesErrors(d: QuestionnaireDraft, s: Secrets): Errors {
    const e: Errors = {};
    if (d.other_bank_biks.length > MAX_BANKS) e.other_bank_biks = `Не больше ${MAX_BANKS} банков`;
    d.other_bank_biks.forEach((b, i) => {
        if (!BIK.test(b)) e[`other_bank_biks[${i}]`] = 'БИК — 9 цифр';
    });
    markDuplicates(e, 'other_bank_biks', d.other_bank_biks);
    for (const [mp, token] of Object.entries(s.marketplace_tokens)) {
        if (token !== undefined && token !== '' && !token.trim()) e[`marketplace_tokens.${mp}`] = 'Пустой токен';
    }
    return e;
}

export function collateralErrors(d: QuestionnaireDraft): Errors {
    const e: Errors = {};
    const c = d.collateral;
    if (c.cadastral_numbers.length + c.vins.length + c.serials.length > MAX_COLLATERAL) e.collateral = `Не больше ${MAX_COLLATERAL} объектов залога`;
    c.cadastral_numbers.forEach((n, i) => {
        if (!CADASTRAL.test(n)) e[`collateral.cadastral_numbers[${i}]`] = 'Номер вида 50:21:0110203:845';
    });
    c.vins.forEach((v, i) => {
        if (!VIN.test(v)) e[`collateral.vins[${i}]`] = 'VIN — 17 символов, без I, O, Q';
    });
    c.serials.forEach((s, i) => {
        if (!s.trim() || s.length > 64) e[`collateral.serials[${i}]`] = 'От 1 до 64 символов';
    });
    markDuplicates(e, 'collateral.cadastral_numbers', c.cadastral_numbers);
    markDuplicates(e, 'collateral.vins', c.vins, (v) => v.toUpperCase());
    markDuplicates(e, 'collateral.serials', c.serials);
    return e;
}

/** К какому документу относится подпись: X.sig → X; без расширения у X — единственный документ с той же основой. */
export function signedTarget(sig: FileEntry, files: FileEntry[]): FileEntry | undefined | 'ambiguous' {
    // имена сравниваем без учёта регистра — как data-collector
    const base = sig.name.replace(/\.(sig|p7s)$/i, '').toLowerCase();
    const exact = files.find((f) => f.name.toLowerCase() === base);
    if (exact) return exact;
    if (base.includes('.')) return undefined;
    const same = files.filter((f) => !isSignatureName(f.name) && f.name.replace(/\.[^.]+$/, '').toLowerCase() === base);
    if (same.length > 1) return 'ambiguous';
    return same[0];
}

/** Открепленная подпись — к ней нашёлся документ; без пары подпись присоединённая и сама считается документом. */
export const isDetachedSignature = (f: FileEntry, files: FileEntry[]) => {
    if (!isSignatureName(f.name)) return false;
    const target = signedTarget(f, files);
    return target !== undefined && target !== 'ambiguous';
};

/** Ошибка у конкретного файла (ключ — FileEntry.key) и общая — files. */
export function filesErrors(d: QuestionnaireDraft): Errors {
    const e: Errors = {};
    if (d.files.length > MAX_FILES) e.files = `Не больше ${MAX_FILES} файлов вместе с подписями`;
    const names = new Map<string, string>();
    const signed = new Set<string>();
    for (const f of d.files) {
        if (!ALLOWED_EXTENSIONS.includes(extOf(f.name))) e[f.key] = 'Подойдут PDF, DOCX, XLSX, JPG, PNG, XML и подписи SIG, P7S';
        else if (f.size === 0) e[f.key] = 'Пустой файл';
        else if (f.size > MAX_FILE_BYTES) e[f.key] = 'Файл больше 20 МБ';
        const lower = f.name.toLowerCase();
        if (names.has(lower)) e[f.key] = 'Файл с таким именем уже есть';
        else names.set(lower, f.key);
        if (!isSignatureName(f.name)) continue;
        const target = signedTarget(f, d.files);
        if (target === 'ambiguous') e[f.key] = 'Непонятно, какой документ подписан: назовите подпись как документ + .sig';
        else if (target && isSignatureName(target.name)) e[f.key] = 'Подпись не может подписывать другую подпись';
        else if (target && signed.has(target.key)) e[f.key] = `У «${target.name}» уже есть подпись`;
        else if (target) signed.add(target.key);
    }
    return e;
}

export function reviewErrors(d: QuestionnaireDraft, needsPoa: boolean): Errors {
    const e: Errors = {};
    if (d.consents.tax && !d.tax_scope.length) e['consents.tax.scope'] = 'Отметьте хотя бы одно';
    if (needsPoa && d.poa_number && !UUID.test(d.poa_number.trim().toLowerCase())) e.poa_number = 'Номер МЧД — UUID, например 0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37';
    if (needsPoa && d.representative_inn && !PERSON_INN.test(d.representative_inn)) e.representative_inn = 'ИНН физлица — 12 цифр';
    return e;
}

// ---------------------------------------------------------------- анкета для data-collector

const addMonths = (iso: string, months: number) => {
    const d = new Date(iso);
    d.setMonth(d.getMonth() + months);
    return d.toISOString().slice(0, 10);
};

/** Согласие: id — номер в духе банка, срок действия — полгода с даты подписания. */
const consent = (signedAt: string, id: string) => ({ id, signed_at: signedAt, valid_until: addMonths(signedAt, 6) });

export function toQuestionnaire(d: QuestionnaireDraft, s: Secrets, number: string, needsPoa: boolean): QuestionnaireInput {
    const suffix = number.replace(/\D/g, '');
    const tokens = Object.fromEntries(Object.entries(s.marketplace_tokens).filter(([, t]) => t?.trim())) as Secrets['marketplace_tokens'];
    const director = d.with_director
        ? { ...d.director, middle_name: d.director.middle_name.trim() || undefined, last_name: d.director.last_name.trim(), first_name: d.director.first_name.trim() }
        : undefined;
    const c = d.collateral;
    return {
        age: d.age ?? 0,
        marital_status: d.marital_status ?? 'SINGLE',
        children_count: d.children_count ?? 0,
        product_code: d.product_code,
        requested: d.requested,
        purpose: d.purpose?.code ? { code: d.purpose.code, description: d.purpose.description?.trim() || undefined } : undefined,
        documents: d.files.filter((f) => !isDetachedSignature(f, d.files)).map((f) => ({ file_name: f.name, type: f.type ?? 'OTHER' })),
        director,
        consents: {
            bki_company: d.consents.bki_company ? consent(d.consents.bki_company, `CNS-${suffix}-BKI-C`) : undefined,
            bki_person: d.consents.bki_person && director ? consent(d.consents.bki_person, `CNS-${suffix}-BKI-P`) : undefined,
            tax: d.consents.tax ? { ...consent(d.consents.tax, `TAX-CNS-${suffix}`), scope: d.tax_scope } : undefined,
        },
        other_bank_biks: d.other_bank_biks.length ? d.other_bank_biks : undefined,
        marketplace_tokens: Object.keys(tokens).length ? tokens : undefined,
        ofd_token: s.ofd_token.trim() || undefined,
        collateral:
            c.cadastral_numbers.length + c.vins.length + c.serials.length
                ? {
                      cadastral_numbers: c.cadastral_numbers.length ? c.cadastral_numbers : undefined,
                      vins: c.vins.length ? c.vins : undefined,
                      serials: c.serials.length ? c.serials : undefined,
                  }
                : undefined,
        poa_number: needsPoa && d.poa_number.trim() ? d.poa_number.trim().toLowerCase() : undefined,
        representative_inn: needsPoa && d.representative_inn ? d.representative_inn : undefined,
    };
}

/** Чего не хватит для сбора — как domain.SkippedSystems у data-collector. */
export function skippedPreview(d: QuestionnaireDraft, s: Secrets): string[] {
    const list: string[] = [];
    if (!d.consents.bki_company && !(d.with_director && d.consents.bki_person)) list.push('кредитную историю — нет согласия на запрос в БКИ');
    if (!d.consents.tax) list.push('налоговую отчётность — нет согласия');
    if (!d.collateral.cadastral_numbers.length) list.push('ЕГРН — нет недвижимости в залог');
    if (!d.with_director) list.push('Госуслуги и проверки руководителя — нет его данных');
    if (!d.other_bank_biks.length) list.push('счета в других банках — не указаны банки');
    if (!Object.values(s.marketplace_tokens).some((t) => t?.trim())) list.push('маркетплейсы — нет токенов продавца');
    if (!s.ofd_token.trim()) list.push('онлайн-кассы — нет токена ОФД');
    return list;
}
