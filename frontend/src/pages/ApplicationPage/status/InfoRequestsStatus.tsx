import { useRef, useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Typography } from '@alfalab/core-components/typography';

import { ApiError } from '../../../api/client';
import { useAnswerInfoRequest, useDocuments, useInfoRequests } from '../../../api/queries';
import type { ApplicationStatusView } from '../../../api/types';
import { formatDay, pluralize } from '../../../shared/lib/format';
import { newIdempotencyKey } from '../../../shared/lib/idempotency';
import { StageBar } from '../components/StageBar';
import { type AnswerDraft, isAnswerReady, toApiValue } from './answers';
import { InfoRequestCard } from './InfoRequestCard';
import './InfoRequestsStatus.css';

const EMPTY: AnswerDraft = { comment: '' };

/** Экран 08: банк задал вопросы. Ответы уходят по одному; когда отвечены все обязательные, рассмотрение продолжается. */
export function InfoRequestsStatus({ status }: { status: ApplicationStatusView }) {
    const requests = useInfoRequests(status.id);
    const documents = useDocuments(status.id);
    const answer = useAnswerInfoRequest(status.id);
    const [drafts, setDrafts] = useState<Record<string, AnswerDraft>>({});
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [sending, setSending] = useState(false);
    const [uploading, setUploading] = useState<Record<string, boolean>>({});
    const keys = useRef<Record<string, string>>({});

    const open = (requests.data ?? []).filter((r) => r.state === 'OPEN');
    const docsOf = (id: string) => (documents.data ?? []).filter((d) => d.info_request_id === id);
    const draftOf = (id: string) => drafts[id] ?? EMPTY;
    const ready = open.filter((r) => isAnswerReady(r, draftOf(r.id), docsOf(r.id)));
    const missingRequired = open.some((r) => r.required && !ready.includes(r));
    const checking = (documents.data ?? []).some((d) => d.signatures?.state === 'PENDING' || d.recognition?.state === 'PROCESSING');

    const send = async () => {
        setSending(true);
        const nextErrors: Record<string, string> = {};
        // обязательные — последними: после последнего из них заявка уходит на рассмотрение и остальные ответы банк уже не примет
        const order = [...ready].sort((a, b) => Number(a.required) - Number(b.required));
        for (const r of order) {
            const draft = draftOf(r.id);
            const idempotencyKey = (keys.current[r.id] ??= newIdempotencyKey());
            try {
                await answer.mutateAsync({
                    infoRequestId: r.id,
                    idempotencyKey,
                    value: r.input.type === 'NONE' ? undefined : toApiValue(r, draft.value),
                    document_ids: docsOf(r.id).map((d) => d.id),
                    comment: draft.comment || undefined,
                });
            } catch (e) {
                // исправленный ответ — новый запрос, а не повтор
                if (e instanceof ApiError && e.status < 500) delete keys.current[r.id];
                nextErrors[r.id] =
                    e instanceof ApiError ? (e.problem?.errors?.map((x) => x.message).join('; ') ?? e.message) : 'Не удалось отправить';
            }
        }
        setErrors(nextErrors);
        setSending(false);
    };

    const deadline = status.deadline_at && formatDay(status.deadline_at);

    return (
        <div className='info-requests-status'>
            <section className='info-requests-status__card'>
                <StageBar stages={status.stages} clientStage={status.client_stage} />
                <div className='info-requests-status__text'>
                    <Typography.Title tag='h2' view='small' font='system'>
                        {status.title}
                    </Typography.Title>
                    <Typography.Text view='primary-large' color='secondary'>
                        {deadline ? `Ответьте до ${deadline} — как только ответите, рассмотрение продолжится автоматически` : status.subtitle}
                    </Typography.Text>
                </div>
            </section>

            {requests.isLoading && <Skeleton visible={true} className='info-requests-status__skeleton' />}
            {requests.isError && <Plate view='negative' title='Не удалось загрузить вопросы банка' />}
            {documents.isError && <Plate view='negative' title='Не удалось загрузить документы' />}

            {open.map((r, i) => (
                <InfoRequestCard
                    key={r.id}
                    index={i + 1}
                    request={r}
                    applicationId={status.id}
                    answer={draftOf(r.id)}
                    documents={docsOf(r.id)}
                    onChange={(next) => setDrafts((prev) => ({ ...prev, [r.id]: next }))}
                    error={errors[r.id]}
                    onUploadingChange={(busy) => setUploading((prev) => prev[r.id] === busy ? prev : { ...prev, [r.id]: busy })}
                />
            ))}

            {open.length > 0 && (
                <div className='info-requests-status__actions'>
                    <Button
                        view='primary'
                        size={56}
                        onClick={send}
                        loading={sending}
                        disabled={documents.isError || Object.values(uploading).some(Boolean) || missingRequired || !ready.length || !status.allowed_actions.includes('ANSWER_INFO_REQUEST')}
                    >
                        Отправить ответы
                    </Button>
                    <Typography.Text view='primary-small' color='secondary'>
                        {checking
                            ? 'Проверяем загруженные документы…'
                            : `Ответили на ${ready.length} из ${open.length} ${pluralize(open.length, 'вопроса', 'вопросов', 'вопросов')}`}
                    </Typography.Text>
                </div>
            )}
        </div>
    );
}
