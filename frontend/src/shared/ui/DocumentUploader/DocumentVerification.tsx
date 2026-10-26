import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Typography } from '@alfalab/core-components/typography';

import { ApiError } from '../../../api/client';
import { documentRequest } from '../../../api/documents';
import { queryKeys, useReverifyDocument } from '../../../api/queries';
import type { Document } from '../../../api/types';

type Check = { code?: string; status?: string; message?: string };
type Report = {
    verdict?: string;
    profile_notice?: string;
    failure_reason?: string;
    policy_result?: { message?: string; satisfied?: boolean };
    trust_mode?: string;
    signer?: { full_name?: string };
    stages?: Record<string, { status: string; reason?: string; warnings?: string[] }>;
    signatures?: { signer?: { full_name?: string }; checks?: Check[] }[];
};
const VERDICTS: Record<string, string> = {
    VALID: 'Подпись верна', VALID_WITH_WARNINGS: 'Подпись верна, с замечаниями',
    INVALID: 'Подпись недействительна', INDETERMINATE: 'Подпись не удалось подтвердить',
};

export function DocumentVerification({ applicationId, document: doc, editable }: { applicationId: string; document: Document; editable: boolean }) {
    const [expanded, setExpanded] = useState(false);
    const retry = useReverifyDocument(applicationId);
    const report = useQuery({
        queryKey: queryKeys.signatureReport(applicationId, doc.id, `${doc.signatures?.state}:${doc.signatures?.verified_at ?? ''}`),
        enabled: expanded,
        retry: false,
        queryFn: async () => {
            if (doc.signatures?.state === 'PENDING') return null;
            try { return await documentRequest<Report>(applicationId, `/${encodeURIComponent(doc.id)}/signature-report`); }
            catch (error) { if (error instanceof ApiError && error.status === 404) return null; throw error; }
        },
        refetchInterval: (query) => query.state.data === null && doc.signatures?.state === 'PENDING' ? 1500 : false,
    });
    const r = report.data;
    const signers = r?.signatures?.map((s) => s.signer?.full_name).filter(Boolean) ?? [];
    const messages = [r?.failure_reason, r?.policy_result?.message,
        ...Object.values(r?.stages ?? {}).flatMap((s) => [s.reason, ...(s.warnings ?? [])]),
        ...(r?.signatures ?? []).flatMap((s) => (s.checks ?? []).filter((c) => !['PASS', 'PASSED', 'VERIFIED', 'NOT_REQUIRED'].includes(c.status ?? '')).map((c) => c.message)),
    ].filter((message): message is string => Boolean(message));
    if (!doc.signatures || doc.signatures.state === 'NOT_SIGNED') return null;
    return (
        <div className='document-uploader__verification'>
            <div className='document-uploader__actions'>
                <Button view='text' size={32} onClick={() => setExpanded(!expanded)} aria-expanded={expanded}>
                    {expanded ? 'Скрыть отчёт' : 'Отчёт о подписи'}
                </Button>
                {editable && (
                    <Button view='secondary' size={32} loading={retry.isPending} disabled={doc.signatures.state === 'PENDING'} onClick={() => retry.mutate(doc.id)}>
                        Проверить подпись повторно
                    </Button>
                )}
            </div>
            {retry.isError && <Plate view='negative' title='Не удалось запустить проверку'>{retry.error.message}</Plate>}
            {expanded && (
                <div role='region' aria-label={`Проверка подписи: ${doc.file_name}`}>
                    {report.isPending && <Typography.Text view='primary-small'>Загружаем отчёт…</Typography.Text>}
                    {report.isError && <Plate view='negative' title='Не удалось загрузить отчёт'>{report.error.message}</Plate>}
                    {r === null && <Typography.Text view='primary-small'>Отчёт ещё не готов</Typography.Text>}
                    {r && <>
                        <Typography.Text tag='p' view='primary-small' weight='bold'>
                            {r.policy_result?.satisfied === false && ['VALID', 'VALID_WITH_WARNINGS'].includes(r.verdict ?? '')
                                ? 'Подпись верна, требования к подписанию не выполнены'
                                : VERDICTS[r.verdict ?? ''] ?? 'Проверка завершена'}
                        </Typography.Text>
                        {(r.signer?.full_name || signers.length > 0) && <Typography.Text tag='p' view='primary-small'>Подписант: {r.signer?.full_name ?? signers.join(', ')}</Typography.Text>}
                        {messages.length > 0 && <ul>{[...new Set(messages)].map((message) => <li key={message}>{message}</li>)}</ul>}
                        {r.profile_notice && <Typography.Text tag='p' view='primary-small' color='secondary'>{r.profile_notice}</Typography.Text>}
                        {!r.profile_notice && r.trust_mode === 'MOCK' && <Typography.Text tag='p' view='primary-small' color='secondary'>Проверка выполнена в тестовом профиле.</Typography.Text>}
                    </>}
                </div>
            )}
        </div>
    );
}
