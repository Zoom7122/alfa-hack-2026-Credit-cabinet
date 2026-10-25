import { IconButton } from '@alfalab/core-components/icon-button';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Status } from '@alfalab/core-components/status';
import { Typography } from '@alfalab/core-components/typography';
import { CrossMIcon } from '@alfalab/icons-glyph/CrossMIcon';

import { ApiError } from '../../../api/client';
import { useCollectorSignatureReport } from '../../../api/collector/queries';
import type { ReportStage, VerificationStatus } from '../../../api/collector/signatureReport';
import type { CollectorDocument } from '../../../api/collector/types';
import { formatDate, formatUpdated } from '../../../shared/lib/format';
import { SummaryList } from '../../../shared/ui/SummaryList/SummaryList';
import './CollectorSignaturePanel.css';

export const SIGNATURE_PANEL_ID = 'collector-signature-panel';

type Color = 'green' | 'orange' | 'red' | 'grey';

const STATUS: Record<VerificationStatus, { label: string; color: Color }> = {
    VERIFIED: { label: 'пройдена', color: 'green' },
    INVALID: { label: 'не пройдена', color: 'red' },
    INDETERMINATE: { label: 'не определить', color: 'orange' },
    NOT_REQUIRED: { label: 'не нужна', color: 'grey' },
};

const VERDICT: Record<string, { label: string; color: Color }> = {
    VALID: { label: 'Подпись верна', color: 'green' },
    VALID_WITH_WARNINGS: { label: 'Подпись верна, с замечаниями', color: 'green' },
    INVALID: { label: 'Подпись недействительна', color: 'red' },
    INDETERMINATE: { label: 'Подпись не удалось проверить', color: 'orange' },
};

const STAGES: { id: 'integrity' | 'format' | 'certificate' | 'identity' | 'authority'; title: string }[] = [
    { id: 'integrity', title: 'Целостность документа' },
    { id: 'format', title: 'Формат подписи' },
    { id: 'certificate', title: 'Сертификат' },
    { id: 'identity', title: 'Личность подписанта' },
    { id: 'authority', title: 'Полномочия подписанта' },
];

function Stage({ title, stage }: { title: string; stage?: ReportStage }) {
    const status = stage ? STATUS[stage.status] : undefined;
    return (
        <li className='collector-signature__stage'>
            <span className='collector-signature__stage-head'>
                <Typography.Text view='primary-small'>{title}</Typography.Text>
                {status && (
                    <Status view='muted-alt' color={status.color} size={20} uppercase={false}>
                        {status.label}
                    </Status>
                )}
            </span>
            {stage?.reason && (
                <Typography.Text view='primary-small' color='secondary'>
                    {stage.reason}
                </Typography.Text>
            )}
            {stage?.warnings?.map((w) => (
                <Typography.Text key={w} view='primary-small' color='secondary'>
                    {w}
                </Typography.Text>
            ))}
        </li>
    );
}

/** Панель «Проверка подписи» (экран 15) по отчёту worker'а подписи data-collector. */
export function CollectorSignaturePanel({ applicationId, document, onClose }: { applicationId: string; document: CollectorDocument; onClose: () => void }) {
    const report = useCollectorSignatureReport(applicationId, document.id, document.signature_state ?? '');
    const r = report.data;
    const verdict = r ? (VERDICT[r.verdict] ?? { label: r.verdict, color: 'grey' as Color }) : undefined;

    return (
        <aside id={SIGNATURE_PANEL_ID} className='collector-signature' aria-labelledby='collector-signature-title'>
            <div className='collector-signature__head'>
                <Typography.Title id='collector-signature-title' tag='h2' view='xsmall' font='system'>
                    Проверка подписи
                </Typography.Title>
                <IconButton icon={CrossMIcon} size={32} view='secondary' onClick={onClose} aria-label='Закрыть отчёт о подписи' />
            </div>
            <Typography.Text view='primary-small' color='secondary'>
                {document.file_name}
            </Typography.Text>

            {report.isPending && <Skeleton visible={true} className='collector-signature__skeleton' />}
            {report.isError && (
                <Plate view='attention' title={report.error instanceof ApiError && report.error.status === 404 ? 'Подпись ещё проверяем' : 'Не удалось загрузить отчёт'} />
            )}

            {r && verdict && (
                <>
                    <Status view='muted-alt' color={verdict.color} size={24} uppercase={false}>
                        {verdict.label}
                    </Status>
                    {r.failure_reason && <Plate view='negative' title={r.failure_reason} />}
                    <SummaryList
                        plain={true}
                        rows={[
                            { label: 'Подписант', value: r.signer ? [r.signer.full_name, r.signer.position].filter(Boolean).join(', ') : '—' },
                            ...(r.signer?.organization?.name ? [{ label: 'Организация', value: r.signer.organization.name }] : []),
                            { label: 'Время подписи', value: r.signing_time?.value ? formatUpdated(r.signing_time.value) : 'не указано' },
                            { label: 'Сертификат выдан', value: r.certificate?.issuer?.name ?? '—' },
                            ...(r.certificate?.valid_to ? [{ label: 'Действует до', value: formatDate(r.certificate.valid_to) }] : []),
                            { label: 'Проверено', value: formatUpdated(r.verified_at) },
                        ]}
                    />
                    <ul className='collector-signature__stages' aria-label='Этапы проверки'>
                        {STAGES.map((s) => (
                            <Stage key={s.id} title={s.title} stage={r.stages[s.id]} />
                        ))}
                    </ul>
                    {r.policy_result?.message && (
                        <Typography.Text view='primary-small' color='secondary'>
                            {r.policy_result.message}
                        </Typography.Text>
                    )}
                    {r.profile_notice && <Plate view='common' title='Тестовый профиль проверки'>{r.profile_notice}</Plate>}
                </>
            )}
        </aside>
    );
}
