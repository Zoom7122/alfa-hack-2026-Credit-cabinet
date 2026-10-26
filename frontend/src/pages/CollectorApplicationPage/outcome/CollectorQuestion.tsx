import { useState } from 'react';
import { Attach } from '@alfalab/core-components/attach';
import { Button } from '@alfalab/core-components/button';
import { Input } from '@alfalab/core-components/input';
import { Plate } from '@alfalab/core-components/plate';
import { Status } from '@alfalab/core-components/status';
import { Typography } from '@alfalab/core-components/typography';

import { ApiError } from '../../../api/client';
import { useAnswerInfoRequest } from '../../../api/collector/queries';
import type { CollectorInfoRequest } from '../../../api/collector/types';
import type { InfoRequest } from '../../../api/types';
import { ValueField } from '../../ApplicationPage/status/InfoRequestCard';
import { type AnswerDraft, isAnswerReady, toApiValue } from '../../ApplicationPage/status/answers';
import './CollectorQuestion.css';

const ACCEPT = '.pdf,.docx,.xlsx,.jpg,.jpeg,.png,.xml,.sig,.p7s';

/** Вопрос data-collector в форме контракта — для общего поля значения. */
function asInfoRequest(q: CollectorInfoRequest): InfoRequest {
    const input = (q.input ?? {}) as { type?: InfoRequest['input']['type']; options?: { value: string; label: string }[] };
    return {
        id: q.id ?? '',
        state: 'OPEN',
        required: Boolean(q.required),
        title: q.title ?? '',
        description: q.description,
        input: { type: input.type ?? 'TEXT', options: input.options },
        created_at: q.created_at ?? '',
    };
}

type Props = { applicationId: string; question: CollectorInfoRequest; index: number };

/** Вопрос банка по заявке data-collector: значение по типу вопроса, комментарий, документы — и отправка. */
export function CollectorQuestion({ applicationId, question, index }: Props) {
    const request = asInfoRequest(question);
    const needsFiles = Boolean((question.documents as { required?: boolean } | undefined)?.required);
    const [answer, setAnswer] = useState<AnswerDraft>({ comment: '' });
    const [files, setFiles] = useState<File[]>([]);
    const send = useAnswerInfoRequest(applicationId);
    const titleId = `collector-question-${question.id}`;
    const valueReady = request.input.type === 'NONE' || isAnswerReady(request, answer, []) || (request.input.type === 'TEXT' && answer.comment.trim() !== '');
    const ready = valueReady && (!needsFiles || files.length > 0);
    const error = send.error instanceof ApiError ? (send.error.problem?.errors?.[0]?.message ?? send.error.message) : send.error ? 'Не удалось отправить ответ' : undefined;

    return (
        <section className='collector-question' aria-labelledby={titleId}>
            <div className='collector-question__head'>
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
            <div className='collector-question__fields'>
                {request.input.type !== 'NONE' && <ValueField request={request} answer={answer} onChange={setAnswer} />}
                <Input
                    label='Комментарий, если нужно'
                    value={answer.comment}
                    onChange={(_, { value }) => setAnswer({ ...answer, comment: value })}
                    maxLength={2000}
                    block={true}
                    size={56}
                />
            </div>
            <Attach
                accept={ACCEPT}
                multiple={true}
                value={files}
                noFileText={needsFiles ? 'Приложите документ' : 'Документы — по желанию'}
                size={40}
                buttonContent='Выбрать файлы'
                buttonProps={{ view: 'secondary' }}
                onChange={(_, { files: picked }) => setFiles(picked)}
            />
            {error && (
                <Plate view='negative' title='Ответ не принят'>
                    {error}
                </Plate>
            )}
            <Button
                view='primary'
                size={48}
                disabled={!ready}
                loading={send.isPending}
                onClick={() => send.mutate({ requestId: question.id ?? '', value: toApiValue(request, answer.value), comment: answer.comment, files })}
                aria-describedby={titleId}
            >
                Отправить ответ
            </Button>
        </section>
    );
}
