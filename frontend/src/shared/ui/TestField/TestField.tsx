import { type ReactNode, useState } from 'react';
import { Checkbox } from '@alfalab/core-components/checkbox';

import './TestField.css';

export type TestVariant = 'good' | 'neutral' | 'bad';
export type TestValues<T> = Record<TestVariant, T>;

type Props<T> = {
    label: string;
    value: T | undefined;
    values: TestValues<T>;
    onChange: (value: T, variant: TestVariant) => void;
    children: ReactNode;
    disabled?: boolean;
};

/** Подстановка меняет обычное значение поля: валидация и сохранение остаются общими. */
export function TestField<T extends string | number | boolean>({ label, value, values, onChange, children, disabled }: Props<T>) {
    const [variant, setVariant] = useState<TestVariant>('good');
    const [applied, setApplied] = useState(false);
    const checked = applied && value === values[variant];
    return (
        <div className='test-field' data-testid={`test-field:${label}`}>
            {children}
            {!disabled && <div className='test-field__controls' role='group' aria-label='Тестовые значения' aria-description={`Для поля «${label}»`}>
                <Checkbox label='Подставить тестовое значение' size={24} disabled={disabled} checked={checked}
                    onChange={(_, { checked: next }) => {
                        setApplied(next);
                        if (next) onChange(values[variant], variant);
                    }} />
                <select aria-label='Вариант тестового значения' value={variant} disabled={disabled}
                    onChange={(event) => {
                        const next = event.target.value as TestVariant;
                        setVariant(next);
                        if (checked) onChange(values[next], next);
                    }}>
                    <option value='good'>Хорошее</option>
                    <option value='neutral'>Нейтральное</option>
                    <option value='bad'>Плохое</option>
                </select>
            </div>}
        </div>
    );
}
