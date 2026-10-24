import { AmountInput } from '@alfalab/core-components/amount-input';
import { Button } from '@alfalab/core-components/button';
import { Checkbox } from '@alfalab/core-components/checkbox';
import { Input } from '@alfalab/core-components/input';
import { Select } from '@alfalab/core-components/select';
import { PlusMIcon } from '@alfalab/icons-glyph/PlusMIcon';

import type { ExistingDebt } from '../../../api/types';
import { DEBT_KIND_LABELS } from '../../../shared/lib/dictionaries';
import { completeEntries, type Entry, entryKey, toEntries } from '../../../shared/lib/entries';
import { useSessionState } from '../../../shared/lib/sessionState';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { EntryCard } from '../components/EntryCard';
import { StepLayout } from '../components/StepLayout';
import type { StepProps } from '../stepProps';

const KINDS = Object.keys(DEBT_KIND_LABELS) as ExistingDebt['kind'][];

const isComplete = (d: Entry<ExistingDebt>) =>
    Boolean(d.kind && d.lender?.trim() && d.outstanding !== undefined && d.monthly_payment !== undefined);

export function DebtsStep({ app, draft, hrefFor }: StepProps) {
    const [noDebts, setNoDebts] = useSessionState(`credit-cabinet:wizard:${app.id}:no-debts`, () => app.has_no_debts === true);
    // неполные записи в черновик не уходят — держим их во вкладке, чтобы не потерять при переходе по шагам
    const [debts, setDebts] = useSessionState<Entry<ExistingDebt>[]>(`credit-cabinet:wizard:${app.id}:debts`, () => toEntries(app.debts));

    const save = (nextNoDebts: boolean, nextDebts: Entry<ExistingDebt>[]) => {
        setNoDebts(nextNoDebts);
        setDebts(nextDebts);
        draft.update({ has_no_debts: nextNoDebts, debts: nextNoDebts ? [] : completeEntries(nextDebts, isComplete) });
    };
    const edit = (key: string, patch: Partial<ExistingDebt>) =>
        save(
            noDebts,
            debts.map((d) => (d.key === key ? { ...d, ...patch } : d)),
        );

    return (
        <StepLayout
            title='Кредиты и долги'
            subtitle='Кредиты, лизинг, овердрафты и гарантии в любых банках — так рассчитаем посильный платёж'
            backHref={hrefFor('finance')}
            nextHref={hrefFor('collateral')}
        >
            <FormCard>
                <Checkbox
                    label='У компании нет действующих кредитов, лизинга и гарантий'
                    checked={noDebts}
                    onChange={(_, { checked }) => save(checked, checked ? [] : debts)}
                    size={24}
                />
            </FormCard>

            {!noDebts && (
                <FormCard
                    title='Действующие обязательства'
                    subtitle={debts.length ? undefined : 'Добавьте каждый кредит отдельно'}
                >
                    {debts.map((d, i) => (
                        <EntryCard
                            key={d.key}
                            title={d.kind ? `${DEBT_KIND_LABELS[d.kind]}${d.lender ? ` · ${d.lender}` : ''}` : `Обязательство ${i + 1}`}
                            removeLabel={`Удалить обязательство ${i + 1}`}
                            onRemove={() => save(noDebts, debts.filter((x) => x.key !== d.key))}
                            incomplete={!isComplete(d)}
                        >
                            <Select
                                label='Вид'
                                size={56}
                                block={true}
                                options={KINDS.map((k) => ({ key: k, content: DEBT_KIND_LABELS[k] }))}
                                selected={d.kind ?? null}
                                onChange={({ selected }) => selected && edit(d.key, { kind: selected.key as ExistingDebt['kind'] })}
                            />
                            <Input
                                label='Банк или лизинговая компания'
                                value={d.lender ?? ''}
                                onChange={(_, { value }) => edit(d.key, { lender: value })}
                                block={true}
                                size={56}
                            />
                            <AmountInput
                                label='Остаток долга'
                                value={d.outstanding ?? null}
                                minority={1}
                                integersOnly={true}
                                onChange={(_, { value }) => edit(d.key, { outstanding: value ?? undefined })}
                                block={true}
                                size={56}
                            />
                            <AmountInput
                                label='Платёж в месяц'
                                value={d.monthly_payment ?? null}
                                minority={1}
                                integersOnly={true}
                                onChange={(_, { value }) => edit(d.key, { monthly_payment: value ?? undefined })}
                                block={true}
                                size={56}
                            />
                            <Checkbox
                                label='Есть просрочка'
                                checked={d.has_overdue === true}
                                onChange={(_, { checked }) => edit(d.key, { has_overdue: checked })}
                                size={24}
                            />
                        </EntryCard>
                    ))}
                    <div>
                        <Button
                            view='secondary'
                            size={48}
                            leftAddons={<PlusMIcon />}
                            onClick={() => save(false, [...debts, { key: entryKey() }])}
                        >
                            Добавить обязательство
                        </Button>
                    </div>
                </FormCard>
            )}
        </StepLayout>
    );
}
