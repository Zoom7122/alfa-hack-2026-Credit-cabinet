import { type ReactNode, useId } from 'react';
import { Button } from '@alfalab/core-components/button';
import { ModalDesktop } from '@alfalab/core-components/modal/desktop';
import { Typography } from '@alfalab/core-components/typography';

import './ConfirmDialog.css';

type Props = {
    title: string;
    children?: ReactNode;
    confirmLabel: string;
    onConfirm: () => void;
    onClose: () => void;
    loading?: boolean;
};

/** Подтверждение необратимого действия (удаление документа и т. п.). */
export function ConfirmDialog({ title, children, confirmLabel, onConfirm, onClose, loading }: Props) {
    const titleId = useId();
    return (
        <ModalDesktop open={true} onClose={onClose} size={500} wrapperProps={{ 'aria-labelledby': titleId, 'aria-modal': true }}>
            <ModalDesktop.Header hasCloser={true} />
            <ModalDesktop.Content>
                <div className='confirm-dialog'>
                    <Typography.Title id={titleId} tag='h2' view='small' font='system'>
                        {title}
                    </Typography.Title>
                    {children}
                </div>
            </ModalDesktop.Content>
            <ModalDesktop.Footer>
                <ModalDesktop.Controls
                    primary={
                        <Button view='primary' size={48} onClick={onConfirm} loading={loading}>
                            {confirmLabel}
                        </Button>
                    }
                    secondary={
                        <Button view='secondary' size={48} onClick={onClose}>
                            Отмена
                        </Button>
                    }
                />
            </ModalDesktop.Footer>
        </ModalDesktop>
    );
}
