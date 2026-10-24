import { useState } from 'react';
import { Plate } from '@alfalab/core-components/plate';
import { Select } from '@alfalab/core-components/select';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Typography } from '@alfalab/core-components/typography';

import { useDocuments } from '../../../api/queries';
import type { ApplicationStatusView, DocumentType } from '../../../api/types';
import { DOCUMENT_LABELS } from '../../../shared/lib/dictionaries';
import { DocumentUploader } from '../../../shared/ui/DocumentUploader/DocumentUploader';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import './DocumentsTab.css';

/** Что клиент может загрузить сам (подпись и сгенерированные банком — не здесь). */
const UPLOADABLE = (Object.keys(DOCUMENT_LABELS) as DocumentType[]).filter((t) => t !== 'SIGNATURE' && t !== 'LOAN_AGREEMENT_DRAFT');

/** Удалять документ можно только в черновике или до ответа на уточнение (deleteDocument в openapi.yaml). */
const DELETABLE_IN = new Set(['DRAFT', 'WAITING_FOR_INFO']);

/** Вкладка «Документы»: всё, что приложено к заявке; догрузить — пока статус позволяет. */
export function DocumentsTab({ status }: { status: ApplicationStatusView }) {
    const documents = useDocuments(status.id);
    const [type, setType] = useState<DocumentType>('OTHER');
    const editable = status.allowed_actions.includes('UPLOAD_DOCUMENT');
    const docs = documents.data ?? [];

    return (
        <FormCard
            title='Документы заявки'
            subtitle={editable ? 'Можно догрузить, пока банк рассматривает заявку' : undefined}
            className='documents-tab'
        >
            {documents.isLoading && <Skeleton visible={true} className='documents-tab__skeleton' />}
            {documents.isError && <Plate view='negative' title='Не удалось загрузить документы' />}
            {editable && (
                <Select
                    label='Тип документа'
                    size={56}
                    block={true}
                    options={UPLOADABLE.map((t) => ({ key: t, content: DOCUMENT_LABELS[t] }))}
                    selected={type}
                    onChange={({ selected }) => selected && setType(selected.key as DocumentType)}
                />
            )}
            {documents.isSuccess && !docs.length && !editable && (
                <Typography.Text view='primary-medium' color='secondary'>
                    К заявке пока ничего не приложено
                </Typography.Text>
            )}
            <DocumentUploader
                applicationId={status.id}
                type={type}
                documents={docs}
                editable={editable}
                deletable={DELETABLE_IN.has(status.status)}
            />
        </FormCard>
    );
}
