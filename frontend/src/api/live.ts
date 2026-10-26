import { useEffect } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { API_MOCK_ENABLED } from '../config';
import { API_BASE_URL } from './client';
import { queryKeys } from './queries';

/** Конверт события (docs/websocket.md). Событие — подсказка «перечитай», данные берём из REST. */
type LiveEvent = { id?: string; type: string; data?: { application_id?: string; from?: string; to?: string } };

const PING_MS = 25_000;
const MAX_RETRY_MS = 30_000;

function socketUrl(token: string, lastEventId: string | undefined) {
    const url = new URL(`${API_BASE_URL}/ws`, window.location.href);
    url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
    url.searchParams.set('token', token);
    // сервер дошлёт пропущенное из журнала (опционально по docs/websocket.md)
    if (lastEventId) url.searchParams.set('last_event_id', lastEventId);
    return url.toString();
}

/** Один сокет на вкладку: по событиям перечитываем затронутые запросы, после переподключения — всё по заявкам. */
export function useLiveUpdates(token: string | null) {
    const queryClient = useQueryClient();

    useEffect(() => {
        if (!API_MOCK_ENABLED || !token) return;
        let socket: WebSocket | undefined;
        let retry = 0;
        let reconnect: ReturnType<typeof setTimeout> | undefined;
        let ping: ReturnType<typeof setInterval> | undefined;
        let stopped = false;
        let lastEventId: string | undefined;

        const refresh = (id: string | undefined) => {
            if (id) void queryClient.invalidateQueries({ predicate: queryKeys.anyOf(id) });
            void queryClient.invalidateQueries({ queryKey: ['applications', 'list'] });
        };

        const handle = ({ type, data }: LiveEvent) => {
            const id = data?.application_id;
            switch (type) {
                case 'application.status_changed':
                    refresh(id);
                    // вход в подписание или выход из него меняет задачи директора (/me.pending_signatures)
                    if (data?.from === 'SIGNING' || data?.to === 'SIGNING') void queryClient.invalidateQueries({ queryKey: queryKeys.me });
                    break;
                case 'info_request.created':
                case 'offers.issued':
                    refresh(id);
                    break;
                case 'signing.requested':
                case 'signing.completed':
                    refresh(id);
                    // задача «подписать договор» приходит в /me.pending_signatures
                    void queryClient.invalidateQueries({ queryKey: queryKeys.me });
                    break;
                case 'application.updated':
                    if (id) void queryClient.invalidateQueries({ queryKey: queryKeys.application(id) });
                    break;
                case 'document.recognized':
                    if (id) void queryClient.invalidateQueries({ queryKey: queryKeys.documents(id) });
                    break;
                case 'document.signatures_verified':
                    if (id) {
                        void queryClient.invalidateQueries({ queryKey: queryKeys.documents(id) });
                        void queryClient.invalidateQueries({ queryKey: ['applications', 'signature-report', id] });
                    }
                    break;
                case 'backoffice.queue_changed':
                    // андеррайтеру: очередь и открытое досье
                    void queryClient.invalidateQueries({ queryKey: ['backoffice', 'queue'] });
                    if (id) void queryClient.invalidateQueries({ queryKey: queryKeys.boDossier(id) });
                    break;
                default:
                    break;
            }
        };

        const connect = () => {
            socket = new WebSocket(socketUrl(token, lastEventId));
            socket.onopen = () => {
                // событие могло проскочить, пока сокета не было (в том числе между первым REST-ответом и подключением), —
                // перечитываем открытое: источник правды всё равно REST
                void queryClient.invalidateQueries({ queryKey: ['applications'] });
                void queryClient.invalidateQueries({ queryKey: queryKeys.me });
                void queryClient.invalidateQueries({ queryKey: ['backoffice'] });
                retry = 0;
                ping = setInterval(() => socket?.send(JSON.stringify({ type: 'ping', data: {} })), PING_MS);
            };
            socket.onmessage = (e) => {
                try {
                    const event = JSON.parse(String(e.data)) as LiveEvent;
                    if (event.id) lastEventId = event.id;
                    handle(event);
                } catch {
                    /* не JSON — пропускаем */
                }
            };
            socket.onclose = (e) => {
                clearInterval(ping);
                // 4401 — токен не подошёл: переподключаться бессмысленно, разберётся гард по /me
                if (stopped || e.code === 4401) return;
                reconnect = setTimeout(connect, Math.min(MAX_RETRY_MS, 1000 * 2 ** retry++));
            };
        };

        connect();
        return () => {
            stopped = true;
            clearTimeout(reconnect);
            clearInterval(ping);
            socket?.close();
        };
    }, [token, queryClient]);
}
