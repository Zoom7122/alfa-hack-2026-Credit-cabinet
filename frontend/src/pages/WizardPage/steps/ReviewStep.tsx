import { type ReactNode, useRef, useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Checkbox } from '@alfalab/core-components/checkbox';
import { Divider } from '@alfalab/core-components/divider';
import { Link as KitLink } from '@alfalab/core-components/link';
import { Plate } from '@alfalab/core-components/plate';
import { Typography } from '@alfalab/core-components/typography';
import { useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from 'react-router-dom';

import { ApiError } from '../../../api/client';
import { queryKeys, useDocuments, useSubmitApplication } from '../../../api/queries';
import type { Application } from '../../../api/types';
import {
    COLLATERAL_LABELS,
    DOCUMENT_LABELS,
    fieldLabel,
    GUARANTOR_RELATION_LABELS,
    PURPOSE_LABELS,
} from '../../../shared/lib/dictionaries';
import { formatMoney, formatMonths, pluralize, shortName } from '../../../shared/lib/format';
import { newIdempotencyKey } from '../../../shared/lib/idempotency';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { StepLayout } from '../components/StepLayout';
import type { StepProps } from '../stepProps';
import { missingDocuments, reportingYear, type StepId, stepOfField } from '../steps';
import './ReviewStep.css';

type Row = [label: string, value: ReactNode];

function Section({ title, rows, editHref }: { title: string; rows: Row[]; editHref: string }) {
    return (
        <div className='review-step__section'>
            <div className='review-step__section-head'>
                <Typography.Title tag='h2' view='xsmall' font='system'>
                    {title}
                </Typography.Title>
                <KitLink Component={Link} href={editHref} view='default' aria-label={`Изменить: ${title}`}>
                    Изменить
                </KitLink>
            </div>
            <dl className='review-step__rows'>
                {rows.map(([label, value]) => (
                    <div key={label} className='review-step__row'>
                        <dt className='review-step__label'>
                            <Typography.Text view='primary-medium' color='secondary'>
                                {label}
                            </Typography.Text>
                        </dt>
                        <dd className='review-step__value'>
                            <Typography.Text view='primary-medium'>{value ?? '—'}</Typography.Text>
                        </dd>
                    </div>
                ))}
            </dl>
        </div>
    );
}

function debtsText(app: Application) {
    if (app.has_no_debts) return 'Нет';
    if (!app.debts?.length) return undefined;
    const monthly = app.debts.reduce((sum, d) => sum + d.monthly_payment, 0);
    return `${app.debts.length} · платёж ${formatMoney(monthly)}/мес.`;
}

function collateralText(app: Application, required: boolean) {
    if (app.collateral?.length) {
        return app.collateral.map((c) => `${COLLATERAL_LABELS[c.type]}, ${formatMoney(c.estimated_value)}`).join('; ');
    }
    return required ? undefined : 'Не требуется для этой суммы';
}

function guarantorsText(app: Application) {
    if (!app.guarantors?.length) return 'Нет';
    return app.guarantors
        .map((g) => [g.type === 'INDIVIDUAL' ? shortName(g.name) : g.name, g.relation && GUARANTOR_RELATION_LABELS[g.relation].toLowerCase()].filter(Boolean).join(', '))
        .join('; ');
}

/** Ошибки 422 по шагам — со ссылками туда, где их поправить. */
function groupByStep(fields: string[]) {
    const groups = new Map<StepId, string[]>();
    for (const f of fields) {
        const step = stepOfField(f);
        groups.set(step, [...(groups.get(step) ?? []), fieldLabel(f)]);
    }
    return groups;
}

export function ReviewStep({ app, product, draft, hrefFor }: StepProps) {
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const documents = useDocuments(app.id);
    const submit = useSubmitApplication();
    const idempotencyKey = useRef(newIdempotencyKey());
    const [consents, setConsents] = useState({
        personal_data: app.consents?.personal_data === true,
        credit_bureau: app.consents?.credit_bureau === true,
    });

    const year = reportingYear(app);
    const threshold = product?.collateral?.required_above_amount;
    const collateralRequired = threshold !== null && threshold !== undefined && (app.requested?.amount ?? 0) > threshold;
    const missingData = app.completeness.missing_fields.filter((f) => !f.startsWith('consents.'));
    const missingDocs = missingDocuments(product, app, documents.data);
    const consentsGiven = consents.personal_data && consents.credit_bureau;

    const problem = submit.error instanceof ApiError && submit.error.status === 422 ? submit.error.problem : undefined;
    const serverErrors = problem?.code === 'DRAFT_INCOMPLETE' ? (problem.errors ?? []) : [];
    const errorFields = serverErrors.length ? serverErrors.map((e) => e.field) : missingData;
    const missingGroups = groupByStep(errorFields.filter((f) => !f.startsWith('consents.')));
    const consentError = problem?.code === 'CONSENT_REQUIRED';
    // 422, который не разложить по полям анкеты, показываем текстом сервера
    const otherProblem = problem && !consentError && !serverErrors.length;

    const setConsent = (key: keyof typeof consents, checked: boolean) => {
        setConsents((prev) => ({ ...prev, [key]: checked }));
        draft.update({ consents: { [key]: checked } });
    };

    const send = async () => {
        try {
            await draft.flush();
            const version = queryClient.getQueryData<Application>(queryKeys.application(app.id))?.version ?? app.version;
            await submit.mutateAsync({ id: app.id, version, idempotencyKey: idempotencyKey.current });
            navigate(`/applications/${app.id}`, { replace: true });
        } catch (e) {
            if (!(e instanceof ApiError)) return;
            // однозначный отказ: исправленная попытка — это новый запрос, со старым ключом бэк вернул бы тот же ответ
            if (e.status >= 400 && e.status < 500) idempotencyKey.current = newIdempotencyKey();
            // версия устарела — перечитываем, следующая попытка уйдёт со свежей
            if (e.status === 412) await queryClient.refetchQueries({ queryKey: queryKeys.application(app.id) });
            // сервер не видит согласий — снимаем галочки, которых у него нет
            if (e.code === 'CONSENT_REQUIRED') {
                const fields = new Set(e.problem?.errors?.map((x) => x.field));
                setConsents((prev) => ({
                    personal_data: prev.personal_data && !fields.has('consents.personal_data'),
                    credit_bureau: prev.credit_bureau && !fields.has('consents.credit_bureau'),
                }));
            }
        }
    };

    const hint = missingGroups.size
        ? 'Заполните недостающие поля, чтобы отправить'
        : !consentsGiven
          ? 'Дайте оба согласия, чтобы отправить'
          : undefined;

    return (
        <StepLayout
            title='Проверьте заявку'
            backHref={hrefFor('documents')}
            nextLabel='Отправить заявку'
            onNext={send}
            nextDisabled={!consentsGiven || missingGroups.size > 0 || draft.status === 'error'}
            nextLoading={submit.isPending}
            hint={hint}
        >
            <FormCard>
                <Section
                    title='Кредит'
                    editHref={hrefFor('terms')}
                    rows={[
                        ['Продукт', product?.name],
                        ['Сумма и срок', app.requested && `${formatMoney(app.requested.amount)} на ${formatMonths(app.requested.term_months)}`],
                        ['Цель', app.purpose?.description || (app.purpose?.code && PURPOSE_LABELS[app.purpose.code])],
                    ]}
                />
                <Divider />
                <Section
                    title='Финансы'
                    editHref={hrefFor('finance')}
                    rows={[
                        [`Выручка за ${year}`, app.financials?.annual_revenue !== undefined ? formatMoney(app.financials.annual_revenue) : undefined],
                        [
                            'Поступления в месяц',
                            app.financials?.avg_monthly_turnover !== undefined ? formatMoney(app.financials.avg_monthly_turnover) : undefined,
                        ],
                        ['Действующие кредиты', debtsText(app)],
                    ]}
                />
                <Divider />
                <Section
                    title='Залог и поручители'
                    editHref={hrefFor('collateral')}
                    rows={[
                        ['Залог', collateralText(app, collateralRequired)],
                        ['Поручители', guarantorsText(app)],
                    ]}
                />
            </FormCard>

            {missingGroups.size > 0 && (
                <Plate view='negative' title='Не хватает данных'>
                    <ul className='review-step__missing'>
                        {[...missingGroups].map(([step, labels]) => (
                            <li key={step}>
                                <KitLink Component={Link} href={hrefFor(step)} view='default'>
                                    {labels.join(', ')}
                                </KitLink>
                            </li>
                        ))}
                    </ul>
                </Plate>
            )}

            {missingDocs.length > 0 && (
                <Plate
                    view='attention'
                    title={
                        missingDocs.includes('FINANCIAL_STATEMENTS')
                            ? 'Можно отправить без отчётности'
                            : `Можно отправить без ${pluralize(missingDocs.length, 'документа', 'документов', 'документов')}`
                    }
                    buttons={
                        <Button Component={Link} href={hrefFor('documents')} view='secondary' size={32}>
                            Загрузить
                        </Button>
                    }
                >
                    Но с ними решение будет быстрее и сумма может быть выше: {missingDocs.map((t) => DOCUMENT_LABELS[t].toLowerCase()).join(', ')}
                </Plate>
            )}

            <FormCard>
                {consentError && (
                    <Plate view='negative' title='Банк не получил согласия'>
                        Отметьте их ещё раз и отправьте заявку.
                    </Plate>
                )}
                <Checkbox
                    label='Согласен на обработку персональных данных'
                    checked={consents.personal_data}
                    onChange={(_, { checked }) => setConsent('personal_data', checked)}
                    size={24}
                />
                <Checkbox
                    label='Согласен на запрос кредитной истории в бюро кредитных историй'
                    checked={consents.credit_bureau}
                    onChange={(_, { checked }) => setConsent('credit_bureau', checked)}
                    size={24}
                />
            </FormCard>

            {submit.error && (otherProblem || !(submit.error instanceof ApiError && submit.error.status === 422)) && (
                <Plate view='negative' title='Не получилось отправить заявку'>
                    {submit.error instanceof ApiError && submit.error.status === 412
                        ? 'Заявку изменили в другой вкладке — мы обновили данные, проверьте и отправьте ещё раз.'
                        : (problem?.detail ?? submit.error.message)}
                </Plate>
            )}
        </StepLayout>
    );
}
