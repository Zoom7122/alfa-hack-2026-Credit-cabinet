import type { ApplicationStatus, ClientStage, RepresentativeRole } from '../../api/types';

export const ROLE_LABELS: Record<RepresentativeRole, string> = {
    DIRECTOR: 'Директор',
    OWNER: 'Собственник',
    CFO: 'Финансовый директор',
    ACCOUNTANT: 'Бухгалтер',
    AUTHORIZED_EMPLOYEE: 'Сотрудник',
};

type StatusColor = 'green' | 'orange' | 'red' | 'blue' | 'grey';

/** Клиентские этапы из docs/fsm.md: клиент не видит внутренние статусы конвейера. */
export const STAGE_BADGES: Record<ClientStage, { label: string; color: StatusColor; contrast?: boolean }> = {
    FILLING: { label: 'Черновик', color: 'grey' },
    REVIEW: { label: 'Рассматриваем', color: 'blue' },
    ACTION_REQUIRED: { label: 'Нужно уточнить', color: 'orange' },
    OFFER: { label: 'Решение готово', color: 'green' },
    SIGNING: { label: 'Подписание договора', color: 'grey', contrast: true },
    DONE: { label: 'Кредит выдан', color: 'green' },
    CLOSED: { label: 'Заявка закрыта', color: 'grey' },
};

/** Уточнение для закрытых заявок — почему закрыта. */
export const CLOSED_REASONS: Partial<Record<ApplicationStatus, string>> = {
    REJECTED: 'Банк отказал',
    DECLINED: 'Вы отказались от предложения',
    CANCELLED: 'Заявка отозвана',
    EXPIRED: 'Истёк срок',
};

export const ACTIVE_STAGES: ClientStage[] = ['FILLING', 'REVIEW', 'ACTION_REQUIRED', 'OFFER', 'SIGNING'];

/** Этапы, по которым на главной нужен лёгкий /status (вопросы банка, кто подписывает, решение). */
export const STAGES_WITH_TASKS: ClientStage[] = ['ACTION_REQUIRED', 'OFFER', 'SIGNING'];
