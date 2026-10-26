import { type QueryClient, useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';

import type { SignatureReportJson } from './signatureReport';

import { ApiError, unwrap } from '../client';
import { collector } from './client';
import type { CollectionTask, CollectorApplication, QuestionnaireInput, RegisterInput } from './types';

export const collectorKeys = {
    application: (id: string) => ['collector', 'application', id] as const,
    collection: (id: string) => ['collector', 'collection', id] as const,
    applications: ['collector', 'applications'] as const,
    applicants: ['collector', 'applicants'] as const,
    timeline: (id: string) => ['collector', 'timeline', id] as const,
    signatureReport: (appId: string, docId: string, state: string) => ['collector', 'signature-report', appId, docId, state] as const,
    lookup: (inn: string) => ['collector', 'lookup', inn] as const,
    decision: (id: string) => ['collector', 'decision', id] as const,
    offers: (id: string) => ['collector', 'offers', id] as const,
    infoRequests: (id: string) => ['collector', 'info-requests', id] as const,
};

const POLL_MS = 2_000;
const IN_PROGRESS = new Set(['SUBMITTED', 'ENRICHING']);
/** После сбора заявку оценивает банк: VERIFYING → SCORING → итог. */
const SCORING = new Set(['VERIFYING', 'SCORING']);
/** Сбор идёт минуты; дольше — значит, завис: дальше не опрашиваем. */
const MAX_POLL_MS = 30 * 60_000;

/**
 * Опрашивать ли заявку. Если сбор не удался, data-collector оставляет заявку в ENRICHING навсегда (задача — FAILED),
 * поэтому смотрим ещё и на задачу, если она уже в кэше.
 */
function shouldPoll(app: CollectorApplication | undefined, queryClient: QueryClient) {
    if (!app) return false;
    const recent = !app.submitted_at || Date.now() - new Date(app.submitted_at).getTime() < MAX_POLL_MS;
    // после сбора ждём оценку банка; подписи worker проверяет и в итоговых статусах
    if (SCORING.has(app.status ?? '')) return recent;
    if (!IN_PROGRESS.has(app.status ?? '')) return recent && Boolean(app.documents?.some((d) => d.signature_state === 'PENDING'));
    const task = app.collection_task_id ? queryClient.getQueryData<CollectionTask>(collectorKeys.collection(app.collection_task_id)) : undefined;
    if (task?.status === 'FAILED' || task?.status === 'COMPLETED') return task.status === 'COMPLETED';
    return recent;
}

/** Регистрация заявителя от имени вошедшего: компания и черновик (или его прежний черновик по этой компании). */
export function useRegister() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (body: RegisterInput) => unwrap(await collector.POST('/applicants', { body })),
        onSuccess: (app) => {
            queryClient.setQueryData(collectorKeys.application(app.id ?? ''), app);
            // новая компания в профиле, новая заявка в списке
            void queryClient.invalidateQueries({ queryKey: collectorKeys.applications });
            void queryClient.invalidateQueries({ queryKey: collectorKeys.applicants });
            void queryClient.invalidateQueries({ queryKey: ['me'] });
        },
    });
}

/** Статус заявки; пока идёт сбор (SUBMITTED, ENRICHING) — опрашиваем: событий у data-collector нет. */
export function useCollectorApplication(id: string | undefined) {
    const queryClient = useQueryClient();
    return useQuery({
        queryKey: collectorKeys.application(id ?? ''),
        queryFn: async () => unwrap(await collector.GET('/applications/{id}', { params: { path: { id: id ?? '' } } })),
        enabled: Boolean(id),
        refetchInterval: (query) => (shouldPoll(query.state.data, queryClient) ? POLL_MS : false),
    });
}

export function useCollection(taskId: string | undefined) {
    return useQuery({
        // задача завершилась — больше не меняется
        staleTime: (query) => (query.state.data?.status === 'COMPLETED' || query.state.data?.status === 'FAILED' ? Infinity : 0),
        queryKey: collectorKeys.collection(taskId ?? ''),
        queryFn: async () => unwrap(await collector.GET('/collections/{id}', { params: { path: { id: taskId ?? '' } } })),
        enabled: Boolean(taskId),
        refetchInterval: (query) => {
            const status = query.state.data?.status;
            return status === 'COMPLETED' || status === 'FAILED' ? false : POLL_MS;
        },
    });
}

/** Заявки компаний пользователя (свои и коллег), новые сверху. Пока какая-то собирается — опрашиваем. */
export function useCollectorApplications() {
    const queryClient = useQueryClient();
    return useQuery({
        queryKey: collectorKeys.applications,
        queryFn: async () => unwrap(await collector.GET('/applications')).items,
        refetchInterval: (query) => (query.state.data?.some((a) => shouldPoll(a, queryClient)) ? POLL_MS : false),
    });
}

/** Компании пользователя с реквизитами из ЕГРЮЛ (после сбора). */
export function useCollectorApplicants() {
    return useQuery({
        queryKey: collectorKeys.applicants,
        queryFn: async () => unwrap(await collector.GET('/applicants')).items,
    });
}

/** Журнал заявки; опрашивается, пока опрашивается сама заявка (сбор, проверка подписи). */
export function useCollectorTimeline(app: CollectorApplication | undefined) {
    const queryClient = useQueryClient();
    const id = app?.id ?? '';
    return useQuery({
        queryKey: collectorKeys.timeline(id),
        queryFn: async () => unwrap(await collector.GET('/applications/{id}/timeline', { params: { path: { id } } })).items,
        enabled: Boolean(id),
        refetchInterval: () => (shouldPoll(queryClient.getQueryData(collectorKeys.application(id)), queryClient) ? POLL_MS : false),
    });
}

/** Отчёт о подписи. state — signature_state документа: сменился (проверили) — перечитываем. */
export function useCollectorSignatureReport(applicationId: string, documentId: string | undefined, state: string) {
    return useQuery({
        queryKey: collectorKeys.signatureReport(applicationId, documentId ?? '', state),
        queryFn: async () =>
            unwrap(
                await collector.GET('/applications/{id}/documents/{documentId}/signature-report', {
                    params: { path: { id: applicationId, documentId: documentId ?? '' } },
                }),
            ) as SignatureReportJson,
        enabled: Boolean(documentId),
        retry: false,
        staleTime: Infinity,
    });
}

/** Подсказка реквизитов по ИНН — синхронно из ЕГРЮЛ через data-collector. */
export function useCompanyLookup(inn: string | undefined) {
    return useQuery({
        queryKey: collectorKeys.lookup(inn ?? ''),
        queryFn: async () => unwrap(await collector.POST('/counterparty/lookup', { body: { inn: inn ?? '' } })),
        enabled: Boolean(inn),
        retry: false,
        staleTime: 5 * 60_000,
    });
}

/** Скачать файл документа: с токеном, поэтому через fetch, а не ссылкой. */
export async function downloadCollectorDocument(applicationId: string, documentId: string, fileName: string) {
    const { data, response, error } = await collector.GET('/applications/{id}/documents/{documentId}/content', {
        params: { path: { id: applicationId, documentId } },
        parseAs: 'blob',
    });
    if (error !== undefined || !response.ok || !data) throw new ApiError(response.status, 'Не удалось скачать файл');
    const url = URL.createObjectURL(data);
    const a = document.createElement('a');
    a.href = url;
    a.download = fileName;
    a.click();
    // сразу отзывать нельзя: Safari и Firefox начинают загрузку не мгновенно
    setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

export type SubmitFile = { file: File; name: string };

/**
 * Анкета и файлы одним multipart: часть questionnaire (JSON) и по части files на файл.
 * onDone — анкета принята (или её уже приняли раньше, 409): колбэк хука, сработает и если страницу уже закрыли.
 */
export function useSubmitQuestionnaire(applicationId: string, onDone?: () => void) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ questionnaire, files }: { questionnaire: QuestionnaireInput; files: SubmitFile[] }) => {
            const form = new FormData();
            form.append('questionnaire', JSON.stringify(questionnaire));
            // имя части — имя файла: по нему data-collector связывает документ, его тип и подпись X.sig
            for (const f of files) form.append('files', f.file, f.name);
            return unwrap(
                await collector.POST('/applications/{id}/questionnaire', {
                    params: { path: { id: applicationId } },
                    body: { questionnaire: '' },
                    bodySerializer: () => form,
                }),
            );
        },
        onSuccess: (submitted) => {
            if (submitted.application) queryClient.setQueryData(collectorKeys.application(applicationId), submitted.application);
            onDone?.();
        },
        onError: (error) => {
            if (error instanceof ApiError && error.status === 409) {
                onDone?.();
                void queryClient.invalidateQueries({ queryKey: collectorKeys.application(applicationId) });
            }
        },
    });
}

/** Задачи сбора для заявок, которые ещё «собираются», — чтобы на главной отличить идущий сбор от упавшего. */
export function useCollectionsOf(apps: CollectorApplication[]) {
    const tasks = apps.filter((a) => a.status === 'ENRICHING' && a.collection_task_id).map((a) => a.collection_task_id ?? '');
    return useQueries({
        queries: tasks.map((id) => ({
            queryKey: collectorKeys.collection(id),
            queryFn: async () => unwrap(await collector.GET('/collections/{id}', { params: { path: { id } } })),
            staleTime: 30_000,
        })),
        combine: (results) => new Map(results.flatMap((r) => (r.data?.id ? [[r.data.id, r.data] as const] : []))),
    });
}

/** Итог по заявке для клиента: причины отказа, когда подать снова. 404 — ещё не оценили. */
export function useCollectorDecision(id: string, enabled: boolean) {
    return useQuery({
        queryKey: collectorKeys.decision(id),
        queryFn: async () => unwrap(await collector.GET('/applications/{id}/decision', { params: { path: { id } } })),
        enabled,
        retry: false,
    });
}

/** Предложения банка по заявке. */
export function useCollectorOffers(id: string, enabled: boolean) {
    return useQuery({
        queryKey: collectorKeys.offers(id),
        queryFn: async () => unwrap(await collector.GET('/applications/{id}/offers', { params: { path: { id } } })).items,
        enabled,
    });
}

/** Вопросы банка по заявке. */
export function useCollectorInfoRequests(id: string, enabled: boolean) {
    return useQuery({
        queryKey: collectorKeys.infoRequests(id),
        queryFn: async () => unwrap(await collector.GET('/applications/{id}/info-requests', { params: { path: { id } } })).items,
        enabled,
    });
}

/** Ответ на вопрос банка: значение, комментарий и файлы одним multipart. */
export function useAnswerInfoRequest(applicationId: string) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ requestId, value, comment, files }: { requestId: string; value?: unknown; comment?: string; files: File[] }) => {
            const form = new FormData();
            form.append('answer', JSON.stringify({ value, comment: comment?.trim() || undefined }));
            for (const f of files) form.append('files', f, f.name);
            return unwrap(
                await collector.POST('/applications/{id}/info-requests/{requestId}/answer', {
                    params: { path: { id: applicationId, requestId } },
                    // без body openapi-fetch не вызывает bodySerializer — тело заменит FormData
                    body: {},
                    bodySerializer: () => form,
                }),
            );
        },
        onSuccess: (res) => {
            if (res.info_requests) queryClient.setQueryData(collectorKeys.infoRequests(applicationId), res.info_requests);
            // ответили на всё — заявка уходит дальше: перечитываем статус
            void queryClient.invalidateQueries({ queryKey: collectorKeys.application(applicationId) });
            void queryClient.invalidateQueries({ queryKey: collectorKeys.timeline(applicationId) });
        },
    });
}
