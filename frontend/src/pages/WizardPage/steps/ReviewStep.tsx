import { type ReactNode, useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Checkbox } from '@alfalab/core-components/checkbox';
import { Divider } from '@alfalab/core-components/divider';
import { Input } from '@alfalab/core-components/input';
import { Link as KitLink } from '@alfalab/core-components/link';
import { Plate } from '@alfalab/core-components/plate';
import { Typography } from '@alfalab/core-components/typography';
import { Link, useNavigate } from 'react-router-dom';

import { ApiError } from '../../../api/client';
import { type SubmitFile, useSubmitQuestionnaire } from '../../../api/collector/queries';
import { MARITAL_LABELS, type TaxScope } from '../../../api/collector/types';
import { DOCUMENT_LABELS, PURPOSE_LABELS } from '../../../shared/lib/dictionaries';
import { formatMoney, formatMonths, pluralize } from '../../../shared/lib/format';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { StepLayout } from '../components/StepLayout';
import { discardDraft, type Errors, isDetachedSignature, skippedPreview, toQuestionnaire } from '../draft';
import { getFile } from '../fileStore';
import type { StepProps } from '../stepProps';
import { errorsOf, STEPS, type StepId, stepOfField } from '../steps';
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

const TAX_SCOPES: { id: TaxScope; label: string }[] = [
    { id: 'DECLARATIONS', label: 'Декларации: прибыль, НДС, УСН' },
    { id: 'ENS_BALANCE', label: 'Сальдо единого налогового счёта' },
];

type Props = StepProps & {
    /** Ошибки 422 data-collector — родитель раздаёт их по шагам. */
    onServerErrors: (errors: Errors) => void;
};

/** Шаг «Согласия и отправка»: согласия субъектов, МЧД, сводка и отправка анкеты с файлами одним запросом. */
export function ReviewStep({ userId, app, draft, hrefFor, errors, serverErrors, needsPoa, onServerErrors }: Props) {
    const navigate = useNavigate();
    const d = draft.value;
    const submit = useSubmitQuestionnaire(app.id ?? '', () => discardDraft(userId, app.id ?? ''));
    const [tried, setTried] = useState(false);
    const [missingFiles, setMissingFiles] = useState<string[]>([]);
    // файлы читаются из IndexedDB до запроса — всё это время кнопка занята, чтобы не отправить дважды
    const [reading, setReading] = useState(false);
    const errorOf = (field: string) => (tried ? errors[field] : undefined) ?? serverErrors[field];

    const toggleConsent = (key: keyof typeof d.consents, checked: boolean) =>
        draft.update((prev) => ({ consents: { ...prev.consents, [key]: checked ? new Date().toISOString() : undefined } }));

    // что не так на других шагах — со ссылками
    const blocking = STEPS.filter((s) => s.id !== 'review' && Object.keys(errorsOf(s.id, d, draft.secrets, needsPoa)).length);
    const skipped = skippedPreview(d, draft.secrets);
    const documents = d.files.filter((f) => !isDetachedSignature(f, d.files));
    const signatures = d.files.length - documents.length;

    const send = async () => {
        if (reading || submit.isPending) return;
        setTried(true);
        if (blocking.length || Object.keys(errors).length) return;
        setReading(true);
        // файлы — из браузера, в том же порядке, что в списке: по индексу data-collector укажет, какой файл не прошёл
        const files: SubmitFile[] = [];
        const lost: string[] = [];
        for (const f of d.files) {
            const file = await getFile(f.key).catch(() => undefined);
            if (file) files.push({ file, name: f.name });
            else lost.push(f.name);
        }
        setMissingFiles(lost);
        setReading(false);
        if (lost.length) return;
        onServerErrors({});
        submit.mutate(
            { questionnaire: toQuestionnaire(d, draft.secrets, app.number ?? '', needsPoa), files },
            {
                // черновик и файлы чистит сам хук (onDone) — даже если страницу уже закрыли
                onSuccess: () => navigate(`/applications/${app.id}`, { replace: true }),
                onError: (e) => {
                    if (e instanceof ApiError && (e.status === 422 || e.status === 400)) {
                        onServerErrors(Object.fromEntries((e.problem?.errors ?? []).map((x) => [x.field ?? '', x.message ?? 'Ошибка'])));
                    }
                },
            },
        );
    };

    const error = submit.error instanceof ApiError ? submit.error : undefined;
    const fieldErrors = error && (error.status === 422 || error.status === 400) ? (error.problem?.errors ?? []) : [];
    const byStep = new Map<StepId, string[]>();
    for (const e of fieldErrors) {
        const step = stepOfField(e.field ?? '');
        byStep.set(step, [...(byStep.get(step) ?? []), e.message ?? '']);
    }

    return (
        <StepLayout
            title='Согласия и отправка'
            subtitle='Проверьте данные — после отправки анкету не изменить'
            backHref={hrefFor('documents')}
            nextLabel='Отправить заявку'
            onNext={() => void send()}
            nextLoading={submit.isPending || reading}
        >
            <FormCard title='Согласия'>
                <Checkbox
                    label='Согласие компании на запрос кредитной истории в БКИ'
                    checked={Boolean(d.consents.bki_company)}
                    onChange={(_, { checked }) => toggleConsent('bki_company', checked)}
                    size={24}
                />
                <Checkbox
                    label='Согласие руководителя на запрос его кредитной истории'
                    checked={d.with_director && Boolean(d.consents.bki_person)}
                    disabled={!d.with_director}
                    hint={d.with_director ? undefined : 'Нужны данные руководителя — шаг «Руководитель и анкета»'}
                    error={errorOf('consents.bki_person')}
                    onChange={(_, { checked }) => toggleConsent('bki_person', checked)}
                    size={24}
                />
                <Checkbox
                    label='Согласие на получение налоговой отчётности'
                    checked={Boolean(d.consents.tax)}
                    onChange={(_, { checked }) => toggleConsent('tax', checked)}
                    size={24}
                />
                {d.consents.tax && (
                    <div className='review-step__scopes' role='group' aria-label='Что можно запросить в налоговой'>
                        {TAX_SCOPES.map((s) => (
                            <Checkbox
                                key={s.id}
                                label={s.label}
                                checked={d.tax_scope.includes(s.id)}
                                onChange={(_, { checked }) =>
                                    draft.update((prev) => ({ tax_scope: checked ? [...prev.tax_scope, s.id] : prev.tax_scope.filter((x) => x !== s.id) }))
                                }
                                size={24}
                            />
                        ))}
                        {errorOf('consents.tax.scope') && (
                            <Typography.Text view='primary-small' color='negative'>
                                {errorOf('consents.tax.scope')}
                            </Typography.Text>
                        )}
                    </div>
                )}
                <Typography.Text view='primary-small' color='secondary'>
                    Согласия действуют полгода. Без них банк не запросит эти данные — решение займёт больше времени.
                </Typography.Text>
            </FormCard>

            {needsPoa && (
                <FormCard title='Доверенность'>
                    <Typography.Text view='primary-small' color='secondary'>
                        Вы оформляете заявку не как руководитель — укажите машиночитаемую доверенность (МЧД), если она уже есть.
                    </Typography.Text>
                    <Input
                        label='Номер МЧД'
                        value={d.poa_number}
                        onChange={(_, { value }) => draft.update({ poa_number: value.trim() })}
                        error={errorOf('poa_number')}
                        hint='Например, 0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37'
                        block={true}
                        size={56}
                    />
                    <Input
                        label='Ваш ИНН физлица'
                        value={d.representative_inn}
                        inputMode='numeric'
                        onChange={(_, { value }) => draft.update({ representative_inn: value.replace(/\D/g, '').slice(0, 12) })}
                        error={errorOf('representative_inn')}
                        block={true}
                        size={56}
                    />
                </FormCard>
            )}

            <FormCard>
                <Section
                    title='Сумма, срок и цель'
                    editHref={hrefFor('terms')}
                    rows={[
                        ['Сумма', d.requested && formatMoney(d.requested.amount)],
                        ['Срок', d.requested && formatMonths(d.requested.term_months)],
                        ['Цель', d.purpose?.code && PURPOSE_LABELS[d.purpose.code]],
                    ]}
                />
                <Divider />
                <Section
                    title='Руководитель и анкета'
                    editHref={hrefFor('borrower')}
                    rows={[
                        ['Возраст', d.age === undefined ? undefined : `${d.age} ${pluralize(d.age, 'год', 'года', 'лет')}`],
                        ['Семейное положение', d.marital_status && MARITAL_LABELS[d.marital_status]],
                        ['Дети и иждивенцы', d.children_count],
                        ['Руководитель', d.with_director ? [d.director.last_name, d.director.first_name, d.director.middle_name].filter(Boolean).join(' ') || undefined : 'не указан'],
                    ]}
                />
                <Divider />
                <Section
                    title='Счета, площадки и залог'
                    editHref={hrefFor('sources')}
                    rows={[
                        ['Другие банки', d.other_bank_biks.length || 'нет'],
                        ['Маркетплейсы', Object.entries(draft.secrets.marketplace_tokens).filter(([, t]) => t?.trim()).map(([m]) => (m === 'ozon' ? 'Ozon' : 'Wildberries')).join(', ') || 'нет'],
                        ['Онлайн-кассы', draft.secrets.ofd_token.trim() ? 'токен ОФД указан' : 'нет'],
                        ['Залог', d.collateral.cadastral_numbers.length + d.collateral.vins.length + d.collateral.serials.length || 'нет'],
                    ]}
                />
                <Divider />
                <Section
                    title='Документы'
                    editHref={hrefFor('documents')}
                    rows={[
                        ['Файлы', documents.length ? documents.map((f) => `${f.name} (${DOCUMENT_LABELS[f.type ?? 'OTHER']})`).join(', ') : 'нет'],
                        ['Подписи', signatures || 'нет'],
                    ]}
                />
            </FormCard>

            {skipped.length > 0 && (
                <Plate view='common' title='Не сможем проверить'>
                    <ul className='review-step__missing'>
                        {skipped.map((s) => (
                            <li key={s}>
                                <Typography.Text view='primary-small'>{s}</Typography.Text>
                            </li>
                        ))}
                    </ul>
                </Plate>
            )}

            {tried && blocking.length > 0 && (
                <Plate view='negative' title='Анкету пока не отправить'>
                    <ul className='review-step__missing'>
                        {blocking.map((s) => (
                            <li key={s.id}>
                                <KitLink Component={Link} href={hrefFor(s.id)} view='default'>
                                    {s.title}
                                </KitLink>
                            </li>
                        ))}
                    </ul>
                </Plate>
            )}
            {missingFiles.length > 0 && (
                <Plate view='negative' title='Файлы пропали из браузера'>
                    Загрузите заново на шаге «Документы»: {missingFiles.join(', ')}
                </Plate>
            )}

            {byStep.size > 0 && (
                <Plate view='negative' title='Банк не принял анкету'>
                    <ul className='review-step__missing'>
                        {[...byStep].map(([step, messages]) => (
                            <li key={step}>
                                <KitLink Component={Link} href={hrefFor(step)} view='default'>
                                    {STEPS.find((s) => s.id === step)?.title}
                                </KitLink>
                                <Typography.Text view='primary-small'> — {messages.join('; ')}</Typography.Text>
                            </li>
                        ))}
                    </ul>
                </Plate>
            )}
            {error?.status === 409 && (
                <Plate view='attention' title='Анкету по этой заявке уже отправили'>
                    <Button Component={Link} href={`/applications/${app.id}`} view='secondary' size={40}>
                        К статусу заявки
                    </Button>
                </Plate>
            )}
            {submit.error && !fieldErrors.length && error?.status !== 409 && (
                <Plate view='negative' title='Не получилось отправить заявку'>
                    {error?.status === 413 ? 'Файлы слишком большие: все вместе — не больше 200 МБ' : submit.error.message}
                </Plate>
            )}
        </StepLayout>
    );
}
