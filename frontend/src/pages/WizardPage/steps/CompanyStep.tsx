import { useState } from 'react';
import { Input } from '@alfalab/core-components/input';
import { Plate } from '@alfalab/core-components/plate';
import { Radio } from '@alfalab/core-components/radio';
import { RadioGroup } from '@alfalab/core-components/radio-group';
import { Spinner } from '@alfalab/core-components/spinner';
import { Typography } from '@alfalab/core-components/typography';
import { useNavigate } from 'react-router-dom';

import { ApiError } from '../../../api/client';
import { useFindDraft, useRegister } from '../../../api/collector/queries';
import { COLLECTOR_ROLE_LABELS, COLLECTOR_ROLES, type CollectorApplication, type CollectorPurpose, type CollectorRole } from '../../../api/collector/types';
import { useCounterpartyLookup } from '../../../api/queries';
import type { CounterpartyPrefill, Me } from '../../../api/types';
import { formatIsoDate } from '../../../shared/lib/format';
import { isInn } from '../../../shared/lib/inn';
import { useDebouncedValue } from '../../../shared/lib/useDebouncedValue';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { StepLayout } from '../components/StepLayout';
import { emptyDraft, readDraft, writeDraft } from '../draft';
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

/** Реквизиты из подсказки реестра — только для чтения. Источник правды — ЕГРЮЛ, его опросит data-collector. */
function CompanyFacts({ company }: { company: CounterpartyPrefill }) {
    const isSoleProprietor = company.applicant_type === 'SOLE_PROPRIETOR';
    const facts: { label: string; value?: string; wide?: boolean }[] = [
        { label: isSoleProprietor ? 'ОГРНИП' : 'ОГРН', value: company.ogrn },
        { label: 'Дата регистрации', value: company.registration_date && formatIsoDate(company.registration_date) },
        { label: 'Основной ОКВЭД', value: company.okved_main && `${company.okved_main.code} ${company.okved_main.title ?? ''}`.trim(), wide: true },
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

function RoleCard({ value, onChange, disabled, error }: { value?: CollectorRole; onChange?: (r: CollectorRole) => void; disabled?: boolean; error?: string }) {
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
                hint={disabled ? 'Роль указана при регистрации заявки' : undefined}
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
            <Plate view='negative' title='Компания ликвидируется или реорганизуется'>
                Кредит для неё оформить не получится.
            </Plate>
        );
    }
    if (data) return <Plate view='positive' title='Нашли компанию в реестре'>Остальное о компании соберём сами из открытых источников</Plate>;
    if (error instanceof ApiError && error.status === 404) {
        return (
            <Plate view='attention' title='В подсказках реестра такой компании нет'>
                Проверьте цифры. Если всё верно — продолжайте: реквизиты возьмём из ЕГРЮЛ при сборе данных.
            </Plate>
        );
    }
    return null;
}

/** Поля из 422 data-collector → поля формы. */
const SERVER_FIELDS: Record<string, 'inn' | 'personInn' | 'role'> = { 'company.inn': 'inn', 'representative.inn': 'personInn', 'representative.role': 'role' };

function CreateCompanyStep({ me, productCode, requested, purpose }: CreateProps) {
    const navigate = useNavigate();
    const register = useRegister(me.id);
    const findDraft = useFindDraft(me.id);
    const [searching, setSearching] = useState(false);
    const [inn, setInn] = useState('');
    const [name, setName] = useState('');
    const [personInn, setPersonInn] = useState('');
    const [chosenRole, setChosenRole] = useState<CollectorRole>();

    const debouncedInn = useDebouncedValue(inn, 400);
    const validInn = isInn(debouncedInn) && debouncedInn === inn ? inn : undefined;
    const lookup = useCounterpartyLookup(validInn);
    const soleProprietor = validInn?.length === 12;
    const suggestedRole = lookup.data?.director_name === me.full_name ? (soleProprietor ? 'OWNER' : 'DIRECTOR') : undefined;
    const role = chosenRole ?? suggestedRole;
    const liquidating = Boolean(lookup.data?.registry_status && lookup.data.registry_status !== 'ACTIVE');
    const shownName = name || lookup.data?.short_name || '';

    const serverErrors: Partial<Record<'inn' | 'personInn' | 'role', string>> = {};
    if (register.error instanceof ApiError) {
        for (const e of register.error.problem?.errors ?? []) {
            const field = SERVER_FIELDS[e.field ?? ''];
            if (field) serverErrors[field] = e.message;
        }
    }
    const innError = serverErrors.inn ?? (inn.length === 11 && inn === debouncedInn ? 'ИНН — 10 цифр для юрлица или 12 для ИП' : undefined);
    const personInnError = serverErrors.personInn ?? (personInn && personInn.length !== 12 ? 'ИНН физлица — 12 цифр' : undefined);
    const canContinue = Boolean(validInn && role && !liquidating && !personInnError);

    /** В черновик — продукт, сумма и цель, которые только что выбрали в подборе (даже если черновик уже был). */
    const openDraft = (id: string) => {
        const choice = { product_code: productCode, ...(requested && { requested }), ...(purpose && { purpose: { code: purpose } }) };
        const existing = readDraft(me.id, id);
        writeDraft(me.id, id, existing ? { ...existing, ...choice, purpose: purpose ? { ...existing.purpose, code: purpose } : existing.purpose } : emptyDraft(choice));
        navigate(`/applications/${id}/edit/terms`, { replace: true });
    };

    const submit = async () => {
        if (!validInn || !role) return;
        setSearching(true);
        const draft = await findDraft(validInn);
        setSearching(false);
        if (draft?.id) {
            openDraft(draft.id);
            return;
        }
        register.mutate(
            {
                representative: { full_name: me.full_name, inn: personInn || undefined, role },
                company: { inn: validInn, name: shownName || undefined },
            },
            {
                // data-collector мог вернуть уже существующий черновик (тот же ИНН физлица) — правки в нём сохраняем
                onSuccess: (app) => openDraft(app.id ?? ''),
            },
        );
    };

    const otherError = register.error && !Object.keys(serverErrors).length;

    return (
        <StepLayout
            title='От чьего имени подаёте заявку'
            subtitle='Введите ИНН — остальное о компании соберём сами'
            onNext={() => void submit()}
            nextDisabled={!canContinue}
            nextLoading={register.isPending || searching}
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
                    <RoleCard value={role} onChange={setChosenRole} error={serverErrors.role} />
                    <FormCard title='Кто оформляет заявку'>
                        <Input label='ФИО' value={me.full_name} readOnly={true} block={true} size={56} />
                        <Input
                            label='Ваш ИНН физлица'
                            value={personInn}
                            onChange={(_, { value }) => setPersonInn(value.replace(/\D/g, '').slice(0, 12))}
                            inputMode='numeric'
                            block={true}
                            size={56}
                            hint='Необязательно: по нему банк узнает вас в следующих заявках'
                            error={personInnError}
                        />
                    </FormCard>
                </>
            )}

            {otherError && (
                <Plate view='negative' title='Не получилось создать заявку'>
                    {register.error.message}
                </Plate>
            )}
        </StepLayout>
    );
}
