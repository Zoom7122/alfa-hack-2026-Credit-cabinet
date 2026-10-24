import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, isUnauthorized, unwrap } from './client';
import { session } from './session';
import type { ApplicationStatus, ApplicationStatusView, ApplicationSummary } from './types';

/**
 * Ключи запросов. Общий префикс 'applications' позволяет по WS-событию
 * application.status_changed инвалидировать и список, и /status одной командой.
 */
export const queryKeys = {
    testUsers: ['auth', 'test-users'] as const,
    me: ['me'] as const,
    products: ['products'] as const,
    applicants: ['applicants'] as const,
    applications: (params: { applicantId?: string; status?: ApplicationStatus[] }) =>
        ['applications', 'list', params] as const,
    applicationStatus: (id: string) => ['applications', 'status', id] as const,
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

export function useProducts() {
    return useQuery({
        queryKey: queryKeys.products,
        queryFn: async () => unwrap(await api.GET('/products')).items,
        staleTime: 10 * 60 * 1000,
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
