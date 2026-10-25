import type { Applicant, Document, SignatureReport, UserRef } from '../api/types';
import { ROLE_LABELS } from '../shared/lib/dictionaries';
import { directorOf, type StoredApplication, testUsers } from './data';
import type { StoredDocument } from './seedWorkflow';

/*
 * Проверка ЭП в моке по чек-листу docs/e-signature.md. Криптография всегда «в порядке»,
 * а вердикт и политика честно следуют из того, кто подписал и чего не хватает. Чистые функции — без доступа к хранилищу.
 */

type Policy = SignatureReport['policy'];
type SignatureInfo = NonNullable<SignatureReport['signatures']>[number];
type Check = NonNullable<SignatureInfo['checks']>[number];
type Context = { documents: StoredDocument[]; applicant?: Applicant };

const DAY = 86_400_000;

/** Подпись документа: откреплённая (.sig со ссылкой на него) или встроенная в сам файл. */
export function signatureSourceOf(doc: StoredDocument, documents: StoredDocument[]): StoredDocument | undefined {
    if (doc.type === 'SIGNATURE' || doc.mock_signature) return doc;
    return documents
        .filter((d) => d.type === 'SIGNATURE' && d.signed_document_id === doc.id)
        .sort((a, b) => b.uploaded_at.localeCompare(a.uploaded_at))[0];
}

/** Что подписано: для откреплённой подписи — документ, на который она ссылается. */
const signedOf = (signature: StoredDocument, documents: StoredDocument[]) =>
    signature.type === 'SIGNATURE' ? documents.find((d) => d.id === signature.signed_document_id) : signature;

/** Политика — по типу подписанного документа (таблица «Политики» в docs/e-signature.md). */
export function policyFor(signed: StoredDocument | undefined): Policy {
    if (signed?.type === 'LOAN_AGREEMENT_DRAFT') return 'LOAN_CONTRACT';
    if (signed?.type === 'POWER_OF_ATTORNEY') return 'POWER_OF_ATTORNEY';
    if (signed?.type === 'FINANCIAL_STATEMENTS' || signed?.type === 'TAX_RETURN') return 'FINANCIAL_STATEMENTS';
    return 'GENERIC';
}

/**
 * Кто подписал. Доверенность и кредитный договор по сценарию подписывает руководитель — даже если файл загрузил сотрудник;
 * остальное в моке подписывает тот, кто загрузил.
 */
function signerOf(signature: StoredDocument, policy: Policy, app: StoredApplication): UserRef {
    if (policy === 'POWER_OF_ATTORNEY' || policy === 'LOAN_CONTRACT') {
        const director = directorOf(app.applicant.id);
        if (director) return director;
    }
    return signature.uploaded_by ?? { id: 'unknown', full_name: 'Подписант' };
}

function roleOf(userId: string, applicantId: string) {
    return testUsers.find((u) => u.id === userId)?.companies?.find((c) => c.applicant_id === applicantId)?.role;
}

/** Время проверки — фиксированное от загрузки, чтобы отчёт не «плавал» от запроса к запросу. */
const verifiedAt = (signature: StoredDocument) => signature.verify_at ?? new Date(signature.uploaded_at).getTime() + 60_000;

export function buildReport(app: StoredApplication, signature: StoredDocument, { documents, applicant }: Context): SignatureReport {
    const signed = signedOf(signature, documents);
    const policy = policyFor(signed);
    const signer = signerOf(signature, policy, app);
    const isHead = signer.id === directorOf(app.applicant.id)?.id;
    const soleProprietor = applicant?.applicant_type === 'SOLE_PROPRIETOR';
    const embedded = signature.type !== 'SIGNATURE';
    const noTimestamp = signature.mock_signature?.verdict === 'VALID_WITH_WARNINGS';
    const uploaded = new Date(signature.uploaded_at).getTime();
    const checkedAt = new Date(verifiedAt(signature)).toISOString();
    const needsHead = policy !== 'GENERIC';

    const checks: Check[] = [
        { code: 'FORMAT_PARSED', status: 'PASSED' },
        { code: 'INTEGRITY', status: 'PASSED', message: 'Документ не изменён после подписи' },
        { code: 'SIGNATURE_VALUE', status: 'PASSED' },
        ...(embedded ? [{ code: 'DOCUMENT_NOT_MODIFIED' as const, status: 'PASSED' as const }] : []),
        { code: 'CERT_VALID_AT_SIGNING_TIME', status: 'PASSED', message: 'Сертификат действовал на момент подписи' },
        noTimestamp
            ? { code: 'SIGNING_TIME_TRUSTED', status: 'WARNING', message: 'Нет метки доверенного времени' }
            : { code: 'SIGNING_TIME_TRUSTED', status: 'PASSED', message: 'Время подписи подтверждено меткой времени' },
        { code: 'CHAIN_TO_TRUSTED_ROOT', status: 'PASSED' },
        { code: 'NOT_REVOKED', status: 'PASSED', message: 'Сертификат не отозван' },
        ...(needsHead
            ? [
                  isHead
                      ? {
                            code: 'SIGNER_AUTHORITY' as const,
                            status: 'PASSED' as const,
                            message: soleProprietor ? 'Подписал сам предприниматель' : 'Подписант — руководитель компании по ЕГРЮЛ',
                        }
                      : {
                            code: 'SIGNER_AUTHORITY' as const,
                            status: 'WARNING' as const,
                            message: 'Подписант не руководитель — нужна подпись руководителя',
                        },
              ]
            : []),
    ];
    // любое замечание снижает вердикт до «с замечаниями» (термины ETSI в docs/e-signature.md)
    const verdict = checks.some((c) => c.status === 'FAILED')
        ? 'INVALID'
        : checks.some((c) => c.status === 'WARNING')
          ? 'VALID_WITH_WARNINGS'
          : 'VALID';
    const requiredSigners = needsHead ? [{ role: 'APPLICANT_HEAD', satisfied: isHead, signature_index: isHead ? 0 : null }] : [];
    const satisfied = verdict !== 'INVALID' && requiredSigners.every((r) => r.satisfied);
    const role = roleOf(signer.id, app.applicant.id);

    const info: SignatureInfo = {
        index: 0,
        format: embedded ? 'PADES' : noTimestamp ? 'CADES_BES' : 'CADES_T',
        container: embedded ? 'PDF_EMBEDDED' : 'DETACHED',
        kind: 'QUALIFIED',
        verdict,
        signing_time: { value: signature.uploaded_at, source: noTimestamp ? 'SIGNED_ATTRIBUTE' : 'TIMESTAMP', tsa_name: null },
        signer: {
            kind: isHead ? (soleProprietor ? 'SOLE_PROPRIETOR' : 'LEGAL_ENTITY_HEAD') : 'LEGAL_ENTITY_EMPLOYEE',
            full_name: signer.full_name,
            position: isHead ? (soleProprietor ? 'Индивидуальный предприниматель' : 'Генеральный директор') : role ? ROLE_LABELS[role] : null,
            organization: {
                name: app.applicant.short_name,
                inn: app.applicant.inn,
                ...(applicant?.ogrn ? { ogrn: applicant.ogrn } : {}),
            },
        },
        certificate: {
            serial_number: `01${signature.id.slice(-8)}`,
            thumbprint: 'mock-thumbprint',
            valid_from: new Date(uploaded - 200 * DAY).toISOString(),
            valid_to: new Date(uploaded + 365 * DAY).toISOString(),
            issuer: { name: 'УЦ ФНС (тест)', accredited: true },
            key_algorithm: 'GOST_2012_256',
            revocation: { status: 'GOOD', method: 'OCSP', checked_at: checkedAt },
        },
        authority: {
            basis: isHead ? (soleProprietor ? 'SOLE_PROPRIETOR' : 'REGISTRY_HEAD') : 'NONE',
            matches_applicant: true,
        },
        checks,
    };

    return {
        id: `sigr_${signature.id}`,
        document_id: signed?.id ?? null,
        file_name: signed?.file_name ?? signature.file_name,
        document_sha256: 'mock-sha256',
        verdict,
        policy,
        policy_result: {
            satisfied,
            required_signers: requiredSigners,
            ...(satisfied ? {} : { message: `Нет подписи руководителя ${app.applicant.short_name}` }),
        },
        trust_mode: 'MOCK',
        verified_at: checkedAt,
        signatures: [info],
    } satisfies SignatureReport;
}

/** Сводка Document.signatures — из того же отчёта, чтобы бейдж и панель не расходились. */
export function signatureSummary(
    doc: StoredDocument,
    app: StoredApplication | undefined,
    context: Context,
    now: number,
): Document['signatures'] {
    const signature = signatureSourceOf(doc, context.documents);
    if (!signature || !app) return { state: 'NOT_SIGNED' };
    const policy = policyFor(signedOf(signature, context.documents));
    if (signature.verify_at && now < signature.verify_at) return { state: 'PENDING', policy };
    const report = buildReport(app, signature, context);
    return {
        state: 'DONE',
        verdict: report.verdict,
        signatures_count: report.signatures?.length ?? 1,
        policy,
        policy_satisfied: report.policy_result.satisfied,
        verified_at: report.verified_at,
    };
}
