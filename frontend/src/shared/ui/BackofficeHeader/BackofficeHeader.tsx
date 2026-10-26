import { Button } from '@alfalab/core-components/button';
import { Typography } from '@alfalab/core-components/typography';
import { Link, useLocation, useSearchParams } from 'react-router-dom';

import type { Me } from '../../../api/types';
import { initials, splitName } from '../../lib/format';
import { Logo } from '../Logo/Logo';
import './BackofficeHeader.css';

type Props = { me: Me; onLogout: () => void };

type NavItem = { to: string; label: string; active: (path: string, assignee: string | null) => boolean };

/** «Очередь» и «Мои заявки» — одна страница с разным фильтром исполнителя. */
const NAV: NavItem[] = [
    {
        to: '/backoffice',
        label: 'Очередь',
        active: (path, assignee) => (path === '/backoffice' && assignee !== 'me') || path.startsWith('/backoffice/applications'),
    },
    { to: '/backoffice?assignee=me', label: 'Мои заявки', active: (path, assignee) => path === '/backoffice' && assignee === 'me' },
];

/** Тёмная шапка рабочего места андеррайтера (макеты 16–17). */
export function BackofficeHeader({ me, onLogout }: Props) {
    const { pathname } = useLocation();
    const [params] = useSearchParams();
    const assignee = params.get('assignee');
    // как в макете: «Соколова Мария» — без отчества
    const { last, first } = splitName(me.full_name);

    return (
        <header className='backoffice-header'>
            <div className='backoffice-header__inner'>
                <Link to='/backoffice' className='backoffice-header__logo' aria-label='Кредитный конвейер — очередь заявок'>
                    <Logo />
                    <Typography.Text view='primary-large' weight='bold' color='primary-inverted'>
                        Кредитный конвейер
                    </Typography.Text>
                </Link>
                <nav className='backoffice-header__nav' aria-label='Разделы'>
                    {NAV.map((item) => {
                        const active = item.active(pathname, assignee);
                        return (
                            <Link
                                key={item.label}
                                to={item.to}
                                aria-current={active ? 'page' : undefined}
                                className={`backoffice-header__link${active ? ' backoffice-header__link_active' : ''}`}
                            >
                                <Typography.Text view='primary-medium' weight={active ? 'bold' : 'regular'}>
                                    {item.label}
                                </Typography.Text>
                            </Link>
                        );
                    })}
                </nav>
                <div className='backoffice-header__user'>
                    <div className='backoffice-header__user-text'>
                        <Typography.Text view='primary-small' weight='bold' color='primary-inverted'>
                            {last} {first}
                        </Typography.Text>
                        <Typography.Text view='primary-small' color='secondary-inverted'>
                            Андеррайтер
                        </Typography.Text>
                    </div>
                    <span className='backoffice-header__avatar' aria-hidden='true'>
                        <Typography.Text view='primary-small' weight='bold' color='primary-inverted'>
                            {initials(me.full_name)}
                        </Typography.Text>
                    </span>
                    <Button view='text' size={32} onClick={onLogout} colors='inverted'>
                        Выйти
                    </Button>
                </div>
            </div>
        </header>
    );
}
