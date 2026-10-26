import { type ReactNode, useEffect, useState } from 'react';
import { Attach } from '@alfalab/core-components/attach';
import { Button } from '@alfalab/core-components/button';
import { Dropzone } from '@alfalab/core-components/dropzone';
import { FileUploadItem } from '@alfalab/core-components/file-upload-item';
import { Plate } from '@alfalab/core-components/plate';
import { Typography } from '@alfalab/core-components/typography';

import { useDeleteDocument, useUploadDocument } from '../../../api/queries';
import type { Document, DocumentType } from '../../../api/types';
import { formatDate, formatFileSize } from '../../lib/format';
import { createTestDocument, TEST_DOCUMENT_NOTICE } from '../../lib/testDocument';
import { DocumentVerification } from './DocumentVerification';
import './DocumentUploader.css';

const ACCEPT = '.pdf,.docx,.xlsx,.jpg,.jpeg,.png,.sig,.p7s';
const DEFAULT_HINT = 'PDF, DOCX, XLSX, JPG, PNG · до 20 МБ';

type Upload = { key: string; name: string; size: number; error?: string };

const isSignatureFile = (file: File) => /\.(sig|p7s)$/i.test(file.name);

/**
 * К чему относится подпись: «doverennost.xml.sig» → «doverennost.xml». Без совпадения по имени — к файлу из той же загрузки,
 * а среди ранее загруженных — только если он единственный, чтобы не прикрепить подпись к чужому документу.
 */
function signedTarget(file: File, batch: Document[], existing: Document[]) {
    const base = file.name.replace(/\.(sig|p7s)$/i, '');
    return [...batch, ...existing].find((d) => d.file_name === base) ??
        (batch.length === 1 ? batch[0] : batch.length === 0 && existing.length === 1 ? existing[0] : undefined);
}

type Props = {
    applicationId: string;
    /** Тип, с которым уйдут новые файлы. */
    type: DocumentType;
    documents: Document[];
    /** Доп. содержимое под документом (например, «Подставить распознанное»). */
    renderExtra?: (doc: Document) => ReactNode;
    /** Документ — ответ на вопрос банка (info_request_id в загрузке). */
    infoRequestId?: string;
    /** Подсказка под зоной загрузки и допустимые расширения. */
    hint?: string;
    accept?: string;
    /** Можно загружать (по allowed_actions); без этого — только список. */
    editable?: boolean;
    /** Можно удалять: по контракту — только в DRAFT или до ответа на уточнение. */
    deletable?: boolean;
    /** К каким уже загруженным документам можно прикрепить .sig (если список рисуется не здесь). */
    signTargets?: Document[];
    onUploadingChange?: (uploading: boolean) => void;
};

function signatureText(doc: Document) {
    const sig = doc.signatures;
    if (!sig || sig.state === 'NOT_SIGNED') return undefined;
    if (sig.state === 'PENDING') return 'проверяем подпись…';
    if (sig.state === 'FAILED') return 'подпись не удалось проверить';
    if (sig.verdict === 'VALID' || sig.verdict === 'VALID_WITH_WARNINGS') return sig.policy_satisfied === false ? 'подпись верна, но не все подписи' : 'подпись верна';
    if (sig.verdict === 'INVALID') return 'подпись недействительна';
    return 'подпись пока не проверить';
}

function recognitionText(doc: Document) {
    switch (doc.recognition?.state) {
        case 'PENDING':
        case 'PROCESSING':
            return 'распознаём…';
        case 'DONE':
            return 'распознали';
        case 'FAILED':
            return 'не удалось распознать — проверим вручную';
        default:
            return undefined;
    }
}

/** Dropzone + список файлов. Распознавание идёт на бэке, статус приходит в Document.recognition. */
export function DocumentUploader({
    applicationId,
    type,
    documents,
    renderExtra,
    infoRequestId,
    hint = DEFAULT_HINT,
    accept = ACCEPT,
    editable = true,
    deletable = editable,
    signTargets = [],
    onUploadingChange,
}: Props) {
    const upload = useUploadDocument(applicationId);
    const remove = useDeleteDocument(applicationId);
    const [uploads, setUploads] = useState<Upload[]>([]);
    const [addingTest, setAddingTest] = useState(false);
    const [testError, setTestError] = useState<string>();
    const uploading = addingTest || uploads.some((u) => !u.error);
    useEffect(() => { onUploadingChange?.(uploading); }, [uploading, onUploadingChange]);

    const track = (file: File) => {
        const key = `${file.name}-${file.size}-${file.lastModified}-${Math.random()}`;
        setUploads((list) => [...list, { key, name: file.name, size: file.size }]);
        return {
            done: () => setUploads((list) => list.filter((u) => u.key !== key)),
            fail: (e: unknown) =>
                setUploads((list) =>
                    list.map((u) => (u.key === key ? { ...u, error: (e instanceof Error && e.message) || 'Не удалось загрузить' } : u)),
                ),
        };
    };

    // у каждого файла свой промис: колбэки mutate() срабатывают только у последнего вызова
    const send = async (files: File[]) => {
        const signatures = files.filter(isSignatureFile);
        const regular = files.filter((f) => !isSignatureFile(f));
        const uploaded = await Promise.all(
            regular.map((file) => {
                const t = track(file);
                return upload
                    .mutateAsync({ file, type, infoRequestId })
                    .then((doc) => {
                        t.done();
                        return doc;
                    })
                    .catch((e: unknown) => {
                        t.fail(e);
                        return undefined;
                    });
            }),
        );
        // откреплённая подпись — документ SIGNATURE со ссылкой на подписанный файл (контракт uploadDocument)
        const notSignature = (d: Document) => d.type !== 'SIGNATURE';
        const batch = uploaded.filter((d): d is Document => Boolean(d)).filter(notSignature);
        const existing = [...documents, ...signTargets].filter(notSignature);
        for (const file of signatures) {
            const t = track(file);
            const target = regular.length && !batch.length ? undefined : signedTarget(file, batch, existing);
            if (!target) {
                t.fail(
                    new Error(
                        existing.length
                            ? 'Не поняли, к какому документу подпись: назовите файл как документ, например «Устав.pdf.sig»'
                            : 'Сначала загрузите документ, к которому относится подпись',
                    ),
                );
                continue;
            }
            upload
                .mutateAsync({ file, type: 'SIGNATURE', infoRequestId, signedDocumentId: target.id })
                .then(t.done)
                .catch(t.fail);
        }
    };

    const addTest = async () => {
        setAddingTest(true);
        setTestError(undefined);
        try {
            await send([await createTestDocument()]);
        } catch (error) {
            setTestError(error instanceof Error ? error.message : 'Не удалось добавить тестовый PDF');
        } finally {
            setAddingTest(false);
        }
    };

    return (
        <div className='document-uploader'>
            {editable && (
                <Dropzone onDrop={(files) => void send(files)} block={true}>
                    <div className='document-uploader__zone'>
                        <Typography.Text view='primary-large' weight='bold'>
                            Перетащите файлы сюда
                        </Typography.Text>
                        <Typography.Text view='primary-small' color='secondary'>
                            {hint}
                        </Typography.Text>
                        <Attach
                            accept={accept}
                            multiple={true}
                            value={[]}
                            noFileText=''
                            size={40}
                            buttonContent='Выбрать на компьютере'
                            buttonProps={{ view: 'secondary' }}
                            onChange={(_, { files }) => void send(files)}
                        />
                    </div>
                </Dropzone>
            )}

            {editable && accept.split(',').some((ext) => ['.pdf', 'application/pdf'].includes(ext.trim())) && (
                <Button view='secondary' size={40} loading={addingTest} disabled={uploading} onClick={() => void addTest()}>
                    Добавить тестовый PDF
                </Button>
            )}
            {testError && <Plate view='negative' title={testError} />}

            {remove.error && (
                <Plate view='negative' title='Не удалось удалить файл'>
                    {remove.error.message}
                </Plate>
            )}

            {(documents.length > 0 || uploads.length > 0) && (
                <ul className='document-uploader__list' aria-label='Загруженные файлы'>
                    {documents.map((doc) => {
                        const recognizing =
                            doc.recognition?.state === 'PENDING' || doc.recognition?.state === 'PROCESSING' || doc.signatures?.state === 'PENDING';
                        const signedName =
                            doc.type === 'SIGNATURE' ? documents.find((d) => d.id === doc.signed_document_id)?.file_name : undefined;
                        const note =
                            [signedName && `подпись к ${signedName}`, recognitionText(doc), signatureText(doc)].filter(Boolean).join(' · ') ||
                            undefined;
                        const done = doc.recognition?.state === 'DONE' || doc.signatures?.state === 'DONE';
                        const bundledSignature = doc.type === 'SIGNATURE' && documents.some((parent) =>
                            parent.id === doc.signed_document_id && ['FINANCIAL_STATEMENTS', 'TAX_RETURN'].includes(parent.type));
                        return (
                            <li key={doc.id} className='document-uploader__item'>
                                {doc.is_test && <Typography.Text view='primary-small' color='secondary'>{TEST_DOCUMENT_NOTICE}</Typography.Text>}
                                <FileUploadItem
                                    id={doc.id}
                                    title={doc.file_name}
                                    // LOADING показывает subtitle, SUCCESS/UPLOADED — размер и «дату»: туда кладём итог распознавания
                                    subtitle={`${formatFileSize(doc.size_bytes)} · ${note ?? ''}`}
                                    size={doc.size_bytes}
                                    uploadDate={[formatDate(doc.uploaded_at), note].filter(Boolean).join(' · ')}
                                    uploadStatus={recognizing ? 'LOADING' : done ? 'SUCCESS' : 'UPLOADED'}
                                    showDelete={deletable}
                                    disableButtons={remove.isPending}
                                    onDelete={(id) => remove.mutate(id)}
                                >
                                    <FileUploadItem.StatusControl />
                                    <FileUploadItem.Content />
                                    <FileUploadItem.Actions />
                                </FileUploadItem>
                                {infoRequestId && !bundledSignature && <DocumentVerification applicationId={applicationId} document={doc} editable={editable} />}
                                {renderExtra?.(doc)}
                            </li>
                        );
                    })}
                    {uploads.map((u) => (
                        <li key={u.key} className='document-uploader__item'>
                            <FileUploadItem
                                title={u.name}
                                subtitle={u.error ? undefined : `${formatFileSize(u.size)} · загружаем…`}
                                uploadStatus={u.error ? 'ERROR' : 'LOADING'}
                                error={u.error}
                                showDelete={Boolean(u.error)}
                                onDelete={() => setUploads((list) => list.filter((x) => x.key !== u.key))}
                            >
                                <FileUploadItem.StatusControl />
                                <FileUploadItem.Content />
                                <FileUploadItem.Actions />
                            </FileUploadItem>
                        </li>
                    ))}
                </ul>
            )}
        </div>
    );
}
