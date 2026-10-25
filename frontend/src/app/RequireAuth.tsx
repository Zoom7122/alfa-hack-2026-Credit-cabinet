import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Spinner } from '@alfalab/core-components/spinner';
import { Navigate, Outlet, useLocation } from 'react-router-dom';

import { isUnauthorized } from '../api/client';
import { useMe } from '../api/queries';
import type { UserRole } from '../api/types';
import { type AuthContext, homeFor, useLogout, useToken } from './useAuth';
import './RequireAuth.css';

/**
 * Пускает дальше только залогиненного пользователя с нужной ролью.
 * На вход уводит только без токена или при 401 — при остальных ошибках /me показывает плашку,
 * иначе получается петля /login ↔ / (токен есть, а /me падает).
 */
export function RequireAuth({ role }: { role: UserRole }) {
    const token = useToken();
    const location = useLocation();
    const logout = useLogout();
    const me = useMe(Boolean(token));

    // при 401 токен уже сброшен в useMe, здесь только уводим на вход
    if (!token || isUnauthorized(me.error)) {
        return <Navigate to='/login' replace={true} state={{ from: location.pathname }} />;
    }

    if (me.isError) {
        return (
            <div className='auth-state'>
                <Plate view='negative' title='Не удалось загрузить профиль' className='auth-state__plate'>
                    <div className='auth-state__actions'>
                        <Button view='primary' size={40} onClick={() => me.refetch()} loading={me.isFetching}>
                            Повторить
                        </Button>
                        <Button view='secondary' size={40} onClick={logout}>
                            Выйти
                        </Button>
                    </div>
                </Plate>
            </div>
        );
    }

    if (!me.data) {
        return (
            <div className='auth-state'>
                <Spinner visible={true} preset={48} />
            </div>
        );
    }

    if (!me.data.roles.includes(role)) {
        return <Navigate to={homeFor(me.data)} replace={true} />;
    }

    return <Outlet context={{ me: me.data } satisfies AuthContext} />;
}
