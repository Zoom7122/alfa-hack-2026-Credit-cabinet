import { PasswordInput } from '@alfalab/core-components/password-input';
import { Plate } from '@alfalab/core-components/plate';

import type { Marketplace } from '../../../api/collector/types';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { ListField } from '../components/ListField';
import { StepLayout } from '../components/StepLayout';
import { MAX_BANKS } from '../draft';
import { type StepProps, useStepGate } from '../stepProps';

const MARKETPLACES: { id: Marketplace; label: string }[] = [
    { id: 'wildberries', label: 'Токен продавца Wildberries' },
    { id: 'ozon', label: 'Токен продавца Ozon' },
];

/** Шаг «Счета и площадки»: откуда ещё взять обороты — другие банки, маркетплейсы, онлайн-кассы. Всё необязательно. */
export function SourcesStep({ draft, hrefFor, errors, serverErrors }: StepProps) {
    const d = draft.value;
    const { marketplace_tokens: tokens, ofd_token: ofd } = draft.secrets;
    const gate = useStepGate(errors, serverErrors, hrefFor('collateral'));

    return (
        <StepLayout
            title='Счета и площадки'
            subtitle='Необязательно, но чем больше источников, тем точнее решение и выше сумма'
            backHref={hrefFor('scoring')}
            onNext={gate.onNext}
            hint={gate.hint}
        >
            <FormCard title='Счета в других банках'>
                <ListField
                    label='БИК банка'
                    values={d.other_bank_biks}
                    onChange={(other_bank_biks) => draft.update({ other_bank_biks })}
                    errorOf={(i) => gate.errorOf(`other_bank_biks[${i}]`)}
                    addLabel='Добавить банк'
                    max={MAX_BANKS}
                    hint='9 цифр. Обороты и блокировки по этим счетам запросим через мультибанкинг'
                    normalize={(v) => v.replace(/\D/g, '').slice(0, 9)}
                    inputMode='numeric'
                />
            </FormCard>

            <FormCard title='Маркетплейсы и онлайн-кассы'>
                {MARKETPLACES.map((m) => (
                    <PasswordInput
                        key={m.id}
                        label={m.label}
                        value={tokens[m.id] ?? ''}
                        onChange={(_, { value }) => draft.updateSecrets({ marketplace_tokens: { ...tokens, [m.id]: value } })}
                        error={gate.errorOf(`marketplace_tokens.${m.id}`)}
                        autoComplete='off'
                        block={true}
                        size={56}
                    />
                ))}
                <PasswordInput
                    label='Токен оператора фискальных данных (ОФД)'
                    value={ofd}
                    onChange={(_, { value }) => draft.updateSecrets({ ofd_token: value })}
                    error={gate.errorOf('ofd_token')}
                    autoComplete='off'
                    block={true}
                    size={56}
                />
                <Plate view='common' rounded={true}>
                    Токены храним только в этой вкладке до отправки заявки. Банк использует их один раз для сбора продаж и чеков.
                </Plate>
            </FormCard>
        </StepLayout>
    );
}
