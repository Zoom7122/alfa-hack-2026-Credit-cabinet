import { type ReactNode, useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Status } from '@alfalab/core-components/status';
import { Tab, Tabs } from '@alfalab/core-components/tabs';
import { Typography } from '@alfalab/core-components/typography';
import { Link, useParams, useSearchParams } from 'react-router-dom';

import { ApiError } from '../../api/client';
import { useBoAssign, useBoDecide, useBoDossier, useBoQueue, useProducts } from '../../api/queries';
import type { Me } from '../../api/types';
import { BO_STATUS_BADGES, GRADE_COLORS, slaLeft } from '../../shared/lib/backoffice';
import { formatMoney, formatMonths, shortName } from '../../shared/lib/format';
import { useNow } from '../../shared/lib/useNow';
import { Breadcrumbs } from '../../shared/ui/Breadcrumbs/Breadcrumbs';
import { EventList } from '../../shared/ui/EventList/EventList';
import { FormCard } from '../../shared/ui/FormCard/FormCard';
import { DecisionPanel, type DecisionMode } from './components/DecisionPanel';
import { BoDocumentsTab } from './tabs/BoDocumentsTab';
import { CompanyTab } from './tabs/CompanyTab';
import { InfoRequestsTab } from './tabs/InfoRequestsTab';
import { OverviewTab } from './tabs/OverviewTab';
import { ScoringTab } from './tabs/ScoringTab';
import { SourcesTab } from './tabs/SourcesTab';
import './DossierPage.css';

const TABS = ['overview', 'sources', 'scoring', 'documents', 'requests', 'history', 'company'] as const;
type TabId = (typeof TABS)[number];
const isTab = (v: string | null): v is TabId => TABS.includes(v as TabId);

/** Экран 17: досье заявки для андеррайтера; решение (экран 18) — в боковой панели. */
export function DossierPage({ me }: { me: Me }) {
    const { applicationId } = useParams();
    const [params, setParams] = useSearchParams();
    const tabParam = params.get('tab');
    const tab: TabId = isTab(tabParam) ? tabParam : 'overview';
    const dossier = useBoDossier(applicationId);
    const queue = useBoQueue();
    const products = useProducts();
    const assign = useBoAssign();
    // мутация решения живёт здесь: её ошибка (409) должна пережить закрытие панели
    const decide = useBoDecide(applicationId ?? '');
    const [decision, setDecision] = useState<DecisionMode>();
    const now = useNow();

    if (dossier.isError && !dossier.data) {
        const notFound = dossier.error instanceof ApiError && dossier.error.status === 404;
        return (
            <div className='dossier'>
                <Plate view='negative' title={notFound ? 'Заявка не найдена' : 'Не удалось загрузить досье'}>
                    <Button Component={Link} href='/backoffice' view='secondary' size={40}>
                        К очереди
                    </Button>
                </Plate>
            </div>
        );
    }
    const data = dossier.data;
    if (!data) {
        return (
            <div className='dossier'>
                <Skeleton visible={true} className='dossier__skeleton' />
            </div>
        );
    }

    const app = data.application;
    // исполнитель и SLA — в элементе очереди (BoApplicationListItem), в досье их нет
    const item = queue.data?.find((i) => i.id === app.id);
    const assignee = item?.assignee;
    const mine = assignee?.id === me.id;
    const manual = app.status === 'MANUAL_REVIEW';
    const foreign = Boolean(assignee) && !mine;
    // заявку решили (в том числе в другой вкладке) — панель решения больше не нужна
    if (decision && (!manual || foreign)) setDecision(undefined);
    const status = BO_STATUS_BADGES[app.status];
    const grade = data.score?.grade;
    const sla = manual ? slaLeft(item?.sla_due_at, now) : undefined;
    const productName = products.data?.find((p) => p.code === app.product_code)?.name ?? app.product_code;
    const meta = [
        app.number,
        productName,
        app.requested && `${formatMoney(app.requested.amount)} на ${formatMonths(app.requested.term_months)}`,
        sla && (sla.overdue ? 'SLA просрочен' : `SLA через ${sla.text}`),
        manual && (mine ? 'в работе у вас' : assignee ? `в работе у ${shortName(assignee.full_name)}` : 'не назначен'),
    ]
        .filter(Boolean)
        .join(' · ');

    /** Решение — только исполнителю: неназначенную заявку сначала берём в работу. */
    const openDecision = (mode: DecisionMode) => {
        decide.reset();
        if (mine) setDecision(mode);
        else assign.mutate(app.id, { onSuccess: () => setDecision(mode) });
    };

    const counts: Partial<Record<TabId, number>> = { sources: data.sources.length, documents: data.documents?.length ?? 0 };
    const titles: Record<TabId, string> = {
        overview: 'Обзор',
        sources: 'Источники',
        scoring: 'Скоринг',
        documents: 'Документы',
        requests: 'Уточнения',
        history: 'История',
        company: 'Данные компании',
    };

    const content: Record<TabId, ReactNode> = {
        overview: <OverviewTab dossier={data} />,
        sources: <SourcesTab applicationId={app.id} sources={data.sources} />,
        scoring: <ScoringTab score={data.score} />,
        documents: <BoDocumentsTab documents={data.documents ?? []} />,
        requests: <InfoRequestsTab requests={data.info_requests ?? []} />,
        history: (
            <FormCard title='История заявки'>
                <EventList events={data.timeline} showStaff={true} />
            </FormCard>
        ),
        company: <CompanyTab applicant={data.applicant} />,
    };

    return (
        <div className='dossier'>
            <Breadcrumbs items={[{ label: 'Очередь', to: '/backoffice' }, { label: app.number }]} />
            <div className='dossier__head'>
                <div className='dossier__titles'>
                    <div className='dossier__title'>
                        <Typography.TitleResponsive tag='h1' view='large' font='system'>
                            {data.applicant.short_name ?? app.applicant.short_name}
                        </Typography.TitleResponsive>
                        {status && (
                            <Status view='muted-alt' color={status.color} size={24} uppercase={false}>
                                {status.label}
                            </Status>
                        )}
                        {grade && (
                            <Status view='muted-alt' color={GRADE_COLORS[grade]} size={24} uppercase={false}>
                                Грейд {grade}
                            </Status>
                        )}
                    </div>
                    <Typography.Text view='primary-small' color='secondary'>
                        {meta}
                    </Typography.Text>
                </div>
                {manual && !foreign && (
                    <div className='dossier__actions'>
                        {!mine && (
                            <Button view='secondary' size={48} onClick={() => assign.mutate(app.id)} loading={assign.isPending}>
                                Взять в работу
                            </Button>
                        )}
                        <Button view='secondary' size={48} onClick={() => openDecision('REQUEST_INFO')} disabled={assign.isPending}>
                            Запросить данные
                        </Button>
                        <Button view='outlined' size={48} onClick={() => openDecision('REJECT')} disabled={assign.isPending}>
                            Отказать
                        </Button>
                        <Button view='primary' size={48} onClick={() => openDecision('APPROVE')} disabled={assign.isPending}>
                            Одобрить
                        </Button>
                    </div>
                )}
            </div>
            {manual && foreign && assignee && (
                <Plate view='common' title={`Заявка в работе у ${shortName(assignee.full_name)}`}>
                    Решение принимает исполнитель. Если нужно переназначить — обратитесь к руководителю.
                </Plate>
            )}
            {assign.error && <Plate view='negative' title='Не получилось взять в работу'>{assign.error.message}</Plate>}
            {!decision && decide.error && (
                <Plate view='negative' title='Решение не сохранено'>
                    {decide.error.message}
                </Plate>
            )}

            <Tabs
                selectedId={tab}
                onChange={(_, { selectedId }) => setParams(selectedId === 'overview' ? {} : { tab: String(selectedId) }, { replace: true })}
                keepMounted={false}
            >
                {TABS.map((id) => (
                    <Tab key={id} id={id} title={counts[id] !== undefined ? `${titles[id]} · ${counts[id]}` : titles[id]}>
                        <div className='dossier__panel'>{content[id]}</div>
                    </Tab>
                ))}
            </Tabs>

            {decision && <DecisionPanel dossier={data} mode={decision} decide={decide} onModeChange={setDecision} onClose={() => setDecision(undefined)} />}
        </div>
    );
}
