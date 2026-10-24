import { Typography } from '@alfalab/core-components/typography';

import type { ClientStage, StageItem } from '../../../api/types';
import { VisuallyHidden } from '../../../shared/ui/VisuallyHidden/VisuallyHidden';
import './StageBar.css';

type Props = { stages: StageItem[]; clientStage: ClientStage };

const STATE_LABELS: Record<StageItem['state'], string> = {
    DONE: 'пройден',
    CURRENT: 'сейчас',
    UPCOMING: 'впереди',
    SKIPPED: 'не понадобился',
};

/** Этапы заявки из ApplicationStatusView.stages. Пока ждём ответа клиента, текущий этап — «Нужно уточнить». */
export function StageBar({ stages, clientStage }: Props) {
    const attention = clientStage === 'ACTION_REQUIRED';
    return (
        <ol className='stage-bar' aria-label='Этапы заявки'>
            {stages.map((stage) => {
                const current = stage.state === 'CURRENT';
                const title = current && attention ? 'Нужно уточнить' : stage.title;
                const state = current && attention ? 'attention' : stage.state.toLowerCase();
                return (
                    <li
                        key={stage.code}
                        className={`stage-bar__stage stage-bar__stage_state_${state}`}
                        aria-current={current ? 'step' : undefined}
                    >
                        <span className='stage-bar__line' aria-hidden='true' />
                        <Typography.Text
                            view='primary-small'
                            weight={current ? 'bold' : 'regular'}
                            color={current ? 'primary' : 'secondary'}
                        >
                            {title}
                            <VisuallyHidden>, {STATE_LABELS[stage.state]}</VisuallyHidden>
                        </Typography.Text>
                    </li>
                );
            })}
        </ol>
    );
}
