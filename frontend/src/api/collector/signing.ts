import { type QueryClient, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { unwrap } from '../client';
import { collector } from './client';
import { collectorKeys } from './queries';
import type { CollectorSigningSession } from './types';

export const collectorSigningKey = (id: string) => ['collector', 'signing', id] as const;

async function refreshSigning(client: QueryClient, id: string, session?: CollectorSigningSession) {
    if (session) client.setQueryData(collectorSigningKey(id), session);
    await Promise.all([
        ...[collectorKeys.application(id), collectorKeys.applications, collectorKeys.offers(id), collectorKeys.timeline(id), ['me']].map((queryKey) =>
            client.invalidateQueries({ queryKey }),
        ),
        ...(!session ? [client.invalidateQueries({ queryKey: collectorSigningKey(id) })] : []),
    ]);
}

export function useCollectorSigning(id: string, enabled = true) {
    return useQuery({
        queryKey: collectorSigningKey(id),
        queryFn: async () => unwrap(await collector.GET('/applications/{id}/signing', { params: { path: { id } } })),
        enabled: Boolean(id) && enabled,
        retry: false,
        refetchInterval: (query) => (query.state.data?.state === 'AWAITING_SIGNER' ? 2_000 : false),
    });
}

export function useAcceptCollectorOffer(id: string) {
    const client = useQueryClient();
    return useMutation({
        mutationFn: async (offerId: string) =>
            unwrap(await collector.POST('/applications/{id}/offers/{offerId}/accept', { params: { path: { id, offerId } } })),
        onSuccess: (session) => refreshSigning(client, id, session),
        onError: () => refreshSigning(client, id),
    });
}

export function useConfirmCollectorSigning(id: string) {
    const client = useQueryClient();
    return useMutation({
        mutationFn: async (code: string) =>
            unwrap(await collector.POST('/applications/{id}/signing/confirm', { params: { path: { id } }, body: { method: 'SIMPLE_CODE', code } })),
        onSuccess: (session) => refreshSigning(client, id, session),
        onError: () => refreshSigning(client, id),
    });
}
