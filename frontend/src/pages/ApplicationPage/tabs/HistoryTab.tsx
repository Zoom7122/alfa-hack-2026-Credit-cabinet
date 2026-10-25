import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';

import { useTimeline } from '../../../api/queries';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { EventList } from '../../../shared/ui/EventList/EventList';
import './HistoryTab.css';

/** Вкладка «История»: клиентская версия журнала заявки, новые сверху. */
export function HistoryTab({ applicationId }: { applicationId: string }) {
    const timeline = useTimeline(applicationId);
    return (
        <FormCard title='История заявки' className='history-tab'>
            {timeline.isLoading && <Skeleton visible={true} className='history-tab__skeleton' />}
            {timeline.isError && <Plate view='negative' title='Не удалось загрузить историю' />}
            {timeline.data && <EventList events={timeline.data} />}
        </FormCard>
    );
}
