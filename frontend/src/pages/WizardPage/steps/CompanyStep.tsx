import { useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Input } from '@alfalab/core-components/input';
import { Plate } from '@alfalab/core-components/plate';
import { Radio } from '@alfalab/core-components/radio';
import { RadioGroup } from '@alfalab/core-components/radio-group';
import { Spinner } from '@alfalab/core-components/spinner';
import { Typography } from '@alfalab/core-components/typography';
import { Link, useNavigate } from 'react-router-dom';

import { ApiError } from '../../../api/client';
import { useApplications, useCounterpartyLookup, useCreateApplicant, useCreateApplication } from '../../../api/queries';
import type { Applicant, CounterpartyPrefill, Me, PurposeCode, RepresentativeRole, RequestedTerms } from '../../../api/types';
import { ACTIVE_STAGES, ROLE_LABELS } from '../../../shared/lib/dictionaries';
import { formatIsoDate } from '../../../shared/lib/format';
import { useIdempotencyKeys } from '../../../shared/lib/idempotency';
import { isInn } from '../../../shared/lib/inn';
import { applicationPath } from '../../../shared/lib/routes';
import { useDebouncedValue } from '../../../shared/lib/useDebouncedValue';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { StepLayout } from '../components/StepLayout';
import './CompanyStep.css';

const ROLES: RepresentativeRole[] = ['DIRECTOR', 'OWNER', 'CFO', 'ACCOUNTANT', 'AUTHORIZED_EMPLOYEE'];
const ROLE_OPTION_LABELS: Record<RepresentativeRole, string> = { ...ROLE_LABELS, AUTHORIZED_EMPLOYEE: 'Другой сотрудник' };


type CreateProps = {
    mode: 'create';
    me: Me;
    productCode: string;
    requested?: RequestedTerms;
    purpose?: PurposeCode;
};

type EditProps = {
    mode: 'edit';
    me: Me;
    applicant?: Applicant;
    nextHref: string;
};

export function CompanyStep(props: CreateProps | EditProps) {
    return props.mode === 'create' ? <CreateCompanyStep {...props} /> : <EditCompanyStep {...props} />;
}

/** Реквизиты из реестра только для чтения. */
function CompanyFacts({ company }: { company: CounterpartyPrefill }) {
    const isSoleProprietor = company.applicant_type === 'SOLE_PROPRIETOR';
    const facts: { label: string; value?: string; wide?: boolean }[] = [
        { label: 'Название', value: company.short_name ?? company.full_name },
        { label: isSoleProprietor ? 'ОГРНИП' : 'ОГРН', value: company.ogrn },
        { label: 'Дата регистрации', value: company.registration_date && formatIsoDate(company.registration_date) },
        { label: 'Основной ОКВЭД', value: company.okved_main && `${company.okved_main.code} ${company.okved_main.title ?? ''}`.trim() },
        { label: 'Юридический адрес', value: company.legal_address?.full, wide: true },
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

function RoleCard({ value, onChange, disabled }: { value?: RepresentativeRole; onChange?: (r: RepresentativeRole) => void; disabled?: boolean }) {
    return (
        <FormCard title='Ваша роль в компании'>
            <RadioGroup
                name='representative_role'
                direction='horizontal'
                value={value ?? null}
                onChange={(_, payload) => onChange?.(payload.value as RepresentativeRole)}
                disabled={disabled}
                hint={disabled ? 'Роль указана при подключении компании' : undefined}
                radioListClassName='company-step__roles'
            >
                {ROLES.map((role) => (
                    <Radio key={role} value={role} label={ROLE_OPTION_LABELS[role]} size={24} />
                ))}
            </RadioGroup>
            <Plate view='common' rounded={true}>
                Подписать кредитный договор может только директор. Если вы не директор, банк попросит доверенность, а подписать
                договор нужно будет директору в его кабинете.
            </Plate>
        </FormCard>
    );
}

function EditCompanyStep({ applicant, nextHref }: EditProps) {
    return (
        <StepLayout title='От чьего имени подаёте заявку' subtitle='Компанию в черновике поменять нельзя' nextHref={nextHref}>
            <FormCard>
                <Input
                    label='ИНН компании или ИП'
                    value={applicant?.inn ?? ''}
                    readOnly={true}
                    block={true}
                    size={56}
                    hint='Для другой компании начните новую заявку'
                />
                {applicant && <CompanyFacts company={applicant} />}
            </FormCard>
            <RoleCard value={applicant?.my_role} disabled={true} />
        </StepLayout>
    );
}

function lookupPlate(error: unknown, data: CounterpartyPrefill | undefined) {
    if (data) {
        if (data.registry_status && data.registry_status !== 'ACTIVE') {
            return (
                <Plate view='negative' title='Компания ликвидируется или реорганизуется'>
                    Кредит для неё оформить не получится.
                </Plate>
            );
        }
        return (
            <Plate view='positive' title='Нашли компанию в реестре'>
                {data.already_known
                    ? 'Данные с прошлой заявки подставим автоматически'
                    : 'Остальное о компании соберём сами из открытых источников'}
            </Plate>
        );
    }
    if (error instanceof ApiError && error.status === 404) {
        return (
            <Plate view='negative' title='Не нашли компанию с таким ИНН'>
                Проверьте цифры — возможно, опечатка.
            </Plate>
        );
    }
    if (error instanceof ApiError && error.status === 504) {
        return (
            <Plate view='attention' title='Реестр сейчас не отвечает'>
                Можно продолжать — реквизиты подтянем сами, пока банк рассматривает заявку.
            </Plate>
        );
    }
    if (error && !(error instanceof ApiError && error.status === 422)) {
        return <Plate view='negative' title='Не получилось проверить ИНН. Попробуйте ещё раз.' />;
    }
    return null;
}

function CreateCompanyStep({ me, productCode, requested, purpose }: CreateProps) {
    const navigate = useNavigate();
    const [inn, setInn] = useState(me.companies[0]?.inn ?? '');
    const [chosenRole, setChosenRole] = useState<RepresentativeRole>();
    const { keyFor, forget } = useIdempotencyKeys();

    const debouncedInn = useDebouncedValue(inn, 400);
    const validInn = isInn(debouncedInn) && debouncedInn === inn ? inn : undefined;
    const lookup = useCounterpartyLookup(validInn);
    const createApplicant = useCreateApplicant();
    const createApplication = useCreateApplication();

    const linked = me.companies.find((c) => c.inn === validInn);
    const companyApplications = useApplications({ applicantId: linked?.applicant_id });
    const existing = companyApplications.data?.find((a) => a.product_code === productCode && ACTIVE_STAGES.includes(a.client_stage));
    const suggestedRole = lookup.data?.director_name === me.full_name ? 'DIRECTOR' : undefined;
    const role = linked?.role ?? chosenRole ?? suggestedRole;

    const registryTimeout = lookup.error instanceof ApiError && lookup.error.status === 504;
    const found = lookup.data && (!lookup.data.registry_status || lookup.data.registry_status === 'ACTIVE');
    const canContinue = Boolean(validInn && role && (found || registryTimeout));
    const submitting = createApplicant.isPending || createApplication.isPending;
    const submitError = createApplicant.error ?? createApplication.error;

    const innError =
        lookup.error instanceof ApiError && lookup.error.status === 422
            ? (lookup.error.problem?.errors?.[0]?.message ?? 'Проверьте ИНН')
            : inn.length === 11 && inn === debouncedInn
              ? 'ИНН — 10 цифр для юрлица или 12 для ИП'
              : undefined;

    const submit = async () => {
        if (!validInn || !role) return;
        const applicantBody = { inn: validInn, representative_role: role };
        let applicantId = linked?.applicant_id;
        try {
            applicantId ??= (await createApplicant.mutateAsync({ ...applicantBody, idempotencyKey: keyFor('applicant', applicantBody) })).id;
        } catch (e) {
            if (e instanceof ApiError && e.status < 500) forget('applicant', applicantBody);
            return; // ошибку покажем плашкой ниже
        }
        const applicationBody = { applicant_id: applicantId, product_code: productCode, requested, purpose: purpose && { code: purpose } };
        try {
            const app = await createApplication.mutateAsync({ ...applicationBody, idempotencyKey: keyFor('application', applicationBody) });
            navigate(`/applications/${app.id}/edit/terms`, { replace: true });
        } catch (e) {
            if (e instanceof ApiError && e.status < 500) forget('application', applicationBody);
        }
    };

    return (
        <StepLayout
            title='От чьего имени подаёте заявку'
            subtitle='Введите ИНН — остальное заполним сами'
            onNext={submit}
            nextDisabled={!canContinue}
            nextLoading={submitting}
            hint={!canContinue && validInn && found && !role ? 'Укажите вашу роль в компании' : undefined}
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
                {lookup.data && found && <CompanyFacts company={lookup.data} />}
            </FormCard>

            {(found || registryTimeout) && <RoleCard value={role} onChange={setChosenRole} disabled={Boolean(linked)} />}

            {submitError instanceof ApiError && submitError.code === 'ACTIVE_APPLICATION_EXISTS' ? (
                <Plate view='attention' title='У компании уже есть заявка на этот продукт'>
                    {existing ? (
                        <Button Component={Link} href={applicationPath(existing)} view='secondary' size={40}>
                            Перейти к заявке {existing.number}
                        </Button>
                    ) : (
                        'Продолжите её с главной или выберите другой продукт.'
                    )}
                </Plate>
            ) : (
                submitError && <Plate view='negative' title='Не получилось создать заявку'>{submitError.message}</Plate>
            )}
        </StepLayout>
    );
}
