import { Plate } from '@alfalab/core-components/plate';
import { Table } from '@alfalab/core-components/table';
import { Typography } from '@alfalab/core-components/typography';

import type { ScoreResult } from '../../../api/types';
import { SOURCE_LABELS } from '../../../shared/lib/backoffice';
import { formatDate } from '../../../shared/lib/format';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { ScoreCard } from '../components/ScoreCard';
import './ScoringTab.css';

function sourceLabel(source?: string) {
    if (source && source in SOURCE_LABELS) return SOURCE_LABELS[source as keyof typeof SOURCE_LABELS];
    if (source === 'DERIVED') return 'расчёт';
    if (source === 'DECLARED') return 'анкета';
    return '—';
}

/** Вкладка «Скоринг»: признаки с источниками и баллами, стоп-факторы, лимит. */
export function ScoringTab({ score }: { score?: ScoreResult | null }) {
    if (!score) return <Plate view='common' title='Скоринг ещё не посчитан' />;
    return (
        <div className='scoring-tab'>
            <ScoreCard score={score} />
            <FormCard
                title='Признаки'
                subtitle={`Модель ${score.model_version}${score.engine ? ` · ${score.engine}` : ''} · грейд ${score.grade} · посчитано ${formatDate(score.computed_at)}`}
            >
                <Table wrapper={false}>
                    <Table.THead>
                        <Table.THeadCell>Признак</Table.THeadCell>
                        <Table.THeadCell>Значение</Table.THeadCell>
                        <Table.THeadCell>Источник</Table.THeadCell>
                        <Table.THeadCell textAlign='right'>Баллы</Table.THeadCell>
                    </Table.THead>
                    <Table.TBody>
                        {score.factors.map((f) => (
                            <Table.TRow key={f.code}>
                                <Table.TCell>
                                    <Typography.Text view='primary-small'>{f.title}</Typography.Text>
                                </Table.TCell>
                                <Table.TCell>
                                    <Typography.Text view='primary-small'>
                                        {f.bucket ?? '—'}
                                        {f.degraded && ' · источник недоступен, значение по умолчанию'}
                                    </Typography.Text>
                                </Table.TCell>
                                <Table.TCell>
                                    <Typography.Text view='primary-small'>{sourceLabel(f.source)}</Typography.Text>
                                </Table.TCell>
                                <Table.TCell>
                                    <Typography.Text view='primary-small' weight='bold' color={f.points < 0 ? 'negative' : 'primary'}>
                                        {f.points > 0 ? `+${f.points}` : f.points}
                                    </Typography.Text>
                                </Table.TCell>
                            </Table.TRow>
                        ))}
                    </Table.TBody>
                </Table>
                {score.stop_factors && score.stop_factors.length > 0 && (
                    <Plate view='negative' title='Стоп-факторы'>
                        {score.stop_factors.join(', ')}
                    </Plate>
                )}
            </FormCard>
        </div>
    );
}
