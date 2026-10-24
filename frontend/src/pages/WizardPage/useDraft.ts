import { useCallback, useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { ApiError } from '../../api/client';
import { type DraftPatch, patchApplication, queryKeys } from '../../api/queries';
import type { Application } from '../../api/types';
import { combinePatches, mergePatch } from '../../shared/lib/mergePatch';
import { readSession, writeSession } from '../../shared/lib/sessionState';

const AUTOSAVE_DELAY_MS = 800;

export type DraftStatus = 'idle' | 'pending' | 'saving' | 'saved' | 'error';

/** Поле, которое сервер не принял (422): его убрали из очереди, остальное сохраняется. */
export type RejectedField = { field: string; message: string };

export type Draft = {
    /** Запомнить изменение: сразу видно в кэше заявки, на сервер уходит с задержкой. */
    update: (patch: DraftPatch) => void;
    /** Сохранить всё накопленное и дождаться ответа. Отклоняется, если сохранить не удалось. */
    flush: () => Promise<void>;
    status: DraftStatus;
    savedAt?: string;
    error?: unknown;
    rejected: RejectedField[];
    /** Заявку меняли в другой вкладке — наши правки легли поверх. */
    conflict: boolean;
};

const isEmpty = (patch: object) => Object.keys(patch).length === 0;
const pendingKey = (id: string) => `credit-cabinet:draft-pending:${id}`;

/** Что не успели сохранить в прошлый раз — шаги должны сразу показать это, а не серверную версию. */
export const unsavedLeftover = (id: string) => readSession<DraftPatch>(pendingKey(id));

/** Убрать из патча поля, которые отклонил сервер: «guarantors[0].inn» → весь guarantors, «financials.x» → только x. */
function withoutFields(patch: DraftPatch, fields: string[]): DraftPatch {
    const result: Record<string, unknown> = structuredClone(patch);
    for (const field of fields) {
        const [top = '', ...rest] = field.replace(/\[\d+\].*$/, '').split('.');
        const nested = result[top];
        if (rest.length && nested && typeof nested === 'object' && !Array.isArray(nested)) {
            delete (nested as Record<string, unknown>)[rest.join('.')];
            if (isEmpty(nested)) delete result[top];
        } else {
            delete result[top];
        }
    }
    return result as DraftPatch;
}

/**
 * Автосохранение черновика через PATCH (JSON Merge Patch) с If-Match.
 * Запросы идут строго по очереди, чтобы версия в If-Match всегда была свежей.
 * Неотправленное лежит в sessionStorage: не теряется при уходе со страницы, ошибке сети или перезагрузке.
 */
export function useDraft(app: Application): Draft {
    const queryClient = useQueryClient();
    const id = app.id;
    const pending = useRef<DraftPatch>({});
    /** Тела, которые уже в очереди или в полёте. */
    const queued = useRef<DraftPatch[]>([]);
    const queue = useRef<Promise<void>>(Promise.resolve());
    const lastError = useRef<unknown>(undefined);
    const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
    const [status, setStatus] = useState<DraftStatus>('idle');
    const [savedAt, setSavedAt] = useState<string>();
    const [error, setError] = useState<unknown>();
    const [rejected, setRejected] = useState<RejectedField[]>([]);
    const [conflict, setConflict] = useState(false);

    const unsaved = useCallback(() => [...queued.current, pending.current].reduce(combinePatches, {}), []);
    const persistUnsaved = useCallback(() => {
        const all = unsaved();
        writeSession(pendingKey(id), isEmpty(all) ? undefined : all);
    }, [id, unsaved]);

    /** Ответ сервера + всё, что ещё не сохранено, — так кэш не мигает старыми значениями. */
    const showInCache = useCallback(
        (server: Application) => queryClient.setQueryData(queryKeys.application(id), mergePatch(server, unsaved())),
        [id, queryClient, unsaved],
    );

    const send = useCallback(
        async (body: DraftPatch, keepalive: boolean): Promise<Application> => {
            const key = queryKeys.application(id);
            for (let attempt = 0; ; attempt++) {
                const version = queryClient.getQueryData<Application>(key)?.version;
                if (version === undefined) throw new Error('Заявка ещё не загружена');
                try {
                    return await patchApplication(id, version, body, { keepalive });
                } catch (e) {
                    if (attempt > 0 || !(e instanceof ApiError && e.status === 412)) throw e;
                    // кто-то сохранил раньше нас: берём свежую версию и кладём свои правки поверх
                    await queryClient.refetchQueries({ queryKey: key, exact: true });
                    const fresh = queryClient.getQueryData<Application>(key);
                    if (fresh) showInCache(fresh);
                    setConflict(true);
                }
            }
        },
        [id, queryClient, showInCache],
    );

    const enqueue = useCallback(
        (keepalive = false) => {
            const body = pending.current;
            pending.current = {};
            queued.current.push(body);
            setStatus('saving');
            queue.current = queue.current.then(async () => {
                try {
                    const saved = await send(body, keepalive);
                    queued.current = queued.current.filter((b) => b !== body);
                    lastError.current = undefined;
                    showInCache(saved);
                    setSavedAt(saved.updated_at);
                    setError(undefined);
                    setStatus(isEmpty(unsaved()) ? 'saved' : 'pending');
                } catch (e) {
                    queued.current = queued.current.filter((b) => b !== body);
                    lastError.current = e;
                    setError(e);
                    setStatus('error');
                    if (e instanceof ApiError && e.status === 422) {
                        // отклонённое выкидываем, иначе каждый следующий PATCH упирался бы в ту же ошибку
                        const fields = (e.problem?.errors ?? []).map((x) => x.field);
                        setRejected(e.problem?.errors?.map(({ field, message }) => ({ field, message })) ?? []);
                        const rest = fields.length ? withoutFields(body, fields) : {};
                        pending.current = combinePatches(rest, pending.current);
                        // кэш — к серверному состоянию плюс то, что ещё отправим
                        await queryClient.refetchQueries({ queryKey: queryKeys.application(id), exact: true }).catch(() => undefined);
                        const fresh = queryClient.getQueryData<Application>(queryKeys.application(id));
                        if (fresh) showInCache(fresh);
                    } else if (e instanceof ApiError && e.status === 409) {
                        // черновик уже не черновик (отправили из другой вкладки) — перечитываем, страница уведёт на статус
                        pending.current = {};
                        void queryClient.invalidateQueries({ queryKey: queryKeys.application(id), exact: true });
                    } else {
                        // сеть или 5xx — вернём в очередь, «Повторить» отправит снова
                        pending.current = combinePatches(body, pending.current);
                    }
                }
                persistUnsaved();
            });
        },
        [id, persistUnsaved, queryClient, send, showInCache, unsaved],
    );

    const flush = useCallback(async () => {
        clearTimeout(timer.current);
        const hadPending = !isEmpty(pending.current);
        // отклонённые поля уже выброшены: повторное нажатие после 422 не должно упираться в ту же ошибку
        if (!hadPending && lastError.current instanceof ApiError && lastError.current.status < 500) lastError.current = undefined;
        if (hadPending) enqueue();
        await queue.current;
        if (lastError.current) throw lastError.current;
        setRejected([]);
    }, [enqueue]);

    const update = useCallback(
        (patch: DraftPatch) => {
            pending.current = combinePatches(pending.current, patch);
            persistUnsaved();
            queryClient.setQueryData<Application>(queryKeys.application(id), (prev) => prev && mergePatch(prev, patch));
            setStatus('pending');
            clearTimeout(timer.current);
            timer.current = setTimeout(() => void flush().catch(() => undefined), AUTOSAVE_DELAY_MS);
        },
        [flush, id, persistUnsaved, queryClient],
    );

    // то, что не успели сохранить в прошлый раз (ушли со страницы, упала сеть), — докидываем при открытии
    useEffect(() => {
        const leftover = readSession<DraftPatch>(pendingKey(id));
        if (!leftover || isEmpty(leftover)) return;
        const t = setTimeout(() => update(leftover));
        return () => clearTimeout(t);
    }, [id, update]);

    useEffect(() => {
        const onBeforeUnload = (e: BeforeUnloadEvent) => {
            if (isEmpty(unsaved())) return;
            clearTimeout(timer.current);
            if (!isEmpty(pending.current)) enqueue(true);
            e.preventDefault();
        };
        window.addEventListener('beforeunload', onBeforeUnload);
        return () => {
            window.removeEventListener('beforeunload', onBeforeUnload);
            // ушли внутри приложения: дописываем; если не выйдет — останется в sessionStorage до следующего открытия
            void flush().catch(() => undefined);
        };
    }, [enqueue, flush, unsaved]);

    return { update, flush, status, savedAt, error, rejected, conflict };
}
