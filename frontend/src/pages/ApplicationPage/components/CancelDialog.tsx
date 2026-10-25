import { useId, useRef, useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { ModalDesktop } from '@alfalab/core-components/modal/desktop';
import { Plate } from '@alfalab/core-components/plate';
import { Textarea } from '@alfalab/core-components/textarea';
import { Typography } from '@alfalab/core-components/typography';

import { ApiError } from '../../../api/client';
import { useCancelApplication } from '../../../api/queries';
import { newIdempotencyKey } from '../../../shared/lib/idempotency';
import './CancelDialog.css';

type Props = { applicationId: string; open: boolean; onClose: () => void };

/** Подтверждение отзыва заявки (POST /cancel). */
export function CancelDialog({ applicationId, open, onClose }: Props) {
    const cancel = useCancelApplication(applicationId);
    const [comment, setComment] = useState('');
    const key = useRef(newIdempotencyKey());
    const titleId = useId();

    const confirm = () =>
        cancel.mutate(
            { comment, idempotencyKey: key.current },
            {
                onSuccess: onClose,
                // при сбое сети запрос мог пройти — повторяем с тем же ключом; новый — только после однозначного отказа
                onError: (e) => {
                    if (e instanceof ApiError && e.status < 500) key.current = newIdempotencyKey();
                },
            },
        );

    return (
        <ModalDesktop open={open} onClose={onClose} size={500} wrapperProps={{ 'aria-labelledby': titleId, 'aria-modal': true }}>
            <ModalDesktop.Header hasCloser={true} />
            <ModalDesktop.Content>
                <div className='cancel-dialog'>
                    <Typography.Title id={titleId} tag='h2' view='small' font='system'>
                        Отозвать заявку?
                    </Typography.Title>
                    <Typography.Text view='primary-medium' color='secondary'>
                        Банк перестанет её рассматривать. Если кредит понадобится, подайте новую заявку — данные о компании подставим.
                    </Typography.Text>
                    <Textarea
                        label='Почему отзываете (необязательно)'
                        aria-label='Почему отзываете'
                        value={comment}
                        onChange={(e) => setComment(e.target.value)}
                        maxLength={1000}
                        block={true}
                        minRows={2}
                    />
                    {cancel.error && (
                        <Plate view='negative' title='Не получилось отозвать заявку'>
                            {cancel.error.message}
                        </Plate>
                    )}
                </div>
            </ModalDesktop.Content>
            <ModalDesktop.Footer>
                <ModalDesktop.Controls
                    primary={
                        <Button view='primary' size={48} onClick={confirm} loading={cancel.isPending}>
                            Отозвать
                        </Button>
                    }
                    secondary={
                        <Button view='secondary' size={48} onClick={onClose}>
                            Не отзывать
                        </Button>
                    }
                />
            </ModalDesktop.Footer>
        </ModalDesktop>
    );
}
