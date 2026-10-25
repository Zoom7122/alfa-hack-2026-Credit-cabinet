import type { Document } from '../../../api/types';

/* Роли документов подписания. В контракте роли нет (оба могут быть LOAN_AGREEMENT_DRAFT, см. db-schema.md) — различаем по названию. */

export const isSchedule = (doc: Document) => /^График/i.test(doc.file_name);

export const contractOf = (documents: Document[] | undefined) =>
    documents?.find((d) => d.type === 'LOAN_AGREEMENT_DRAFT' && !isSchedule(d));
