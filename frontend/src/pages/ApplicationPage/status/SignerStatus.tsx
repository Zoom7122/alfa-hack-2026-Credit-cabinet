import { type ComponentRef, useEffect, useRef, useState } from 'react';
import { Attach } from '@alfalab/core-components/attach';
import { Button } from '@alfalab/core-components/button';
import { Checkbox } from '@alfalab/core-components/checkbox';
import { CodeInput } from '@alfalab/core-components/code-input';
import { Plate } from '@alfalab/core-components/plate';
import { Segment, SegmentedControl } from '@alfalab/core-components/segmented-control';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Typography } from '@alfalab/core-components/typography';

import { ApiError } from '../../../api/client';
import {
    type ConfirmSigningBody,
    useConfirmSigning,
    useDocuments,
    useOffers,
    useSigning,
    useStartSigning,
    useTimeline,
    useUploadDocument,
} from '../../../api/queries';
import type { Application, ApplicationStatusView, Document, SigningMethod } from '../../../api/types';
import { formatMoney, formatMonths, formatRate, formatUpdated, initials, shortName } from '../../../shared/lib/format';
import { newIdempotencyKey, useIdempotencyKeys } from '../../../shared/lib/idempotency';
import { InfoCard } from '../../../shared/ui/InfoCard/InfoCard';
import { StageBar } from '../components/StageBar';
import { SummaryList } from '../../../shared/ui/SummaryList/SummaryList';
import { contractOf } from './signingDocs';
import { SigningDocuments } from './SigningDocuments';
import './SignerStatus.css';

type Props = { app: Application; me: { id: string }; status: ApplicationStatusView };

const CODE_LENGTH = 6;
const formatCode = (code: string) => `${code.slice(0, 3)} ${code.slice(3)}`;

function confirmError(e: unknown) {
    if (!(e instanceof ApiError)) return e instanceof Error ? e.message : 'Не получилось подписать';
    if (e.code === 'SIGNATURE_INVALID') return e.problem?.detail ?? e.problem?.errors?.[0]?.message ?? 'Подпись не прошла проверку';
    if (e.code === 'NOT_AUTHORIZED_SIGNER') return 'Подписать договор может только директор компании';
    if (e.status === 409) return 'Статус заявки изменился — обновили страницу';
    return e.problem?.detail ?? e.message;
}

/** Последняя подпись к текущему договору: повторная загрузка после неудачной проверки заменяет прежнюю. */
function latestSignature(documents: Document[] | undefined, contract: Document | undefined) {
    if (!contract) return undefined;
    return (documents ?? [])
        .filter((d) => d.type === 'SIGNATURE' && d.signed_document_id === contract.id && d.uploaded_at >= contract.uploaded_at)
        .sort((a, b) => b.uploaded_at.localeCompare(a.uploaded_at))[0];
}

/** КЭП годится для подписания: проверка завершена, вердикт положительный и политика LOAN_CONTRACT выполнена. */
function signatureOk(doc: Document | undefined) {
    const sig = doc?.signatures;
    return sig?.state === 'DONE' && (sig.verdict === 'VALID' || sig.verdict === 'VALID_WITH_WARNINGS') && sig.policy_satisfied !== false;
}

/** Экран 11: директор подписывает договор — кодом из интерфейса (ПЭП) или КЭП с откреплённой подписью. */
export function SignerStatus({ app, me, status }: Props) {
    const signing = useSigning(app.id);
    const offers = useOffers(app.id);
    const timeline = useTimeline(app.id);
    const documents = useDocuments(app.id);
    const start = useStartSigning(app.id);
    const confirm = useConfirmSigning(app.id);
    const upload = useUploadDocument(app.id);
    const { keyFor, forget } = useIdempotencyKeys();
    const [chosenMethod, setChosenMethod] = useState<SigningMethod>();
    const [code, setCode] = useState('');
    const [agreed, setAgreed] = useState(false);
    const requested = useRef<SigningMethod>(undefined);
    const codeRef = useRef<ComponentRef<typeof CodeInput>>(null);

    const session = signing.data;
    // бэк может формировать договор только по signing/start — тогда GET отвечает 404
    const notStarted = signing.error instanceof ApiError && signing.error.status === 404;
    // способ, уже выбранный на сервере (например, до перезагрузки), важнее значения по умолчанию
    const method: SigningMethod = chosenMethod ?? session?.method ?? 'SIMPLE_CODE';
    const offer = offers.data?.find((o) => o.id === session?.offer_id);
    const contract = contractOf(session?.documents);
    const signature = latestSignature(documents.data, contract);
    const accepted = timeline.data?.find((e) => e.type === 'OFFER_ACCEPTED');
    const preparedBy = accepted?.actor;
    const awaitingMe = status.signing?.awaiting_me === true;

    const { mutate: startSigning } = start;
    // способ подписи фиксируем на сервере (signing/start); для кода он же выдаёт демо-код
    useEffect(() => {
        if (!awaitingMe || signing.isLoading) return;
        const ready = session && session.method === method && (method !== 'SIMPLE_CODE' || session.demo_code);
        if (ready || (!session && !notStarted) || requested.current === method) return;
        requested.current = method;
        startSigning(
            { method, idempotencyKey: newIdempotencyKey() },
            {
                // не получилось — даём повторить кнопкой, без бесконечных автоповторов
                onError: () => {
                    requested.current = undefined;
                },
            },
        );
    }, [awaitingMe, signing.isLoading, session, notStarted, method, startSigning]);

    const retryStart = () => {
        requested.current = method;
        startSigning(
            { method, idempotencyKey: newIdempotencyKey() },
            {
                onError: () => {
                    requested.current = undefined;
                },
            },
        );
    };

    const canSubmit = agreed && (method === 'SIMPLE_CODE' ? code.length === CODE_LENGTH : signatureOk(signature));
    const codeError = method === 'SIMPLE_CODE' && confirm.error instanceof ApiError && confirm.error.code === 'SIGNATURE_INVALID';

    const submit = () => {
        const body: ConfirmSigningBody =
            method === 'SIMPLE_CODE' ? { method, code } : { method, signature_document_id: signature?.id ?? '' };
        // ключ — на конкретное тело: другой код или способ подписи = новый запрос
        confirm.mutate(
            { ...body, idempotencyKey: keyFor('confirm', body) },
            {
                onError: (e) => {
                    if (e instanceof ApiError && e.status < 500) forget('confirm', body);
                    if (e instanceof ApiError && e.code === 'SIGNATURE_INVALID') {
                        setCode('');
                        codeRef.current?.reset();
                    }
                },
            },
        );
    };

    const uploadSignature = (files: File[]) => {
        const file = files[0];
        if (!file || !contract) return;
        upload.mutate({ file, type: 'SIGNATURE', signedDocumentId: contract.id });
    };

    if (signing.isLoading || (notStarted && start.isPending)) return <Skeleton visible={true} className='signer-status__skeleton' />;
    if (!session) {
        return (
            <Plate
                view='negative'
                title='Не удалось подготовить договор'
                buttons={
                    <Button view='secondary' size={32} onClick={notStarted ? retryStart : () => void signing.refetch()}>
                        Повторить
                    </Button>
                }
            />
        );
    }

    return (
        <div className='signer-status'>
            <StageBar stages={status.stages} clientStage={status.client_stage} />
            <div className='signer-status__columns'>
                <div className='signer-status__main'>
                    <SigningDocuments applicationId={app.id} documents={session.documents} offer={offer} />

                    <section className='signer-status__card' aria-labelledby='signer-method-title'>
                        <Typography.Title id='signer-method-title' tag='h2' view='xsmall' font='system'>
                            Способ подписи
                        </Typography.Title>
                        <SegmentedControl selectedId={method} onChange={(id) => setChosenMethod(id as SigningMethod)}>
                            <Segment id='SIMPLE_CODE' title='Код подтверждения' />
                            <Segment id='QUALIFIED_SIGNATURE' title='Подпись КЭП' />
                        </SegmentedControl>

                        {start.error && (
                            <Plate
                                view='negative'
                                title={method === 'SIMPLE_CODE' ? 'Не удалось получить код' : 'Не удалось выбрать способ подписи'}
                                buttons={
                                    <Button view='secondary' size={32} onClick={retryStart} loading={start.isPending}>
                                        Повторить
                                    </Button>
                                }
                            />
                        )}

                        {method === 'SIMPLE_CODE' ? (
                            <>
                                <Typography.Text view='primary-small' color='secondary'>
                                    Введите код. В демо-режиме он показан ниже — SMS не отправляем.
                                </Typography.Text>
                                <CodeInput
                                    ref={codeRef}
                                    fields={CODE_LENGTH}
                                    onChange={setCode}
                                    error={codeError ? confirmError(confirm.error) : undefined}
                                />
                                {session.demo_code && (
                                    <div className='signer-status__demo'>
                                        <Typography.Text view='primary-small'>
                                            Демо-код: <b>{formatCode(session.demo_code)}</b> · осталось попыток: {session.attempts_left}
                                        </Typography.Text>
                                    </div>
                                )}
                            </>
                        ) : (
                            <>
                                <Typography.Text view='primary-small' color='secondary'>
                                    Подпишите файл договора в программе для КЭП (например, КриптоПро) и загрузите файл подписи .sig —
                                    проверим, что подпись ваша и относится к этому договору.
                                </Typography.Text>
                                <Attach
                                    accept='.sig,.p7s'
                                    value={[]}
                                    noFileText=''
                                    size={40}
                                    buttonContent='Загрузить подпись'
                                    buttonProps={{ view: 'secondary', loading: upload.isPending }}
                                    onChange={(_, { files }) => uploadSignature(files)}
                                />
                                {signature && (
                                    <Typography.Text view='primary-small' color={signatureOk(signature) ? 'positive' : 'secondary'}>
                                        {signature.file_name} ·{' '}
                                        {signature.signatures?.state === 'PENDING'
                                            ? 'проверяем подпись…'
                                            : signatureOk(signature)
                                              ? 'подпись верна'
                                              : 'подпись не подходит — загрузите другую'}
                                    </Typography.Text>
                                )}
                                {upload.error && (
                                    <Plate view='negative' title='Не удалось загрузить подпись'>
                                        {upload.error.message}
                                    </Plate>
                                )}
                            </>
                        )}

                        <Checkbox
                            label='Ознакомился с условиями договора и графиком платежей'
                            checked={agreed}
                            onChange={(_, { checked }) => setAgreed(checked)}
                            size={24}
                        />
                        {confirm.error && !codeError && (
                            <Plate view='negative' title='Не получилось подписать'>
                                {confirmError(confirm.error)}
                            </Plate>
                        )}
                        <div>
                            <Button view='accent' size={56} onClick={submit} disabled={!canSubmit} loading={confirm.isPending}>
                                Подписать и получить кредит
                            </Button>
                        </div>
                    </section>
                </div>

                <aside className='signer-status__aside'>
                    {offer && (
                        <InfoCard title='Условия'>
                            <SummaryList
                                plain={true}
                                rows={[
                                    { label: 'Сумма', value: formatMoney(offer.amount), bold: true },
                                    { label: 'Срок', value: formatMonths(offer.term_months) },
                                    { label: 'Ставка', value: formatRate(offer.rate_annual) },
                                    { label: 'Платёж', value: formatMoney(offer.monthly_payment) },
                                ]}
                            />
                        </InfoCard>
                    )}
                    {preparedBy && preparedBy.id !== me.id && (
                        <InfoCard title='Подготовил(а)'>
                            <div className='signer-status__person'>
                                <span className='signer-status__avatar' aria-hidden='true'>
                                    <Typography.Text view='primary-small' weight='bold'>
                                        {initials(preparedBy.full_name)}
                                    </Typography.Text>
                                </span>
                                <span className='signer-status__person-text'>
                                    <Typography.Text view='primary-medium' weight='bold'>
                                        {shortName(preparedBy.full_name)}
                                    </Typography.Text>
                                    <Typography.Text view='primary-small' color='secondary'>
                                        Выбрал(а) предложение {accepted && formatUpdated(accepted.at)}
                                    </Typography.Text>
                                </span>
                            </div>
                        </InfoCard>
                    )}
                </aside>
            </div>
        </div>
    );
}
