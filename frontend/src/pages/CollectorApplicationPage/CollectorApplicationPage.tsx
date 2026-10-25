import type { ReactNode } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Status } from '@alfalab/core-components/status';
import { Tab, Tabs } from '@alfalab/core-components/tabs';
import { Typography } from '@alfalab/core-components/typography';
import { Link, Navigate, useParams, useSearchParams } from 'react-router-dom';

import { ApiError } from '../../api/client';
import { useCollection, useCollectorApplication, useCollectorTimeline } from '../../api/collector/queries';
import { COLLECTOR_ROLE_LABELS, type CollectorApplication } from '../../api/collector/types';
import { useProducts } from '../../api/queries';
import type { ClientStage, StageItem, TimelineEvent } from '../../api/types';
import { COLLECTION_FAILED, COLLECTOR_STATUS } from '../../shared/lib/collector';
import { PURPOSE_LABELS } from '../../shared/lib/dictionaries';
import { formatMoney, formatMonths, formatUpdated } from '../../shared/lib/format';
import { Breadcrumbs } from '../../shared/ui/Breadcrumbs/Breadcrumbs';
import { EventList } from '../../shared/ui/EventList/EventList';
import { FormCard } from '../../shared/ui/FormCard/FormCard';
import { InfoCard } from '../../shared/ui/InfoCard/InfoCard';
import { SummaryList } from '../../shared/ui/SummaryList/SummaryList';
import { VisuallyHidden } from '../../shared/ui/VisuallyHidden/VisuallyHidden';
import { StageBar } from '../ApplicationPage/components/StageBar';
import { CollectionCard } from './components/CollectionCard';
import { CollectorDocumentsTab } from './tabs/CollectorDocumentsTab';
import './CollectorApplicationPage.css';

/** Этапы, которые видны по data-collector: анкета → сбор данных → проверка банком (дальше сервисов пока нет). */
function stagesOf(app: CollectorApplication, failed: boolean): StageItem[] {
    const collected = app.status === 'VERIFYING' && !failed;
    const step = (code: ClientStage, title: string, state: StageItem['state']): StageItem => ({ code, title, state });
    return [
        step('FILLING', 'Анкета', 'DONE'),
        step('REVIEW', 'Сбор данных', collected ? 'DONE' : 'CURRENT'),
        step('OFFER', 'Проверка банком', collected ? 'CURRENT' : 'UPCOMING'),
        step('DONE', 'Решение', 'UPCOMING'),
    ];
}

const rowsOf = (items: [string, ReactNode][]) => items.filter(([, value]) => value).map(([label, value]) => ({ label, value }));

/** /applications/:id для заявок data-collector: статус сбора, итог по системам, документы. */
type TabId = 'status' | 'documents' | 'history';
const isTab = (v: string | null): v is TabId => v === 'status' || v === 'documents' || v === 'history';

export function CollectorApplicationPage() {
    const { applicationId } = useParams();
    const [params, setParams] = useSearchParams();
    const tabParam = params.get('tab');
    const tab: TabId = isTab(tabParam) ? tabParam : 'status';
    const application = useCollectorApplication(applicationId);
    const collection = useCollection(application.data?.collection_task_id);
    const products = useProducts();

    if (application.isError && !application.data) {
        const notFound = application.error instanceof ApiError && application.error.status === 404;
        return (
            <div className='collector-application'>
                <Plate view='negative' title={notFound ? 'Заявка не найдена' : 'Не удалось загрузить заявку'}>
                    <Button Component={Link} href='/' view='secondary' size={40}>
                        На главную
                    </Button>
                </Plate>
            </div>
        );
    }
    const app = application.data;
    if (!app) {
        return (
            <div className='collector-application'>
                <Skeleton visible={true} className='collector-application__skeleton' />
            </div>
        );
    }
    // анкету ещё не отправили — дозаполнять
    if (app.status === 'DRAFT') return <Navigate to={`/applications/${app.id}/edit`} replace={true} />;

    const failed = collection.data?.status === 'FAILED';
    const view = failed ? COLLECTION_FAILED : COLLECTOR_STATUS[app.status ?? 'SUBMITTED'];
    const productName = products.data?.find((p) => p.code === app.product_code)?.name ?? 'Кредит';
    const company = app.applicant;
    const rep = app.representative;
    const meta = [
        app.number,
        app.requested?.term_months && formatMonths(app.requested.term_months),
        app.submitted_at && `отправлена ${formatUpdated(app.submitted_at)}`,
    ]
        .filter(Boolean)
        .join(' · ');

    return (
        <div className='collector-application'>
            <Breadcrumbs items={[{ label: 'Кредиты', to: '/' }, { label: `Заявка ${app.number}` }]} />
            {/* смену статуса по опросу озвучиваем */}
            <VisuallyHidden role='status'>{view.title}</VisuallyHidden>
            <div className='collector-application__head'>
                <div className='collector-application__titles'>
                    <Typography.TitleResponsive tag='h1' view='large' font='system'>
                        {productName}
                        {app.requested?.amount ? ` · ${formatMoney(app.requested.amount)}` : ''}
                    </Typography.TitleResponsive>
                    <Typography.Text view='primary-small' color='secondary'>
                        {meta}
                    </Typography.Text>
                </div>
                <Status view='muted-alt' color={view.color} size={24} uppercase={false}>
                    {view.label}
                </Status>
            </div>

            <Tabs
                selectedId={tab}
                onChange={(_, { selectedId }) => setParams(selectedId === 'status' ? {} : { tab: String(selectedId) }, { replace: true })}
                keepMounted={false}
            >
                <Tab id='status' title='Статус'>
                    <div className='collector-application__columns collector-application__panel'>
                        <div className='collector-application__main'>
                            <FormCard>
                                <StageBar stages={stagesOf(app, failed)} clientStage='REVIEW' />
                                <div className='collector-application__state'>
                                    <Typography.Title tag='h2' view='small' font='system'>
                                        {view.title}
                                    </Typography.Title>
                                    <Typography.Text view='primary-medium' color='secondary'>
                                        {view.subtitle}
                                    </Typography.Text>
                                </div>
                            </FormCard>
                            <CollectionCard taskId={app.collection_task_id} />
                        </div>

                        <aside className='collector-application__aside'>
                            <InfoCard title='Компания'>
                                <SummaryList
                                    plain={true}
                                    rows={rowsOf([
                                        ['Название', company?.name],
                                        ['ИНН', company?.inn],
                                        [
                                            company?.type === 'SOLE_PROPRIETOR' ? 'ОГРНИП' : 'ОГРН',
                                            company?.ogrn ?? (app.status === 'VERIFYING' ? 'нет в ЕГРЮЛ' : 'после сбора данных'),
                                        ],
                                    ])}
                                />
                            </InfoCard>
                            <InfoCard title='Заявка'>
                                <SummaryList
                                    plain={true}
                                    rows={rowsOf([
                                        ['Сумма', app.requested?.amount ? formatMoney(app.requested.amount) : undefined],
                                        ['Срок', app.requested?.term_months ? formatMonths(app.requested.term_months) : undefined],
                                        ['Цель', app.purpose?.code && PURPOSE_LABELS[app.purpose.code]],
                                        ['Оформляет', rep?.full_name],
                                        ['Роль', rep?.role && COLLECTOR_ROLE_LABELS[rep.role]],
                                        ['Подписывает договор', rep?.can_sign ? 'сам' : 'по доверенности'],
                                    ])}
                                />
                            </InfoCard>
                        </aside>
                    </div>
                </Tab>
                <Tab id='documents' title={`Документы · ${app.documents?.length ?? 0}`}>
                    <div className='collector-application__panel'>
                        <CollectorDocumentsTab applicationId={app.id ?? ''} documents={app.documents ?? []} />
                    </div>
                </Tab>
                <Tab id='history' title='История'>
                    <div className='collector-application__panel'>
                        <HistoryTab app={app} />
                    </div>
                </Tab>
            </Tabs>
        </div>
    );
}

/** История заявки — журнал data-collector (application_events). */
function HistoryTab({ app }: { app: CollectorApplication }) {
    const timeline = useCollectorTimeline(app);
    if (timeline.isPending) return <Skeleton visible={true} className='collector-application__skeleton' />;
    if (timeline.isError) return <Plate view='negative' title='Не удалось загрузить историю' />;
    // EventList кита кабинета ждёт TimelineEvent контракта: тип для неё — только иконка, текст — message
    const events: TimelineEvent[] = timeline.data.map((e) => ({
        id: e.id,
        at: e.at,
        type: e.type.startsWith('document.') ? 'DOCUMENT_UPLOADED' : 'STATUS_CHANGED',
        message: e.message,
        actor_kind: e.actor_kind,
    }));
    return (
        <FormCard title='История заявки'>
            <EventList events={events} />
        </FormCard>
    );
}
