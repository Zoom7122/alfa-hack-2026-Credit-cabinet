import { isCollectorId } from '../shared/lib/collector';
import { API_BASE_URL, ApiError, unwrap } from './client';
import { COLLECTOR_BASE_URL } from './collector/client';
import { session } from './session';
import type { Document } from './types';

export type StoredDocument = Document & { generation?: number };
export type FinancialResult = {
    state?: 'PENDING';
    extracted?: Record<string, unknown>;
    indicators?: { name: string; period: string; period_kind?: string; amount: string; data_eligible: boolean }[];
    diagnostics?: { code: string; severity: string; message: string }[];
    signature?: { policy_result?: { satisfied: boolean }; verified_at?: string };
};

/** Document routes are also implemented by data-collector, whose generated Swagger omits them. */
export async function documentRequest<T>(applicationId: string, suffix = '', init?: RequestInit): Promise<T> {
    const base = isCollectorId(applicationId) ? COLLECTOR_BASE_URL : API_BASE_URL;
    const headers = new Headers(init?.headers);
    const token = session.getToken();
    if (token) headers.set('Authorization', `Bearer ${token}`);
    const response = await fetch(`${base}/applications/${encodeURIComponent(applicationId)}/documents${suffix}`, { ...init, headers });
    let body: unknown;
    if (response.status !== 204) {
        try { body = await response.json(); }
        catch { throw new ApiError(response.ok ? 502 : response.status, 'Не удалось загрузить документы'); }
    }
    return unwrap({ response, ...(response.ok ? { data: body as T } : { error: body }) });
}

/** Keep recognition tied to the current generation, including after re-verification. */
export async function loadDocumentResult(applicationId: string, doc: StoredDocument): Promise<Document> {
    if (!['FINANCIAL_STATEMENTS', 'TAX_RETURN'].includes(doc.type) || (isCollectorId(applicationId) && !doc.generation)) return doc;
    try {
        const result = await documentRequest<FinancialResult>(applicationId, `/${encodeURIComponent(doc.id)}/financial-result`);
        if (result.state === 'PENDING') return { ...doc, recognition: { state: 'PROCESSING' } };
        const extracted = { ...result.extracted };
        for (const indicator of result.indicators ?? []) {
            const prefixes: Record<string, string> = { accounting_revenue: 'revenue', usn_taxable_income: 'revenue', net_profit: 'net_profit' };
            const prefix = prefixes[indicator.name];
            const amount = Number(indicator.amount);
            if (prefix && /^\d{4}$/.test(indicator.period) && indicator.data_eligible && Number.isFinite(amount)) extracted[`${prefix}_${indicator.period}`] = amount;
        }
        const failed = result.diagnostics?.some((d) => d.severity === 'ERROR' && ['PARSE_FAILED', 'UNSUPPORTED', 'PARSER_HASH_MISMATCH'].includes(d.code));
        return {
            ...doc,
            recognition: { state: failed ? 'FAILED' : 'DONE', extracted },
            signatures: doc.signatures && {
                ...doc.signatures,
                policy_satisfied: result.signature?.policy_result?.satisfied ?? doc.signatures.policy_satisfied,
                verified_at: result.signature?.verified_at ?? doc.signatures.verified_at,
            },
        };
    } catch (error) {
        if (error instanceof ApiError && [409, 422].includes(error.status)) return { ...doc, recognition: { state: 'FAILED' } };
        throw error;
    }
}
