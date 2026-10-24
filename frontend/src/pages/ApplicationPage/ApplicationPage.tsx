import { useEffect, useRef, useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Indicator } from '@alfalab/core-components/indicator';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Status } from '@alfalab/core-components/status';
import { Tab, Tabs } from '@alfalab/core-components/tabs';
import { Typography } from '@alfalab/core-components/typography';
import { Link, Navigate, useParams, useSearchParams } from 'react-router-dom';

import { ApiError } from '../../api/client';
import { useApplication, useApplicationStatus, useProducts, useTimeline } from '../../api/queries';
import type { Application, ApplicationStatusView, Me } from '../../api/types';
import { STAGE_BADGES } from '../../shared/lib/dictionaries';
import { formatMoney, formatMonths, formatUpdated } from '../../shared/lib/format';
import { Breadcrumbs } from '../../shared/ui/Breadcrumbs/Breadcrumbs';
import { VisuallyHidden } from '../../shared/ui/VisuallyHidden/VisuallyHidden';
import { CancelDialog } from './components/CancelDialog';
import { EventList } from './components/EventList';
import { InfoCard } from './components/InfoCard';
import { ParamsCard } from './components/ParamsCard';
import { InfoRequestsStatus } from './status/InfoRequestsStatus';
import { OffersStatus } from './status/OffersStatus';
import { CompletedStatus } from './status/CompletedStatus';
import { ReviewStatus } from './status/ReviewStatus';
import { SignerStatus } from './status/SignerStatus';
import { StageStatus } from './status/StageStatus';
import { WaitingSignerStatus } from './status/WaitingSignerStatus';
import { DocumentsTab } from './tabs/DocumentsTab';
import { HistoryTab } from './tabs/HistoryTab';
import './ApplicationPage.css';

type TabId = 'status' | 'documents' | 'history';
const TABS: TabId[] = ['status', 'documents', 'history'];
const isTab = (v: string | null): v is TabId => TABS.includes(v as TabId);

/** Правая колонка: что за заявка, последние события, отзыв. */
function Aside({ app, status, onCancel }: { app: Application; status: ApplicationStatusView; onCancel: () => void }) {
    const timeline = useTimeline(app.id);
    const asking = status.client_stage === 'ACTION_REQUIRED';
    return (
        <aside className='application-page__aside'>
            {asking ? (
                <InfoCard title='Почему банк спрашивает'>
                    <Typography.Text view='primary-small' color='secondary'>
                        Мы сверяем данные заявки с другими сведениями о компании. Иногда для точного решения нужно немного больше
                        информации — это обычная часть рассмотрения.
                    </Typography.Text>
                </InfoCard>
            ) : (
                <>
                    <ParamsCard app={app} />
                    {timeline.data && timeline.data.length > 0 && (
                        <InfoCard title='Последние события'>
                            <EventList events={timeline.data.slice(0, 3)} activeFirst={status.client_stage === 'REVIEW'} />
                        </InfoCard>
                    )}
                </>
            )}
            {status.allowed_actions.includes('CANCEL') && (
                <Button view='outlined' size={48} block={true} onClick={onCancel}>
                    Отозвать заявку
                </Button>
            )}
        </aside>
    );
}

/** Этап сменился, пока страница открыта (ответили, выбрали предложение, пришло событие) — переводим фокус на новый этап. */
function useFocusOnStageChange(stage: ApplicationStatusView['client_stage']) {
    const ref = useRef<HTMLDivElement>(null);
    const previous = useRef(stage);
    useEffect(() => {
        if (previous.current !== stage) ref.current?.focus({ preventScroll: true });
        previous.current = stage;
    }, [stage]);
    return ref;
}

type StageProps = { app: Application; me: Me; status: ApplicationStatusView; onCancel: () => void };

function StatusTab({ app, me, status, onCancel }: StageProps) {
    const focusRef = useFocusOnStageChange(status.client_stage);
    return (
        <div ref={focusRef} tabIndex={-1} className='application-page__stage'>
            <StageContent app={app} me={me} status={status} onCancel={onCancel} />
        </div>
    );
}

function StageContent({ app, me, status, onCancel }: StageProps) {
    // на решении предложения занимают всю ширину, как в макете 09
    if (status.client_stage === 'OFFER') return <OffersStatus app={app} status={status} />;
    // подписание: директору — экран подписи (11), остальным — «ждём директора» (12)
    if (status.client_stage === 'SIGNING') {
        return status.signing?.awaiting_me ? (
            <SignerStatus app={app} me={me} status={status} />
        ) : (
            <WaitingSignerStatus app={app} status={status} />
        );
    }
    if (status.client_stage === 'DONE') return <CompletedStatus app={app} status={status} />;
    const main =
        status.client_stage === 'REVIEW' ? (
            <ReviewStatus status={status} />
        ) : status.client_stage === 'ACTION_REQUIRED' ? (
            <InfoRequestsStatus status={status} />
        ) : (
            <StageStatus status={status} />
        );
    return (
        <div className='application-page__columns'>
            <div className='application-page__main'>{main}</div>
            <Aside app={app} status={status} onCancel={onCancel} />
        </div>
    );
}

/** /applications/:id — заявка после отправки: статус (экраны 07–10), документы, история. */
export function ApplicationPage({ me }: { me: Me }) {
    const { applicationId } = useParams();
    const [params, setParams] = useSearchParams();
    const tabParam = params.get('tab');
    const tab: TabId = isTab(tabParam) ? tabParam : 'status';
    const application = useApplication(applicationId);
    const status = useApplicationStatus(applicationId);
    const products = useProducts();
    const [cancelling, setCancelling] = useState(false);

    const error = application.error ?? status.error;
    if (error && !(application.data && status.data)) {
        const notFound = error instanceof ApiError && error.status === 404;
        return (
            <div className='application-page'>
                <Plate view='negative' title={notFound ? 'Заявка не найдена' : 'Не удалось загрузить заявку'}>
                    <Button Component={Link} href='/' view='secondary' size={40}>
                        На главную
                    </Button>
                </Plate>
            </div>
        );
    }

    const app = application.data;
    const view = status.data;
    if (!app || !view) {
        return (
            <div className='application-page'>
                <Skeleton visible={true} className='application-page__skeleton' />
            </div>
        );
    }

    // черновик редактируют в анкете
    if (view.client_stage === 'FILLING') return <Navigate to={`/applications/${app.id}/edit`} replace={true} />;

    const productName = products.data?.find((p) => p.code === app.product_code)?.name ?? 'Кредит';
    const badge = STAGE_BADGES[view.client_stage];
    const meta = [
        app.number,
        app.requested && formatMonths(app.requested.term_months),
        app.submitted_at && `отправлена ${formatUpdated(app.submitted_at)}`,
    ]
        .filter(Boolean)
        .join(' · ');
    // бейдж — только пока заявка ждёт ответов; необязательные вопросы после этого не держат его
    const questions = view.client_stage === 'ACTION_REQUIRED' ? (view.open_info_requests ?? 0) : 0;

    return (
        <div className='application-page'>
            <Breadcrumbs items={[{ label: 'Кредиты', to: '/' }, { label: `Заявка ${app.number}` }]} />
            {/* смену этапа (в том числе по событию с сервера) озвучиваем */}
            <VisuallyHidden role='status'>{view.title}</VisuallyHidden>
            <div className='application-page__head'>
                <div className='application-page__titles'>
                    <Typography.TitleResponsive tag='h1' view='large' font='system'>
                        {productName}
                        {app.requested && ` · ${formatMoney(app.requested.amount)}`}
                    </Typography.TitleResponsive>
                    <Typography.Text view='primary-small' color='secondary'>
                        {meta}
                    </Typography.Text>
                </div>
                <Status view={badge.contrast ? 'contrast' : 'muted-alt'} color={badge.color} size={24} uppercase={false}>
                    {badge.label}
                </Status>
            </div>

            <Tabs
                selectedId={tab}
                onChange={(_, { selectedId }) => setParams(selectedId === 'status' ? {} : { tab: String(selectedId) }, { replace: true })}
                keepMounted={false}
            >
                <Tab
                    id='status'
                    title='Статус'
                    rightAddons={questions > 0 ? <Indicator value={questions} view='red' size={20} /> : undefined}
                >
                    <div className='application-page__panel'>
                        <StatusTab app={app} me={me} status={view} onCancel={() => setCancelling(true)} />
                    </div>
                </Tab>
                <Tab id='documents' title='Документы'>
                    <div className='application-page__panel'>
                        <DocumentsTab status={view} />
                    </div>
                </Tab>
                <Tab id='history' title='История'>
                    <div className='application-page__panel'>
                        <HistoryTab applicationId={app.id} />
                    </div>
                </Tab>
            </Tabs>

            {/* этап сменился и отозвать уже нельзя — окно закрываем, а не ждём 409 */}
            {cancelling && view.allowed_actions.includes('CANCEL') && (
                <CancelDialog applicationId={app.id} open={true} onClose={() => setCancelling(false)} />
            )}
        </div>
    );
}
