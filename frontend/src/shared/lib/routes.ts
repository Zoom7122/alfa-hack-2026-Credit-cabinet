import type { ApplicationSummary } from '../../api/types';

/** Куда вести по заявке: черновик — в анкету, остальное — на экран статуса. */
export const applicationPath = (app: Pick<ApplicationSummary, 'id' | 'client_stage'>) =>
    app.client_stage === 'FILLING' ? `/applications/${app.id}/edit` : `/applications/${app.id}`;
