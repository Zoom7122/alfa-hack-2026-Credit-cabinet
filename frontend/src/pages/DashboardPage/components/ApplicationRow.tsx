import { Status } from '@alfalab/core-components/status';
import { Typography } from '@alfalab/core-components/typography';
import { BriefcaseMIcon } from '@alfalab/icons-glyph/BriefcaseMIcon';
import { BusMIcon } from '@alfalab/icons-glyph/BusMIcon';
import { ToolsMIcon } from '@alfalab/icons-glyph/ToolsMIcon';
import { Link } from 'react-router-dom';

import type { ApplicationStatusView, ApplicationSummary } from '../../../api/types';
import { CLOSED_REASONS, STAGE_BADGES } from '../../../shared/lib/dictionaries';
import { formatMoney, formatMonths, formatUpdated, pluralize } from '../../../shared/lib/format';
import { applicationPath } from '../../../shared/lib/routes';
import './ApplicationRow.css';

const PRODUCT_ICONS: Record<string, typeof BriefcaseMIcon> = {
    WORKING_CAPITAL: BriefcaseMIcon,
    VEHICLE: BusMIcon,
    EQUIPMENT: ToolsMIcon,
};

type Props = {
    application: ApplicationSummary;
    productName: string;
    status?: ApplicationStatusView;
};

/** Подсказка под статусом. Где есть /status — берём текст из него (subtitle по контракту). */
function hintFor(app: ApplicationSummary, status?: ApplicationStatusView): { text: string; link?: boolean } | undefined {
    switch (app.client_stage) {
        case 'FILLING':
            return { text: 'Продолжить заполнение', link: true };
        case 'SIGNING':
            if (status?.signing?.awaiting_me) return { text: 'Ждёт вашей подписи' };
            return status?.subtitle ? { text: status.subtitle } : undefined;
        case 'ACTION_REQUIRED': {
            const n = status?.open_info_requests ?? 0;
            return n ? { text: `${n} ${pluralize(n, 'вопрос', 'вопроса', 'вопросов')} от банка` } : undefined;
        }
        case 'CLOSED': {
            const reason = CLOSED_REASONS[app.status];
            return reason ? { text: reason } : undefined;
        }
        default:
            return status?.subtitle ? { text: status.subtitle } : undefined;
    }
}

export function ApplicationRow({ application: app, productName, status }: Props) {
    const Icon = PRODUCT_ICONS[app.product_code] ?? BriefcaseMIcon;
    const badge = STAGE_BADGES[app.client_stage];
    const hint = hintFor(app, status);
    const meta = [
        app.number,
        app.requested && formatMonths(app.requested.term_months),
        app.client_stage === 'FILLING' ? `черновик от ${formatUpdated(app.updated_at)}` : `обновлено ${formatUpdated(app.updated_at)}`,
    ]
        .filter(Boolean)
        .join(' · ');

    return (
        <Link to={applicationPath(app)} className='application-row'>
            <span className='application-row__icon' aria-hidden='true'>
                <Icon />
            </span>
            <span className='application-row__main'>
                <Typography.Text view='primary-large' weight='bold'>
                    {productName}
                    {app.requested && ` · ${formatMoney(app.requested.amount)}`}
                </Typography.Text>
                <Typography.Text view='primary-small' color='secondary'>
                    {meta}
                </Typography.Text>
            </span>
            <span className='application-row__state'>
                <Status view={badge.contrast ? 'contrast' : 'muted-alt'} color={badge.color} size={24} uppercase={false}>
                    {badge.label}
                </Status>
                {hint && (
                    <Typography.Text
                        view='primary-small'
                        color={hint.link ? 'link' : 'secondary'}
                        className='application-row__hint'
                    >
                        {hint.text}
                    </Typography.Text>
                )}
            </span>
        </Link>
    );
}
