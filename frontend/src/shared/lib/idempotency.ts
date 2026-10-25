import { useCallback, useRef } from 'react';

/** UUID для Idempotency-Key. randomUUID есть только в secure context — по http с IP собираем из getRandomValues. */
export function newIdempotencyKey(): string {
    if (typeof crypto.randomUUID === 'function') return crypto.randomUUID();
    const b = crypto.getRandomValues(new Uint8Array(16));
    b[6] = ((b[6] ?? 0) & 0x0f) | 0x40;
    b[8] = ((b[8] ?? 0) & 0x3f) | 0x80;
    const hex = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

/**
 * Ключ на конкретное тело запроса: повтор того же запроса (сеть, двойной клик) идёт с тем же ключом,
 * а изменённый (другой ИНН, роль) — с новым, иначе бэк вернул бы ресурс от прошлого тела.
 */
export function useIdempotencyKeys() {
    const keys = useRef(new Map<string, string>());
    const keyFor = useCallback((scope: string, body: unknown) => {
        const id = `${scope}:${JSON.stringify(body)}`;
        let key = keys.current.get(id);
        if (!key) {
            key = newIdempotencyKey();
            keys.current.set(id, key);
        }
        return key;
    }, []);
    /** После однозначного отказа (4xx) исправленная попытка — новый запрос. */
    const forget = useCallback((scope: string, body: unknown) => {
        keys.current.delete(`${scope}:${JSON.stringify(body)}`);
    }, []);
    return { keyFor, forget };
}
