import { useRef, useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Link } from '@alfalab/core-components/link';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Typography } from '@alfalab/core-components/typography';

import { ApiError } from '../../../api/client';
import { useAcceptOffer, useOffers, useSigning } from '../../../api/queries';
import type { Application, ApplicationStatusView, Offer } from '../../../api/types';
import { formatDay, shortName } from '../../../shared/lib/format';
import { newIdempotencyKey } from '../../../shared/lib/idempotency';
import { StageBar } from '../components/StageBar';
import { DeclineDialog } from './DeclineDialog';
import { OfferCard } from './OfferCard';
import { RetentionOffer } from './RetentionOffer';
import './OffersStatus.css';

type Props = { app: Application; status: ApplicationStatusView };

function acceptError(e: unknown) {
    if (e instanceof ApiError && e.status === 410) return 'Предложение больше не действует — обновили список.';
    if (e instanceof ApiError && e.status === 412) return 'Заявку только что изменили — проверьте предложения и выберите ещё раз.';
    if (e instanceof ApiError && e.status === 409) return 'Решение по заявке уже изменилось.';
    return e instanceof Error ? e.message : 'Не получилось выбрать предложение';
}

/** Экран 09: банк готов выдать кредит — выбор предложения или отказ (экран 10). */
export function OffersStatus({ app, status }: Props) {
    const offers = useOffers(status.id);
    // заявка вернулась с подписания (кончились попытки или подпись не прошла) — объясняем, что случилось
    const signing = useSigning(status.id);
    const signingFailed = signing.data?.state === 'FAILED';
    const accept = useAcceptOffer(status.id);
    const [declining, setDeclining] = useState(false);
    const keys = useRef<Record<string, string>>({});

    const active = (offers.data ?? []).filter((o) => o.state === 'ACTIVE');
    const regular = active.filter((o) => o.kind !== 'RETENTION');
    const retention = active.find((o) => o.kind === 'RETENTION');
    const base = regular.find((o) => o.kind === 'REQUESTED') ?? regular[0];
    const guarantors = (app.guarantors ?? []).map((g) => (g.type === 'INDIVIDUAL' ? shortName(g.name) : g.name));
    const canAccept = status.allowed_actions.includes('ACCEPT_OFFER');

    const choose = (offer: Offer) => {
        const idempotencyKey = (keys.current[offer.id] ??= newIdempotencyKey());
        accept.mutate(
            { offerId: offer.id, version: status.version, idempotencyKey },
            {
                // устаревший статус перечитывает сам хук; исправленная попытка — новый запрос
                onError: (e) => {
                    if (e instanceof ApiError && e.status < 500) delete keys.current[offer.id];
                },
            },
        );
    };

    return (
        <div className='offers-status'>
            <StageBar stages={status.stages} clientStage={status.client_stage} />
            <div className='offers-status__head'>
                <Typography.TitleResponsive tag='h2' view='medium' font='system'>
                    Банк готов выдать кредит
                </Typography.TitleResponsive>
                <Typography.Text view='primary-large' color='secondary'>
                    Выберите подходящий вариант.
                    {status.deadline_at && ` Предложения действуют до ${formatDay(status.deadline_at)}.`}
                </Typography.Text>
            </div>

            {signingFailed && (
                <Plate view='attention' title='Подписать договор не удалось'>
                    Попытки подтверждения закончились или подпись не прошла проверку. Выберите предложение ещё раз — договор
                    сформируем заново.
                </Plate>
            )}
            {offers.isLoading && <Skeleton visible={true} className='offers-status__skeleton' />}
            {offers.isError && <Plate view='negative' title='Не удалось загрузить предложения' />}
            {accept.error && (
                <Plate view='negative' title='Не получилось выбрать предложение'>
                    {acceptError(accept.error)}
                </Plate>
            )}

            {retention && !declining && (
                <section className='offers-status__retention' aria-label='Специальное предложение'>
                    <RetentionOffer offer={retention} base={base} companyName={app.applicant.short_name} />
                    <div>
                        <Button view='primary' size={48} onClick={() => choose(retention)} loading={accept.isPending} disabled={!canAccept}>
                            Принять предложение
                        </Button>
                    </div>
                </section>
            )}

            <section className='offers-status__grid' aria-label='Предложения банка'>
                {regular.map((o) => (
                    <OfferCard
                        key={o.id}
                        offer={o}
                        base={base}
                        guarantorNames={guarantors}
                        onChoose={() => choose(o)}
                        loading={accept.isPending && accept.variables?.offerId === o.id}
                        disabled={!canAccept || accept.isPending}
                    />
                ))}
            </section>

            {status.allowed_actions.includes('DECLINE_OFFERS') && (
                <div>
                    <Link pseudo={true} view='default' onClick={() => setDeclining(true)}>
                        Ни одно не подходит
                    </Link>
                </div>
            )}

            {declining && (
                <DeclineDialog
                    status={status}
                    open={declining}
                    onClose={() => setDeclining(false)}
                    base={base}
                    companyName={app.applicant.short_name}
                />
            )}
        </div>
    );
}
