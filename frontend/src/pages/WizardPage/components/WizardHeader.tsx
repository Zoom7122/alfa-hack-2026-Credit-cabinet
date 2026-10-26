import { Button } from '@alfalab/core-components/button';
import { Typography } from '@alfalab/core-components/typography';
import { Link } from 'react-router-dom';

import { formatTime } from '../../../shared/lib/format';
import { Logo } from '../../../shared/ui/Logo/Logo';
import { VisuallyHidden } from '../../../shared/ui/VisuallyHidden/VisuallyHidden';
import './WizardHeader.css';

/** Черновик хранится в браузере — сохраняется сразу, ошибок сети у него нет. */
type DraftStatus = 'idle' | 'saved';

type Props = {
    status?: DraftStatus;
    savedAt?: string;
    exitLabel: string;
    onExit: () => void;
};

function statusText(status: DraftStatus | undefined, savedAt: string | undefined) {
    switch (status) {
        case 'saved':
            return savedAt ? `✓ Черновик сохранён в ${formatTime(savedAt)}` : '✓ Черновик сохранён';
        case 'idle':
            return 'Изменения сохраняются автоматически';
        default:
            return undefined;
    }
}

/** Шапка анкеты: без навигации кабинета, со статусом автосохранения. */
export function WizardHeader({ status, savedAt, exitLabel, onExit }: Props) {
    const text = statusText(status, savedAt);
    // вслух — только итог «сохранено», а не на каждое нажатие клавиши
    const announcement = status === 'saved' ? text : '';
    return (
        <header className='wizard-header'>
            <Link to='/' className='wizard-header__logo' aria-label='Альфа Бизнес — на главную'>
                <Logo />
                <Typography.Text view='primary-large' weight='bold' className='wizard-header__brand'>
                    Альфа Бизнес
                </Typography.Text>
            </Link>
            <div className='wizard-header__actions'>
                <VisuallyHidden role='status'>{announcement}</VisuallyHidden>
                {text && (
                    <Typography.Text view='primary-small' color='secondary' className='wizard-header__status' aria-hidden='true'>
                        {text}
                    </Typography.Text>
                )}
                <Button view='outlined' size={40} onClick={onExit}>
                    {exitLabel}
                </Button>
            </div>
        </header>
    );
}
