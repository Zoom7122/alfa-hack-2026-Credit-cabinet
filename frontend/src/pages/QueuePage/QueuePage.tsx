import { useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Input } from '@alfalab/core-components/input';
import { Plate } from '@alfalab/core-components/plate';
import { Select } from '@alfalab/core-components/select';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Status } from '@alfalab/core-components/status';
import { Table } from '@alfalab/core-components/table';
import { Tag } from '@alfalab/core-components/tag';
import { Typography } from '@alfalab/core-components/typography';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';

import { ApiError } from '../../api/client';
import { useBoAssign, useBoQueue, useProducts } from '../../api/queries';
import type { BoApplicationListItem, Me } from '../../api/types';
import { useNow } from '../../shared/lib/useNow';
import { BO_STATUS_BADGES, dueToday, GRADE_COLORS, IN_PROCESSING, slaLeft, sourceLabel } from '../../shared/lib/backoffice';
import { formatMoney, formatMonths, formatTime, shortName } from '../../shared/lib/format';
import './QueuePage.css';

type Group = 'manual' | 'waiting' | 'processing' | 'all';

const GROUPS: { id: Group; label: string; match: (i: BoApplicationListItem) => boolean }[] = [
    { id: 'manual', label: 'Ручное рассмотрение', match: (i) => i.status === 'MANUAL_REVIEW' },
    { id: 'waiting', label: 'Ждут клиента', match: (i) => i.status === 'WAITING_FOR_INFO' },
    { id: 'processing', label: 'В обработке', match: (i) => IN_PROCESSING.includes(i.status) },
    { id: 'all', label: 'Все', match: () => true },
];

const ASSIGNEES = [
    { key: 'all', content: 'Все' },
    { key: 'me', content: 'Мне' },
    { key: 'none', content: 'Не назначен' },
];

const isGroup = (v: string | null): v is Group => GROUPS.some((g) => g.id === v);

function Stat({ label, value, urgent }: { label: string; value?: number; urgent?: boolean }) {
    return (
        <div className='queue__stat' role='group' aria-label={label}>
            <Typography.Text view='primary-small' color='secondary'>
                {label}
            </Typography.Text>
            <Typography.TitleResponsive tag='div' view='medium' font='system' color={urgent && value ? 'accent' : 'primary'}>
                {value ?? '—'}
            </Typography.TitleResponsive>
        </div>
    );
}

function SourcesCell({ item }: { item: BoApplicationListItem }) {
    const failed = item.degraded_sources ?? [];
    if (!failed.length) {
        return (
            <Status view='muted-alt' color='grey' size={24} uppercase={false}>
                Все ответили
            </Status>
        );
    }
    const [first] = failed;
    return (
        <Status view='muted-alt' color='orange' size={24} uppercase={false}>
            {first && sourceLabel(first)} — ошибка{failed.length > 1 ? ` (+${failed.length - 1})` : ''}
        </Status>
    );
}

/** Экран 16: очередь андеррайтера — ручное рассмотрение по SLA, ждут клиента, в обработке. */
export function QueuePage({ me }: { me: Me }) {
    const navigate = useNavigate();
    const [params, setParams] = useSearchParams();
    const groupParam = params.get('group');
    const group: Group = isGroup(groupParam) ? groupParam : 'manual';
    const assignee = params.get('assignee') ?? 'all';
    const [search, setSearch] = useState('');
    const now = useNow();

    // статистика — по всей очереди, таблица — с фильтром исполнителя
    const all = useBoQueue();
    const filtered = useBoQueue(assignee === 'all' ? {} : { assignee });
    const products = useProducts();
    const assign = useBoAssign();

    const items = filtered.data ?? [];
    const everything = all.data ?? [];
    const manual = everything.filter((i) => i.status === 'MANUAL_REVIEW');
    const query = search.trim().toLowerCase();
    const shown = items
        .filter((i) => GROUPS.find((g) => g.id === group)?.match(i))
        .filter((i) => !query || i.number.toLowerCase().includes(query) || i.applicant.inn?.includes(query) || i.applicant.short_name?.toLowerCase().includes(query));
    const productName = (code?: string) => products.data?.find((p) => p.code === code)?.name ?? code ?? '';

    const setParam = (key: string, value: string | undefined) => {
        const next = new URLSearchParams(params);
        if (value) next.set(key, value);
        else next.delete(key);
        setParams(next, { replace: true });
    };

    const take = (id: string) => assign.mutate(id, { onSuccess: () => navigate(`/backoffice/applications/${id}`) });

    return (
        <div className='queue'>
            <div className='queue__head'>
                <Typography.TitleResponsive tag='h1' view='large' font='system'>
                    {assignee === 'me' ? 'Мои заявки' : 'Очередь заявок'}
                </Typography.TitleResponsive>
                <Typography.Text view='primary-small' color='secondary'>
                    {all.dataUpdatedAt ? `Обновлено в ${formatTime(new Date(all.dataUpdatedAt).toISOString())}` : ''}
                </Typography.Text>
            </div>

            <div className='queue__stats'>
                <Stat label='Ждут ручного рассмотрения' value={all.data && manual.length} />
                <Stat label='В работе у меня' value={all.data && manual.filter((i) => i.assignee?.id === me.id).length} />
                <Stat label='SLA истекает сегодня' value={all.data && manual.filter((i) => dueToday(i.sla_due_at, new Date(now))).length} urgent={true} />
                <Stat label='Ждут ответа клиента' value={all.data && everything.filter((i) => i.status === 'WAITING_FOR_INFO').length} />
            </div>

            <section className='queue__card' aria-label='Заявки'>
                <div className='queue__filters'>
                    <div className='queue__groups' role='group' aria-label='Статус'>
                        {GROUPS.map((g) => (
                            <Tag
                                key={g.id}
                                size={40}
                                checked={group === g.id}
                                aria-pressed={group === g.id}
                                onClick={() => setParam('group', g.id === 'manual' ? undefined : g.id)}
                            >
                                {g.id === 'all' ? g.label : `${g.label} · ${items.filter(g.match).length}`}
                            </Tag>
                        ))}
                    </div>
                    <div className='queue__search'>
                        <Select
                            label='Исполнитель'
                            size={48}
                            options={ASSIGNEES}
                            selected={assignee}
                            onChange={({ selected }) => setParam('assignee', selected && selected.key !== 'all' ? selected.key : undefined)}
                            className='queue__assignee'
                        />
                        <Input
                            label='Поиск по ИНН, номеру'
                            size={48}
                            value={search}
                            onChange={(_, { value }) => setSearch(value)}
                            className='queue__query'
                        />
                    </div>
                </div>

                {filtered.isLoading && <Skeleton visible={true} className='queue__skeleton' />}
                {filtered.isError && (
                    <Plate view='negative' title='Не удалось загрузить очередь'>
                        {filtered.error instanceof ApiError && filtered.error.status === 403 ? 'Нужна роль андеррайтера' : filtered.error.message}
                    </Plate>
                )}
                {assign.error && <Plate view='negative' title='Не получилось взять в работу'>{assign.error.message}</Plate>}
                {filtered.isSuccess && shown.length === 0 && (
                    <Typography.Text view='primary-medium' color='secondary'>
                        {query ? 'Ничего не нашли' : 'Здесь пока пусто'}
                    </Typography.Text>
                )}

                {shown.length > 0 && (
                    <Table wrapper={false}>
                        <Table.THead>
                            <Table.THeadCell>Заявка</Table.THeadCell>
                            <Table.THeadCell>Компания</Table.THeadCell>
                            <Table.THeadCell>Запрос</Table.THeadCell>
                            <Table.THeadCell width={72}>Грейд</Table.THeadCell>
                            <Table.THeadCell>Почему на ручном</Table.THeadCell>
                            <Table.THeadCell width={170}>Источники</Table.THeadCell>
                            <Table.THeadCell width={130}>Исполнитель</Table.THeadCell>
                            <Table.THeadCell width={100}>SLA</Table.THeadCell>
                            <Table.THeadCell width={150} textAlign='right'>
                                Действие
                            </Table.THeadCell>
                        </Table.THead>
                        <Table.TBody>
                            {shown.map((item) => {
                                const sla = slaLeft(item.sla_due_at, now);
                                const status = BO_STATUS_BADGES[item.status];
                                const free = item.status === 'MANUAL_REVIEW' && !item.assignee;
                                return (
                                    <Table.TRow key={item.id}>
                                        <Table.TCell>
                                            <span className='queue__cell'>
                                                <Typography.Text view='primary-small' weight='bold'>
                                                    {item.number}
                                                </Typography.Text>
                                                <Typography.Text view='primary-small' color='secondary'>
                                                    {productName(item.product_code)}
                                                </Typography.Text>
                                            </span>
                                        </Table.TCell>
                                        <Table.TCell>
                                            <span className='queue__cell'>
                                                <Typography.Text view='primary-small'>{item.applicant.short_name}</Typography.Text>
                                                <Typography.Text view='primary-small' color='secondary'>
                                                    ИНН {item.applicant.inn}
                                                </Typography.Text>
                                            </span>
                                        </Table.TCell>
                                        <Table.TCell>
                                            <span className='queue__cell queue__cell_nowrap'>
                                                <Typography.Text view='primary-small'>{formatMoney(item.requested.amount)}</Typography.Text>
                                                <Typography.Text view='primary-small' color='secondary'>
                                                    {formatMonths(item.requested.term_months)}
                                                </Typography.Text>
                                            </span>
                                        </Table.TCell>
                                        <Table.TCell>
                                            {item.grade ? (
                                                <Status view='muted-alt' color={GRADE_COLORS[item.grade as keyof typeof GRADE_COLORS] ?? 'grey'} size={24}>
                                                    {item.grade}
                                                </Status>
                                            ) : (
                                                <Typography.Text view='primary-small' color='tertiary'>
                                                    —
                                                </Typography.Text>
                                            )}
                                        </Table.TCell>
                                        <Table.TCell>
                                            {item.status === 'MANUAL_REVIEW' ? (
                                                <Typography.Text view='primary-small'>{item.manual_review_reasons?.join('; ')}</Typography.Text>
                                            ) : (
                                                status && (
                                                    <Status view='muted-alt' color={status.color} size={24} uppercase={false}>
                                                        {status.label}
                                                    </Status>
                                                )
                                            )}
                                        </Table.TCell>
                                        <Table.TCell>
                                            <SourcesCell item={item} />
                                        </Table.TCell>
                                        <Table.TCell>
                                            <Typography.Text view='primary-small' color={item.assignee ? 'primary' : 'secondary'}>
                                                {item.assignee ? shortName(item.assignee.full_name) : 'Не назначен'}
                                            </Typography.Text>
                                        </Table.TCell>
                                        <Table.TCell>
                                            <Typography.Text
                                                view='primary-small'
                                                weight={sla?.urgent ? 'bold' : 'regular'}
                                                color={sla?.urgent ? 'negative' : 'primary'}
                                                className='queue__sla'
                                            >
                                                {sla?.text ?? '—'}
                                            </Typography.Text>
                                        </Table.TCell>
                                        <Table.TCell>
                                            <span className='queue__action'>
                                                {free ? (
                                                    <Button
                                                        view='primary'
                                                        size={32}
                                                        onClick={() => take(item.id)}
                                                        loading={assign.isPending && assign.variables === item.id}
                                                        aria-label={`Взять в работу ${item.number}`}
                                                    >
                                                        Взять в работу
                                                    </Button>
                                                ) : (
                                                    <Button
                                                        Component={Link}
                                                        href={`/backoffice/applications/${item.id}`}
                                                        view='secondary'
                                                        size={32}
                                                        aria-label={`Открыть ${item.number}`}
                                                    >
                                                        Открыть
                                                    </Button>
                                                )}
                                            </span>
                                        </Table.TCell>
                                    </Table.TRow>
                                );
                            })}
                        </Table.TBody>
                    </Table>
                )}
            </section>
        </div>
    );
}
