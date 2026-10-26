import type { components } from './schema';
import type { CollateralType, Product } from '../types';

/** Продукт каталога data-collector → тип контракта, по которому написаны подбор продукта и анкета. */
export function toProduct(p: components['schemas']['Product']): Product {
    return {
        code: p.code ?? '',
        name: p.name ?? '',
        description: p.description,
        applicant_types: (p.applicant_types ?? []) as Product['applicant_types'],
        purposes: (p.purposes ?? []) as Product['purposes'],
        amount: { min: p.amount?.min ?? 0, max: p.amount?.max ?? 0 },
        term_months: { min: p.term_months?.min ?? 0, max: p.term_months?.max ?? 0 },
        rate_annual_from: p.rate_annual_from ?? 0,
        collateral: { required_above_amount: p.collateral?.required_above_amount ?? null, accepted_types: (p.collateral?.accepted_types ?? []) as CollateralType[] },
        guarantee_required: p.guarantee_required,
    };
}
