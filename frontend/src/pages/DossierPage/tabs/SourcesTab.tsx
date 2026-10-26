import { Status } from '@alfalab/core-components/status';
import { Table } from '@alfalab/core-components/table';
import { Typography } from '@alfalab/core-components/typography';

import type { SourceResult } from '../../../api/types';
import { SOURCE_MODE_LABELS, SOURCE_STATE_LABELS, sourceLabel } from '../../../shared/lib/backoffice';
import { formatUpdated } from '../../../shared/lib/format';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';

const STATE_COLORS: Record<SourceResult['state'], 'green' | 'orange' | 'blue' | 'grey'> = {
    DONE: 'green',
    FAILED: 'orange',
    RUNNING: 'blue',
    PENDING: 'grey',
    SKIPPED: 'grey',
};

/** Вкладка «Источники»: состояние сбора по каждой системе и ошибки (только для сотрудника). */
export function SourcesTab({ sources }: { sources: SourceResult[] }) {
    return (
        <FormCard title='Источники данных' subtitle='Клиент не видит, откуда собраны данные'>
            <Table wrapper={false}>
                <Table.THead>
                    <Table.THeadCell>Источник</Table.THeadCell>
                    <Table.THeadCell>Критичность</Table.THeadCell>
                    <Table.THeadCell>Состояние</Table.THeadCell>
                    <Table.THeadCell>Режим</Table.THeadCell>
                    <Table.THeadCell>Получено</Table.THeadCell>
                    <Table.THeadCell>Комментарий</Table.THeadCell>
                </Table.THead>
                <Table.TBody>
                    {sources.map((s) => (
                        <Table.TRow key={s.code}>
                            <Table.TCell>
                                <Typography.Text view='primary-small' weight='bold'>
                                    {sourceLabel(s.code)}
                                </Typography.Text>
                            </Table.TCell>
                            <Table.TCell>
                                <Typography.Text view='primary-small'>{s.criticality === 'CRITICAL' ? 'критичный' : 'обычный'}</Typography.Text>
                            </Table.TCell>
                            <Table.TCell>
                                <Status view='muted-alt' color={STATE_COLORS[s.state]} size={24} uppercase={false}>
                                    {SOURCE_STATE_LABELS[s.state]}
                                </Status>
                            </Table.TCell>
                            <Table.TCell>
                                <Typography.Text view='primary-small'>{s.mode ? SOURCE_MODE_LABELS[s.mode] : '—'}</Typography.Text>
                            </Table.TCell>
                            <Table.TCell>
                                <Typography.Text view='primary-small'>{s.fetched_at ? formatUpdated(s.fetched_at) : '—'}</Typography.Text>
                            </Table.TCell>
                            <Table.TCell>
                                <Typography.Text view='primary-small' color='secondary'>
                                    {s.error ?? s.skip_reason ?? (s.attempts && s.attempts > 1 ? `попыток: ${s.attempts}` : '')}
                                </Typography.Text>
                            </Table.TCell>
                        </Table.TRow>
                    ))}
                </Table.TBody>
            </Table>
        </FormCard>
    );
}
