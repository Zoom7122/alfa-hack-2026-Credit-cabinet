import { Typography } from '@alfalab/core-components/typography';

import type { ApplicationStatusView } from '../../../api/types';
import { CLOSED_REASONS } from '../../../shared/lib/dictionaries';
import { StageBar } from '../components/StageBar';
import './StageStatus.css';

/** Этапы без отдельного экрана (подписание, выдача, закрыта): этапы + заголовок и подпись из /status. */
export function StageStatus({ status }: { status: ApplicationStatusView }) {
    const reason = status.client_stage === 'CLOSED' ? CLOSED_REASONS[status.status] : undefined;
    return (
        <section className='stage-status'>
            <StageBar stages={status.stages} clientStage={status.client_stage} />
            <div className='stage-status__text'>
                <Typography.Title tag='h2' view='small' font='system'>
                    {status.title}
                </Typography.Title>
                {(status.subtitle || reason) && (
                    <Typography.Text view='primary-large' color='secondary'>
                        {status.subtitle ?? reason}
                    </Typography.Text>
                )}
            </div>
        </section>
    );
}
