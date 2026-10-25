import type { Product } from '../../api/types';

/** Пока пользователь печатает, значение может быть пустым или вне диапазона — зажимаем на blur. */
export type TermsValue = { amount: number | ''; term: number | '' };

type Ranges = Pick<Product, 'amount' | 'term_months'>;

export const clamp = (n: number, min: number, max: number) => Math.min(max, Math.max(min, n));

/** Сумма и срок, если оба в пределах продукта — только такие можно считать и отправлять. */
export function validTerms(product: Ranges | undefined, value: TermsValue) {
    if (!product || value.amount === '' || value.term === '') return undefined;
    const inRange = (n: number, r: { min: number; max: number }) => n >= r.min && n <= r.max;
    if (!inRange(value.amount, product.amount) || !inRange(value.term, product.term_months)) return undefined;
    return { amount: value.amount, term_months: value.term };
}

/** Привести сумму и срок к диапазону продукта (при смене продукта). */
export function fitTerms(product: Ranges, value: TermsValue): TermsValue {
    return {
        amount: clamp(value.amount === '' ? product.amount.min : value.amount, product.amount.min, product.amount.max),
        term: clamp(value.term === '' ? product.term_months.min : value.term, product.term_months.min, product.term_months.max),
    };
}
