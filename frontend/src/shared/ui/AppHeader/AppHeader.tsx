import { Button } from '@alfalab/core-components/button';
import { IconButton } from '@alfalab/core-components/icon-button';
import { Indicator } from '@alfalab/core-components/indicator';
import { Typography } from '@alfalab/core-components/typography';
import { BellMIcon } from '@alfalab/icons-glyph/BellMIcon';
import { NavLink } from 'react-router-dom';

import type { CompanyAccess, Me } from '../../../api/types';
import { ROLE_LABELS } from '../../lib/dictionaries';
import { initials, shortName } from '../../lib/format';
import { Logo } from '../Logo/Logo';
import './AppHeader.css';

type Props = {
    me: Me;
    company?: CompanyAccess;
    hasNotifications?: boolean;
    onLogout: () => void;
};

const NAV = [
    { to: '/', label: 'Кредиты', end: true },
    { to: '/accounts', label: 'Счета' },
    { to: '/documents', label: 'Документы' },
];

export function AppHeader({ me, company, hasNotifications, onLogout }: Props) {
    return (
        <header className='app-header'>
            <div className='app-header__inner'>
                <NavLink to='/' className='app-header__logo' aria-label='Альфа Бизнес — на главную'>
                    <Logo />
                    <Typography.Text view='primary-large' weight='bold'>
                        Альфа Бизнес
                    </Typography.Text>
                </NavLink>

                <nav className='app-header__nav' aria-label='Разделы'>
                    {NAV.map((item) => (
                        <NavLink
                            key={item.to}
                            to={item.to}
                            end={item.end}
                            className={({ isActive }) =>
                                `app-header__nav-link${isActive ? ' app-header__nav-link_active' : ''}`
                            }
                        >
                            {({ isActive }) => (
                                <Typography.Text view='primary-medium' weight={isActive ? 'bold' : 'regular'}>
                                    {item.label}
                                </Typography.Text>
                            )}
                        </NavLink>
                    ))}
                </nav>

                <div className='app-header__actions'>
                    <div className='app-header__bell'>
                        <IconButton
                            icon={BellMIcon}
                            view='secondary'
                            size={40}
                            aria-label={hasNotifications ? 'Уведомления, есть новые' : 'Уведомления'}
                        />
                        {hasNotifications && <Indicator view='red' size={8} border={true} className='app-header__bell-dot' />}
                    </div>

                    <div className='app-header__user'>
                        <div className='app-header__user-text'>
                            <Typography.Text view='primary-small' weight='bold'>
                                {company?.short_name ?? me.full_name}
                            </Typography.Text>
                            <Typography.Text view='primary-small' color='secondary'>
                                {shortName(me.full_name)}
                                {company && ` · ${ROLE_LABELS[company.role]}`}
                            </Typography.Text>
                        </div>
                        <span className='app-header__avatar' aria-hidden='true'>
                            <Typography.Text view='primary-small' weight='bold'>
                                {initials(me.full_name)}
                            </Typography.Text>
                        </span>
                    </div>

                    <Button view='text' size={32} onClick={onLogout}>
                        Выйти
                    </Button>
                </div>
            </div>
        </header>
    );
}
