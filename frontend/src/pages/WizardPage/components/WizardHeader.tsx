import { Button } from '@alfalab/core-components/button';
import { Typography } from '@alfalab/core-components/typography';
import { Link } from 'react-router-dom';

import { formatTime } from '../../../shared/lib/format';
import { Logo } from '../../../shared/ui/Logo/Logo';
import type { DraftStatus } from '../useDraft';
import { VisuallyHidden } from '../../../shared/ui/VisuallyHidden/VisuallyHidden';
import './WizardHeader.css';

type Props = {
    status?: DraftStatus;
    savedAt?: string;
    onRetry?: () => void;
    exitLabel: string;
    onExit: () => void;
    exiting?: boolean;
};

function statusText(status: DraftStatus | undefined, savedAt: string | undefined) {
    switch (status) {
        case 'pending':
        case 'saving':
            return 'Сохраняем…';
        case 'saved':
            return savedAt ? `✓ Черновик сохранён в ${formatTime(savedAt)}` : '✓ Черновик сохранён';
        case 'idle':
            return 'Изменения сохраняются автоматически';
        default:
            return undefined;
    }
}

/** Шапка анкеты: без навигации кабинета, со статусом автосохранения. */
export function WizardHeader({ status, savedAt, onRetry, exitLabel, onExit, exiting }: Props) {
    const text = statusText(status, savedAt);
    // вслух — только итог (сохранено / ошибка), а не «Сохраняем…» на каждое нажатие клавиши
    const announcement = status === 'error' ? 'Не удалось сохранить черновик' : status === 'saved' ? text : '';
    return (
        <header className='wizard-header'>
            <Link to='/' className='wizard-header__logo' aria-label='Альфа Бизнес — на главную'>
                <Logo />
                <Typography.Text view='primary-large' weight='bold'>
                    Альфа Бизнес
                </Typography.Text>
            </Link>
            <div className='wizard-header__actions'>
                <VisuallyHidden role='status'>{announcement}</VisuallyHidden>
                {status === 'error' ? (
                    <div className='wizard-header__status' aria-hidden='true'>
                        <Typography.Text view='primary-small' color='negative'>
                            Не удалось сохранить черновик
                        </Typography.Text>
                    </div>
                ) : (
                    text && (
                        <Typography.Text view='primary-small' color='secondary' className='wizard-header__status' aria-hidden='true'>
                            {text}
                        </Typography.Text>
                    )
                )}
                {status === 'error' && (
                    <Button view='text' size={32} onClick={onRetry}>
                        Повторить
                    </Button>
                )}
                <Button view='outlined' size={40} onClick={onExit} loading={exiting}>
                    {exitLabel}
                </Button>
            </div>
        </header>
    );
}
