import { useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';

import { ApiError } from '../../../api/client';
import { useAnswerInfoRequest } from '../../../api/collector/queries';
import { useDocuments } from '../../../api/queries';
import type { CollectorInfoRequest } from '../../../api/collector/types';
import type { InfoRequest } from '../../../api/types';
import { InfoRequestCard } from '../../ApplicationPage/status/InfoRequestCard';
import { type AnswerDraft, isAnswerReady, toApiValue } from '../../ApplicationPage/status/answers';

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
        target_field: q.target_field,
        documents: q.documents as InfoRequest['documents'],
        created_at: q.created_at ?? '',
    };
}

type Props = { applicationId: string; question: CollectorInfoRequest; index: number };

/** Вопрос банка по заявке data-collector: значение по типу вопроса, комментарий, документы — и отправка. */
export function CollectorQuestion({ applicationId, question, index }: Props) {
    const request = asInfoRequest(question);
    const [answer, setAnswer] = useState<AnswerDraft>({ comment: '' });
    const [uploading, setUploading] = useState(false);
    const documents = useDocuments(applicationId);
    const attached = (documents.data ?? []).filter((doc) => doc.info_request_id === question.id);
    const send = useAnswerInfoRequest(applicationId);
    const ready = isAnswerReady(request, answer, attached) && !uploading && !documents.isPending && !documents.isError;
    const error = send.error instanceof ApiError ? (send.error.problem?.errors?.[0]?.message ?? send.error.message) : send.error ? 'Не удалось отправить ответ' : undefined;

    return (
        <InfoRequestCard index={index} request={request} applicationId={applicationId} answer={answer} documents={attached} onChange={setAnswer} onUploadingChange={setUploading}>
            {documents.isError && <Plate view='negative' title='Не удалось загрузить документы' />}
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
                onClick={() => send.mutate({ requestId: question.id ?? '', value: toApiValue(request, answer.value), comment: answer.comment, documentIds: attached.map((doc) => doc.id) })}
            >
                Отправить ответ
            </Button>
        </InfoRequestCard>
    );
}
