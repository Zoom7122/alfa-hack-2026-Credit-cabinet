/** Формат ИНН из контракта: 10 цифр — юрлицо, 12 — ИП или физлицо. */
export const isInn = (value: unknown): value is string => typeof value === 'string' && /^(\d{10}|\d{12})$/.test(value);

const checkDigit = (digits: number[], weights: number[]) =>
    (weights.reduce((sum, w, i) => sum + w * (digits[i] ?? 0), 0) % 11) % 10;

/** Формат + контрольные цифры ФНС. Бэк проверяет так же — лучше поймать опечатку сразу. */
export function isValidInn(value: unknown): value is string {
    if (!isInn(value)) return false;
    const d = [...value].map(Number);
    if (d.length === 10) return checkDigit(d, [2, 4, 10, 3, 5, 9, 4, 6, 8]) === d[9];
    return (
        checkDigit(d, [7, 2, 4, 10, 3, 5, 9, 4, 6, 8]) === d[10] && checkDigit(d, [3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8]) === d[11]
    );
}
