import { useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Checkbox } from '@alfalab/core-components/checkbox';
import { Input } from '@alfalab/core-components/input';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Typography } from '@alfalab/core-components/typography';

import { downloadCollectorDocument } from '../../../api/collector/queries';
import { useCollectorSigning, useConfirmCollectorSigning } from '../../../api/collector/signing';
import { formatMoney, formatUpdated } from '../../../shared/lib/format';

/** Демо-код выдаёт и проверяет data-collector; состояние переживает перезагрузку страницы. */
export function CollectorSigning({ applicationId }: { applicationId: string }) {
    const signing = useCollectorSigning(applicationId);
    const confirm = useConfirmCollectorSigning(applicationId);
    const [code, setCode] = useState('');
    const [agreed, setAgreed] = useState(false);
    const [downloading, setDownloading] = useState(false);
    const [downloadError, setDownloadError] = useState<string>();
    const session = signing.data;

    if (signing.isPending) return <Skeleton visible={true} className='collector-outcome__skeleton' />;
    if (!session) {
        return <Plate view='negative' title='Не удалось загрузить подписание' buttons={<Button size={40} onClick={() => void signing.refetch()}>Повторить</Button>} />;
    }

    const signed = session.state === 'SIGNED';
    const download = async () => {
        const doc = session.documents[0];
        if (!doc?.id) return;
        setDownloading(true);
        setDownloadError(undefined);
        try {
            await downloadCollectorDocument(applicationId, doc.id, doc.file_name ?? 'Кредитный договор.txt');
        } catch {
            setDownloadError('Не удалось скачать договор — попробуйте ещё раз');
        } finally {
            setDownloading(false);
        }
    };

    return (
        <section className='collector-outcome collector-signing' aria-labelledby='collector-signing-title'>
            <Typography.Title id='collector-signing-title' tag='h2' view='small' font='system'>
                {signed ? 'Договор подписан' : 'Подписание договора'}
            </Typography.Title>
            <Typography.Text view='primary-medium'>
                {signed ? 'Подписал' : 'Подписант'}: {session.required_signer.full_name}
                {session.signed_at ? ` · ${formatUpdated(session.signed_at)}` : ''}
            </Typography.Text>
            <Button view='secondary' size={40} onClick={() => void download()} loading={downloading}>
                Скачать кредитный договор
            </Button>
            {downloadError && <Plate view='negative' title={downloadError} />}
            {signed ? (
                <Plate view='positive' title='Оформление завершено'>
                    {session.disbursement?.amount !== undefined && `${formatMoney(session.disbursement.amount)} · `}
                    Демонстрационная выдача завершена. Реальные деньги не перечисляются.
                </Plate>
            ) : session.awaiting_me ? (
                <form
                    className='collector-signing__form'
                    onSubmit={(event) => {
                        event.preventDefault();
                        if (agreed && code.length === 6 && !confirm.isPending) confirm.mutate(code);
                    }}
                >
                    <Plate view='common' title='Демо-подписание'>
                        Введите код, показанный ниже. Он проверяется на сервере, SMS не отправляется.
                    </Plate>
                    <Typography.Text view='primary-medium'>
                        Демо-код: <b data-testid='collector-demo-code'>{session.demo_code}</b>
                    </Typography.Text>
                    <Typography.Text view='primary-small' color='secondary'>
                        Действует до {formatUpdated(session.code_expires_at)} · осталось попыток: {session.attempts_left}
                    </Typography.Text>
                    <Input
                        label='Код подтверждения'
                        value={code}
                        inputMode='numeric'
                        autoComplete='one-time-code'
                        maxLength={6}
                        disabled={confirm.isPending}
                        onChange={(event) => {
                            setCode(event.target.value.replace(/\D/g, ''));
                            confirm.reset();
                        }}
                    />
                    <Checkbox
                        label='Я ознакомился с договором и согласен с условиями'
                        checked={agreed}
                        disabled={confirm.isPending}
                        onChange={(_, { checked }) => setAgreed(checked)}
                    />
                    {confirm.error && <Plate view='negative' title='Не удалось подписать договор'>{confirm.error.message}</Plate>}
                    <Button type='submit' view='primary' size={48} disabled={!agreed || code.length !== 6} loading={confirm.isPending}>
                        Подписать договор
                    </Button>
                </form>
            ) : (
                <Plate view='common' title='Ожидаем подпись'>
                    {session.state === 'AWAITING_SIGNER'
                        ? `Подтвердить договор может ${session.required_signer.full_name}. Войдите в кабинет под его учётной записью.`
                        : 'Подписание не завершено. Вернитесь к предложению и получите новый код.'}
                </Plate>
            )}
        </section>
    );
}
