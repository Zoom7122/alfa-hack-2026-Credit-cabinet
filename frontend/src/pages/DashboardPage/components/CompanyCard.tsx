import { Button } from '@alfalab/core-components/button';
import { Divider } from '@alfalab/core-components/divider';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Typography } from '@alfalab/core-components/typography';
import { PlusMIcon } from '@alfalab/icons-glyph/PlusMIcon';
import { Link } from 'react-router-dom';

import type { Applicant, CompanyAccess } from '../../../api/types';
import { ROLE_LABELS } from '../../../shared/lib/dictionaries';
import './CompanyCard.css';

type Props = { company: CompanyAccess; applicant?: Applicant; loading?: boolean };

export function CompanyCard({ company, applicant, loading }: Props) {
    const rows: [string, string | undefined][] = [
        ['ИНН', company.inn],
        [applicant?.applicant_type === 'SOLE_PROPRIETOR' ? 'ОГРНИП' : 'ОГРН', applicant?.ogrn],
        ['Ваша роль', ROLE_LABELS[company.role]],
        // год из YYYY-MM-DD без парсинга даты: new Date() сдвинул бы 1 января в отрицательных часовых поясах
        ['Работает с', applicant?.registration_date && `${applicant.registration_date.slice(0, 4)} года`],
    ];

    return (
        <section className='company-card' aria-labelledby='company-card-title'>
            <Typography.Text tag='div' view='caps' color='tertiary' weight='bold'>
                Компания
            </Typography.Text>
            <Typography.Title id='company-card-title' tag='h2' view='xsmall' font='system'>
                {company.short_name}
            </Typography.Title>
            <dl className='company-card__facts'>
                {rows.map(([label, value]) => (
                    <div key={label} className='company-card__fact'>
                        <dt className='company-card__label'>
                            <Typography.Text view='primary-medium' color='secondary'>
                                {label}
                            </Typography.Text>
                        </dt>
                        <dd className='company-card__value'>
                            {value ? (
                                <Typography.Text view='primary-medium'>{value}</Typography.Text>
                            ) : loading ? (
                                <Skeleton visible={true} className='company-card__skeleton' />
                            ) : (
                                '—'
                            )}
                        </dd>
                    </div>
                ))}
            </dl>
            <Divider />
            <Button
                Component={Link}
                href='/applicants/new'
                view='text'
                size={32}
                leftAddons={<PlusMIcon />}
                className='company-card__add'
            >
                Добавить компанию
            </Button>
        </section>
    );
}
