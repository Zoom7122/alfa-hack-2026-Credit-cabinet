import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Status } from '@alfalab/core-components/status';
import { Typography } from '@alfalab/core-components/typography';
import { CheckmarkCompactSIcon } from '@alfalab/icons-glyph/CheckmarkCompactSIcon';
import { CrossCompactSIcon } from '@alfalab/icons-glyph/CrossCompactSIcon';
import { ExclamationCompactSIcon } from '@alfalab/icons-glyph/ExclamationCompactSIcon';

import { ApiError } from '../../../api/client';
import { useSignatureReport } from '../../../api/queries';
import type { Document, SignatureReport } from '../../../api/types';
import { formatDate, shortName } from '../../../shared/lib/format';
import { SummaryList, type SummaryRow } from '../components/SummaryList';
import { VERDICT_BADGES } from './documentBadges';
import './SignaturePanel.css';

type SignatureInfo = NonNullable<SignatureReport['signatures']>[number];
type Check = NonNullable<SignatureInfo['checks']>[number];

export const SIGNATURE_PANEL_ID = 'signature-panel';

const KIND_LABELS: Record<SignatureInfo['kind'], string> = {
    QUALIFIED: 'квалифицированная подпись',
    UNQUALIFIED: 'неквалифицированная подпись',
    UNKNOWN: 'подпись',
};

/** Подписи проверок без текста от бэка — по чек-листу docs/e-signature.md. */
const CHECK_LABELS: Partial<Record<Check['code'], string>> = {
    FORMAT_PARSED: 'Подпись прочитана',
    INTEGRITY: 'Документ не изменён после подписи',
    SIGNATURE_VALUE: 'Подпись математически верна',
    DOCUMENT_NOT_MODIFIED: 'После подписи в PDF ничего не меняли',
    CERT_VALID_AT_SIGNING_TIME: 'Сертификат действовал на момент подписи',
    SIGNING_TIME_TRUSTED: 'Время подписи подтверждено',
    CHAIN_TO_TRUSTED_ROOT: 'Сертификат выдан доверенным центром',
    ISSUER_ACCREDITED: 'Удостоверяющий центр аккредитован',
    NOT_REVOKED: 'Сертификат не отозван',
    QUALIFIED_ATTRIBUTES: 'Подпись квалифицированная',
    KEY_USAGE: 'Ключ разрешено использовать для подписи',
    SIGNER_MATCHES_APPLICANT: 'Сертификат выдан на компанию-заявителя',
    SIGNER_AUTHORITY: 'У подписанта есть право подписи',
    POA_VALID: 'Доверенность действует',
};

const ROLE_LABELS: Record<string, string> = { APPLICANT_HEAD: 'Подпись руководителя' };

const CHECK_ICONS = { PASSED: CheckmarkCompactSIcon, WARNING: ExclamationCompactSIcon, FAILED: CrossCompactSIcon };

function rowsOf(signature: SignatureInfo): SummaryRow[] {
    const rows: SummaryRow[] = [];
    if (signature.signer) rows.push({ label: 'Подписант', value: shortName(signature.signer.full_name) });
    if (signature.signer?.position) rows.push({ label: 'Должность', value: signature.signer.position });
    if (signature.signer?.organization?.name) rows.push({ label: 'Организация', value: signature.signer.organization.name });
    if (signature.certificate) rows.push({ label: 'Сертификат', value: `до ${formatDate(signature.certificate.valid_to)}` });
    if (signature.certificate?.issuer.name) rows.push({ label: 'Удостоверяющий центр', value: signature.certificate.issuer.name });
    return rows;
}

function SignatureBlock({ signature, index, total }: { signature: SignatureInfo; index: number; total: number }) {
    const checks = (signature.checks ?? []).filter((c) => c.status !== 'SKIPPED');
    const badge = VERDICT_BADGES[signature.verdict];
    return (
        <div className='signature-panel__signature'>
            {total > 1 && (
                <div className='signature-panel__signature-head'>
                    <Typography.Text view='primary-medium' weight='bold'>
                        Подпись {index + 1} из {total}
                    </Typography.Text>
                    <Status view='muted-alt' color={badge.color} size={20} uppercase={false}>
                        {badge.label}
                    </Status>
                </div>
            )}
            <Typography.Text view='primary-small' color='secondary'>
                {KIND_LABELS[signature.kind]}
            </Typography.Text>
            <SummaryList plain={true} rows={rowsOf(signature)} />
            {checks.length > 0 && (
                <ul className='signature-panel__checks' aria-label='Что проверили'>
                    {checks.map((check) => {
                        const Icon = CHECK_ICONS[check.status as keyof typeof CHECK_ICONS];
                        return (
                            <li key={check.code} className={`signature-panel__check signature-panel__check_status_${check.status.toLowerCase()}`}>
                                <span className='signature-panel__mark' aria-hidden='true'>
                                    {Icon && <Icon />}
                                </span>
                                <Typography.Text view='primary-small'>{check.message ?? CHECK_LABELS[check.code] ?? check.code}</Typography.Text>
                            </li>
                        );
                    })}
                </ul>
            )}
        </div>
    );
}

type Props = {
    applicationId: string;
    document?: Document;
    /** Есть ли в заявке вообще подписанные документы — для пустого состояния. */
    hasSigned: boolean;
};

/** Текст, когда отчёта нет: не выбран, не подписан, проверка идёт или не удалась. */
function emptyText(document: Document | undefined, hasSigned: boolean) {
    if (!document) {
        return hasSigned
            ? 'Выберите подписанный документ — покажем, кто подписал и что проверили.'
            : 'У документов этой заявки пока нет электронных подписей.';
    }
    switch (document.signatures?.state) {
        case 'PENDING':
            return 'Проверяем подпись…';
        case 'FAILED':
            return 'Проверить подпись не удалось — попробуем ещё раз автоматически.';
        default:
            return `${document.file_name} — без электронной подписи`;
    }
}

/** Экран 15, справа: отчёт проверки ЭП выбранного документа — кто подписал и что проверили. */
export function SignaturePanel({ applicationId, document, hasSigned }: Props) {
    const ready = document?.signatures?.state === 'DONE';
    const report = useSignatureReport(applicationId, ready ? document?.id : undefined, document?.signatures?.verified_at);
    const data = report.data;
    const signatures = data?.signatures ?? [];
    const badge = data && (data.policy_result.satisfied ? VERDICT_BADGES[data.verdict] : { label: 'Не хватает подписей', color: 'orange' as const });
    // 404 при готовой сводке — отчёт ещё собирается (гонка с проверкой), это не ошибка
    const notReady = report.error instanceof ApiError && report.error.status === 404;

    return (
        <section id={SIGNATURE_PANEL_ID} className='signature-panel' aria-labelledby='signature-panel-title' aria-busy={report.isFetching}>
            <div className='signature-panel__head'>
                <Typography.Title id='signature-panel-title' tag='h2' view='xsmall' font='system'>
                    Проверка подписи
                </Typography.Title>
                {badge && (
                    <Status view='muted-alt' color={badge.color} size={24} uppercase={false}>
                        {badge.label}
                    </Status>
                )}
            </div>

            {/* вслух — только короткая сводка, а не весь чек-лист при каждом переключении */}
            <Typography.Text view='primary-small' color='secondary' role='status'>
                {data
                    ? [data.file_name, badge?.label.toLowerCase()].filter(Boolean).join(' · ')
                    : ready
                      ? notReady
                          ? 'Отчёт о проверке ещё готовится'
                          : ''
                      : emptyText(document, hasSigned)}
            </Typography.Text>

            {report.isLoading && <Skeleton visible={true} className='signature-panel__skeleton' />}
            {report.isError && !notReady && <Plate view='negative' title='Не удалось загрузить отчёт о подписи' />}

            {data && (
                <>
                    {data.policy_result.required_signers && data.policy_result.required_signers.length > 0 && (
                        <ul className='signature-panel__required' aria-label='Обязательные подписи'>
                            {data.policy_result.required_signers.map((r) => (
                                <li key={r.role} className={`signature-panel__check signature-panel__check_status_${r.satisfied ? 'passed' : 'warning'}`}>
                                    <span className='signature-panel__mark' aria-hidden='true'>
                                        {r.satisfied ? <CheckmarkCompactSIcon /> : <ExclamationCompactSIcon />}
                                    </span>
                                    <Typography.Text view='primary-small'>
                                        {ROLE_LABELS[r.role] ?? r.role}: {r.satisfied ? 'есть' : 'нет'}
                                    </Typography.Text>
                                </li>
                            ))}
                        </ul>
                    )}
                    {data.policy_result.message && (
                        <Typography.Text view='primary-small' color='attention'>
                            {data.policy_result.message}
                        </Typography.Text>
                    )}
                    {signatures.map((signature, i) => (
                        <SignatureBlock key={signature.index} signature={signature} index={i} total={signatures.length} />
                    ))}
                </>
            )}
        </section>
    );
}
