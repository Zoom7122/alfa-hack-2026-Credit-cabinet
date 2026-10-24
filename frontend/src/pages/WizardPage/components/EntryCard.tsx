import type { ReactNode } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Typography } from '@alfalab/core-components/typography';
import { TrashCanLineSIcon } from '@alfalab/icons-glyph/TrashCanLineSIcon';

import './EntryCard.css';

type Props = {
    title: string;
    removeLabel: string;
    onRemove: () => void;
    /** Пока запись не заполнена до конца, в черновик она не уходит. */
    incomplete?: boolean;
    children: ReactNode;
};

/** Одна запись в повторяемом списке: кредит, залог, поручитель. */
export function EntryCard({ title, removeLabel, onRemove, incomplete, children }: Props) {
    return (
        <div className='entry-card'>
            <div className='entry-card__head'>
                <Typography.Text view='primary-large' weight='bold'>
                    {title}
                </Typography.Text>
                <Button view='text' size={32} leftAddons={<TrashCanLineSIcon />} onClick={onRemove} aria-label={removeLabel}>
                    Удалить
                </Button>
            </div>
            <div className='entry-card__grid'>{children}</div>
            {incomplete && (
                <Typography.Text view='primary-small' color='secondary'>
                    Заполните обязательные поля — тогда запись сохранится в черновике
                </Typography.Text>
            )}
        </div>
    );
}
