import type { Document, InfoRequest } from '../../../api/types';

export type AnswerDraft = { value?: number | string | boolean; comment: string };

/** Поле даты кита отдаёт «ДД.ММ.ГГГГ», в API уходит ISO-дата. */
const DATE_RE = /^(\d{2})\.(\d{2})\.(\d{4})$/;
/** Число вводят строкой («1,5»), чтобы не съедались запятая и незаконченная дробь. */
const NUMBER_RE = /^-?\d+([.,]\d+)?$/;

export function toApiValue(request: InfoRequest, value: AnswerDraft['value']) {
    if (request.input.type === 'DATE' && typeof value === 'string') return value.replace(DATE_RE, '$3-$2-$1');
    if (request.input.type === 'NUMBER' && typeof value === 'string') return Number(value.replace(',', '.'));
    return value;
}

/** Готов ли ответ к отправке: значение по типу поля и документы, если они обязательны. */
export function isAnswerReady(request: InfoRequest, answer: AnswerDraft, documents: Document[]) {
    if (request.documents?.required && !documents.length) return false;
    const { value } = answer;
    switch (request.input.type) {
        case 'MONEY':
            return typeof value === 'number';
        case 'NUMBER':
            return typeof value === 'string' && NUMBER_RE.test(value.trim());
        case 'TEXT':
            return typeof value === 'string' && value.trim().length > 0;
        case 'DATE':
            return typeof value === 'string' && DATE_RE.test(value);
        case 'BOOLEAN':
            return typeof value === 'boolean';
        case 'CHOICE':
            return typeof value === 'string';
        default:
            return true;
    }
}
