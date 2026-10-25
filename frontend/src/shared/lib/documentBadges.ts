import type { Document, SignatureReport } from '../../api/types';
import { formatCompactMoney } from './format';

/* Бейджи вкладки «Документы» (экран 15): распознавание и электронная подпись. */

export type BadgeColor = 'green' | 'orange' | 'red' | 'grey' | 'blue';
export type Badge = { label: string; color: BadgeColor };

export const VERDICT_BADGES: Record<SignatureReport['verdict'], Badge> = {
    VALID: { label: 'Действительна', color: 'green' },
    VALID_WITH_WARNINGS: { label: 'Есть замечания', color: 'orange' },
    INVALID: { label: 'Недействительна', color: 'red' },
    INDETERMINATE: { label: 'Не удалось проверить', color: 'grey' },
};

/** Что распознали: выручку из отчётности показываем числом, остальное — «Распознано». */
export function recognitionBadge(doc: Document): Badge | undefined {
    const r = doc.recognition;
    switch (r?.state) {
        case 'PENDING':
        case 'PROCESSING':
            return { label: 'Распознаём…', color: 'blue' };
        case 'FAILED':
            return { label: 'Не распознан', color: 'orange' };
        case 'DONE': {
            const revenue = Object.entries(r.extracted ?? {}).find(([key]) => key.startsWith('revenue_'))?.[1];
            return typeof revenue === 'number'
                ? { label: `Выручка ${formatCompactMoney(revenue)} ₽`, color: 'green' }
                : { label: 'Распознано', color: 'green' };
        }
        default:
            return undefined;
    }
}

export function signatureBadge(doc: Document): Badge {
    const s = doc.signatures;
    if (!s || s.state === 'NOT_SIGNED') return { label: 'Без подписи', color: 'grey' };
    if (s.state === 'PENDING') return { label: 'Проверяем…', color: 'blue' };
    if (s.state === 'FAILED') return { label: 'Проверка не удалась', color: 'red' };
    // вердикт документа = худшая обязательная подпись + выполнена ли политика (docs/e-signature.md)
    if (s.policy_satisfied === false && s.verdict !== 'INVALID') return { label: 'Не хватает подписей', color: 'orange' };
    if (s.verdict === 'VALID') return { label: 'КЭП верна', color: 'green' };
    return s.verdict ? VERDICT_BADGES[s.verdict] : { label: 'Проверено', color: 'grey' };
}
