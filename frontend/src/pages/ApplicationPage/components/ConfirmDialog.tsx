import { type ReactNode, useId } from 'react';
import { Button } from '@alfalab/core-components/button';
import { ModalResponsive } from '@alfalab/core-components/modal';
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
        <ModalResponsive breakpoint={600} open={true} onClose={onClose} size={500} wrapperProps={{ 'aria-labelledby': titleId, 'aria-modal': true }}>
            <ModalResponsive.Header hasCloser={true} />
            <ModalResponsive.Content>
                <div className='confirm-dialog'>
                    <Typography.Title id={titleId} tag='h2' view='small' font='system'>
                        {title}
                    </Typography.Title>
                    {children}
                </div>
            </ModalResponsive.Content>
            <ModalResponsive.Footer>
                <ModalResponsive.Controls
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
            </ModalResponsive.Footer>
        </ModalResponsive>
    );
}
