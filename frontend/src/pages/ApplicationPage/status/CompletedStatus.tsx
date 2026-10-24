import { Button } from '@alfalab/core-components/button';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { CheckmarkMIcon } from '@alfalab/icons-glyph/CheckmarkMIcon';
import { Link } from 'react-router-dom';

import { useOffers, useSigning } from '../../../api/queries';
import type { Application, ApplicationStatusView, SigningMethod } from '../../../api/types';
import { formatDate, formatDay, formatMoney, shortName } from '../../../shared/lib/format';
import { ResultCard } from '../components/ResultCard';
import { StageBar } from '../components/StageBar';
import { type SummaryRow, SummaryList } from '../components/SummaryList';
import { contractOf } from './signingDocs';
import { SigningDocuments } from './SigningDocuments';
import './CompletedStatus.css';

type Props = { app: Application; status: ApplicationStatusView };

const METHOD_LABELS: Record<SigningMethod, string> = {
    SIMPLE_CODE: 'код подтверждения',
    QUALIFIED_SIGNATURE: 'электронная подпись (КЭП)',
};

/** Даты первого платежа в контракте нет — показываем ориентир: месяц после подписания. */
function firstPaymentDate(signedAt: string) {
    const d = new Date(signedAt);
    d.setMonth(d.getMonth() + 1);
    return d.toISOString();
}

/** «Кредитный договор № КД-2026-000123.pdf» → «КД-2026-000123». */
const contractNumber = (fileName: string) => /№\s*(\S+?)(?:\.pdf)?$/i.exec(fileName)?.[1] ?? fileName.replace(/\.pdf$/i, '');

/** Экран 13: договор подписан, деньги на счёте. */
export function CompletedStatus({ app, status }: Props) {
    const signing = useSigning(app.id);
    const offers = useOffers(app.id);

    const session = signing.data;
    const offer = offers.data?.find((o) => o.id === session?.offer_id);
    const contract = contractOf(session?.documents);
    const payout = session?.disbursement;
    const signedAt = session?.signed_at;

    const rows: SummaryRow[] = [];
    if (contract) rows.push({ label: 'Договор', value: `${contractNumber(contract.file_name)}${signedAt ? ` от ${formatDate(signedAt)}` : ''}` });
    if (offer && signedAt) {
        rows.push({
            label: 'Первый платёж',
            value: `${formatMoney(offer.monthly_payment)} — около ${formatDay(firstPaymentDate(signedAt))}`,
            bold: true,
        });
    }
    if (session) {
        const method = session.method && METHOD_LABELS[session.method];
        rows.push({ label: 'Подписал', value: [shortName(session.required_signer.full_name), method].filter(Boolean).join(', ') });
    }

    if (signing.isLoading) return <Skeleton visible={true} className='completed-status__skeleton' />;

    return (
        <ResultCard
            icon={<CheckmarkMIcon />}
            tone='positive'
            title='Кредит выдан'
            text={
                payout?.amount !== undefined
                    ? `${formatMoney(payout.amount)} зачислены на счёт ${payout.account_masked ?? 'компании'}`
                    : status.subtitle
            }
            actions={
                <Button Component={Link} href='/' view='primary' size={56}>
                    На главную
                </Button>
            }
        >
            <StageBar stages={status.stages} clientStage={status.client_stage} />
            {rows.length > 0 && <SummaryList rows={rows} />}
            {/* по кнопке на файл: несколько скачиваний подряд браузер может заблокировать */}
            {session && session.documents.length > 0 && (
                <SigningDocuments
                    applicationId={app.id}
                    documents={session.documents}
                    offer={offer}
                    title='Документы по кредиту'
                    actionLabel='Скачать'
                />
            )}
        </ResultCard>
    );
}
