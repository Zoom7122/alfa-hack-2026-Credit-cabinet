import { delay, http, HttpResponse } from 'msw';

import type { Product } from '../../api/types';
import { products } from '../data';
import { productByCode, termsErrors } from './catalog';
import { notFound, url, validation } from './handlers';

/* Каталог и калькулятор data-collector (catalog_handler.go): те же 7 продуктов, что в domain/product.go. Токен не нужен. */

const PURPOSES = ['WORKING_CAPITAL', 'EQUIPMENT', 'VEHICLE', 'REAL_ESTATE', 'REFINANCING', 'TENDER', 'BUSINESS_EXPANSION', 'OTHER'];
const TYPES = ['LEGAL_ENTITY', 'SOLE_PROPRIETOR'];

const toCollector = ({ code, name, description, applicant_types, purposes, amount, term_months, rate_annual_from, collateral, guarantee_required }: Product) => ({
    code,
    name,
    description,
    applicant_types,
    purposes,
    amount,
    term_months,
    rate_annual_from,
    collateral: { required_above_amount: collateral?.required_above_amount ?? null, accepted_types: collateral?.accepted_types ?? [] },
    guarantee_required: Boolean(guarantee_required),
});

/** Аннуитет как decision.Annuity: ставка в процентах, округление до рубля. */
function annuity(amount: number, rate: number, months: number) {
    const r = rate / 100 / 12;
    if (r === 0) return Math.round(amount / months);
    const k = (r * (1 + r) ** months) / ((1 + r) ** months - 1);
    return Math.round(amount * k);
}

export const collectorCatalogHandlers = [
    http.get(url('/products'), async ({ request }) => {
        await delay(150);
        const params = new URL(request.url).searchParams;
        const purpose = params.get('purpose') ?? '';
        const type = params.get('applicant_type') ?? '';
        const errors = [];
        if (purpose && !PURPOSES.includes(purpose)) errors.push({ field: 'purpose', code: 'invalid_value', message: `допустимые значения: ${PURPOSES.join(', ')}` });
        if (type && !TYPES.includes(type)) errors.push({ field: 'applicant_type', code: 'invalid_value', message: `допустимые значения: ${TYPES.join(', ')}` });
        if (errors.length) return validation(422, 'неверный фильтр каталога', errors);
        const items = products
            .filter((p) => !purpose || p.purposes?.includes(purpose as never))
            .filter((p) => !type || p.applicant_types.includes(type as never))
            .map(toCollector);
        return HttpResponse.json({ items });
    }),

    http.get(url('/products/:code'), async ({ params }) => {
        await delay(100);
        const p = productByCode(String(params.code));
        return p ? HttpResponse.json(toCollector(p)) : notFound('продукт не найден');
    }),

    http.post(url('/products/:code/calculate'), async ({ request, params }) => {
        await delay(200);
        const body = (await request.json().catch(() => null)) as { amount?: unknown; term_months?: unknown } | null;
        if (typeof body?.amount !== 'number' || typeof body.term_months !== 'number') {
            return validation(400, 'тело запроса не прошло проверку', [
                ...(typeof body?.amount !== 'number' ? [{ field: 'amount', code: 'required', message: 'обязательное поле' }] : []),
                ...(typeof body?.term_months !== 'number' ? [{ field: 'term_months', code: 'required', message: 'обязательное поле' }] : []),
            ]);
        }
        const p = productByCode(String(params.code));
        if (!p) return notFound('продукт не найден');
        const errors = termsErrors(p, body.amount, body.term_months);
        if (errors.length) return validation(422, 'проверьте сумму и срок', errors);
        return HttpResponse.json({
            rate_annual_from: p.rate_annual_from,
            monthly_payment_from: annuity(body.amount, p.rate_annual_from, body.term_months),
            disclaimer: 'Предварительный расчёт по условиям продукта. Точные условия — после рассмотрения заявки.',
        });
    }),
];
