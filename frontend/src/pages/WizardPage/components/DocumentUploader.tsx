import { type ReactNode, useState } from 'react';
import { Attach } from '@alfalab/core-components/attach';
import { Dropzone } from '@alfalab/core-components/dropzone';
import { FileUploadItem } from '@alfalab/core-components/file-upload-item';
import { Typography } from '@alfalab/core-components/typography';

import { useDeleteDocument, useUploadDocument } from '../../../api/queries';
import type { Document, DocumentType } from '../../../api/types';
import { formatDate, formatFileSize } from '../../../shared/lib/format';
import './DocumentUploader.css';

const ACCEPT = '.pdf,.docx,.xlsx,.jpg,.jpeg,.png';

type Upload = { key: string; name: string; size: number; error?: string };

type Props = {
    applicationId: string;
    /** Тип, с которым уйдут новые файлы. */
    type: DocumentType;
    documents: Document[];
    /** Доп. содержимое под документом (например, «Подставить распознанное»). */
    renderExtra?: (doc: Document) => ReactNode;
};

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
export function DocumentUploader({ applicationId, type, documents, renderExtra }: Props) {
    const upload = useUploadDocument(applicationId);
    const remove = useDeleteDocument(applicationId);
    const [uploads, setUploads] = useState<Upload[]>([]);

    const send = (files: File[]) => {
        for (const file of files) {
            const key = `${file.name}-${file.size}-${file.lastModified}-${Math.random()}`;
            setUploads((list) => [...list, { key, name: file.name, size: file.size }]);
            // у каждого файла свой промис: колбэки mutate() срабатывают только у последнего вызова
            upload
                .mutateAsync({ file, type })
                .then(() => setUploads((list) => list.filter((u) => u.key !== key)))
                .catch((e: unknown) =>
                    setUploads((list) =>
                        list.map((u) =>
                            u.key === key ? { ...u, error: (e instanceof Error && e.message) || 'Не удалось загрузить' } : u,
                        ),
                    ),
                );
        }
    };

    return (
        <div className='document-uploader'>
            <Dropzone onDrop={send} block={true}>
                <div className='document-uploader__zone'>
                    <Typography.Text view='primary-large' weight='bold'>
                        Перетащите файлы сюда
                    </Typography.Text>
                    <Typography.Text view='primary-small' color='secondary'>
                        PDF, DOCX, XLSX, JPG, PNG · до 20 МБ
                    </Typography.Text>
                    <Attach
                        accept={ACCEPT}
                        multiple={true}
                        value={[]}
                        noFileText=''
                        size={40}
                        buttonContent='Выбрать на компьютере'
                        buttonProps={{ view: 'secondary' }}
                        onChange={(_, { files }) => send(files)}
                    />
                </div>
            </Dropzone>

            {(documents.length > 0 || uploads.length > 0) && (
                <ul className='document-uploader__list' aria-label='Загруженные файлы'>
                    {documents.map((doc) => {
                        const recognizing = doc.recognition?.state === 'PENDING' || doc.recognition?.state === 'PROCESSING';
                        const note = recognitionText(doc);
                        return (
                            <li key={doc.id} className='document-uploader__item'>
                                <FileUploadItem
                                    id={doc.id}
                                    title={doc.file_name}
                                    // LOADING показывает subtitle, SUCCESS/UPLOADED — размер и «дату»: туда кладём итог распознавания
                                    subtitle={`${formatFileSize(doc.size_bytes)} · ${note ?? ''}`}
                                    size={doc.size_bytes}
                                    uploadDate={[formatDate(doc.uploaded_at), note].filter(Boolean).join(' · ')}
                                    uploadStatus={recognizing ? 'LOADING' : doc.recognition?.state === 'DONE' ? 'SUCCESS' : 'UPLOADED'}
                                    showDelete={true}
                                    disableButtons={remove.isPending}
                                    onDelete={(id) => remove.mutate(id)}
                                >
                                    <FileUploadItem.StatusControl />
                                    <FileUploadItem.Content />
                                    <FileUploadItem.Actions />
                                </FileUploadItem>
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
