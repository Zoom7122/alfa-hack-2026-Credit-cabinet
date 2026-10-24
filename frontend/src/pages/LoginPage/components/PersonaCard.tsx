import type { KeyboardEvent } from 'react';
import { Typography } from '@alfalab/core-components/typography';

import type { TestUser } from '../../../api/types';
import { ROLE_LABELS } from '../../../shared/lib/dictionaries';
import { initials } from '../../../shared/lib/format';
import './PersonaCard.css';

type Props = {
    user: TestUser;
    checked: boolean;
    onSelect: () => void;
    onKeyDown?: (e: KeyboardEvent<HTMLButtonElement>) => void;
};

function subtitle(user: TestUser) {
    const company = user.companies?.[0];
    if (!company) return 'Андеррайтер · рабочее место банка';
    return [ROLE_LABELS[company.role], company.short_name, company.can_sign && 'подписывает договор']
        .filter(Boolean)
        .join(' · ');
}

/** Тестовый пользователь — один вариант в radiogroup выбора на экране входа. */
export function PersonaCard({ user, checked, onSelect, onKeyDown }: Props) {
    return (
        <button
            type='button'
            role='radio'
            aria-checked={checked}
            tabIndex={checked ? 0 : -1}
            className={`persona-card${checked ? ' persona-card_checked' : ''}`}
            onClick={onSelect}
            onKeyDown={onKeyDown}
        >
            <span className='persona-card__avatar' aria-hidden='true'>
                <Typography.Text view='primary-small' weight='bold'>
                    {initials(user.full_name)}
                </Typography.Text>
            </span>
            <span className='persona-card__body'>
                <Typography.Text view='primary-large' weight='bold'>
                    {user.full_name}
                </Typography.Text>
                <Typography.Text view='primary-small' color='secondary'>
                    {subtitle(user)}
                </Typography.Text>
                <Typography.Text view='primary-small' className='persona-card__persona'>
                    {user.persona}
                </Typography.Text>
            </span>
        </button>
    );
}
