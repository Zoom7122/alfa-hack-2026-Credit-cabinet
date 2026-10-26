import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Typography } from '@alfalab/core-components/typography';
import { Link } from 'react-router-dom';

import { useCollectorDecision, useCollectorInfoRequests, useCollectorOffers } from '../../../api/collector/queries';
import { useAcceptCollectorOffer } from '../../../api/collector/signing';
import type { CollectorApplication, CollectorOffer } from '../../../api/collector/types';
import type { Offer } from '../../../api/types';
import { formatIsoDate, formatMoney, formatMonths } from '../../../shared/lib/format';
import { OfferCard } from '../../ApplicationPage/status/OfferCard';
import { CollectorQuestion } from './CollectorQuestion';
import { CollectorSigning } from './CollectorSigning';
import './CollectorOutcome.css';

/** Чем условия отличаются от запрошенных; пусто — дали как просили. */
function difference(o: CollectorOffer, requested: CollectorApplication['requested']) {
    if (!requested) return undefined;
    const parts: string[] = [];
    if (o.amount !== requested.amount) parts.push(`сумма вместо ${formatMoney(requested.amount ?? 0)}`);
    if (o.term_months !== requested.term_months) parts.push(`срок вместо ${formatMonths(requested.term_months ?? 0)}`);
    return parts.length ? `Отличается от заявки: ${parts.join(', ')}` : undefined;
}

/** Предложение data-collector в форме контракта — для общей карточки. Изменённые условия — не «как вы запросили». */
const asOffer = (o: CollectorOffer, app: CollectorApplication): Offer => {
    const diff = difference(o, app.requested);
    return {
        id: o.id ?? '',
        kind: diff ? 'ALTERNATIVE' : (o.kind ?? 'REQUESTED'),
        difference_from_requested: diff,
        state: o.state ?? 'ACTIVE',
        recommended: o.recommended,
        product_code: o.product_code || (app.product_code ?? ''),
        amount: o.amount ?? 0,
        term_months: o.term_months ?? 0,
        rate_annual: o.rate_annual ?? 0,
        monthly_payment: o.monthly_payment ?? 0,
        total_payment: (o.monthly_payment ?? 0) * (o.term_months ?? 0),
        guarantee_required: o.guarantee_required,
        conditions: o.conditions,
        valid_until: o.valid_until ?? '',
    };
};

/** Итог оценки для клиента: предложение, отказ с причинами или вопросы банка. */
export function CollectorOutcome({ app }: { app: CollectorApplication }) {
    const id = app.id ?? '';
    const status = app.status;
    const offers = useCollectorOffers(id, status === 'OFFERED');
    const decision = useCollectorDecision(id, status === 'REJECTED');
    const questions = useCollectorInfoRequests(id, status === 'WAITING_FOR_INFO');
    const accept = useAcceptCollectorOffer(id);

    if (status === 'SIGNING' || status === 'COMPLETED') return <CollectorSigning applicationId={id} />;

    if (status === 'OFFERED') {
        const active = (offers.data ?? []).filter((o) => o.state === 'ACTIVE');
        return (
            <section className='collector-outcome' aria-labelledby='collector-outcome-title'>
                <Typography.Title id='collector-outcome-title' tag='h2' view='small' font='system'>
                    {active.length > 1 ? 'Предложения банка' : 'Предложение банка'}
                </Typography.Title>
                {offers.isPending && <Skeleton visible={true} className='collector-outcome__skeleton' />}
                {offers.isError && <Plate view='negative' title='Не удалось загрузить предложения' />}
                <div className='collector-outcome__offers'>
                    {active.map((o) => (
                        <OfferCard key={o.id} offer={asOffer(o, app)} guarantorNames={[]}
                            onChoose={() => accept.mutate(o.id ?? '')}
                            loading={accept.isPending && accept.variables === o.id} disabled={accept.isPending} />
                    ))}
                </div>
                {accept.error && <Plate view='negative' title='Не удалось выбрать предложение'>{accept.error.message}</Plate>}
                <Typography.Text view='primary-small' color='secondary'>
                    Выберите предложение — сформируем договор для демонстрационного подписания кодом.
                </Typography.Text>
            </section>
        );
    }

    if (status === 'REJECTED') {
        const d = decision.data;
        return (
            <section className='collector-outcome' aria-labelledby='collector-outcome-title'>
                <Typography.Title id='collector-outcome-title' tag='h2' view='small' font='system'>
                    Почему банк отказал
                </Typography.Title>
                {decision.isPending && <Skeleton visible={true} className='collector-outcome__skeleton' />}
                {d && (
                    <ul className='collector-outcome__reasons'>
                        {(d.reasons?.length
                            ? d.reasons
                            : [{ code: 'GENERAL', title: 'Заявка не прошла проверку банка', description: undefined }]
                        ).map((r) => (
                            <li key={r.code} className='collector-outcome__reason'>
                                <Typography.Text view='primary-medium'>{r.title}</Typography.Text>
                                {r.description && (
                                    <Typography.Text view='primary-small' color='secondary'>
                                        {r.description}
                                    </Typography.Text>
                                )}
                            </li>
                        ))}
                    </ul>
                )}
                {d?.reapply_after && (
                    <Typography.Text view='primary-medium' color='secondary'>
                        Подать новую заявку можно с {formatIsoDate(d.reapply_after)}.
                    </Typography.Text>
                )}
                <Button Component={Link} href='/products' view='secondary' size={48}>
                    Подобрать другой кредит
                </Button>
            </section>
        );
    }

    if (status === 'WAITING_FOR_INFO') {
        const items = questions.data ?? [];
        const open = items.filter((q) => q.state === 'OPEN');
        const answered = items.filter((q) => q.state === 'ANSWERED');
        return (
            <section className='collector-outcome' aria-labelledby='collector-outcome-title'>
                <Typography.Title id='collector-outcome-title' tag='h2' view='small' font='system'>
                    Вопросы банка
                </Typography.Title>
                <Typography.Text view='primary-medium' color='secondary'>
                    Ответьте на обязательные вопросы — после этого банк продолжит рассмотрение.
                </Typography.Text>
                {questions.isPending && <Skeleton visible={true} className='collector-outcome__skeleton' />}
                {questions.isError && <Plate view='negative' title='Не удалось загрузить вопросы' />}
                {open.map((q, i) => (
                    <CollectorQuestion key={q.id} applicationId={id} question={q} index={i + 1} />
                ))}
                {answered.length > 0 && (
                    <Typography.Text view='primary-small' color='secondary'>
                        Уже ответили: {answered.map((q) => q.title).join(', ')}
                    </Typography.Text>
                )}
            </section>
        );
    }
    return null;
}
