import { useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { IconButton } from '@alfalab/core-components/icon-button';
import { Plate } from '@alfalab/core-components/plate';
import { Select } from '@alfalab/core-components/select';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Status } from '@alfalab/core-components/status';
import { Table } from '@alfalab/core-components/table';
import { Typography } from '@alfalab/core-components/typography';
import { DownloadMIcon } from '@alfalab/icons-glyph/DownloadMIcon';
import { PlusMIcon } from '@alfalab/icons-glyph/PlusMIcon';
import { TrashCanLineMIcon } from '@alfalab/icons-glyph/TrashCanLineMIcon';

import { useDeleteDocument, useDocuments } from '../../../api/queries';
import type { ApplicationStatusView, Document, DocumentType } from '../../../api/types';
import { useDownload } from '../../../api/useDownload';
import { DOCUMENT_LABELS } from '../../../shared/lib/dictionaries';
import { formatFileSize, formatUpdated, shortName } from '../../../shared/lib/format';
import { DocumentUploader } from '../../../shared/ui/DocumentUploader/DocumentUploader';
import { VisuallyHidden } from '../../../shared/ui/VisuallyHidden/VisuallyHidden';
import { type Badge, recognitionBadge, signatureBadge } from './documentBadges';
import { SIGNATURE_PANEL_ID, SignaturePanel } from './SignaturePanel';
import { ConfirmDialog } from '../components/ConfirmDialog';
import './DocumentsTab.css';

/** Что клиент может загрузить сам (подпись и сгенерированные банком — не здесь). */
const UPLOADABLE = (Object.keys(DOCUMENT_LABELS) as DocumentType[]).filter((t) => t !== 'SIGNATURE' && t !== 'LOAN_AGREEMENT_DRAFT');

/** Удалять документ можно только в черновике или до ответа на уточнение (deleteDocument в openapi.yaml). */
const DELETABLE_IN = new Set(['DRAFT', 'WAITING_FOR_INFO']);

function BadgeCell({ badge }: { badge?: Badge }) {
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

/**
 * Отчёт о подписи показываем у подписанного документа; у его откреплённой .sig — нет: это тот же отчёт,
 * а без документа (если он удалён) подписи подтверждать нечего.
 */
const hasOwnReport = (doc: Document) => doc.type !== 'SIGNATURE';

/** Подпись документа в строке: размер или «подпись к …» для откреплённой подписи. */
function details(doc: Document, all: Document[]) {
    if (doc.type === 'SIGNATURE') {
        const signed = all.find((d) => d.id === doc.signed_document_id);
        return [formatFileSize(doc.size_bytes), signed && `подпись к «${signed.file_name}»`].filter(Boolean).join(' · ');
    }
    return formatFileSize(doc.size_bytes);
}

/** Экран 15: все документы заявки — что распознали, чем подписаны, кто загрузил; справа — отчёт о подписи. */
export function DocumentsTab({ status }: { status: ApplicationStatusView }) {
    const documents = useDocuments(status.id);
    const remove = useDeleteDocument(status.id);
    const { download, pendingId, error: downloadError } = useDownload(status.id);
    const [type, setType] = useState<DocumentType>('OTHER');
    const [uploading, setUploading] = useState(false);
    const [selectedId, setSelectedId] = useState<string>();
    const [deleting, setDeleting] = useState<Document>();

    const editable = status.allowed_actions.includes('UPLOAD_DOCUMENT');
    const deletable = DELETABLE_IN.has(status.status);
    const docs = documents.data ?? [];
    // по умолчанию в панели — первый подписанный документ
    const selected = docs.find((d) => d.id === selectedId) ?? docs.find((d) => d.signatures?.state === 'DONE' && hasOwnReport(d));

    return (
        <div className='documents-tab'>
            <section className='documents-tab__main' aria-labelledby='documents-tab-title'>
                <div className='documents-tab__head'>
                    <Typography.Title id='documents-tab-title' tag='h2' view='small' font='system'>
                        Документы заявки
                    </Typography.Title>
                    {editable && (
                        <Button
                            view='secondary'
                            size={48}
                            leftAddons={<PlusMIcon />}
                            onClick={() => setUploading((v) => !v)}
                            aria-expanded={uploading}
                            aria-controls='documents-tab-upload'
                            // подпись .sig привязывается к уже загруженным — без списка привязать не к чему
                            disabled={!documents.isSuccess}
                        >
                            Загрузить
                        </Button>
                    )}
                </div>

                {/* свёрнутый загрузчик не размонтируем: идущие загрузки и их ошибки не теряются */}
                {editable && (
                    <div id='documents-tab-upload' className='documents-tab__upload' hidden={!uploading}>
                        <Select
                            label='Тип документа'
                            size={56}
                            block={true}
                            options={UPLOADABLE.map((t) => ({ key: t, content: DOCUMENT_LABELS[t] }))}
                            selected={type}
                            onChange={({ selected: option }) => option && setType(option.key as DocumentType)}
                        />
                        {/* загруженные сразу появляются в таблице — в загрузчике только идущие загрузки */}
                        <DocumentUploader
                            applicationId={status.id}
                            type={type}
                            documents={[]}
                            signTargets={docs}
                            accept='.pdf,.docx,.xlsx,.jpg,.jpeg,.png,.xml,.sig,.p7s'
                            hint='PDF, DOCX, XLSX, JPG, PNG, XML · подпись .sig — к документу с тем же именем · до 20 МБ'
                        />
                    </div>
                )}

                {documents.isLoading && <Skeleton visible={true} className='documents-tab__skeleton' />}
                {documents.isError && <Plate view='negative' title='Не удалось загрузить документы' />}
                {documents.isSuccess && docs.length === 0 && (
                    <Typography.Text view='primary-medium' color='secondary'>
                        К заявке пока ничего не приложено
                    </Typography.Text>
                )}

                {docs.length > 0 && (
                    <Table wrapper={false}>
                        <Table.THead>
                            <Table.THeadCell>Документ</Table.THeadCell>
                            <Table.THeadCell width={150}>
                                Тип
                            </Table.THeadCell>
                            <Table.THeadCell width={176}>
                                Распознавание
                            </Table.THeadCell>
                            <Table.THeadCell width={156}>
                                Электронная подпись
                            </Table.THeadCell>
                            <Table.THeadCell width={124}>
                                Загружен
                            </Table.THeadCell>
                            <Table.THeadCell textAlign='right' width={88}>
                                <VisuallyHidden>Действия</VisuallyHidden>
                            </Table.THeadCell>
                        </Table.THead>
                        <Table.TBody>
                            {docs.map((doc) => {
                                const signature = signatureBadge(doc);
                                const isSelected = selected?.id === doc.id;
                                return (
                                    <Table.TRow key={doc.id} selected={isSelected}>
                                        <Table.TCell>
                                            <span className='documents-tab__name'>
                                                <Typography.Text view='primary-small' weight='bold'>
                                                    {doc.file_name}
                                                </Typography.Text>
                                                <Typography.Text view='primary-small' color='secondary'>
                                                    {details(doc, docs)}
                                                </Typography.Text>
                                            </span>
                                        </Table.TCell>
                                        <Table.TCell>
                                            <Typography.Text view='primary-small'>
                                                {doc.origin === 'GENERATED' ? 'Сформирован банком' : DOCUMENT_LABELS[doc.type]}
                                            </Typography.Text>
                                        </Table.TCell>
                                        <Table.TCell>
                                            <BadgeCell badge={recognitionBadge(doc)} />
                                        </Table.TCell>
                                        <Table.TCell>
                                            <span className='documents-tab__signature'>
                                                <BadgeCell badge={signature} />
                                                {doc.signatures?.state === 'DONE' && hasOwnReport(doc) && (
                                                    <Button
                                                        view='text'
                                                        size={32}
                                                        onClick={() => setSelectedId(doc.id)}
                                                        aria-controls={SIGNATURE_PANEL_ID}
                                                        aria-current={isSelected ? 'true' : undefined}
                                                        aria-label={`Подробнее о подписи: ${doc.file_name}`}
                                                    >
                                                        Подробнее
                                                    </Button>
                                                )}
                                            </span>
                                        </Table.TCell>
                                        <Table.TCell>
                                            <span className='documents-tab__name documents-tab__name_nowrap'>
                                                <Typography.Text view='primary-small'>
                                                    {doc.uploaded_by ? shortName(doc.uploaded_by.full_name) : 'Банк'}
                                                </Typography.Text>
                                                <Typography.Text view='primary-small' color='secondary'>
                                                    {formatUpdated(doc.uploaded_at)}
                                                </Typography.Text>
                                            </span>
                                        </Table.TCell>
                                        <Table.TCell>
                                            <span className='documents-tab__actions'>
                                                <IconButton
                                                    icon={DownloadMIcon}
                                                    size={32}
                                                    view='secondary'
                                                    onClick={() => download(doc)}
                                                    disabled={pendingId === doc.id}
                                                    aria-label={`Скачать: ${doc.file_name}`}
                                                />
                                                {deletable && doc.origin === 'CLIENT_UPLOAD' && (
                                                    <IconButton
                                                        icon={TrashCanLineMIcon}
                                                        size={32}
                                                        view='secondary'
                                                        onClick={() => setDeleting(doc)}
                                                        disabled={remove.isPending}
                                                        aria-label={`Удалить: ${doc.file_name}`}
                                                    />
                                                )}
                                            </span>
                                        </Table.TCell>
                                    </Table.TRow>
                                );
                            })}
                        </Table.TBody>
                    </Table>
                )}
                {remove.error && (
                    <Plate view='negative' title='Не удалось удалить документ'>
                        {remove.error.message}
                    </Plate>
                )}
                {downloadError && <Plate view='negative' title={downloadError} />}
            </section>

            {deleting && (
                <ConfirmDialog
                    title='Удалить документ?'
                    confirmLabel='Удалить'
                    loading={remove.isPending}
                    onClose={() => setDeleting(undefined)}
                    onConfirm={() => remove.mutate(deleting.id, { onSettled: () => setDeleting(undefined) })}
                >
                    <Typography.Text view='primary-medium' color='secondary'>
                        {deleting.file_name}
                        {docs.some((d) => d.signed_document_id === deleting.id) && ' — вместе с подписью к нему'}
                    </Typography.Text>
                </ConfirmDialog>
            )}

            <aside className='documents-tab__aside'>
                <SignaturePanel
                    applicationId={status.id}
                    document={selected}
                    hasSigned={docs.some((d) => d.signatures?.state === 'DONE')}
                />
            </aside>
        </div>
    );
}
