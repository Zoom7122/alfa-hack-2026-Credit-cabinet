import type { Product } from '../../api/types';
import { products } from '../data';

/** Сумма и срок вне диапазона продукта — поля как в анкете (requested.*). */
export function termsErrors(p: Product, amount: number, term: number) {
    const errors = [];
    if (!(amount >= p.amount.min && amount <= p.amount.max)) {
        errors.push({ field: 'requested.amount', code: 'out_of_range', message: `сумма по продукту «${p.name}» — от ${p.amount.min} до ${p.amount.max} ₽` });
    }
    if (!(term >= p.term_months.min && term <= p.term_months.max)) {
        errors.push({ field: 'requested.term_months', code: 'out_of_range', message: `срок по продукту «${p.name}» — от ${p.term_months.min} до ${p.term_months.max} месяцев` });
    }
    return errors;
}

export const productByCode = (code: string) => products.find((p) => p.code === code);
