import { useId, useRef, useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { ModalResponsive } from '@alfalab/core-components/modal';
import { Plate } from '@alfalab/core-components/plate';
import { Radio } from '@alfalab/core-components/radio';
import { RadioGroup } from '@alfalab/core-components/radio-group';
import { Textarea } from '@alfalab/core-components/textarea';
import { Typography } from '@alfalab/core-components/typography';

import { ApiError } from '../../../api/client';
import { useAcceptOffer, useDeclineOffers } from '../../../api/queries';
import type { ApplicationStatusView, DeclineReason, Offer } from '../../../api/types';
import { DECLINE_REASON_LABELS } from '../../../shared/lib/dictionaries';
import { newIdempotencyKey } from '../../../shared/lib/idempotency';
import { RetentionOffer } from './RetentionOffer';
import './DeclineDialog.css';

const REASONS = Object.keys(DECLINE_REASON_LABELS) as DeclineReason[];

type Props = {
    status: ApplicationStatusView;
    open: boolean;
    onClose: () => void;
    /** Предложение «как запросили» — с ним сравниваем удерживающее. */
    base?: Offer;
    companyName: string;
};

function errorText(e: unknown) {
    if (e instanceof ApiError && e.status === 412) return 'Заявку только что изменили — обновили данные, попробуйте ещё раз.';
    if (e instanceof ApiError && e.status === 410) return 'Предложение больше не действует.';
    return e instanceof Error ? e.message : 'Что-то пошло не так';
}

/**
 * Экран 10: «Ни одно не подходит» → причина отказа.
 * Первый отказ может вернуть удерживающее предложение — показываем его тут же, вторым шагом.
 */
export function DeclineDialog({ status, open, onClose, base, companyName }: Props) {
    const decline = useDeclineOffers(status.id);
    const accept = useAcceptOffer(status.id);
    const [reason, setReason] = useState<DeclineReason>();
    const [comment, setComment] = useState('');
    const [retention, setRetention] = useState<Offer>();
    const keys = useRef({ decline: newIdempotencyKey(), accept: newIdempotencyKey() });
    const titleId = useId();
    const pending = decline.isPending || accept.isPending;
    const error = decline.error ?? accept.error;

    const close = () => {
        decline.reset();
        accept.reset();
        onClose();
    };

    const sendDecline = async () => {
        if (!reason) return;
        try {
            const result = await decline.mutateAsync({ reason, comment, version: status.version, idempotencyKey: keys.current.decline });
            keys.current.decline = newIdempotencyKey();
            const offer = result.retention_offers[0];
            if (offer && !retention) setRetention(offer);
            else close();
        } catch (e) {
            if (e instanceof ApiError && e.status < 500) keys.current.decline = newIdempotencyKey();
        }
    };

    const acceptRetention = async () => {
        if (!retention) return;
        try {
            await accept.mutateAsync({ offerId: retention.id, version: status.version, idempotencyKey: keys.current.accept });
            close();
        } catch (e) {
            if (e instanceof ApiError && e.status < 500) keys.current.accept = newIdempotencyKey();
        }
    };

    return (
        <ModalResponsive breakpoint={600} open={open} onClose={close} size={600} wrapperProps={{ 'aria-labelledby': titleId, 'aria-modal': true }}>
            <ModalResponsive.Header hasCloser={true} />
            <ModalResponsive.Content>
                {retention ? (
                    <RetentionOffer offer={retention} base={base} companyName={companyName} titleId={titleId} />
                ) : (
                    <div className='decline-dialog'>
                        <div className='decline-dialog__head'>
                            <Typography.Title id={titleId} tag='h2' view='small' font='system'>
                                Почему предложения не подошли?
                            </Typography.Title>
                            <Typography.Text view='primary-small' color='secondary'>
                                Ответ поможет банку предложить другие условия
                            </Typography.Text>
                        </div>
                        <RadioGroup
                            name='decline-reason'
                            label='Причина'
                            value={reason ?? null}
                            onChange={(_, { value }) => setReason(value as DeclineReason)}
                        >
                            {REASONS.map((r) => (
                                <Radio key={r} value={r} label={DECLINE_REASON_LABELS[r]} size={24} />
                            ))}
                        </RadioGroup>
                        <Textarea
                            label='Комментарий (необязательно)'
                            aria-label='Комментарий'
                            value={comment}
                            onChange={(e) => setComment(e.target.value)}
                            maxLength={1000}
                            block={true}
                            minRows={3}
                        />
                    </div>
                )}
                {error && (
                    <Plate className='decline-dialog__error' view='negative' title='Не получилось'>
                        {errorText(error)}
                    </Plate>
                )}
            </ModalResponsive.Content>
            <ModalResponsive.Footer>
                {retention ? (
                    <ModalResponsive.Controls
                        primary={
                            <Button view='primary' size={48} onClick={acceptRetention} loading={accept.isPending} disabled={pending}>
                                Принять предложение
                            </Button>
                        }
                        secondary={
                            <Button view='secondary' size={48} onClick={sendDecline} loading={decline.isPending} disabled={pending}>
                                Всё равно отказаться
                            </Button>
                        }
                    />
                ) : (
                    <ModalResponsive.Controls
                        primary={
                            <Button view='primary' size={48} onClick={sendDecline} loading={decline.isPending} disabled={!reason || pending}>
                                Отправить
                            </Button>
                        }
                        secondary={
                            <Button view='secondary' size={48} onClick={close}>
                                Вернуться к предложениям
                            </Button>
                        }
                    />
                )}
            </ModalResponsive.Footer>
        </ModalResponsive>
    );
}
