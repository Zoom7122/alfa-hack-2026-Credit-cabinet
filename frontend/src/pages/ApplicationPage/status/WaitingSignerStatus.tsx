import { useRef } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Typography } from '@alfalab/core-components/typography';
import { PenNibMIcon } from '@alfalab/icons-glyph/PenNibMIcon';

import { ApiError } from '../../../api/client';
import { useOffers, useSigning, useStartSigning } from '../../../api/queries';
import type { Application, ApplicationStatusView } from '../../../api/types';
import { useDownload } from '../../../api/useDownload';
import { formatDay, formatMoney, formatRate, shortName } from '../../../shared/lib/format';
import { newIdempotencyKey } from '../../../shared/lib/idempotency';
import { ResultCard } from '../components/ResultCard';
import { StageBar } from '../components/StageBar';
import { SummaryList } from '../components/SummaryList';
import { contractOf } from './signingDocs';
import './WaitingSignerStatus.css';

type Props = { app: Application; status: ApplicationStatusView };

/** Экран 12: предложение выбрал не директор — договор ждёт его подписи в его кабинете. */
export function WaitingSignerStatus({ app, status }: Props) {
    const signing = useSigning(app.id);
    const offers = useOffers(app.id);
    const remind = useStartSigning(app.id);
    const { download, pendingId, error: downloadError } = useDownload(app.id);
    const remindKey = useRef(newIdempotencyKey());

    const session = signing.data;
    const offer = offers.data?.find((o) => o.id === session?.offer_id);
    const signer = status.signing?.required_signer ?? session?.required_signer;
    const contract = contractOf(session?.documents);

    const sendReminder = () =>
        remind.mutate(
            { idempotencyKey: remindKey.current },
            {
                onSuccess: () => {
                    remindKey.current = newIdempotencyKey();
                },
                onError: (e) => {
                    if (e instanceof ApiError && e.status < 500) remindKey.current = newIdempotencyKey();
                },
            },
        );

    if (signing.isLoading) return <Skeleton visible={true} className='waiting-signer__skeleton' />;

    return (
        <ResultCard
            icon={<PenNibMIcon />}
            tone='neutral'
            title='Договор ждёт подписи директора'
            text={
                signer
                    ? `Задача на подпись — в кабинете директора: ${signer.full_name}. Когда договор будет подписан, деньги поступят на счёт компании.`
                    : 'Когда директор подпишет договор, деньги поступят на счёт компании.'
            }
            actions={
                <>
                    {contract && (
                        <Button view='secondary' size={56} onClick={() => download(contract)} loading={pendingId === contract.id}>
                            Скачать договор
                        </Button>
                    )}
                    {status.allowed_actions.includes('START_SIGNING') && (
                        <Button view='outlined' size={56} onClick={sendReminder} loading={remind.isPending}>
                            Напомнить директору
                        </Button>
                    )}
                </>
            }
        >
            <StageBar stages={status.stages} clientStage={status.client_stage} />
            {offer && (
                <SummaryList
                    rows={[
                        {
                            label: 'Выбранное предложение',
                            value: `${formatMoney(offer.amount)} · ${offer.term_months} мес. · ${formatRate(offer.rate_annual)}`,
                            bold: true,
                        },
                        ...(signer ? [{ label: 'Подписант', value: `${shortName(signer.full_name)}, директор` }] : []),
                        { label: 'Предложение действует до', value: formatDay(offer.valid_until) },
                    ]}
                />
            )}
            {/* живой регион есть всегда — тогда сообщение о напоминании точно озвучится */}
            <Typography.Text view='primary-small' color='positive' role='status'>
                {remind.isSuccess ? 'Напомнили директору — задача снова в его кабинете' : ''}
            </Typography.Text>
            {remind.error && <Plate view='negative' title='Не удалось напомнить директору' />}
            {downloadError && <Plate view='negative' title={downloadError} />}
        </ResultCard>
    );
}
