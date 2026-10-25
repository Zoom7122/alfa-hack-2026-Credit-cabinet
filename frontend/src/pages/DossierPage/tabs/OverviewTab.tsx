import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Status } from '@alfalab/core-components/status';
import { Table } from '@alfalab/core-components/table';
import { Typography } from '@alfalab/core-components/typography';
import { Link } from 'react-router-dom';

import { useBoRefreshSource } from '../../../api/queries';
import type { BoDossier, Finding, Offer, SourceResult } from '../../../api/types';
import { BO_STATUS_BADGES, SOURCE_LABELS, SOURCE_MODE_LABELS, SOURCE_STATE_LABELS } from '../../../shared/lib/backoffice';
import { COLLATERAL_LABELS, fieldLabel } from '../../../shared/lib/dictionaries';
import { formatCompactMoney, formatMoney, formatMonths, formatRate, formatUpdated } from '../../../shared/lib/format';
import { InfoCard } from '../../../shared/ui/InfoCard/InfoCard';
import { ScoreCard } from '../components/ScoreCard';
import './OverviewTab.css';

const capitalize = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

/** Значение из Finding: деньги — в рублях, исполнительные производства — «1 · 42 300 ₽». */
function formatValue(value: unknown, field: string | null | undefined): string {
    if (value === null || value === undefined) return '—';
    if (typeof value === 'number') return field?.includes('revenue') || field?.includes('profit') || field?.includes('turnover') ? formatMoney(value) : String(value);
    if (typeof value === 'object' && 'count' in value && 'amount' in value) {
        const v = value as { count: number; amount: number };
        return `${v.count} · ${formatMoney(v.amount)}`;
    }
    return String(value);
}

const FINDING_TITLES: Record<string, string> = {
    ENFORCEMENT_PROCEEDINGS: 'Исп. производства',
};

const findingTitle = (f: Finding) => (f.field ? capitalize(fieldLabel(f.field)) : (FINDING_TITLES[f.code] ?? f.message));

function FindingsCard({ findings }: { findings: Finding[] }) {
    const conservative = findings.find((f) => f.material && f.code === 'REVENUE_MISMATCH' && typeof f.found_value === 'number');
    return (
        <section className='overview__card' aria-labelledby='overview-findings'>
            <Typography.Title id='overview-findings' tag='h2' view='xsmall' font='system'>
                Расхождения «заявлено ↔ найдено»
            </Typography.Title>
            {findings.length === 0 ? (
                <Typography.Text view='primary-small' color='secondary'>
                    Расхождений не нашли
                </Typography.Text>
            ) : (
                <Table wrapper={false}>
                    <Table.THead>
                        <Table.THeadCell>Поле</Table.THeadCell>
                        <Table.THeadCell>Заявлено</Table.THeadCell>
                        <Table.THeadCell>Найдено</Table.THeadCell>
                        <Table.THeadCell>Источник</Table.THeadCell>
                        <Table.THeadCell width={90}>Влияет</Table.THeadCell>
                    </Table.THead>
                    <Table.TBody>
                        {findings.map((f) => (
                            <Table.TRow key={`${f.code}:${f.field ?? ''}`}>
                                <Table.TCell>
                                    <Typography.Text view='primary-small'>{findingTitle(f)}</Typography.Text>
                                </Table.TCell>
                                <Table.TCell>
                                    <Typography.Text view='primary-small'>{formatValue(f.declared_value, f.field)}</Typography.Text>
                                </Table.TCell>
                                <Table.TCell>
                                    <Typography.Text view='primary-small' weight={f.material ? 'bold' : 'regular'} color={f.material ? 'negative' : 'primary'}>
                                        {formatValue(f.found_value, f.field)}
                                    </Typography.Text>
                                </Table.TCell>
                                <Table.TCell>
                                    <Typography.Text view='primary-small'>{f.source ? SOURCE_LABELS[f.source] : '—'}</Typography.Text>
                                </Table.TCell>
                                <Table.TCell>
                                    <Status view='muted-alt' color={f.material ? 'red' : 'grey'} size={24} uppercase={false}>
                                        {f.material ? 'Да' : 'Нет'}
                                    </Status>
                                </Table.TCell>
                            </Table.TRow>
                        ))}
                    </Table.TBody>
                </Table>
            )}
            {conservative && typeof conservative.found_value === 'number' && (
                <Typography.Text view='primary-small' color='secondary'>
                    Скоринг посчитан на консервативном значении выручки — {formatCompactMoney(conservative.found_value)} ₽
                </Typography.Text>
            )}
        </section>
    );
}

/** Правая колонка: источник — точка состояния, когда и откуда; упавший можно перезапросить. */
function SourcesCard({ applicationId, sources }: { applicationId: string; sources: SourceResult[] }) {
    const refresh = useBoRefreshSource(applicationId);
    return (
        <InfoCard title='Источники данных'>
            <ul className='overview__sources'>
                {sources.map((s) => (
                    <li key={s.code} className='overview__source'>
                        <span className={`overview__dot overview__dot_state_${s.state.toLowerCase()}`} aria-hidden='true' />
                        <Typography.Text view='primary-small' className='overview__source-name'>
                            {SOURCE_LABELS[s.code]}
                        </Typography.Text>
                        {s.state === 'FAILED' ? (
                            <Button
                                view='text'
                                size={32}
                                onClick={() => refresh.mutate(s.code)}
                                loading={refresh.isPending && refresh.variables === s.code}
                                aria-label={`Перезапросить ${SOURCE_LABELS[s.code]}`}
                            >
                                Перезапросить
                            </Button>
                        ) : (
                            <Typography.Text view='primary-small' color='secondary'>
                                {s.state === 'DONE'
                                    ? [s.mode && SOURCE_MODE_LABELS[s.mode], s.fetched_at && formatUpdated(s.fetched_at)].filter(Boolean).join(' · ')
                                    : s.state === 'SKIPPED'
                                      ? (s.skip_reason ?? SOURCE_STATE_LABELS.SKIPPED)
                                      : SOURCE_STATE_LABELS[s.state]}
                            </Typography.Text>
                        )}
                    </li>
                ))}
            </ul>
            {refresh.error && <Plate view='negative' title='Не удалось перезапросить источник' />}
        </InfoCard>
    );
}

const KIND_LABELS: Record<Offer['kind'], string> = { REQUESTED: 'Как запрошено', ALTERNATIVE: 'Альтернатива', RETENTION: 'Удерживающее' };
const STATE_LABELS: Record<Offer['state'], string> = { ACTIVE: 'Ждёт клиента', ACCEPTED: 'Выбрано', WITHDRAWN: 'Отозвано', EXPIRED: 'Истекло' };

function OfferList({ offers, withState }: { offers: Offer[]; withState?: boolean }) {
    return (
        <ul className='overview__offers'>
            {offers.map((o) => {
                const terms = [
                    o.guarantee_required && 'поручитель',
                    ...(o.collateral_required ?? []).map((c) => `залог: ${COLLATERAL_LABELS[c].toLowerCase()}`),
                    ...(o.conditions ?? []),
                ].filter(Boolean);
                return (
                    <li key={o.id} className='overview__offer'>
                        <div className='overview__offer-head'>
                            <Typography.Text view='primary-medium' weight='bold'>
                                {formatMoney(o.amount)} · {formatMonths(o.term_months)}
                            </Typography.Text>
                            <Status view='muted-alt' color={withState && o.state === 'ACCEPTED' ? 'green' : 'grey'} size={20} uppercase={false}>
                                {withState ? STATE_LABELS[o.state] : KIND_LABELS[o.kind]}
                            </Status>
                        </div>
                        <Typography.Text view='primary-small' color='secondary'>
                            {formatRate(o.rate_annual)} · {formatMoney(o.monthly_payment)}/мес.
                            {terms.length > 0 && ` · ${terms.join(' · ')}`}
                        </Typography.Text>
                    </li>
                );
            })}
        </ul>
    );
}

/** Экран 17, «Обзор»: почему на ручном, расхождения, скоринг; справа — предложения движка, источники, другие заявки. */
export function OverviewTab({ dossier }: { dossier: BoDossier }) {
    const engine = dossier.engine_decision;
    const app = dossier.application;
    const proposed = engine?.proposed_offers ?? [];
    const issued = dossier.offers ?? [];
    return (
        <div className='overview'>
            <div className='overview__main'>
                {engine && (
                    <section className='overview__card' aria-labelledby='overview-reason'>
                        <Typography.Title id='overview-reason' tag='h2' view='xsmall' font='system'>
                            {engine.outcome !== 'MANUAL_REVIEW'
                                ? 'Решение движка'
                                : app.status === 'MANUAL_REVIEW'
                                  ? 'Почему заявка на ручном рассмотрении'
                                  : 'Почему заявка была на ручном рассмотрении'}
                        </Typography.Title>
                        {engine.rules_fired.map((rule) => (
                            <Plate key={rule.code} view={rule.effect === 'MANUAL_REVIEW' ? 'attention' : 'common'} title={rule.detail ?? rule.code}>
                                Правило {rule.code} → {rule.effect}
                            </Plate>
                        ))}
                    </section>
                )}
                <FindingsCard findings={dossier.findings} />
                {dossier.score ? (
                    <ScoreCard score={dossier.score} />
                ) : (
                    <Plate view='common' title='Скоринг ещё не посчитан'>
                        Дождитесь сбора данных по компании.
                    </Plate>
                )}
            </div>

            <aside className='overview__aside'>
                {issued.length > 0 && (
                    <InfoCard title='Предложения клиенту'>
                        <OfferList offers={issued} withState={true} />
                    </InfoCard>
                )}
                {proposed.length > 0 && (
                    <InfoCard title='Предложения движка'>
                        <OfferList offers={proposed} />
                    </InfoCard>
                )}
                <SourcesCard applicationId={app.id} sources={dossier.sources} />
                {dossier.other_applications && dossier.other_applications.length > 0 && (
                    <InfoCard title='Другие заявки компании'>
                        <ul className='overview__others'>
                            {dossier.other_applications.map((a) => {
                                const badge = BO_STATUS_BADGES[a.status];
                                return (
                                    <li key={a.id} className='overview__other'>
                                        <Link to={`/backoffice/applications/${a.id}`} className='overview__other-link'>
                                            <Typography.Text view='primary-small'>
                                                {a.number}
                                                {a.requested && ` · ${formatCompactMoney(a.requested.amount)} ₽`}
                                            </Typography.Text>
                                        </Link>
                                        {badge && (
                                            <Status view='muted-alt' color={badge.color} size={20} uppercase={false}>
                                                {badge.label}
                                            </Status>
                                        )}
                                    </li>
                                );
                            })}
                        </ul>
                    </InfoCard>
                )}
            </aside>
        </div>
    );
}
