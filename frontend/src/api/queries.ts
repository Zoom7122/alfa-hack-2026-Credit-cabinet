import { keepPreviousData, useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';

import { API_MOCK_ENABLED } from '../config';
import { newIdempotencyKey } from '../shared/lib/idempotency';
import { isCollectorId } from '../shared/lib/collector';
import { api, ApiError, isUnauthorized, unwrap } from './client';
import { collector } from './collector/client';
import { toBoDossier, toBoListItem } from './collector/backoffice';
import { toProduct } from './collector/catalog';
import { session } from './session';
import { documentRequest, loadDocumentResult, type StoredDocument } from './documents';
import type {
    ApplicantType,
    Application,
    ApplicationInput,
    ApplicationStatus,
    ApplicationStatusView,
    ApplicationSummary,
    DeclineReason,
    Document,
    InfoRequestAnswerInput,
    Me,
    DocumentType,
    PurposeCode,
    RepresentativeRole,
    RequestedTerms,
    SigningMethod,
    BoApplicationListItem,
    BoDecisionInput,
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
    timeline: (id: string) => ['applications', 'timeline', id] as const,
    infoRequests: (id: string) => ['applications', 'info-requests', id] as const,
    offers: (id: string) => ['applications', 'offers', id] as const,
    decision: (id: string) => ['applications', 'decision', id] as const,
    signing: (id: string) => ['applications', 'signing', id] as const,
    signatureReport: (id: string, documentId: string, verifiedAt?: string | null) =>
        ['applications', 'signature-report', id, documentId, verifiedAt ?? null] as const,
    boQueue: (params: { assignee?: string }) => ['backoffice', 'queue', params] as const,
    boDossier: (id: string) => ['backoffice', 'dossier', id] as const,
    /** Всё по одной заявке — для инвалидации по WS-событию. */
    anyOf: (id: string) => (query: { queryKey: readonly unknown[] }) => query.queryKey[0] === 'applications' && query.queryKey[2] === id,
};

export function useTestUsers() {
    return useQuery({
        queryKey: queryKeys.testUsers,
        // вход — у data-collector (живой или его мок): те же токены `mock.<user_id>`, что у остальных моков
        queryFn: async () => unwrap(await collector.GET('/auth/test-users')).items,
        staleTime: Infinity,
    });
}

export function useMockLogin() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (userId: string) =>
            unwrap(await collector.POST('/auth/mock-login', { body: { user_id: userId } })),
        onSuccess: ({ token, user }) => {
            // сначала профиль в кэш, потом токен: подписчики токена сразу видят готовый /me
            queryClient.setQueryData(queryKeys.me, user);
            session.setToken(token);
            // ответ входа — только data-collector; подписи и id компаний основного API — после перечитывания
            void queryClient.invalidateQueries({ queryKey: queryKeys.me });
        },
    });
}

/**
 * Профиль и новые подписания — у data-collector. Основной API (моки по openapi.yaml) добавляет
 * старые демо-подписания для колокольчика и id компаний, на которые завязаны его экраны
 * (совпадение по ИНН). При выключенных демо-моках основной API не запрашиваем.
 */
async function loadMe(): Promise<Me> {
    const [live, legacy] = await Promise.all([
        collector.GET('/me').then(unwrap),
        API_MOCK_ENABLED
            ? api
                  .GET('/me')
                  .then(unwrap)
                  .catch(() => undefined)
            : undefined,
    ]);
    const legacyByInn = new Map((legacy?.companies ?? []).map((c) => [c.inn, c]));
    const companies = live.companies.map((c) => ({ ...c, applicant_id: legacyByInn.get(c.inn)?.applicant_id ?? c.applicant_id }));
    // компании, привязанные в основном API (экран «Добавить компанию»), — тоже в профиле
    for (const c of legacy?.companies ?? []) if (!companies.some((x) => x.inn === c.inn)) companies.push(c);
    return { ...live, companies, pending_signatures: [...new Set([...(live.pending_signatures ?? []), ...(legacy?.pending_signatures ?? [])])] };
}

export function useMe(enabled = true) {
    return useQuery({
        queryKey: queryKeys.me,
        queryFn: async () => {
            try {
                return await loadMe();
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
            unwrap(await collector.GET('/products', { params: { query: { purpose: params.purpose, applicant_type: params.applicantType } } })).items?.map(
                toProduct,
            ) ?? [],
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
                await collector.POST('/products/{code}/calculate', {
                    params: { path: { code: code ?? '' } },
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
        enabled: API_MOCK_ENABLED,
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
        enabled: API_MOCK_ENABLED && Boolean(params.applicantId),
    });
}

/** Лёгкие статусы заявок (вопросы банка, кто подписывает), собранные в Map по id заявки. */
export function useApplicationStatuses(applications: ApplicationSummary[]) {
    return useQueries({
        queries: (API_MOCK_ENABLED ? applications : []).map((app) => ({
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
/** Распознавание или проверка подписи ещё идут. */
const isRecognizing = (doc: Document) => RECOGNIZING.has(doc.recognition?.state ?? '') || doc.signatures?.state === 'PENDING';

/** Документы заявки. Пока что-то распознаётся — перечитываем (WS-события document.recognized пока нет). */
export function useDocuments(applicationId: string | undefined) {
    return useQuery({
        queryKey: queryKeys.documents(applicationId ?? ''),
        queryFn: async () => {
            const id = applicationId ?? '';
            const { items } = await documentRequest<{ items: StoredDocument[] }>(id);
            return Promise.all(items.map((doc) => loadDocumentResult(id, doc)));
        },
        enabled: Boolean(applicationId),
        refetchInterval: (query) => (query.state.data?.some(isRecognizing) ? 1500 : false),
    });
}

export function useUploadDocument(applicationId: string) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({
            file,
            type,
            infoRequestId,
            signedDocumentId,
        }: {
            file: File;
            type: DocumentType;
            infoRequestId?: string;
            /** Для type=SIGNATURE: какой документ подписан (откреплённая .sig/.p7s). */
            signedDocumentId?: string;
        }) => {
            const form = new FormData();
            form.append('file', file);
            form.append('type', type);
            if (infoRequestId) form.append('info_request_id', infoRequestId);
            if (signedDocumentId) form.append('signed_document_id', signedDocumentId);
            if (isCollectorId(applicationId)) return documentRequest<Document>(applicationId, '', { method: 'POST', body: form });
            return unwrap(
                await api.POST('/applications/{applicationId}/documents', {
                    params: { path: { applicationId }, header: { 'Idempotency-Key': newIdempotencyKey() } },
                    // multipart собираем сами; тип поля file в схеме — строка (format: binary)
                    body: { file: '', type, info_request_id: infoRequestId, signed_document_id: signedDocumentId },
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
        mutationFn: async (documentId: string) => {
            if (isCollectorId(applicationId)) return documentRequest<void>(applicationId, `/${encodeURIComponent(documentId)}`, { method: 'DELETE' });
            return unwrap(
                await api.DELETE('/applications/{applicationId}/documents/{documentId}', {
                    params: { path: { applicationId, documentId } },
                }),
            );
        },
        onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.documents(applicationId) }),
    });
}

export function useReverifyDocument(applicationId: string) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (documentId: string) => documentRequest(applicationId, `/${encodeURIComponent(documentId)}/signature-verification`, { method: 'POST' }),
        onSuccess: async () => {
            await queryClient.invalidateQueries({ predicate: queryKeys.anyOf(applicationId) });
            await queryClient.invalidateQueries({ queryKey: ['collector', 'application', applicationId] });
        },
    });
}

const path = (applicationId: string) => ({ params: { path: { applicationId } } });

/** Лёгкий статус для UI. Перечитывается по WS-событию application.status_changed. */
export function useApplicationStatus(id: string | undefined) {
    return useQuery({
        queryKey: queryKeys.applicationStatus(id ?? ''),
        queryFn: async () => unwrap(await api.GET('/applications/{applicationId}/status', path(id ?? ''))),
        enabled: Boolean(id),
    });
}

export function useTimeline(id: string | undefined) {
    return useQuery({
        queryKey: queryKeys.timeline(id ?? ''),
        queryFn: async () => unwrap(await api.GET('/applications/{applicationId}/timeline', path(id ?? ''))).items,
        enabled: Boolean(id),
    });
}

export function useInfoRequests(id: string | undefined, enabled = true) {
    return useQuery({
        queryKey: queryKeys.infoRequests(id ?? ''),
        queryFn: async () => unwrap(await api.GET('/applications/{applicationId}/info-requests', path(id ?? ''))).items,
        enabled: Boolean(id) && enabled,
    });
}

export function useOffers(id: string | undefined, enabled = true) {
    return useQuery({
        queryKey: queryKeys.offers(id ?? ''),
        queryFn: async () => unwrap(await api.GET('/applications/{applicationId}/offers', path(id ?? ''))).items,
        enabled: Boolean(id) && enabled,
    });
}

/** После действия кладём свежий статус в кэш и перечитываем остальное по заявке. */
function useApplyStatus() {
    const queryClient = useQueryClient();
    return (status: ApplicationStatusView) => {
        queryClient.setQueryData(queryKeys.applicationStatus(status.id), status);
        void queryClient.invalidateQueries({
            predicate: (q) => q.queryKey[0] === 'applications' && (q.queryKey[1] === 'list' || (q.queryKey[2] === status.id && q.queryKey[1] !== 'status')),
        });
    };
}

/** Статус ушёл вперёд (409), версия устарела (412), предложение истекло (410) — без перечитывания повтор упрётся в ту же ошибку. */
const STALE = new Set([409, 410, 412]);

function useRefreshOnStale(applicationId: string) {
    const queryClient = useQueryClient();
    return (error: Error) => {
        if (error instanceof ApiError && STALE.has(error.status)) {
            void queryClient.invalidateQueries({ predicate: queryKeys.anyOf(applicationId) });
        }
    };
}

export function useAnswerInfoRequest(applicationId: string) {
    const applyStatus = useApplyStatus();
    const refreshOnStale = useRefreshOnStale(applicationId);
    return useMutation({
        mutationFn: async ({ infoRequestId, idempotencyKey, ...body }: InfoRequestAnswerInput & { infoRequestId: string; idempotencyKey: string }) =>
            unwrap(
                await api.POST('/applications/{applicationId}/info-requests/{infoRequestId}/answer', {
                    params: { path: { applicationId, infoRequestId }, header: { 'Idempotency-Key': idempotencyKey } },
                    body,
                }),
            ),
        onSuccess: ({ application_status }) => applyStatus(application_status),
        onError: refreshOnStale,
    });
}

export function useAcceptOffer(applicationId: string) {
    const applyStatus = useApplyStatus();
    const refreshOnStale = useRefreshOnStale(applicationId);
    return useMutation({
        mutationFn: async ({ offerId, version, idempotencyKey }: { offerId: string; version: number; idempotencyKey: string }) =>
            unwrap(
                await api.POST('/applications/{applicationId}/offers/{offerId}/accept', {
                    params: {
                        path: { applicationId, offerId },
                        header: { 'If-Match': String(version), 'Idempotency-Key': idempotencyKey },
                    },
                }),
            ),
        onSuccess: applyStatus,
        onError: refreshOnStale,
    });
}

export function useDeclineOffers(applicationId: string) {
    const applyStatus = useApplyStatus();
    const refreshOnStale = useRefreshOnStale(applicationId);
    return useMutation({
        mutationFn: async ({
            reason,
            comment,
            version,
            idempotencyKey,
        }: {
            reason: DeclineReason;
            comment?: string;
            version: number;
            idempotencyKey: string;
        }) =>
            unwrap(
                await api.POST('/applications/{applicationId}/offers/decline', {
                    params: { path: { applicationId }, header: { 'If-Match': String(version), 'Idempotency-Key': idempotencyKey } },
                    body: { reason, comment: comment || undefined },
                }),
            ),
        onSuccess: ({ application_status }) => applyStatus(application_status),
        onError: refreshOnStale,
    });
}

export function useCancelApplication(applicationId: string) {
    const applyStatus = useApplyStatus();
    const refreshOnStale = useRefreshOnStale(applicationId);
    return useMutation({
        mutationFn: async ({ comment, idempotencyKey }: { comment?: string; idempotencyKey: string }) =>
            unwrap(
                await api.POST('/applications/{applicationId}/cancel', {
                    params: { path: { applicationId }, header: { 'Idempotency-Key': idempotencyKey } },
                    body: { comment: comment || undefined },
                }),
            ),
        onSuccess: applyStatus,
        onError: refreshOnStale,
    });
}

/** Сессия подписания: договор, график, демо-код (только подписанту), попытки, выдача. */
export function useSigning(id: string | undefined, enabled = true) {
    return useQuery({
        queryKey: queryKeys.signing(id ?? ''),
        queryFn: async () => unwrap(await api.GET('/applications/{applicationId}/signing', path(id ?? ''))),
        enabled: Boolean(id) && enabled,
    });
}

/** Сформировать договор / выбрать способ подписи; не-директору — «напомнить директору». */
export function useStartSigning(applicationId: string) {
    const queryClient = useQueryClient();
    const refreshOnStale = useRefreshOnStale(applicationId);
    return useMutation({
        mutationFn: async ({ method, idempotencyKey }: { method?: SigningMethod; idempotencyKey: string }) =>
            unwrap(
                await api.POST('/applications/{applicationId}/signing/start', {
                    params: { path: { applicationId }, header: { 'Idempotency-Key': idempotencyKey } },
                    body: { method },
                }),
            ),
        onSuccess: (session) => queryClient.setQueryData(queryKeys.signing(applicationId), session),
        onError: refreshOnStale,
    });
}

export type ConfirmSigningBody =
    | { method: 'SIMPLE_CODE'; code: string }
    | { method: 'QUALIFIED_SIGNATURE'; signature_document_id: string };

/** Подписать (только директор). Успех — заявка COMPLETED, выдача имитируется. */
export function useConfirmSigning(applicationId: string) {
    const queryClient = useQueryClient();
    const refreshOnStale = useRefreshOnStale(applicationId);
    return useMutation({
        mutationFn: async ({ idempotencyKey, ...body }: ConfirmSigningBody & { idempotencyKey: string }) =>
            unwrap(
                await api.POST('/applications/{applicationId}/signing/confirm', {
                    params: { path: { applicationId }, header: { 'Idempotency-Key': idempotencyKey } },
                    body,
                }),
            ),
        onSuccess: (session) => {
            queryClient.setQueryData(queryKeys.signing(applicationId), session);
            void queryClient.invalidateQueries({ predicate: queryKeys.anyOf(applicationId) });
            void queryClient.invalidateQueries({ queryKey: ['applications', 'list'] });
            void queryClient.invalidateQueries({ queryKey: queryKeys.me });
        },
        onError: (error) => {
            refreshOnStale(error);
            // неверный код — попыток стало меньше, а после последней заявка вернулась к предложениям
            if (error instanceof ApiError && error.status === 422) {
                void queryClient.invalidateQueries({ predicate: queryKeys.anyOf(applicationId) });
                void queryClient.invalidateQueries({ queryKey: queryKeys.me });
            }
        },
    });
}

/** Скачать файл документа (с токеном — обычной ссылкой не получится). Ссылку на blob отзываем сразу после клика. */
export async function downloadDocument(applicationId: string, doc: Document) {
    const { data, error, response } = await api.GET('/applications/{applicationId}/documents/{documentId}/content', {
        params: { path: { applicationId, documentId: doc.id } },
        parseAs: 'blob',
    });
    if (error !== undefined || !response.ok || !data) throw new ApiError(response.status, 'Не удалось скачать документ');
    const href = URL.createObjectURL(data);
    const link = window.document.createElement('a');
    link.href = href;
    link.download = doc.file_name;
    link.click();
    setTimeout(() => URL.revokeObjectURL(href), 1000);
}

/** Решение банка с объяснением: причины без баллов и источников, рекомендации, когда можно подать снова. */
export function useDecision(id: string | undefined) {
    return useQuery({
        queryKey: queryKeys.decision(id ?? ''),
        queryFn: async () => unwrap(await api.GET('/applications/{applicationId}/decision', path(id ?? ''))),
        enabled: Boolean(id),
    });
}

/**
 * Полный отчёт проверки ЭП документа (404 — не подписан или проверка ещё идёт).
 * Время проверки — часть ключа: после перепроверки отчёт перечитается, а не возьмётся из кэша.
 */
export function useSignatureReport(applicationId: string, documentId: string | undefined, verifiedAt?: string | null) {
    return useQuery({
        queryKey: queryKeys.signatureReport(applicationId, documentId ?? '', verifiedAt),
        queryFn: async () =>
            unwrap(
                await api.GET('/applications/{applicationId}/documents/{documentId}/signature-report', {
                    params: { path: { applicationId, documentId: documentId ?? '' } },
                }),
            ),
        enabled: Boolean(documentId),
        staleTime: 60_000,
    });
}

// ---------------------------------------------------------------- рабочее место банка (API data-collector)

const QUEUE_PAGES = 20;

/** Очередь андеррайтера целиком (статистика и группы считаются по ней). assignee: me | none | user_id. */
export function useBoQueue(params: { assignee?: string } = {}) {
    return useQuery({
        queryKey: queryKeys.boQueue(params),
        queryFn: async () => {
            const items: BoApplicationListItem[] = [];
            let cursor: string | undefined;
            for (let page = 0; page < QUEUE_PAGES; page += 1) {
                const res = unwrap(
                    await collector.GET('/backoffice/applications', { params: { query: { assignee: params.assignee, limit: 100, cursor } } }),
                );
                items.push(...(res.items ?? []).map(toBoListItem));
                if (!res.next_cursor) break;
                cursor = res.next_cursor;
            }
            return items;
        },
        placeholderData: keepPreviousData,
        // SLA и «ждут клиента» меняются со временем; событий у data-collector нет — опрашиваем
        refetchInterval: 30_000,
    });
}

/** Досье заявки: с исполнителем и сроком ручного рассмотрения. Пока заявку оценивают — опрашиваем. */
export function useBoDossier(id: string | undefined) {
    return useQuery({
        queryKey: queryKeys.boDossier(id ?? ''),
        queryFn: async () =>
            toBoDossier(unwrap(await collector.GET('/backoffice/applications/{id}', { params: { path: { id: id ?? '' } } }))),
        enabled: Boolean(id),
        refetchInterval: (query) => (['VERIFYING', 'SCORING'].includes(query.state.data?.application.status ?? '') ? 2_000 : 30_000),
    });
}

export function useBoAssign() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (applicationId: string) =>
            toBoListItem(unwrap(await collector.POST('/backoffice/applications/{id}/assign', { params: { path: { id: applicationId } } }))),
        onSettled: () => void queryClient.invalidateQueries({ queryKey: ['backoffice'] }),
    });
}

export function useBoDecide(applicationId: string) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ version, idempotencyKey, ...body }: BoDecisionInput & { version: number; idempotencyKey: string }) =>
            toBoDossier(
                unwrap(
                    await collector.POST('/backoffice/applications/{id}/decision', {
                        params: { path: { id: applicationId }, header: { 'If-Match': String(version), 'Idempotency-Key': idempotencyKey } },
                        body: body as never,
                    }),
                ),
            ),
        onSuccess: (dossier) => {
            queryClient.setQueryData(queryKeys.boDossier(applicationId), dossier);
            void queryClient.invalidateQueries({ queryKey: ['backoffice', 'queue'] });
        },
        // заявку изменили (412) или её уже решили (409) — перечитываем досье
        onError: (error) => {
            if (error instanceof ApiError && STALE.has(error.status)) {
                void queryClient.invalidateQueries({ queryKey: queryKeys.boDossier(applicationId) });
            }
        },
    });
}
