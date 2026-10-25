import { useCallback, useSyncExternalStore } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';

import { session } from '../api/session';
import { collectOrphanFiles, forgetSecrets } from '../pages/WizardPage/draft';
import type { Me } from '../api/types';

export type AuthContext = { me: Me };

export function useToken() {
    return useSyncExternalStore(session.subscribe, session.getToken);
}

/** Выход: сбросить токен и кэш, вернуться на вход. */
export function useLogout() {
    const queryClient = useQueryClient();
    const navigate = useNavigate();
    return useCallback(() => {
        session.clear();
        queryClient.clear();
        // токены маркетплейсов и ОФД — не дольше сессии; брошенные файлы анкет — из IndexedDB
        forgetSecrets();
        void collectOrphanFiles();
        navigate('/login', { replace: true });
    }, [queryClient, navigate]);
}

/** Домашняя страница пользователя по роли. */
export const homeFor = (me: Me) => (me.roles.includes('CLIENT') ? '/' : '/backoffice');
