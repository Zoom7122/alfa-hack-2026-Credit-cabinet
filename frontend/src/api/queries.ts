import { keepPreviousData, useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';

import { newIdempotencyKey } from '../shared/lib/idempotency';
import { api, isUnauthorized, unwrap } from './client';
import { session } from './session';
import type {
    ApplicantType,
    Application,
    ApplicationInput,
    ApplicationStatus,
    ApplicationStatusView,
    ApplicationSummary,
    Document,
    DocumentType,
    PurposeCode,
    RepresentativeRole,
    RequestedTerms,
} from './types';

type CatalogParams = { purpose?: PurposeCode; applicantType?: ApplicantType };

/**
 * Ключи запросов. Общий префикс 'applications' позволяет по WS-событию
 * application.status_changed инвалидировать и список, и /status одной командой.
 */
export const queryKeys = {
    testUsers: ['auth', 'test-users'] as const,
    me: ['me'] as const,
    products: (params: CatalogParams = {}) => ['products', 'list', params] as const,
    calculation: (code: string, terms: RequestedTerms) => ['products', 'calculate', code, terms] as const,
    counterparty: (inn: string) => ['counterparty', inn] as const,
    applicants: ['applicants'] as const,
    applications: (params: { applicantId?: string; status?: ApplicationStatus[] }) =>
        ['applications', 'list', params] as const,
    application: (id: string) => ['applications', 'detail', id] as const,
    applicationStatus: (id: string) => ['applications', 'status', id] as const,
    documents: (id: string) => ['applications', 'documents', id] as const,
};

export function useTestUsers() {
    return useQuery({
        queryKey: queryKeys.testUsers,
        queryFn: async () => unwrap(await api.GET('/auth/test-users')).items,
        staleTime: Infinity,
    });
}

export function useMockLogin() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (userId: string) =>
            unwrap(await api.POST('/auth/mock-login', { body: { user_id: userId } })),
        onSuccess: ({ token, user }) => {
            // сначала профиль в кэш, потом токен: подписчики токена сразу видят готовый /me
            queryClient.setQueryData(queryKeys.me, user);
            session.setToken(token);
        },
    });
}

export function useMe(enabled = true) {
    return useQuery({
        queryKey: queryKeys.me,
        queryFn: async () => {
            try {
                return unwrap(await api.GET('/me'));
            } catch (error) {
                // токен протух — забываем его, гард уведёт на вход
                if (isUnauthorized(error)) session.clear();
                throw error;
            }
        },
        enabled,
        retry: false,
    });
}

export function useProducts(params: CatalogParams = {}, enabled = true) {
    return useQuery({
        enabled,
        queryKey: queryKeys.products(params),
        queryFn: async () =>
            unwrap(await api.GET('/products', { params: { query: { purpose: params.purpose, applicant_type: params.applicantType } } }))
                .items,
        staleTime: 10 * 60 * 1000,
        placeholderData: keepPreviousData,
    });
}

/** Предварительный платёж. Без terms (сумма вне диапазона, ещё вводят) — не считаем. */
export function useCalculation(code: string | undefined, terms: RequestedTerms | undefined) {
    return useQuery({
        queryKey: queryKeys.calculation(code ?? '', terms ?? { amount: 0, term_months: 0 }),
        queryFn: async () =>
            unwrap(
                await api.POST('/products/{productCode}/calculate', {
                    params: { path: { productCode: code ?? '' } },
                    body: terms ?? { amount: 0, term_months: 0 },
                }),
            ),
        enabled: Boolean(code && terms),
        placeholderData: keepPreviousData,
        staleTime: Infinity,
    });
}

/** Реквизиты по ИНН. 404/504 — нормальные исходы (не нашли, реестр молчит), без повторов. */
export function useCounterpartyLookup(inn: string | undefined) {
    return useQuery({
        queryKey: queryKeys.counterparty(inn ?? ''),
        queryFn: async ({ signal }) => unwrap(await api.POST('/counterparty/lookup', { body: { inn: inn ?? '' }, signal })),
        enabled: Boolean(inn),
        retry: false,
        staleTime: Infinity,
    });
}

export function useApplicants() {
    return useQuery({
        queryKey: queryKeys.applicants,
        queryFn: async () => unwrap(await api.GET('/applicants')).items,
    });
}

// TODO: пагинация по next_cursor, когда заявок станет больше одной страницы
export function useApplications(params: { applicantId?: string; status?: ApplicationStatus[] }) {
    return useQuery({
        queryKey: queryKeys.applications(params),
        queryFn: async () =>
            unwrap(
                await api.GET('/applications', {
                    params: { query: { applicant_id: params.applicantId, status: params.status, limit: 100 } },
                }),
            ).items,
        enabled: Boolean(params.applicantId),
    });
}

/** Лёгкие статусы заявок (вопросы банка, кто подписывает), собранные в Map по id заявки. */
export function useApplicationStatuses(applications: ApplicationSummary[]) {
    return useQueries({
        queries: applications.map((app) => ({
            queryKey: queryKeys.applicationStatus(app.id),
            queryFn: async () =>
                unwrap(await api.GET('/applications/{applicationId}/status', { params: { path: { applicationId: app.id } } })),
        })),
        combine: (results) => {
            const map = new Map<string, ApplicationStatusView>();
            for (const r of results) if (r.data) map.set(r.data.id, r.data);
            return map;
        },
    });
}

export function useCreateApplicant() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (body: { inn: string; representative_role: RepresentativeRole; idempotencyKey: string }) =>
            unwrap(
                await api.POST('/applicants', {
                    params: { header: { 'Idempotency-Key': body.idempotencyKey } },
                    body: { inn: body.inn, representative_role: body.representative_role },
                }),
            ),
        onSuccess: () => {
            // у пользователя появилась компания — она есть в /me и /applicants
            queryClient.invalidateQueries({ queryKey: queryKeys.me });
            queryClient.invalidateQueries({ queryKey: queryKeys.applicants });
        },
    });
}

export function useCreateApplication() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({
            idempotencyKey,
            ...body
        }: ApplicationInput & { applicant_id: string; product_code: string; idempotencyKey: string }) =>
            unwrap(await api.POST('/applications', { params: { header: { 'Idempotency-Key': idempotencyKey } }, body })),
        onSuccess: (app) => {
            queryClient.setQueryData(queryKeys.application(app.id), app);
            queryClient.invalidateQueries({ queryKey: ['applications', 'list'] });
        },
    });
}

export function useApplication(id: string | undefined) {
    return useQuery({
        queryKey: queryKeys.application(id ?? ''),
        queryFn: async () => unwrap(await api.GET('/applications/{applicationId}', { params: { path: { applicationId: id ?? '' } } })),
        enabled: Boolean(id),
    });
}

/** JSON Merge Patch: null очищает поле, массивы уходят целиком. */
export type DraftPatch = {
    [K in keyof ApplicationInput]?: ApplicationInput[K] extends unknown[] | undefined
        ? ApplicationInput[K] | null
        : { [P in keyof NonNullable<ApplicationInput[K]>]?: NonNullable<ApplicationInput[K]>[P] | null } | null;
};

export async function patchApplication(
    id: string,
    version: number,
    patch: DraftPatch,
    options: { keepalive?: boolean } = {},
): Promise<Application> {
    return unwrap(
        await api.PATCH('/applications/{applicationId}', {
            params: { path: { applicationId: id }, header: { 'If-Match': String(version) } },
            // null в merge-patch законен, а в схеме ApplicationInput его нет
            body: patch as ApplicationInput,
            headers: { 'Content-Type': 'application/merge-patch+json' },
            // запрос при закрытии вкладки должен пережить выгрузку страницы
            keepalive: options.keepalive,
        }),
    );
}

export function useSubmitApplication() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ id, version, idempotencyKey }: { id: string; version: number; idempotencyKey: string }) =>
            unwrap(
                await api.POST('/applications/{applicationId}/submit', {
                    params: { path: { applicationId: id }, header: { 'If-Match': String(version), 'Idempotency-Key': idempotencyKey } },
                }),
            ),
        onSuccess: (status) => {
            queryClient.setQueryData(queryKeys.applicationStatus(status.id), status);
            queryClient.invalidateQueries({ queryKey: ['applications'] });
        },
    });
}

const RECOGNIZING = new Set(['PENDING', 'PROCESSING']);
const isRecognizing = (doc: Document) => RECOGNIZING.has(doc.recognition?.state ?? '');

/** Документы заявки. Пока что-то распознаётся — перечитываем (WS-события document.recognized пока нет). */
export function useDocuments(applicationId: string | undefined) {
    return useQuery({
        queryKey: queryKeys.documents(applicationId ?? ''),
        queryFn: async () =>
            unwrap(await api.GET('/applications/{applicationId}/documents', { params: { path: { applicationId: applicationId ?? '' } } }))
                .items,
        enabled: Boolean(applicationId),
        refetchInterval: (query) => (query.state.data?.some(isRecognizing) ? 1500 : false),
    });
}

export function useUploadDocument(applicationId: string) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ file, type }: { file: File; type: DocumentType }) => {
            const form = new FormData();
            form.append('file', file);
            form.append('type', type);
            return unwrap(
                await api.POST('/applications/{applicationId}/documents', {
                    params: { path: { applicationId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
                    // multipart собираем сами; тип поля file в схеме — строка (format: binary)
                    body: { file: '', type },
                    bodySerializer: () => form,
                }),
            );
        },
        onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.documents(applicationId) }),
    });
}

export function useDeleteDocument(applicationId: string) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (documentId: string) =>
            unwrap(
                await api.DELETE('/applications/{applicationId}/documents/{documentId}', {
                    params: { path: { applicationId, documentId } },
                }),
            ),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.documents(applicationId) }),
    });
}
