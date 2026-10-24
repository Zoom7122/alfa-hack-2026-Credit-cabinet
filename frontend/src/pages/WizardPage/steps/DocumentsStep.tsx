import { useState } from 'react';
import { Plate } from '@alfalab/core-components/plate';
import { Select } from '@alfalab/core-components/select';
import { Typography } from '@alfalab/core-components/typography';
import { CheckmarkCompactSIcon } from '@alfalab/icons-glyph/CheckmarkCompactSIcon';

import { useDocuments } from '../../../api/queries';
import type { DocumentType } from '../../../api/types';
import { DOCUMENT_LABELS } from '../../../shared/lib/dictionaries';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { DocumentUploader } from '../components/DocumentUploader';
import { StepLayout } from '../components/StepLayout';
import type { StepProps } from '../stepProps';
import { expectedDocuments } from '../steps';
import './DocumentsStep.css';

/** Что клиент может загрузить сам (подпись и сгенерированные банком — не здесь). */
const UPLOADABLE = (Object.keys(DOCUMENT_LABELS) as DocumentType[]).filter((t) => t !== 'SIGNATURE' && t !== 'LOAN_AGREEMENT_DRAFT');

export function DocumentsStep({ app, product, hrefFor }: StepProps) {
    const documents = useDocuments(app.id);
    const docs = documents.data ?? [];
    const expected = expectedDocuments(product, app);
    const uploaded = new Set(docs.map((d) => d.type));
    const firstMissing = expected.find((t) => !uploaded.has(t));
    const [chosenType, setChosenType] = useState<DocumentType>();
    const type = chosenType ?? firstMissing ?? 'OTHER';
    const types = [...new Set<DocumentType>([...expected, ...UPLOADABLE])];

    return (
        <StepLayout
            title='Документы'
            subtitle='Можно отправить и без них — но с документами решение будет быстрее, а сумма может быть выше'
            backHref={hrefFor('collateral')}
            nextHref={hrefFor('review')}
        >
            {expected.length > 0 && (
                <FormCard title='Что пригодится для этой заявки'>
                    <ul className='documents-step__checklist'>
                        {expected.map((t) => {
                            const done = uploaded.has(t);
                            return (
                                <li key={t} className='documents-step__check'>
                                    <span
                                        className={`documents-step__mark${done ? ' documents-step__mark_done' : ''}`}
                                        aria-hidden='true'
                                    >
                                        {done && <CheckmarkCompactSIcon />}
                                    </span>
                                    <Typography.Text view='primary-medium'>{DOCUMENT_LABELS[t]}</Typography.Text>
                                    <Typography.Text view='primary-small' color={done ? 'positive' : 'secondary'} className='documents-step__state'>
                                        {done ? 'загружено' : 'не загружено'}
                                    </Typography.Text>
                                </li>
                            );
                        })}
                    </ul>
                </FormCard>
            )}

            <FormCard title='Загрузить документ'>
                <Select
                    label='Тип документа'
                    size={56}
                    block={true}
                    options={types.map((t) => ({ key: t, content: DOCUMENT_LABELS[t] }))}
                    selected={type}
                    onChange={({ selected }) => selected && setChosenType(selected.key as DocumentType)}
                />
                <DocumentUploader applicationId={app.id} type={type} documents={docs} />
                {documents.isError && <Plate view='negative' title='Не удалось загрузить список документов' />}
            </FormCard>
        </StepLayout>
    );
}
