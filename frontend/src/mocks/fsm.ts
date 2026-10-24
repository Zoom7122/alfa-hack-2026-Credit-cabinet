import type { ApplicationAction, ApplicationStatus, ClientStage } from '../api/types';

/* Правила FSM для мока — docs/fsm.md. */

const IN_REVIEW: ApplicationAction[] = ['CANCEL', 'UPLOAD_DOCUMENT'];

/**
 * Доступные клиенту действия по статусу. Отзыв на рассмотрении разрешён, как в примере ApplicationStatusView
 * в openapi.yaml и в макете 07 (в fsm.md это опция).
 */
export const ACTIONS: Partial<Record<ApplicationStatus, ApplicationAction[]>> = {
    DRAFT: ['EDIT', 'SUBMIT', 'CANCEL', 'UPLOAD_DOCUMENT'],
    SUBMITTED: IN_REVIEW,
    ENRICHING: IN_REVIEW,
    VERIFYING: IN_REVIEW,
    SCORING: IN_REVIEW,
    DECIDING: IN_REVIEW,
    MANUAL_REVIEW: IN_REVIEW,
    WAITING_FOR_INFO: ['ANSWER_INFO_REQUEST', 'UPLOAD_DOCUMENT', 'CANCEL'],
    OFFERED: ['ACCEPT_OFFER', 'DECLINE_OFFERS'],
};

export function stageOf(status: ApplicationStatus): ClientStage {
    switch (status) {
        case 'DRAFT':
            return 'FILLING';
        case 'WAITING_FOR_INFO':
            return 'ACTION_REQUIRED';
        case 'OFFERED':
            return 'OFFER';
        case 'SIGNING':
            return 'SIGNING';
        case 'COMPLETED':
            return 'DONE';
        case 'REJECTED':
        case 'DECLINED':
        case 'CANCELLED':
        case 'EXPIRED':
            return 'CLOSED';
        default:
            return 'REVIEW';
    }
}
