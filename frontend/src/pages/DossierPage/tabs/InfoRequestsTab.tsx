import { Status } from '@alfalab/core-components/status';
import { Typography } from '@alfalab/core-components/typography';

import type { InfoRequest } from '../../../api/types';
import { formatDay, formatMoney, formatUpdated } from '../../../shared/lib/format';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import './InfoRequestsTab.css';

const STATE_BADGES: Record<InfoRequest['state'], { label: string; color: 'orange' | 'green' | 'grey' }> = {
    OPEN: { label: 'Ждём ответа', color: 'orange' },
    ANSWERED: { label: 'Отвечен', color: 'green' },
    CANCELLED: { label: 'Отменён', color: 'grey' },
};

function answerText(r: InfoRequest) {
    const a = r.answer;
    if (!a) return undefined;
    const value = typeof a.value === 'number' && r.input.type === 'MONEY' ? formatMoney(a.value) : a.value !== undefined ? String(a.value) : undefined;
    return [value, a.document_ids?.length ? `документов: ${a.document_ids.length}` : undefined, a.comment].filter(Boolean).join(' · ');
}

/** Вкладка «Уточнения»: что спросили у клиента и что он ответил. */
export function InfoRequestsTab({ requests }: { requests: InfoRequest[] }) {
    return (
        <FormCard title='Вопросы клиенту'>
            {requests.length === 0 ? (
                <Typography.Text view='primary-medium' color='secondary'>
                    Вопросов клиенту не задавали
                </Typography.Text>
            ) : (
                <ul className='info-requests-tab'>
                    {requests.map((r) => {
                        const badge = STATE_BADGES[r.state];
                        const answer = answerText(r);
                        const meta = [r.required ? 'обязательный' : 'необязательный', `задан ${formatUpdated(r.created_at)}`, r.due_at && `ответить до ${formatDay(r.due_at)}`];
                        return (
                            <li key={r.id} className='info-requests-tab__item'>
                                <div className='info-requests-tab__head'>
                                    <Typography.Text view='primary-medium' weight='bold'>
                                        {r.title}
                                    </Typography.Text>
                                    <Status view='muted-alt' color={badge.color} size={24} uppercase={false}>
                                        {badge.label}
                                    </Status>
                                </div>
                                <Typography.Text view='primary-small' color='secondary'>
                                    {meta.filter(Boolean).join(' · ')}
                                </Typography.Text>
                                {answer && <Typography.Text view='primary-small'>Ответ: {answer}</Typography.Text>}
                            </li>
                        );
                    })}
                </ul>
            )}
        </FormCard>
    );
}
