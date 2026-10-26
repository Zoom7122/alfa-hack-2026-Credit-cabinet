import { useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { IconButton } from '@alfalab/core-components/icon-button';
import { Plate } from '@alfalab/core-components/plate';
import { Status } from '@alfalab/core-components/status';
import { Table } from '@alfalab/core-components/table';
import { Typography } from '@alfalab/core-components/typography';
import { DownloadMIcon } from '@alfalab/icons-glyph/DownloadMIcon';

import { downloadCollectorDocument } from '../../../api/collector/queries';
import { useCollectorSigning } from '../../../api/collector/signing';
import type { CollectorDocument } from '../../../api/collector/types';
import type { DocumentType } from '../../../api/types';
import { DOCUMENT_LABELS } from '../../../shared/lib/dictionaries';
import { formatFileSize, formatUpdated } from '../../../shared/lib/format';
import { SIGNATURE_PANEL_ID, CollectorSignaturePanel } from './CollectorSignaturePanel';
import './CollectorDocumentsTab.css';

type Color = 'green' | 'orange' | 'red' | 'blue' | 'grey';

type Badge = { label: string; color: Color };

const VERDICT: Record<NonNullable<CollectorDocument['signature_verdict']>, Badge> = {
    VALID: { label: 'Подпись верна', color: 'green' },
    VALID_WITH_WARNINGS: { label: 'Верна, есть замечания', color: 'green' },
    INVALID: { label: 'Подпись недействительна', color: 'red' },
    INDETERMINATE: { label: 'Подпись не подтверждена', color: 'orange' },
};

function signatureBadge(doc: CollectorDocument): Badge | undefined {
    switch (doc.signature_state) {
        case 'PENDING':
            return { label: 'Проверяем подпись', color: 'blue' };
        case 'DONE':
            return doc.signature_verdict ? VERDICT[doc.signature_verdict] : { label: 'Проверка завершена', color: 'grey' };
        case 'FAILED':
            return { label: 'Подпись не проверить', color: 'red' };
    }
    return undefined;
}
const RECOGNITION: Record<NonNullable<CollectorDocument['recognition_state']>, Badge | undefined> = {
    NOT_APPLICABLE: undefined,
    PENDING: { label: 'В очереди', color: 'grey' },
    PROCESSING: { label: 'Распознаём', color: 'blue' },
    DONE: { label: 'Распознан', color: 'green' },
    FAILED: { label: 'Проверим вручную', color: 'orange' },
};

function StateBadge({ badge }: { badge?: Badge }) {
    if (!badge) {
        return (
            <Typography.Text view='primary-small' color='tertiary'>
                —
            </Typography.Text>
        );
    }
    return (
        <Status view='muted-alt' color={badge.color} size={24} uppercase={false}>
            {badge.label}
        </Status>
    );
}

/** Отчёт — у каждой открепленной .sig (подписантов бывает несколько), у документа — только при встроенной подписи. */
function hasReport(doc: CollectorDocument, documents: CollectorDocument[]) {
    if (doc.signature_state !== 'DONE' && doc.signature_state !== 'FAILED') return false;
    return doc.type === 'SIGNATURE' || !documents.some((d) => d.signed_document_id === doc.id);
}

/** Вкладка «Документы» (экран 15) для заявки data-collector: файлы анкеты, распознавание, подпись и отчёт о ней. */
export function CollectorDocumentsTab({ applicationId, documents }: { applicationId: string; documents: CollectorDocument[] }) {
    const signing = useCollectorSigning(applicationId, documents.some((d) => d.type === 'LOAN_AGREEMENT_DRAFT'));
    const [selectedId, setSelectedId] = useState<string>();
    const [downloading, setDownloading] = useState<string>();
    const [downloadError, setDownloadError] = useState<string>();
    const selected = documents.find((d) => d.id === selectedId);

    const download = async (doc: CollectorDocument) => {
        setDownloading(doc.id);
        setDownloadError(undefined);
        try {
            await downloadCollectorDocument(applicationId, doc.id ?? '', doc.file_name ?? 'document');
        } catch {
            setDownloadError(`Не удалось скачать «${doc.file_name}»`);
        } finally {
            setDownloading(undefined);
        }
    };

    if (!documents.length) {
        return (
            <Typography.Text view='primary-medium' color='secondary'>
                Документов в анкете не было.
            </Typography.Text>
        );
    }

    return (
        <div className='collector-documents'>
            <section className='collector-documents__main' aria-labelledby='collector-documents-title'>
                <Typography.Title id='collector-documents-title' tag='h2' view='small' font='system'>
                    Документы заявки
                </Typography.Title>
                <Table wrapper={false}>
                    <Table.THead>
                        <Table.THeadCell>Документ</Table.THeadCell>
                        <Table.THeadCell width={180}>Тип</Table.THeadCell>
                        <Table.THeadCell width={156}>Распознавание</Table.THeadCell>
                        <Table.THeadCell width={200}>Подпись</Table.THeadCell>
                        <Table.THeadCell textAlign='right' width={64}>
                            Файл
                        </Table.THeadCell>
                    </Table.THead>
                    <Table.TBody>
                        {documents.map((doc) => {
                            const signed = doc.signed_document_id && documents.find((d) => d.id === doc.signed_document_id);
                            const isSelected = selected?.id === doc.id;
                            return (
                                <Table.TRow key={doc.id} selected={isSelected}>
                                    <Table.TCell>
                                        <span className='collector-documents__name'>
                                            <Typography.Text view='primary-small' weight='bold'>
                                                {doc.file_name}
                                            </Typography.Text>
                                            <Typography.Text view='primary-small' color='secondary'>
                                                {[doc.size_bytes !== undefined && formatFileSize(doc.size_bytes), signed && `подпись к «${signed.file_name}»`, doc.uploaded_at && formatUpdated(doc.uploaded_at)]
                                                    .filter(Boolean)
                                                    .join(' · ')}
                                            </Typography.Text>
                                        </span>
                                    </Table.TCell>
                                    <Table.TCell>
                                        <Typography.Text view='primary-small'>{DOCUMENT_LABELS[(doc.type ?? 'OTHER') as DocumentType]}</Typography.Text>
                                    </Table.TCell>
                                    <Table.TCell>
                                        <StateBadge badge={doc.recognition_state && RECOGNITION[doc.recognition_state]} />
                                    </Table.TCell>
                                    <Table.TCell>
                                        <span className='collector-documents__signature'>
                                            <StateBadge badge={signing.data?.state === 'SIGNED' && signing.data.documents.some((d) => d.id === doc.id) ? { label: 'Подписан демо-кодом', color: 'green' } : signatureBadge(doc)} />
                                            {hasReport(doc, documents) && (
                                                <Button
                                                    view='text'
                                                    size={32}
                                                    onClick={() => setSelectedId(doc.id)}
                                                    aria-controls={SIGNATURE_PANEL_ID}
                                                    aria-current={isSelected ? 'true' : undefined}
                                                    aria-label={`Отчёт о подписи: ${doc.file_name}`}
                                                >
                                                    Отчёт
                                                </Button>
                                            )}
                                        </span>
                                    </Table.TCell>
                                    <Table.TCell>
                                        <span className='collector-documents__actions'>
                                            <IconButton
                                                icon={DownloadMIcon}
                                                size={32}
                                                view='secondary'
                                                onClick={() => void download(doc)}
                                                disabled={downloading === doc.id}
                                                aria-label={`Скачать: ${doc.file_name}`}
                                            />
                                        </span>
                                    </Table.TCell>
                                </Table.TRow>
                            );
                        })}
                    </Table.TBody>
                </Table>
                {downloadError && <Plate view='negative' title={downloadError} />}
            </section>
            {selected && <CollectorSignaturePanel applicationId={applicationId} document={selected} onClose={() => setSelectedId(undefined)} />}
        </div>
    );
}
