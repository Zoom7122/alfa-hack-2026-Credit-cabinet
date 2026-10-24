import { AmountInput } from '@alfalab/core-components/amount-input';
import { Button } from '@alfalab/core-components/button';
import { DateInput } from '@alfalab/core-components/date-input';
import { Input } from '@alfalab/core-components/input';
import { Plate } from '@alfalab/core-components/plate';
import { Radio } from '@alfalab/core-components/radio';
import { RadioGroup } from '@alfalab/core-components/radio-group';
import { Status } from '@alfalab/core-components/status';
import { Textarea } from '@alfalab/core-components/textarea';
import { Typography } from '@alfalab/core-components/typography';

import type { Document, InfoRequest } from '../../../api/types';
import { fieldLabel } from '../../../shared/lib/dictionaries';
import { formatMoney } from '../../../shared/lib/format';
import { DocumentUploader } from '../../../shared/ui/DocumentUploader/DocumentUploader';
import type { AnswerDraft } from './answers';
import './InfoRequestCard.css';

type Props = {
    index: number;
    request: InfoRequest;
    applicationId: string;
    answer: AnswerDraft;
    documents: Document[];
    onChange: (answer: AnswerDraft) => void;
    error?: string;
};

/** Где в распознанном документе лежит значение для поля заявки. */
const EXTRACTED_PREFIX: Record<string, string> = {
    'financials.annual_revenue': 'revenue_',
    'financials.net_profit': 'net_profit_',
};

function recognizedValue(request: InfoRequest, documents: Document[]) {
    const prefix = request.target_field ? EXTRACTED_PREFIX[request.target_field] : undefined;
    if (!prefix) return undefined;
    for (const doc of documents) {
        const extracted = doc.recognition?.state === 'DONE' ? doc.recognition.extracted : undefined;
        const value = Object.entries(extracted ?? {}).find(([key]) => key.startsWith(prefix))?.[1];
        if (typeof value === 'number') return value;
    }
    return undefined;
}

const capitalize = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);


function ValueField({ request, answer, onChange }: Pick<Props, 'request' | 'answer' | 'onChange'>) {
    const label = request.target_field ? capitalize(fieldLabel(request.target_field)) : 'Ответ';
    const set = (value: AnswerDraft['value']) => onChange({ ...answer, value });
    switch (request.input.type) {
        case 'MONEY':
            return (
                <AmountInput
                    label={label}
                    value={typeof answer.value === 'number' ? answer.value : null}
                    minority={1}
                    integersOnly={true}
                    onChange={(_, { value }) => set(value ?? undefined)}
                    block={true}
                    size={56}
                />
            );
        case 'NUMBER':
            return (
                <Input
                    label={label}
                    inputMode='decimal'
                    value={typeof answer.value === 'string' ? answer.value : ''}
                    onChange={(_, { value }) => set(value.replace(/[^\d.,-]/g, ''))}
                    block={true}
                    size={56}
                />
            );
        case 'DATE':
            return (
                <DateInput
                    label={label}
                    value={typeof answer.value === 'string' ? answer.value : ''}
                    onChange={(_, { value }) => set(value)}
                    block={true}
                    size={56}
                />
            );
        case 'TEXT':
            return (
                <Textarea
                    label={label}
                    aria-label={label}
                    value={typeof answer.value === 'string' ? answer.value : ''}
                    onChange={(e) => set(e.target.value)}
                    block={true}
                    minRows={2}
                />
            );
        case 'BOOLEAN':
        case 'CHOICE': {
            const options =
                request.input.type === 'BOOLEAN'
                    ? [
                          { value: 'true', label: 'Да' },
                          { value: 'false', label: 'Нет' },
                      ]
                    : (request.input.options ?? []);
            const current = request.input.type === 'BOOLEAN' && typeof answer.value === 'boolean' ? String(answer.value) : answer.value;
            return (
                <RadioGroup
                    name={request.id}
                    label={label}
                    direction='horizontal'
                    value={typeof current === 'string' ? current : null}
                    onChange={(_, { value }) => set(request.input.type === 'BOOLEAN' ? value === 'true' : value)}
                >
                    {options.map((o) => (
                        <Radio key={o.value} value={o.value} label={o.label} size={24} />
                    ))}
                </RadioGroup>
            );
        }
        default:
            return null;
    }
}

/** Один вопрос банка (экран 08). Формулировки нейтральные — источник расхождения не раскрываем. */
export function InfoRequestCard({ index, request, applicationId, answer, documents, onChange, error }: Props) {
    const recognized = recognizedValue(request, documents);
    const accepted = request.documents?.accepted_types ?? [];
    const titleId = `info-request-${request.id}`;
    return (
        <section className='info-request-card' aria-labelledby={titleId}>
            <div className='info-request-card__head'>
                <Typography.Title id={titleId} tag='h3' view='xsmall' font='system'>
                    {index}. {request.title}
                </Typography.Title>
                {request.required && (
                    <Status view='muted-alt' color='grey' size={24} uppercase={false}>
                        Обязательно
                    </Status>
                )}
            </div>
            {request.description && (
                <Typography.Text view='primary-small' color='secondary'>
                    {request.description}
                </Typography.Text>
            )}

            {request.input.type !== 'NONE' && (
                <div className='info-request-card__fields'>
                    <ValueField request={request} answer={answer} onChange={onChange} />
                    <Input
                        label='Комментарий, если нужно'
                        value={answer.comment}
                        onChange={(_, { value }) => onChange({ ...answer, comment: value })}
                        maxLength={2000}
                        block={true}
                        size={56}
                    />
                </div>
            )}

            {accepted[0] && (
                <DocumentUploader
                    applicationId={applicationId}
                    type={accepted[0]}
                    infoRequestId={request.id}
                    documents={documents}
                    hint={request.documents?.hint}
                    accept={accepted.includes('POWER_OF_ATTORNEY') ? '.xml,.sig,.p7s,.pdf' : undefined}
                    renderExtra={() =>
                        recognized !== undefined &&
                        recognized !== answer.value && (
                            <div className='info-request-card__recognized'>
                                <Typography.Text view='primary-small'>Распознано: {formatMoney(recognized)}</Typography.Text>
                                <Button view='secondary' size={32} onClick={() => onChange({ ...answer, value: recognized })}>
                                    Подставить
                                </Button>
                            </div>
                        )
                    }
                />
            )}

            {error && (
                <Plate view='negative' title='Ответ не принят'>
                    {error}
                </Plate>
            )}
        </section>
    );
}
