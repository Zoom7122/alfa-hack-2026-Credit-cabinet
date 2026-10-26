import type { CollectorDocumentType, CollectorPurpose, MaritalStatus, ScoringAnswers, TaxScope } from '../../api/collector/types';

export type FileEntry = {
    /** Ключ содержимого в IndexedDB (fileStore). */
    key: string;
    name: string;
    size: number;
    /** Тип документа; у открепленной подписи (.sig, .p7s) — нет, она всегда SIGNATURE. */
    type?: CollectorDocumentType;
    isTest?: boolean;
};

export type DirectorDraft = { inn: string; last_name: string; first_name: string; middle_name: string; birth_date: string };

export type QuestionnaireDraft = {
    product_code?: string;
    requested?: { amount?: number; term_months?: number };
    purpose?: { code?: CollectorPurpose; description?: string };
    age?: number;
    marital_status?: MaritalStatus;
    children_count?: number;
    /** false — руководителя не указываем: data-collector не запросит данные физлица. */
    with_director: boolean;
    director: DirectorDraft;
    other_bank_biks: string[];
    collateral: { cadastral_numbers: string[]; vins: string[]; serials: string[] };
    files: FileEntry[];
    /** Время, когда клиент дал согласие (signed_at); нет — согласия нет. */
    consents: { bki_company?: string; bki_person?: string; tax?: string };
    tax_scope: TaxScope[];
    poa_number: string;
    representative_inn: string;
    /** Ответы для оценки банком; чего нет — банк спросит после оценки. */
    scoring: ScoringAnswers;
};

export type DraftInput = Omit<Partial<QuestionnaireDraft>, 'director' | 'collateral'> & {
    director?: Partial<DirectorDraft>;
    collateral?: Partial<QuestionnaireDraft['collateral']>;
};

export type SavedDraft = { data: Omit<DraftInput, 'files'>; version: number; updated_at: string };
