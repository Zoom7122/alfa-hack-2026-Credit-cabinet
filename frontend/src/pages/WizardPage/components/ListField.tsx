import { useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { IconButton } from '@alfalab/core-components/icon-button';
import { Input } from '@alfalab/core-components/input';
import { CrossMIcon } from '@alfalab/icons-glyph/CrossMIcon';
import { PlusMIcon } from '@alfalab/icons-glyph/PlusMIcon';

import './ListField.css';

type Props = {
    label: string;
    values: string[];
    onChange: (values: string[]) => void;
    /** Ошибка строки по индексу. */
    errorOf: (index: number) => string | undefined;
    addLabel: string;
    max: number;
    placeholder?: string;
    hint?: string;
    /** Нормализация ввода: только цифры, верхний регистр и т. п. */
    normalize?: (value: string) => string;
    inputMode?: 'numeric' | 'text';
};

/** Повторяемое поле: БИК, кадастровые номера, VIN, заводские номера. */
export function ListField({ label, values, onChange, errorOf, addLabel, max, placeholder, hint, normalize = (v) => v, inputMode = 'text' }: Props) {
    // свой ключ у строки: после удаления средней фокус и ошибки не переезжают на соседнюю
    const [keys, setKeys] = useState(() => values.map(() => crypto.randomUUID()));
    if (keys.length !== values.length) setKeys(values.map((_, i) => keys[i] ?? crypto.randomUUID()));

    const remove = (index: number) => {
        setKeys(keys.filter((_, j) => j !== index));
        onChange(values.filter((_, j) => j !== index));
    };
    const add = () => {
        setKeys([...keys, crypto.randomUUID()]);
        onChange([...values, '']);
    };

    return (
        <div className='list-field'>
            {values.map((value, i) => (
                <div key={keys[i] ?? i} className='list-field__row'>
                    <div className='list-field__input'>
                        <Input
                            label={`${label} ${i + 1}`}
                            value={value}
                            placeholder={placeholder}
                            inputMode={inputMode}
                            onChange={(_, { value: next }) => onChange(values.map((v, j) => (j === i ? normalize(next) : v)))}
                            error={errorOf(i)}
                            hint={i === 0 ? hint : undefined}
                            block={true}
                            size={56}
                        />
                    </div>
                    <IconButton
                        icon={CrossMIcon}
                        size={40}
                        view='secondary'
                        onClick={() => remove(i)}
                        aria-label={`Убрать: ${label} ${i + 1}`}
                    />
                </div>
            ))}
            {values.length < max && (
                <div>
                    <Button view='text' size={32} leftAddons={<PlusMIcon />} onClick={add}>
                        {addLabel}
                    </Button>
                </div>
            )}
        </div>
    );
}
