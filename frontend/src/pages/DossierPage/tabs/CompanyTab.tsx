import { Table } from '@alfalab/core-components/table';
import { Typography } from '@alfalab/core-components/typography';

import type { ApplicantFull } from '../../../api/types';
import { formatMoney } from '../../../shared/lib/format';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { SummaryList, type SummaryRow } from '../../../shared/ui/SummaryList/SummaryList';
import './CompanyTab.css';

const LABELS: Record<string, string> = {
    status: 'Статус в реестре',
    registration_date: 'Дата регистрации',
    okved: 'Основной ОКВЭД',
    director: 'Руководитель',
    address: 'Юридический адрес',
    active_loans: 'Действующих кредитов',
    overdue_90d: 'Просрочек 90+ дней',
    last_request: 'Последний запрос в БКИ',
    count: 'Исполнительных производств',
    amount: 'Сумма производств',
    as_defendant: 'Дел в роли ответчика',
    as_plaintiff: 'Дел в роли истца',
    client_since: 'Клиент банка с',
    turnover_share: 'Доля оборотов в банке',
};

/** Объект из досье (поля — docs/data-collection.md) в строки «подпись — значение». */
function rowsOf(data: Record<string, unknown> | undefined, money: string[] = []): SummaryRow[] {
    return Object.entries(data ?? {})
        .filter(([, v]) => v !== undefined && v !== null && v !== '')
        .map(([key, v]) => ({ label: LABELS[key] ?? key, value: money.includes(key) && typeof v === 'number' ? formatMoney(v) : String(v) }));
}

/** Вкладка «Данные компании»: реестр, отчётность по годам, кредитная история и риски. */
export function CompanyTab({ applicant }: { applicant: ApplicantFull }) {
    const statements = applicant.financial_statements ?? [];
    return (
        <div className='company-tab'>
            <FormCard title={applicant.short_name ?? applicant.full_name} subtitle={`ИНН ${applicant.inn}${applicant.ogrn ? ` · ОГРН ${applicant.ogrn}` : ''}`}>
                <SummaryList plain={true} rows={rowsOf(applicant.registry)} />
            </FormCard>
            {statements.length > 0 && (
                <FormCard title='Бухгалтерская отчётность' subtitle='ГИР БО'>
                    <Table wrapper={false}>
                        <Table.THead>
                            <Table.THeadCell>Год</Table.THeadCell>
                            <Table.THeadCell textAlign='right'>Выручка</Table.THeadCell>
                            <Table.THeadCell textAlign='right'>Чистая прибыль</Table.THeadCell>
                            <Table.THeadCell textAlign='right'>Активы</Table.THeadCell>
                            <Table.THeadCell textAlign='right'>Капитал</Table.THeadCell>
                        </Table.THead>
                        <Table.TBody>
                            {statements.map((row) => (
                                <Table.TRow key={row.year}>
                                    <Table.TCell>
                                        <Typography.Text view='primary-small'>{row.year}</Typography.Text>
                                    </Table.TCell>
                                    <Table.TCell>
                                        <Typography.Text view='primary-small'>{row.revenue !== undefined ? formatMoney(row.revenue) : '—'}</Typography.Text>
                                    </Table.TCell>
                                    <Table.TCell>
                                        <Typography.Text view='primary-small'>{row.net_profit !== undefined ? formatMoney(row.net_profit) : '—'}</Typography.Text>
                                    </Table.TCell>
                                    <Table.TCell>
                                        <Typography.Text view='primary-small'>{row.total_assets !== undefined ? formatMoney(row.total_assets) : '—'}</Typography.Text>
                                    </Table.TCell>
                                    <Table.TCell>
                                        <Typography.Text view='primary-small'>{row.equity !== undefined ? formatMoney(row.equity) : '—'}</Typography.Text>
                                    </Table.TCell>
                                </Table.TRow>
                            ))}
                        </Table.TBody>
                    </Table>
                </FormCard>
            )}
            <FormCard title='Кредитная история и риски'>
                <SummaryList
                    plain={true}
                    rows={[
                        ...rowsOf(applicant.credit_history),
                        ...rowsOf(applicant.enforcement, ['amount']),
                        ...rowsOf(applicant.litigation),
                        ...rowsOf(applicant.bank_relationship),
                        { label: 'Флаги риска', value: applicant.risk_flags?.length ? applicant.risk_flags.join(', ') : 'нет' },
                    ]}
                />
            </FormCard>
        </div>
    );
}
