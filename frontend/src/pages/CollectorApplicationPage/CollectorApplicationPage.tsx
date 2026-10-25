import type { ReactNode } from 'react';
import { Button } from '@alfalab/core-components/button';
import { FileUploadItem } from '@alfalab/core-components/file-upload-item';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Status } from '@alfalab/core-components/status';
import { Typography } from '@alfalab/core-components/typography';
import { Link, Navigate, useParams } from 'react-router-dom';

import { ApiError } from '../../api/client';
import { useCollection, useCollectorApplication } from '../../api/collector/queries';
import { COLLECTOR_ROLE_LABELS, type CollectorApplication, type CollectorDocument } from '../../api/collector/types';
import { useProducts } from '../../api/queries';
import type { ClientStage, DocumentType, StageItem } from '../../api/types';
import { COLLECTION_FAILED, COLLECTOR_STATUS } from '../../shared/lib/collector';
import { DOCUMENT_LABELS, PURPOSE_LABELS } from '../../shared/lib/dictionaries';
import { formatMoney, formatMonths, formatUpdated } from '../../shared/lib/format';
import { Breadcrumbs } from '../../shared/ui/Breadcrumbs/Breadcrumbs';
import { FormCard } from '../../shared/ui/FormCard/FormCard';
import { InfoCard } from '../../shared/ui/InfoCard/InfoCard';
import { SummaryList } from '../../shared/ui/SummaryList/SummaryList';
import { VisuallyHidden } from '../../shared/ui/VisuallyHidden/VisuallyHidden';
import { StageBar } from '../ApplicationPage/components/StageBar';
import { CollectionCard } from './components/CollectionCard';
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

const SIGNATURE_TEXT: Record<NonNullable<CollectorDocument['signature_state']>, string | undefined> = {
    NOT_SIGNED: undefined,
    PENDING: 'проверим подпись',
    DONE: 'подпись проверена',
    FAILED: 'подпись не удалось проверить',
};
const RECOGNITION_TEXT: Record<NonNullable<CollectorDocument['recognition_state']>, string | undefined> = {
    NOT_APPLICABLE: undefined,
    PENDING: 'распознаем',
    PROCESSING: 'распознаём…',
    DONE: 'распознали',
    FAILED: 'проверим вручную',
};

function DocumentsCard({ documents }: { documents: CollectorDocument[] }) {
    if (!documents.length) return null;
    return (
        <FormCard title='Документы'>
            <ul className='collector-application__documents' aria-label='Документы заявки'>
                {documents.map((d) => {
                    const signedName = d.signed_document_id && documents.find((x) => x.id === d.signed_document_id)?.file_name;
                    const note = [
                        DOCUMENT_LABELS[(d.type ?? 'OTHER') as DocumentType],
                        signedName && `подпись к ${signedName}`,
                        d.signature_state && SIGNATURE_TEXT[d.signature_state],
                        d.recognition_state && RECOGNITION_TEXT[d.recognition_state],
                    ]
                        .filter(Boolean)
                        .join(' · ');
                    return (
                        <li key={d.id} className='collector-application__document'>
                            <FileUploadItem
                                id={d.id}
                                title={d.file_name}
                                size={d.size_bytes}
                                uploadStatus='UPLOADED'
                                showDelete={false}
                            >
                                <FileUploadItem.StatusControl />
                                <FileUploadItem.Content />
                            </FileUploadItem>
                            {note && (
                                <Typography.Text view='primary-small' color='secondary'>
                                    {note}
                                </Typography.Text>
                            )}
                        </li>
                    );
                })}
            </ul>
        </FormCard>
    );
}

const rowsOf = (items: [string, ReactNode][]) => items.filter(([, value]) => value).map(([label, value]) => ({ label, value }));

/** /applications/:id для заявок data-collector: статус сбора, итог по системам, документы. */
export function CollectorApplicationPage() {
    const { applicationId } = useParams();
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
    const meta = [app.number, app.requested?.term_months && formatMonths(app.requested.term_months), app.submitted_at && `отправлена ${formatUpdated(app.submitted_at)}`]
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

            <div className='collector-application__columns'>
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
                    <DocumentsCard documents={app.documents ?? []} />
                </div>

                <aside className='collector-application__aside'>
                    <InfoCard title='Компания'>
                        <SummaryList
                            plain={true}
                            rows={rowsOf([
                                ['Название', company?.name],
                                ['ИНН', company?.inn],
                                [company?.type === 'SOLE_PROPRIETOR' ? 'ОГРНИП' : 'ОГРН', company?.ogrn ?? (app.status === 'VERIFYING' ? 'нет в ЕГРЮЛ' : 'после сбора данных')],
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
        </div>
    );
}
