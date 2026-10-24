import { useMemo, useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Segment, SegmentedControl } from '@alfalab/core-components/segmented-control';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Typography } from '@alfalab/core-components/typography';

import { useMockLogin, useTestUsers } from '../../api/queries';
import type { UserRole } from '../../api/types';
import { shortName } from '../../shared/lib/format';
import { useRovingRadio } from '../../shared/lib/useRovingRadio';
import { Logo } from '../../shared/ui/Logo/Logo';
import { PersonaCard } from './components/PersonaCard';
import './LoginPage.css';

const ROLE_TABS: { id: UserRole; title: string }[] = [
    { id: 'CLIENT', title: 'Клиент' },
    { id: 'UNDERWRITER', title: 'Сотрудник банка' },
];

export function LoginPage() {
    const { data: users, isLoading, isError, refetch } = useTestUsers();
    const login = useMockLogin();
    const [role, setRole] = useState<UserRole>('CLIENT');
    const [selectedId, setSelectedId] = useState<string>();

    const visible = useMemo(() => (users ?? []).filter((u) => u.roles.includes(role)), [users, role]);
    // По умолчанию выбран первый пользователь вкладки.
    const selected = visible.find((u) => u.id === selectedId) ?? visible[0];

    const { ref: radiosRef, onKeyDown: onRadioKeyDown } = useRovingRadio(visible, (user) => setSelectedId(user.id));

    // Переход после входа делает LoginRoute: как только появляется токен, он уводит на домашнюю страницу роли.
    const handleLogin = () => {
        if (selected) login.mutate(selected.id);
    };

    return (
        <div className='login'>
            <aside className='login__promo'>
                <div className='login__brand'>
                    <Logo inverted={true} />
                    <Typography.Text view='primary-large' weight='bold' color='primary-inverted'>
                        Альфа Бизнес
                    </Typography.Text>
                </div>
                <div className='login__promo-text'>
                    <Typography.TitleResponsive tag='h1' view='xlarge' font='system' color='primary-inverted'>
                        Умный кредитный кабинет
                    </Typography.TitleResponsive>
                    <Typography.Text view='primary-large' tag='p' defaultMargins={false} color='primary-inverted'>
                        Подберите кредит под задачу бизнеса, подайте заявку за 10 минут и следите за решением онлайн
                    </Typography.Text>
                </div>
                <Typography.Text view='primary-small' color='secondary-inverted'>
                    Демо-стенд Альфа-Хак 2026 · синтетические данные
                </Typography.Text>
            </aside>

            <main className='login__content'>
                <div className='login__form'>
                    <div className='login__heading'>
                        <Typography.Title tag='h2' view='medium' font='system'>
                            Войти как тестовый пользователь
                        </Typography.Title>
                        <Typography.Text view='primary-medium' color='secondary'>
                            Каждый пользователь показывает свой сценарий. Пароль не нужен.
                        </Typography.Text>
                    </div>

                    <SegmentedControl className='login__roles' selectedId={role} onChange={(id) => setRole(id as UserRole)}>
                        {ROLE_TABS.map((tab) => (
                            <Segment key={tab.id} id={tab.id} title={tab.title} />
                        ))}
                    </SegmentedControl>

                    {isError && (
                        <Plate view='negative' title='Не удалось загрузить пользователей'>
                            <Button view='text' size={32} onClick={() => refetch()}>
                                Повторить
                            </Button>
                        </Plate>
                    )}

                    <div ref={radiosRef} className='login__personas' role='radiogroup' aria-label='Тестовый пользователь'>
                        {isLoading &&
                            [0, 1, 2].map((i) => <Skeleton key={i} visible={true} className='login__persona-skeleton' />)}
                        {visible.map((user, i) => (
                            <PersonaCard
                                key={user.id}
                                user={user}
                                checked={user.id === selected?.id}
                                onSelect={() => setSelectedId(user.id)}
                                onKeyDown={onRadioKeyDown(i)}
                            />
                        ))}
                    </div>

                    {login.isError && <Plate view='negative' title='Не получилось войти. Попробуйте ещё раз.' />}

                    <Button
                        view='primary'
                        size={56}
                        className='login__submit'
                        disabled={!selected}
                        loading={login.isPending}
                        onClick={handleLogin}
                    >
                        {selected ? `Войти как ${shortName(selected.full_name)}` : 'Выберите пользователя'}
                    </Button>
                </div>
            </main>
        </div>
    );
}
