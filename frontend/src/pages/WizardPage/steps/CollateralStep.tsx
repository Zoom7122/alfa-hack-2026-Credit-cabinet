import { AmountInput } from '@alfalab/core-components/amount-input';
import { Button } from '@alfalab/core-components/button';
import { Input } from '@alfalab/core-components/input';
import { Plate } from '@alfalab/core-components/plate';
import { Select } from '@alfalab/core-components/select';
import { PlusMIcon } from '@alfalab/icons-glyph/PlusMIcon';

import type { CollateralItem, CollateralType, Guarantor, Product } from '../../../api/types';
import {
    COLLATERAL_LABELS,
    COLLATERAL_OWNER_LABELS,
    GUARANTOR_RELATION_LABELS,
    GUARANTOR_TYPE_LABELS,
} from '../../../shared/lib/dictionaries';
import { completeEntries, type Entry, entryKey, toEntries } from '../../../shared/lib/entries';
import { isValidInn } from '../../../shared/lib/inn';
import { useSessionState } from '../../../shared/lib/sessionState';
import { formatMoney } from '../../../shared/lib/format';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { EntryCard } from '../components/EntryCard';
import { StepLayout } from '../components/StepLayout';
import type { StepProps } from '../stepProps';

const ALL_COLLATERAL = Object.keys(COLLATERAL_LABELS) as CollateralType[];
const OWNERS = Object.keys(COLLATERAL_OWNER_LABELS) as NonNullable<CollateralItem['owner']>[];
const GUARANTOR_TYPES = Object.keys(GUARANTOR_TYPE_LABELS) as Guarantor['type'][];
const RELATIONS = Object.keys(GUARANTOR_RELATION_LABELS) as NonNullable<Guarantor['relation']>[];

const collateralComplete = (c: Entry<CollateralItem>) => Boolean(c.type && c.estimated_value !== undefined);
const guarantorComplete = (g: Entry<Guarantor>) => Boolean(g.type && g.name?.trim() && isValidInn(g.inn));

function collateralRule(product: Product | undefined, amount: number | undefined) {
    const threshold = product?.collateral?.required_above_amount;
    if (threshold === null || threshold === undefined) return { required: false, text: 'Для этого продукта залог не нужен' };
    if (threshold === 0) return { required: true, text: 'Для этого продукта нужен залог' };
    if ((amount ?? 0) > threshold) return { required: true, text: `Для суммы больше ${formatMoney(threshold)} нужен залог` };
    return { required: false, text: 'Для этой суммы залог не требуется — но с ним условия могут быть лучше' };
}

export function CollateralStep({ app, product, draft, hrefFor }: StepProps) {
    // неполные записи в черновик не уходят — держим их во вкладке, чтобы не потерять при переходе по шагам
    const [collateral, setCollateral] = useSessionState<Entry<CollateralItem>[]>(`credit-cabinet:wizard:${app.id}:collateral`, () =>
        toEntries(app.collateral),
    );
    const [guarantors, setGuarantors] = useSessionState<Entry<Guarantor>[]>(`credit-cabinet:wizard:${app.id}:guarantors`, () =>
        toEntries(app.guarantors),
    );
    const rule = collateralRule(product, app.requested?.amount);
    const accepted = product?.collateral?.accepted_types?.length ? product.collateral.accepted_types : ALL_COLLATERAL;
    const noCollateral = product?.collateral?.required_above_amount === null;

    const saveCollateral = (next: Entry<CollateralItem>[]) => {
        setCollateral(next);
        draft.update({ collateral: completeEntries(next, collateralComplete) });
    };
    const saveGuarantors = (next: Entry<Guarantor>[]) => {
        setGuarantors(next);
        draft.update({ guarantors: completeEntries(next, guarantorComplete) });
    };
    const editCollateral = (key: string, patch: Partial<CollateralItem>) =>
        saveCollateral(collateral.map((c) => (c.key === key ? { ...c, ...patch } : c)));
    const editGuarantor = (key: string, patch: Partial<Guarantor>) =>
        saveGuarantors(guarantors.map((g) => (g.key === key ? { ...g, ...patch } : g)));

    return (
        <StepLayout
            title='Залог и поручители'
            subtitle='Залог и поручительство снижают риск — с ними банк может одобрить больше'
            backHref={hrefFor('debts')}
            nextHref={hrefFor('documents')}
        >
            <FormCard title='Залог'>
                <Plate view={rule.required && !collateral.some(collateralComplete) ? 'attention' : 'common'}>{rule.text}</Plate>
                {collateral.map((c, i) => (
                    <EntryCard
                        key={c.key}
                        title={c.type ? COLLATERAL_LABELS[c.type] : `Залог ${i + 1}`}
                        removeLabel={`Удалить залог ${i + 1}`}
                        onRemove={() => saveCollateral(collateral.filter((x) => x.key !== c.key))}
                        incomplete={!collateralComplete(c)}
                    >
                        <Select
                            label='Что в залоге'
                            size={56}
                            block={true}
                            options={accepted.map((t) => ({ key: t, content: COLLATERAL_LABELS[t] }))}
                            selected={c.type ?? null}
                            onChange={({ selected }) => {
                                if (!selected) return;
                                const type = selected.key as CollateralType;
                                // кадастровый номер и VIN относятся только к своему типу — при смене типа убираем
                                editCollateral(c.key, {
                                    type,
                                    cadastral_number: type === 'REAL_ESTATE' ? c.cadastral_number : undefined,
                                    vin: type === 'VEHICLE' ? c.vin : undefined,
                                });
                            }}
                        />
                        <AmountInput
                            label='Оценочная стоимость'
                            value={c.estimated_value ?? null}
                            minority={1}
                            integersOnly={true}
                            onChange={(_, { value }) => editCollateral(c.key, { estimated_value: value ?? undefined })}
                            block={true}
                            size={56}
                        />
                        <Input
                            label='Описание'
                            placeholder='Например: склад 420 м², Подольск'
                            value={c.description ?? ''}
                            onChange={(_, { value }) => editCollateral(c.key, { description: value })}
                            block={true}
                            size={56}
                        />
                        <Select
                            label='Кому принадлежит'
                            size={56}
                            block={true}
                            options={OWNERS.map((o) => ({ key: o, content: COLLATERAL_OWNER_LABELS[o] }))}
                            selected={c.owner ?? null}
                            onChange={({ selected }) =>
                                selected && editCollateral(c.key, { owner: selected.key as NonNullable<CollateralItem['owner']> })
                            }
                        />
                        {c.type === 'REAL_ESTATE' && (
                            <Input
                                label='Кадастровый номер'
                                hint='Проверим по ЕГРН'
                                value={c.cadastral_number ?? ''}
                                onChange={(_, { value }) => editCollateral(c.key, { cadastral_number: value })}
                                block={true}
                                size={56}
                            />
                        )}
                        {c.type === 'VEHICLE' && (
                            <Input
                                label='VIN'
                                value={c.vin ?? ''}
                                onChange={(_, { value }) => editCollateral(c.key, { vin: value.toUpperCase() })}
                                block={true}
                                size={56}
                            />
                        )}
                    </EntryCard>
                ))}
                {!noCollateral && (
                    <div>
                        <Button
                            view='secondary'
                            size={48}
                            leftAddons={<PlusMIcon />}
                            onClick={() => saveCollateral([...collateral, { key: entryKey() }])}
                        >
                            Добавить залог
                        </Button>
                    </div>
                )}
            </FormCard>

            <FormCard title='Поручители' subtitle={product?.guarantee_required ? 'Для этого продукта нужен хотя бы один поручитель' : undefined}>
                {guarantors.map((g, i) => (
                    <EntryCard
                        key={g.key}
                        title={g.name?.trim() || `Поручитель ${i + 1}`}
                        removeLabel={`Удалить поручителя ${i + 1}`}
                        onRemove={() => saveGuarantors(guarantors.filter((x) => x.key !== g.key))}
                        incomplete={!guarantorComplete(g)}
                    >
                        <Select
                            label='Кто'
                            size={56}
                            block={true}
                            options={GUARANTOR_TYPES.map((t) => ({ key: t, content: GUARANTOR_TYPE_LABELS[t] }))}
                            selected={g.type ?? null}
                            onChange={({ selected }) => selected && editGuarantor(g.key, { type: selected.key as Guarantor['type'] })}
                        />
                        <Select
                            label='Связь с компанией'
                            size={56}
                            block={true}
                            options={RELATIONS.map((r) => ({ key: r, content: GUARANTOR_RELATION_LABELS[r] }))}
                            selected={g.relation ?? null}
                            onChange={({ selected }) =>
                                selected && editGuarantor(g.key, { relation: selected.key as NonNullable<Guarantor['relation']> })
                            }
                        />
                        <Input
                            label={g.type === 'LEGAL_ENTITY' ? 'Название компании' : 'ФИО'}
                            value={g.name ?? ''}
                            onChange={(_, { value }) => editGuarantor(g.key, { name: value })}
                            block={true}
                            size={56}
                        />
                        <Input
                            label='ИНН'
                            value={g.inn ?? ''}
                            inputMode='numeric'
                            onChange={(_, { value }) => editGuarantor(g.key, { inn: value.replace(/\D/g, '').slice(0, 12) })}
                            error={g.inn && g.inn.length >= 10 && !isValidInn(g.inn) ? 'Проверьте ИНН — не сходится контрольная цифра или длина' : undefined}
                            block={true}
                            size={56}
                        />
                    </EntryCard>
                ))}
                <div>
                    <Button
                        view='secondary'
                        size={48}
                        leftAddons={<PlusMIcon />}
                        onClick={() => saveGuarantors([...guarantors, { key: entryKey(), type: 'INDIVIDUAL' }])}
                    >
                        Добавить поручителя
                    </Button>
                </div>
            </FormCard>
        </StepLayout>
    );
}
