import { lazy, type ReactNode, Suspense } from 'react';
import { Spinner } from '@alfalab/core-components/spinner';
import { QueryClient, QueryClientProvider, useQueryClient } from '@tanstack/react-query';
import { BrowserRouter, Navigate, Outlet, Route, Routes, useOutletContext } from 'react-router-dom';

import { ApiError } from '../api/client';
import { useLiveUpdates } from '../api/live';
import { queryKeys } from '../api/queries';
import type { Me } from '../api/types';
import { DashboardPage } from '../pages/DashboardPage/DashboardPage';
import { LoginPage } from '../pages/LoginPage/LoginPage';
import { NotReadyPage } from '../pages/NotReadyPage/NotReadyPage';
import { AppHeader } from '../shared/ui/AppHeader/AppHeader';
import { RequireAuth } from './RequireAuth';
import { type AuthContext, homeFor, useLogout, useToken } from './useAuth';
import './App.css';

const queryClient = new QueryClient({
    defaultOptions: {
        queries: {
            refetchOnWindowFocus: false,
            retry: (failureCount, error) => !(error instanceof ApiError && error.status < 500) && failureCount < 1,
        },
    },
});

// формы с тяжёлыми компонентами кита грузим отдельным чанком
const ProductsPage = lazy(() => import('../pages/ProductsPage/ProductsPage').then((m) => ({ default: m.ProductsPage })));
const NewApplicationPage = lazy(() => import('../pages/WizardPage/WizardPage').then((m) => ({ default: m.NewApplicationPage })));
const ApplicationPage = lazy(() =>
    import('../pages/ApplicationPage/ApplicationPage').then((m) => ({ default: m.ApplicationPage })),
);
const WizardPage = lazy(() => import('../pages/WizardPage/WizardPage').then((m) => ({ default: m.WizardPage })));

function Lazy({ children }: { children: ReactNode }) {
    return (
        <Suspense
            fallback={
                <div className='app__loading'>
                    <Spinner visible={true} preset={48} />
                </div>
            }
        >
            {children}
        </Suspense>
    );
}

/** Кабинет клиента: шапка + страница. */
function ClientLayout() {
    const { me } = useOutletContext<AuthContext>();
    const logout = useLogout();
    useLiveUpdates(useToken());
    // TODO: переключатель компаний, когда у пользователя их несколько
    const company = me.companies[0];

    return (
        <div className='app'>
            <AppHeader me={me} company={company} hasNotifications={Boolean(me.pending_signatures?.length)} onLogout={logout} />
            <main className='app__content'>
                <Outlet context={{ me } satisfies AuthContext} />
            </main>
        </div>
    );
}

function DashboardRoute() {
    const { me } = useOutletContext<AuthContext>();
    return <DashboardPage me={me} company={me.companies[0]} />;
}

function ProductsRoute() {
    const { me } = useOutletContext<AuthContext>();
    return (
        <Lazy>
            <ProductsPage company={me.companies[0]} />
        </Lazy>
    );
}

function NewApplicationRoute() {
    const { me } = useOutletContext<AuthContext>();
    return (
        <Lazy>
            <NewApplicationPage me={me} />
        </Lazy>
    );
}

function WizardRoute() {
    const { me } = useOutletContext<AuthContext>();
    return (
        <Lazy>
            <WizardPage me={me} />
        </Lazy>
    );
}

/** Залогиненного сразу отправляем на его домашнюю страницу — единственное место редиректа после входа. */
function LoginRoute() {
    const token = useToken();
    const qc = useQueryClient();
    if (!token) return <LoginPage />;
    const me = qc.getQueryData<Me>(queryKeys.me);
    return <Navigate to={me ? homeFor(me) : '/'} replace={true} />;
}

export function App() {
    return (
        <QueryClientProvider client={queryClient}>
            <BrowserRouter>
                <Routes>
                    <Route path='/login' element={<LoginRoute />} />
                    <Route element={<RequireAuth role='CLIENT' />}>
                        <Route element={<ClientLayout />}>
                            <Route index={true} element={<DashboardRoute />} />
                            <Route path='products' element={<ProductsRoute />} />
                            <Route
                                path='applications/:applicationId'
                                element={
                                    <Lazy>
                                        <ApplicationPage />
                                    </Lazy>
                                }
                            />
                            <Route path='*' element={<NotReadyPage />} />
                        </Route>
                        {/* анкета — без шапки кабинета */}
                        <Route path='/applications/new' element={<NewApplicationRoute />} />
                        <Route path='/applications/:applicationId/edit/:step?' element={<WizardRoute />} />
                    </Route>
                    <Route path='/backoffice' element={<RequireAuth role='UNDERWRITER' />}>
                        <Route path='*' element={<NotReadyPage standalone={true} />} />
                        <Route index={true} element={<NotReadyPage standalone={true} />} />
                    </Route>
                </Routes>
            </BrowserRouter>
        </QueryClientProvider>
    );
}
