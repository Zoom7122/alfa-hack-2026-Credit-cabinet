import { useState } from 'react';
import { Input } from '@alfalab/core-components/input';
import { Plate } from '@alfalab/core-components/plate';
import { Radio } from '@alfalab/core-components/radio';
import { RadioGroup } from '@alfalab/core-components/radio-group';
import { Spinner } from '@alfalab/core-components/spinner';
import { Typography } from '@alfalab/core-components/typography';
import { useNavigate } from 'react-router-dom';

import { ApiError } from '../../../api/client';
import { useCompanyLookup, useRegister } from '../../../api/collector/queries';
import { COLLECTOR_ROLE_LABELS, COLLECTOR_ROLES, type CollectorApplication, type CollectorPurpose, type CollectorRole } from '../../../api/collector/types';
import type { components } from '../../../api/collector/schema';
import type { Me } from '../../../api/types';
import { formatIsoDate } from '../../../shared/lib/format';
import { isInn } from '../../../shared/lib/inn';
import { useDebouncedValue } from '../../../shared/lib/useDebouncedValue';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { StepLayout } from '../components/StepLayout';
import { loadSavedDraft, saveDraft } from '../draftApi';
import './CompanyStep.css';

type CreateProps = {
    mode: 'create';
    me: Me;
    productCode: string;
    requested?: { amount: number; term_months: number };
    purpose?: CollectorPurpose;
};

type EditProps = { mode: 'edit'; app: CollectorApplication; nextHref: string };

export function CompanyStep(props: CreateProps | EditProps) {
    return props.mode === 'create' ? <CreateCompanyStep {...props} /> : <EditCompanyStep {...props} />;
}

type CounterpartyPrefill = components['schemas']['CounterpartyPrefill'];

/** Реквизиты из подсказки реестра — только для чтения. Источник правды — ЕГРЮЛ, его опросит data-collector. */
function CompanyFacts({ company }: { company: CounterpartyPrefill }) {
    const isSoleProprietor = company.applicant_type === 'SOLE_PROPRIETOR';
    const facts: { label: string; value?: string; wide?: boolean }[] = [
        { label: isSoleProprietor ? 'ОГРНИП' : 'ОГРН', value: company.ogrn },
        { label: 'Дата регистрации', value: company.registration_date && formatIsoDate(company.registration_date) },
        { label: 'КПП', value: company.kpp },
        { label: 'Регион', value: company.region },
        { label: isSoleProprietor ? 'Предприниматель' : 'Руководитель', value: company.director_name, wide: true },
    ];
    return (
        <div className='company-step__facts'>
            {facts
                .filter((f) => f.value)
                .map((f) => (
                    <Input
                        key={f.label}
                        label={f.label}
                        value={f.value}
                        readOnly={true}
                        block={true}
                        size={56}
                        className={`company-step__fact${f.wide ? ' company-step__fact_wide' : ''}`}
                    />
                ))}
        </div>
    );
}

type RoleCardProps = { value?: CollectorRole; onChange?: (r: CollectorRole) => void; disabled?: boolean; lockedHint?: string; error?: string };

function RoleCard({ value, onChange, disabled, lockedHint = 'Роль указана при регистрации заявки', error }: RoleCardProps) {
    return (
        <FormCard title='Ваша роль в компании'>
            <RadioGroup
                name='representative_role'
                // заголовок карточки с группой не связан — даём ей имя явно
                role='radiogroup'
                aria-label='Ваша роль в компании'
                value={value ?? null}
                onChange={(_, payload) => onChange?.(payload.value as CollectorRole)}
                disabled={disabled}
                error={error}
                hint={disabled ? lockedHint : undefined}
                radioListClassName='company-step__roles'
            >
                {COLLECTOR_ROLES.map((role) => (
                    <Radio key={role} value={role} label={COLLECTOR_ROLE_LABELS[role]} size={24} />
                ))}
            </RadioGroup>
            <Plate view='common' rounded={true}>
                Подписать договор без доверенности может генеральный директор, у ИП — сам предприниматель. Остальным понадобится
                машиночитаемая доверенность (МЧД) — её номер укажете перед отправкой.
            </Plate>
        </FormCard>
    );
}

function EditCompanyStep({ app, nextHref }: EditProps) {
    const company = app.applicant;
    return (
        <StepLayout title='От чьего имени подаёте заявку' subtitle='Компанию в черновике поменять нельзя' nextHref={nextHref}>
            <FormCard>
                <Input label='ИНН компании или ИП' value={company?.inn ?? ''} readOnly={true} block={true} size={56} hint='Для другой компании начните новую заявку' />
                <Input label='Название' value={company?.name ?? ''} readOnly={true} block={true} size={56} hint='После сбора данных заменим названием из ЕГРЮЛ' />
                <Input label='Заявку оформляет' value={app.representative?.full_name ?? ''} readOnly={true} block={true} size={56} />
            </FormCard>
            <RoleCard value={app.representative?.role} disabled={true} />
        </StepLayout>
    );
}

function lookupPlate(error: unknown, data: CounterpartyPrefill | undefined) {
    if (data?.registry_status && data.registry_status !== 'ACTIVE') {
        return (
            <Plate view='negative' title='Компания ликвидирована'>
                Кредит для неё оформить не получится.
            </Plate>
        );
    }
    if (data) return <Plate view='positive' title='Нашли компанию в реестре'>Остальное о компании соберём сами из открытых источников</Plate>;
    if (error instanceof ApiError && error.status === 404) {
        return (
            <Plate view='attention' title='В ЕГРЮЛ такой компании нет'>
                Проверьте цифры. Если всё верно — продолжайте: банк перепроверит при сборе данных.
            </Plate>
        );
    }
    if (error) {
        const timeout = error instanceof ApiError && error.status === 504;
        return (
            <Plate view='attention' title={timeout ? 'Реестр сейчас не отвечает' : 'Не удалось проверить компанию в реестре'}>
                Можно продолжать — реквизиты подтянем сами при сборе данных.
            </Plate>
        );
    }
    return null;
}

/** Поля из 422 data-collector → поля формы. */
const SERVER_FIELDS: Record<string, 'inn' | 'role'> = { 'company.inn': 'inn', 'representative.role': 'role' };

function CreateCompanyStep({ me, productCode, requested, purpose }: CreateProps) {
    const navigate = useNavigate();
    const register = useRegister();
    const [inn, setInn] = useState('');
    const [name, setName] = useState('');
    const [draftError, setDraftError] = useState<string>();
    const [opening, setOpening] = useState(false);
    // выбор роли — для конкретного ИНН: сменили ИНН — выбирать заново
    const [chosen, setChosen] = useState<{ inn: string; role: CollectorRole }>();

    const debouncedInn = useDebouncedValue(inn, 400);
    const validInn = isInn(debouncedInn) && debouncedInn === inn ? inn : undefined;
    const lookup = useCompanyLookup(validInn);
    const soleProprietor = validInn?.length === 12;
    const suggestedRole = lookup.data?.director_name === me.full_name ? (soleProprietor ? 'OWNER' : 'DIRECTOR') : undefined;
    // своя компания из профиля — роль в ней уже известна
    const linked = me.companies.find((c) => c.inn === validInn);
    // роль в своей компании задана в профиле: data-collector её при регистрации не меняет
    const role = (linked?.role as CollectorRole | undefined) ?? (chosen && chosen.inn === validInn ? chosen.role : undefined) ?? suggestedRole;
    const liquidating = Boolean(lookup.data?.registry_status && lookup.data.registry_status !== 'ACTIVE');
    const shownName = name || lookup.data?.short_name || '';

    const serverErrors: Partial<Record<'inn' | 'role', string>> = {};
    if (register.error instanceof ApiError) {
        for (const e of register.error.problem?.errors ?? []) {
            const field = SERVER_FIELDS[e.field ?? ''];
            if (field) serverErrors[field] = e.message;
        }
    }
    const innError = serverErrors.inn ?? (inn.length === 11 && inn === debouncedInn ? 'ИНН — 10 цифр для юрлица или 12 для ИП' : undefined);
    const canContinue = Boolean(validInn && role && !liquidating);

    /** В черновик — продукт, сумма и цель, которые только что выбрали в подборе (даже если черновик уже был). */
    const openDraft = async (id: string) => {
        setOpening(true);
        setDraftError(undefined);
        try {
            const choice = { product_code: productCode, ...(requested && { requested }) };
            const existing = await loadSavedDraft(id);
            await saveDraft(id, {
                ...existing.data,
                ...choice,
                purpose: purpose ? { ...existing.data.purpose, code: purpose } : existing.data.purpose,
            }, existing.version);
            navigate(`/applications/${id}/edit/terms`, { replace: true });
        } finally {
            setOpening(false);
        }
    };

    const submit = () => {
        if (!validInn || !role) return;
        // с токеном data-collector оформляет заявку от имени вошедшего пользователя
        // и по той же компании возвращает его прежний черновик — правки в нём сохраняем
        register.mutate(
            { representative: { full_name: me.full_name, role }, company: { inn: validInn, name: shownName || undefined } },
            { onSuccess: (app) => { void openDraft(app.id ?? '').catch((error: unknown) => setDraftError(error instanceof Error ? error.message : 'Не удалось сохранить черновик')); } },
        );
    };

    const otherError = register.error && !Object.keys(serverErrors).length;

    return (
        <StepLayout
            title='От чьего имени подаёте заявку'
            subtitle='Введите ИНН — остальное о компании соберём сами'
            onNext={submit}
            nextDisabled={!canContinue || opening}
            nextLoading={register.isPending || opening}
            hint={!canContinue && validInn && !role ? 'Укажите вашу роль в компании' : undefined}
        >
            <FormCard>
                <Input
                    label='ИНН компании или ИП'
                    value={inn}
                    onChange={(_, { value }) => setInn(value.replace(/\D/g, '').slice(0, 12))}
                    inputMode='numeric'
                    autoComplete='off'
                    block={true}
                    size={56}
                    hint='10 цифр для юрлица, 12 — для ИП'
                    error={innError}
                    rightAddons={lookup.isFetching && <Spinner visible={true} preset={24} />}
                />
                {lookup.isFetching && !lookup.data && (
                    <Typography.Text view='primary-small' color='secondary' role='status'>
                        Ищем компанию в реестре…
                    </Typography.Text>
                )}
                {validInn && !lookup.isFetching && lookupPlate(lookup.error, lookup.data)}
                {validInn && (
                    <Input
                        label='Название'
                        value={shownName}
                        onChange={(_, { value }) => setName(value)}
                        block={true}
                        size={56}
                        hint='Необязательно: после сбора данных заменим названием из ЕГРЮЛ'
                    />
                )}
                {lookup.data && !liquidating && <CompanyFacts company={lookup.data} />}
            </FormCard>

            {validInn && !liquidating && (
                <>
                    <RoleCard
                        value={role}
                        onChange={(next) => validInn && setChosen({ inn: validInn, role: next })}
                        disabled={Boolean(linked)}
                        lockedHint='Роль в этой компании — из вашего профиля'
                        error={serverErrors.role}
                    />
                    <FormCard title='Кто оформляет заявку'>
                        <Input label='ФИО' value={me.full_name} readOnly={true} block={true} size={56} hint='Из вашего профиля' />
                    </FormCard>
                </>
            )}

            {otherError && (
                <Plate view='negative' title='Не получилось создать заявку'>
                    {register.error.message}
                </Plate>
            )}
            {draftError && <Plate view='negative' title='Не удалось сохранить черновик'>{draftError}</Plate>}
        </StepLayout>
    );
}
