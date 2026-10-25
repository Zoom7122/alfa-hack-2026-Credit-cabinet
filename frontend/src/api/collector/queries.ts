import { type QueryClient, useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';

import { ApiError, unwrap } from '../client';
import { collector } from './client';
import { forgetApplication, indexedFor, rememberApplication, useCollectorIndex } from './localIndex';
import type { CollectionTask, CollectorApplication, QuestionnaireInput, RegisterInput } from './types';

export const collectorKeys = {
    application: (id: string) => ['collector', 'application', id] as const,
    collection: (id: string) => ['collector', 'collection', id] as const,
};

const POLL_MS = 2_000;
const IN_PROGRESS = new Set(['SUBMITTED', 'ENRICHING']);
/** Сбор идёт минуты; дольше — значит, завис: дальше не опрашиваем. */
const MAX_POLL_MS = 30 * 60_000;

/**
 * Опрашивать ли заявку. Если сбор не удался, data-collector оставляет заявку в ENRICHING навсегда (задача — FAILED),
 * поэтому смотрим ещё и на задачу, если она уже в кэше.
 */
function shouldPoll(app: CollectorApplication | undefined, queryClient: QueryClient) {
    if (!app || !IN_PROGRESS.has(app.status ?? '')) return false;
    const task = app.collection_task_id ? queryClient.getQueryData<CollectionTask>(collectorKeys.collection(app.collection_task_id)) : undefined;
    if (task?.status === 'FAILED' || task?.status === 'COMPLETED') return task.status === 'COMPLETED' && app.status !== 'VERIFYING';
    return !app.submitted_at || Date.now() - new Date(app.submitted_at).getTime() < MAX_POLL_MS;
}

/** Регистрация заявителя: пользователь, компания и черновик (или уже существующий черновик по этой паре). */
export function useRegister(userId: string) {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (body: RegisterInput) => unwrap(await collector.POST('/applicants', { body })),
        onSuccess: (app, body) => {
            queryClient.setQueryData(collectorKeys.application(app.id ?? ''), app);
            rememberApplication(userId, {
                id: app.id ?? '',
                number: app.number,
                company_inn: body.company.inn,
                company_name: app.applicant?.name ?? body.company.name,
                created_at: app.created_at ?? new Date().toISOString(),
            });
        },
    });
}

const getApplication = async (id: string) => unwrap(await collector.GET('/applications/{id}', { params: { path: { id } } }));

/**
 * Открытый черновик пользователя по компании. Без ИНН физлица data-collector на каждую регистрацию
 * заводит нового пользователя и новый черновик, а удалить черновик нельзя — поэтому сначала ищем свой.
 */
export function useFindDraft(userId: string) {
    const queryClient = useQueryClient();
    return async (companyInn: string) => {
        for (const entry of indexedFor(userId, companyInn)) {
            const app = await queryClient
                .fetchQuery({ queryKey: collectorKeys.application(entry.id), queryFn: () => getApplication(entry.id) })
                .catch(() => undefined);
            if (app?.status === 'DRAFT') return app;
        }
        return undefined;
    };
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

/** Заявки пользователя по локальному индексу — каждая своим GET. */
export function useCollectorApplications(userId: string) {
    const index = useCollectorIndex(userId);
    const queryClient = useQueryClient();
    return useQueries({
        queries: index.map((entry) => ({
            queryKey: collectorKeys.application(entry.id),
            queryFn: async () => {
                try {
                    return unwrap(await collector.GET('/applications/{id}', { params: { path: { id: entry.id } } }));
                } catch (e) {
                    // заявки в сервисе больше нет — убираем её из локального списка
                    if (e instanceof ApiError && e.status === 404) forgetApplication(userId, entry.id);
                    throw e;
                }
            },
            refetchInterval: (query: { state: { data?: CollectorApplication } }) => (shouldPoll(query.state.data, queryClient) ? POLL_MS : false),
        })),
        combine: (results) => ({
            items: results.flatMap((r) => (r.data ? [r.data] : [])),
            isPending: results.some((r) => r.isPending),
            isError: results.some((r) => r.isError),
        }),
    });
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
