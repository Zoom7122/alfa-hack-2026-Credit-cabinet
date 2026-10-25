import { useSyncExternalStore } from 'react';

/**
 * Заявки пользователя в data-collector. Ручки списка у сервиса нет, поэтому id храним в браузере.
 * TODO: убрать, когда data-collector научится отдавать заявки пользователя.
 */
export type IndexedApplication = { id: string; number?: string; company_inn: string; company_name?: string; created_at: string };

const keyFor = (userId: string) => `credit-cabinet:collector-apps:${userId}`;
const listeners = new Set<() => void>();
const cache = new Map<string, { raw: string | null; value: IndexedApplication[] }>();

function read(userId: string): IndexedApplication[] {
    let raw: string | null = null;
    try {
        raw = localStorage.getItem(keyFor(userId));
    } catch {
        /* приватный режим */
    }
    const cached = cache.get(userId);
    if (cached && cached.raw === raw) return cached.value;
    let value: IndexedApplication[] = [];
    try {
        value = raw ? (JSON.parse(raw) as IndexedApplication[]) : [];
    } catch {
        value = [];
    }
    cache.set(userId, { raw, value });
    return value;
}

export function rememberApplication(userId: string, entry: IndexedApplication) {
    const list = read(userId).filter((a) => a.id !== entry.id);
    try {
        localStorage.setItem(keyFor(userId), JSON.stringify([entry, ...list]));
    } catch {
        /* без сохранения заявка пропадёт со списка после перезагрузки — переживём */
    }
    listeners.forEach((l) => l());
}

export function forgetApplication(userId: string, id: string) {
    try {
        localStorage.setItem(keyFor(userId), JSON.stringify(read(userId).filter((a) => a.id !== id)));
    } catch {
        /* без сохранения — переживём */
    }
    listeners.forEach((l) => l());
}

/** Заявки пользователя по компании — среди них ищем открытый черновик, чтобы не заводить второй. */
export const indexedFor = (userId: string, companyInn: string) => read(userId).filter((a) => a.company_inn === companyInn);

function subscribe(listener: () => void) {
    listeners.add(listener);
    const onStorage = (e: StorageEvent) => e.key?.startsWith('credit-cabinet:collector-apps:') && listener();
    window.addEventListener('storage', onStorage);
    return () => {
        listeners.delete(listener);
        window.removeEventListener('storage', onStorage);
    };
}

export function useCollectorIndex(userId: string) {
    return useSyncExternalStore(subscribe, () => read(userId));
}
