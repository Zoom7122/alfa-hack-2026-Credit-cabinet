import { Button } from '@alfalab/core-components/button';
import { Typography } from '@alfalab/core-components/typography';
import { Link, useLocation } from 'react-router-dom';

import { useLogout } from '../../app/useAuth';
import './NotReadyPage.css';

/** Заглушка для экранов, которые ещё не сверстаны. standalone — без шапки кабинета (рабочее место банка). */
export function NotReadyPage({ standalone }: { standalone?: boolean }) {
    const { pathname } = useLocation();
    const logout = useLogout();
    return (
        <div className={`not-ready${standalone ? ' not-ready_standalone' : ''}`}>
            <div className='not-ready__card'>
                <Typography.Title tag='h1' view='small' font='system'>
                    Экран в разработке
                </Typography.Title>
                <Typography.Text view='primary-medium' color='secondary'>
                    {pathname}
                </Typography.Text>
                {standalone ? (
                    <Button view='secondary' size={48} onClick={logout}>
                        Сменить пользователя
                    </Button>
                ) : (
                    <Button Component={Link} href='/' view='secondary' size={48}>
                        На главную
                    </Button>
                )}
            </div>
        </div>
    );
}
