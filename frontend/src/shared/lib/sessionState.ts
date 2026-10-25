import { type Dispatch, type SetStateAction, useCallback, useState } from 'react';

/** Черновой ввод, который ещё нельзя отправить на сервер, держим в sessionStorage вкладки. */
export function readSession<T>(key: string): T | undefined {
    try {
        const raw = sessionStorage.getItem(key);
        return raw === null ? undefined : (JSON.parse(raw) as T);
    } catch {
        return undefined;
    }
}

export function writeSession(key: string, value: unknown) {
    try {
        if (value === undefined) sessionStorage.removeItem(key);
        else sessionStorage.setItem(key, JSON.stringify(value));
    } catch {
        /* приватный режим — живём без сохранения */
    }
}

/** useState, который переживает переход между шагами и перезагрузку вкладки. */
export function useSessionState<T>(key: string, initial: () => T): [T, Dispatch<SetStateAction<T>>] {
    const [value, setValue] = useState<T>(() => readSession<T>(key) ?? initial());
    const set = useCallback<Dispatch<SetStateAction<T>>>(
        (next) =>
            setValue((prev) => {
                const resolved = typeof next === 'function' ? (next as (p: T) => T)(prev) : next;
                writeSession(key, resolved);
                return resolved;
            }),
        [key],
    );
    return [value, set];
}
