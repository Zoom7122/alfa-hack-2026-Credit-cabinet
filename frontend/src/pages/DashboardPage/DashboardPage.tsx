import { useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Tag } from '@alfalab/core-components/tag';
import { Typography } from '@alfalab/core-components/typography';
import { PlusMIcon } from '@alfalab/icons-glyph/PlusMIcon';
import { Link } from 'react-router-dom';

import { useCollectionsOf, useCollectorApplicants, useCollectorApplications } from '../../api/collector/queries';
import { useApplicants, useApplications, useApplicationStatuses, useProducts } from '../../api/queries';
import type { ApplicationStatusView, ApplicationSummary, CompanyAccess, Me } from '../../api/types';
import { COLLECTION_FAILED, COLLECTOR_STATUS, collectorSummary } from '../../shared/lib/collector';
import { ACTIVE_STAGES, STAGES_WITH_TASKS } from '../../shared/lib/dictionaries';
import { formatDay, pluralize, splitName } from '../../shared/lib/format';
import { readDraft } from '../WizardPage/draft';
import { ApplicationRow } from './components/ApplicationRow';
import { CompanyCard } from './components/CompanyCard';
import { TaskCard, type Task } from './components/TaskCard';
import './DashboardPage.css';

type Props = { me: Me; company?: CompanyAccess };
type Filter = 'active' | 'closed';

/** Задачи «Ждут вашего действия» из лёгких статусов заявок. */
function buildTasks(
    apps: ApplicationSummary[],
    statuses: Map<string, ApplicationStatusView>,
    productName: (code: string) => string,
): Task[] {
    const tasks: Task[] = [];
    for (const app of apps) {
        const st = statuses.get(app.id);
        if (!st) continue;
        const about = `Заявка ${app.number} · ${productName(app.product_code)}`;
        const until = st.deadline_at ? ` · до ${formatDay(st.deadline_at)}` : '';

        if (st.client_stage === 'SIGNING' && st.signing?.awaiting_me) {
            tasks.push({ id: app.id, kind: 'sign', title: 'Подпишите кредитный договор', subtitle: about, action: 'Подписать' });
        } else if (st.client_stage === 'ACTION_REQUIRED' && st.open_info_requests) {
            const n = st.open_info_requests;
            tasks.push({
                id: app.id,
                kind: 'info',
                title: `Банк задал ${n} ${pluralize(n, 'вопрос', 'вопроса', 'вопросов')}`,
                subtitle: `${about}${until}`,
                action: 'Ответить',
            });
        } else if (st.client_stage === 'OFFER') {
            tasks.push({ id: app.id, kind: 'offer', title: 'Решение готово — выберите предложение', subtitle: `${about}${until}`, action: 'Посмотреть' });
        }
    }
    return tasks;
}

export function DashboardPage({ me, company }: Props) {
    const [filter, setFilter] = useState<Filter>('active');
    const products = useProducts();
    const applicants = useApplicants();
    const applications = useApplications({ applicantId: company?.applicant_id });

    // заявки data-collector — всех компаний пользователя (его и коллег), с сервера
    const collector = useCollectorApplications();
    // черновик продолжает только автор: чужой (если сервис его вдруг отдаст) не показываем
    const collectorItems = (collector.data ?? []).filter((a) => a.status !== 'DRAFT' || a.representative?.id === me.id);
    const collections = useCollectionsOf(collectorItems);
    const collectorApplicants = useCollectorApplicants();

    // черновики анкеты теперь в data-collector — демо-черновики моков не показываем
    const all = (applications.data ?? []).filter((a) => a.client_stage !== 'FILLING');
    const fromCollector = collectorItems.map((app) => ({ app, summary: collectorSummary(app, app.id ? readDraft(me.id, app.id) : undefined) }));
    const active = all.filter((a) => ACTIVE_STAGES.includes(a.client_stage));
    const closed = all.filter((a) => !ACTIVE_STAGES.includes(a.client_stage));
    // у data-collector все заявки пока активные: дальше VERIFYING он статусы не ведёт
    const shownCollector = filter === 'active' ? fromCollector : [];
    const shown = filter === 'active' ? active : closed;

    const statuses = useApplicationStatuses(all.filter((a) => STAGES_WITH_TASKS.includes(a.client_stage)));
    const productName = (code: string) => products.data?.find((p) => p.code === code)?.name ?? 'Кредит';
    const tasks = buildTasks(active, statuses, productName);
    // реквизиты компании — из data-collector (ЕГРЮЛ после сбора), иначе из моков экранов 07–15
    const profile = collectorApplicants.data?.find((a) => a.inn === company?.inn);
    const legacy = applicants.data?.find((a) => a.id === company?.applicant_id || a.inn === company?.inn);
    // у data-collector реквизиты появляются после сбора; до него — из моков
    const applicant = profile?.ogrn ? profile : (legacy ?? profile);
    const { first, middle } = splitName(me.full_name);

    const filters: { id: Filter; label: string }[] = [
        { id: 'active', label: `Активные · ${active.length + fromCollector.length}` },
        { id: 'closed', label: `Завершённые · ${closed.length}` },
    ];

    return (
        <div className='dashboard'>
            <div className='dashboard__head'>
                <Typography.TitleResponsive tag='h1' view='large' font='system'>
                    Добрый день, {[first, middle].filter(Boolean).join(' ')}
                </Typography.TitleResponsive>
                <Button Component={Link} href='/products' view='accent' size={56} leftAddons={<PlusMIcon />}>
                    Новая заявка
                </Button>
            </div>

            {!company && (
                <Plate view='attention' title='У вас пока нет компаний'>
                    Добавьте компанию по ИНН, чтобы подать заявку на кредит.
                </Plate>
            )}

            {tasks.length > 0 && (
                <section className='dashboard__tasks' aria-labelledby='dashboard-tasks-title'>
                    <Typography.Text id='dashboard-tasks-title' tag='div' view='caps' color='tertiary' weight='bold'>
                        Ждут вашего действия
                    </Typography.Text>
                    <div className='dashboard__tasks-grid'>
                        {tasks.map((task) => (
                            <TaskCard key={task.id} task={task} />
                        ))}
                    </div>
                </section>
            )}

            <div className='dashboard__columns'>
                <section className='dashboard__card dashboard__applications' aria-labelledby='dashboard-apps-title'>
                    <div className='dashboard__card-head'>
                        <Typography.Title id='dashboard-apps-title' tag='h2' view='small' font='system'>
                            Мои заявки
                        </Typography.Title>
                        <div className='dashboard__filters' role='group' aria-label='Какие заявки показать'>
                            {filters.map((f) => (
                                <Tag
                                    key={f.id}
                                    size={40}
                                    checked={filter === f.id}
                                    aria-pressed={filter === f.id}
                                    onClick={() => setFilter(f.id)}
                                >
                                    {f.label}
                                </Tag>
                            ))}
                        </div>
                    </div>

                    {applications.isLoading && (
                        <div className='dashboard__list'>
                            {[0, 1, 2].map((i) => (
                                <Skeleton key={i} visible={true} className='dashboard__row-skeleton' />
                            ))}
                        </div>
                    )}

                    {applications.isError && (
                        <Plate view='negative' title='Не удалось загрузить заявки'>
                            <Button view='text' size={32} onClick={() => applications.refetch()}>
                                Повторить
                            </Button>
                        </Plate>
                    )}

                    {collector.isError && (
                        <Plate view='attention' title='Не все заявки удалось загрузить'>
                            Сервис сбора данных не ответил — часть заявок появится позже.
                        </Plate>
                    )}

                    {applications.isSuccess && !collector.isPending && shown.length + shownCollector.length === 0 && (
                        <div className='dashboard__empty'>
                            <Typography.Text view='primary-medium' color='secondary'>
                                {filter === 'active' ? 'Активных заявок нет' : 'Завершённых заявок пока нет'}
                            </Typography.Text>
                        </div>
                    )}

                    {shown.length + shownCollector.length > 0 && (
                        <ul className='dashboard__list'>
                            {shownCollector.map(({ app, summary }, i) => {
                                const failed = collections.get(app.collection_task_id ?? '')?.status === 'FAILED';
                                const view = failed ? COLLECTION_FAILED : COLLECTOR_STATUS[app.status ?? 'DRAFT'];
                                return (
                                    <li key={summary.id} className={`dashboard__list-item${i > 0 ? ' dashboard__list-item_divided' : ''}`}>
                                        <ApplicationRow
                                            application={summary}
                                            productName={productName(summary.product_code)}
                                            badge={view}
                                            hint={{ text: view.hint, link: app.status === 'DRAFT' }}
                                            company={summary.applicant.inn === company?.inn ? undefined : summary.applicant.short_name}
                                        />
                                    </li>
                                );
                            })}
                            {shown.map((app, i) => (
                                <li key={app.id} className={`dashboard__list-item${i + shownCollector.length > 0 ? ' dashboard__list-item_divided' : ''}`}>
                                    <ApplicationRow
                                        application={app}
                                        productName={productName(app.product_code)}
                                        status={statuses.get(app.id)}
                                    />
                                </li>
                            ))}
                        </ul>
                    )}
                </section>

                <aside className='dashboard__aside'>
                    {company && <CompanyCard company={company} applicant={applicant} loading={collectorApplicants.isLoading && applicants.isLoading} />}

                    <section className='dashboard__card dashboard__card_size_s'>
                        <Typography.Title tag='h2' view='xsmall' font='system'>
                            Не знаете, какой кредит выбрать?
                        </Typography.Title>
                        <Typography.Text view='primary-small' color='secondary'>
                            Ответьте на 2 вопроса — подскажем продукт и покажем примерный платёж
                        </Typography.Text>
                        <Button Component={Link} href='/products' view='secondary' size={48} block={true}>
                            Подобрать кредит
                        </Button>
                    </section>
                </aside>
            </div>
        </div>
    );
}
