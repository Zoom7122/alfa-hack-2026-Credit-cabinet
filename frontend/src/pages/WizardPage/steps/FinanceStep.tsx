import { useState } from 'react';
import { AmountInput } from '@alfalab/core-components/amount-input';
import { Button } from '@alfalab/core-components/button';
import { Input } from '@alfalab/core-components/input';
import { Plate } from '@alfalab/core-components/plate';
import { Select } from '@alfalab/core-components/select';
import { Typography } from '@alfalab/core-components/typography';

import { useApplication, useDocuments } from '../../../api/queries';
import type { DeclaredFinancials, Document } from '../../../api/types';
import { BANKS } from '../../../shared/lib/dictionaries';
import { formatCompactMoney, formatDate } from '../../../shared/lib/format';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { DocumentUploader } from '../components/DocumentUploader';
import { StepLayout } from '../components/StepLayout';
import type { StepProps } from '../stepProps';
import { reportingYear } from '../steps';
import './FinanceStep.css';

type MoneyField = 'annual_revenue' | 'net_profit' | 'avg_monthly_turnover' | 'avg_monthly_expenses';
type Field = MoneyField | 'employees_count';

const REQUIRED_HINTS: Record<Field, string> = {
    annual_revenue: 'Укажите выручку — по ней считаем лимит',
    net_profit: 'Укажите прибыль или убыток',
    avg_monthly_turnover: 'Укажите поступления на счета',
    avg_monthly_expenses: 'Нужно для расчёта платежа, который комфортен бизнесу',
    employees_count: 'Укажите число сотрудников',
};

/** Распознанные из отчётности цифры за нужный год, если есть. */
function extractedFigures(doc: Document, year: number) {
    const extracted = doc.recognition?.state === 'DONE' ? doc.recognition.extracted : undefined;
    const revenue = extracted?.[`revenue_${year}`];
    const profit = extracted?.[`net_profit_${year}`];
    return {
        annual_revenue: typeof revenue === 'number' ? revenue : undefined,
        net_profit: typeof profit === 'number' ? profit : undefined,
    };
}

export function FinanceStep({ app, product, draft, hrefFor }: StepProps) {
    const year = reportingYear(app);
    const [fin, setFin] = useState<DeclaredFinancials>(app.financials ?? {});
    const previous = useApplication(app.prefilled_from ?? undefined);
    const documents = useDocuments(app.id);
    const statements = (documents.data ?? []).filter((d) => d.type === 'FINANCIAL_STATEMENTS');

    const required = (Object.keys(REQUIRED_HINTS) as Field[]).filter((f) => product?.required_fields?.includes(`financials.${f}`));
    // ошибки показываем, только когда раздел уже начали заполнять — пустая форма не должна краснеть
    const started = required.some((f) => fin[f] !== undefined);
    const errorFor = (field: Field) => (started && required.includes(field) && fin[field] === undefined ? REQUIRED_HINTS[field] : undefined);

    const change = (patch: DeclaredFinancials) => {
        setFin((prev) => ({ ...prev, ...patch }));
        draft.update({ financials: { ...patch, annual_revenue_year: year } });
    };
    const clearable = (field: Field, value: number | null) => {
        setFin((prev) => {
            const next = { ...prev };
            if (value === null) delete next[field];
            else next[field] = value;
            return next;
        });
        draft.update({ financials: { [field]: value, annual_revenue_year: year } });
    };

    const money = (field: MoneyField, label: string, hint?: string, allowNegative?: boolean) => (
        <AmountInput
            label={label}
            value={fin[field] ?? null}
            minority={1}
            integersOnly={true}
            positiveOnly={!allowNegative}
            onChange={(_, { value }) => clearable(field, value)}
            block={true}
            size={56}
            hint={hint}
            error={errorFor(field)}
        />
    );

    return (
        <StepLayout
            title='Финансы компании'
            subtitle='Примерных цифр достаточно — мы сверим их с отчётностью сами'
            backHref={hrefFor('terms')}
            nextHref={hrefFor('debts')}
        >
            {app.prefilled_from && (
                <Plate
                    view='common'
                    title={`Подставили данные с прошлой заявки${previous.data ? ` от ${formatDate(previous.data.created_at)}` : ''}`}
                >
                    Проверьте, что цифры актуальны
                </Plate>
            )}

            <FormCard>
                <div className='finance-step__grid'>
                    {money('annual_revenue', `Выручка за ${year} год`)}
                    {money('net_profit', `Чистая прибыль за ${year} год`, 'Если был убыток — укажите со знаком минус', true)}
                    {money('avg_monthly_turnover', 'Поступления на счета в месяц', 'В среднем за последние 6 месяцев')}
                    {money('avg_monthly_expenses', 'Регулярные расходы в месяц', 'Аренда, зарплаты, налоги, платежи по кредитам')}
                    <Input
                        label='Сотрудников'
                        value={fin.employees_count === undefined ? '' : String(fin.employees_count)}
                        inputMode='numeric'
                        onChange={(_, { value }) => {
                            const digits = value.replace(/\D/g, '').slice(0, 6);
                            clearable('employees_count', digits ? Number(digits) : null);
                        }}
                        block={true}
                        size={56}
                        error={errorFor('employees_count')}
                    />
                    <Select
                        label='Основной банк'
                        size={56}
                        block={true}
                        options={BANKS.map((bank) => ({ key: bank, content: bank }))}
                        selected={fin.main_bank ?? null}
                        onChange={({ selected }) => selected && change({ main_bank: selected.key })}
                    />
                </div>
            </FormCard>

            <FormCard title='Загрузите отчётность — заполним поля сами' subtitle='Бухгалтерский баланс и отчёт о финансовых результатах'>
                <DocumentUploader
                    applicationId={app.id}
                    type='FINANCIAL_STATEMENTS'
                    documents={statements}
                    renderExtra={(doc) => {
                        const figures = extractedFigures(doc, year);
                        const differs =
                            (figures.annual_revenue !== undefined && figures.annual_revenue !== fin.annual_revenue) ||
                            (figures.net_profit !== undefined && figures.net_profit !== fin.net_profit);
                        if (!differs) return null;
                        return (
                            <div className='finance-step__extracted'>
                                <Typography.Text view='primary-small'>
                                    В отчётности: выручка {formatCompactMoney(figures.annual_revenue ?? 0)} ₽, прибыль{' '}
                                    {formatCompactMoney(figures.net_profit ?? 0)} ₽
                                </Typography.Text>
                                <Button
                                    view='secondary'
                                    size={32}
                                    onClick={() =>
                                        change({
                                            ...(figures.annual_revenue !== undefined && { annual_revenue: figures.annual_revenue }),
                                            ...(figures.net_profit !== undefined && { net_profit: figures.net_profit }),
                                        })
                                    }
                                >
                                    Подставить
                                </Button>
                            </div>
                        );
                    }}
                />
            </FormCard>
        </StepLayout>
    );
}
