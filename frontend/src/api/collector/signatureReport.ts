/**
 * Отчёт проверки подписи data-collector (GET …/signature-report). В swagger он — просто объект,
 * поэтому тип — по Go-структуре signature.Report (data-collector/internal/signature/report.go); только то, что показываем.
 */
export type VerificationStatus = 'VERIFIED' | 'INVALID' | 'INDETERMINATE' | 'NOT_REQUIRED';

export type ReportStage = {
    status: VerificationStatus;
    reason?: string;
    checks?: { code: string; status: VerificationStatus; message?: string }[];
    warnings?: string[];
};

export type SignatureReportJson = {
    id: string;
    profile: string;
    profile_notice?: string;
    file_name?: string;
    signature_file_name?: string;
    format: string;
    decision: VerificationStatus;
    verdict: 'VALID' | 'VALID_WITH_WARNINGS' | 'INVALID' | 'INDETERMINATE' | string;
    failure_reason?: string;
    policy: string;
    policy_result?: { satisfied: boolean; message: string };
    stages: Record<'integrity' | 'format' | 'certificate' | 'identity' | 'authority' | 'decision', ReportStage>;
    signing_time?: { value: string | null; source: string } | null;
    signer?: {
        kind: string;
        full_name: string;
        position?: string;
        inn_individual?: string;
        organization?: { name?: string; inn?: string; ogrn?: string };
    } | null;
    certificate?: { issuer?: { name: string; accredited: boolean }; subject?: string; serial_number?: string; valid_from?: string; valid_to?: string } | null;
    limitations?: string[];
    verified_at: string;
};
