import { useState } from 'react';
import { Attach } from '@alfalab/core-components/attach';
import { Dropzone } from '@alfalab/core-components/dropzone';
import { FileUploadItem } from '@alfalab/core-components/file-upload-item';
import { Plate } from '@alfalab/core-components/plate';
import { Select } from '@alfalab/core-components/select';
import { Typography } from '@alfalab/core-components/typography';

import type { CollectorDocumentType } from '../../../api/collector/types';
import { DOCUMENT_LABELS } from '../../../shared/lib/dictionaries';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { StepLayout } from '../components/StepLayout';
import { type FileEntry, isDetachedSignature, isSignatureName, safeFileName, signedTarget } from '../draft';
import { deleteFile, putFile } from '../fileStore';
import { type StepProps, useStepGate } from '../stepProps';
import './DocumentsStep.css';

const ACCEPT = '.pdf,.docx,.xlsx,.jpg,.jpeg,.png,.xml,.sig,.p7s';
const TYPES: CollectorDocumentType[] = [
    'FINANCIAL_STATEMENTS', 'TAX_RETURN', 'BANK_STATEMENT', 'MANAGEMENT_REPORT', 'CHARTER', 'POWER_OF_ATTORNEY', 'DIRECTOR_PASSPORT',
    'COLLATERAL_TITLE', 'EGRN_EXTRACT', 'LEASE_AGREEMENT', 'CONTRACT_WITH_CUSTOMER', 'OTHER',
];
const TYPE_OPTIONS = TYPES.map((t) => ({ key: t, content: DOCUMENT_LABELS[t] }));

function signatureNote(file: FileEntry, files: FileEntry[]) {
    const target = signedTarget(file, files);
    if (target === 'ambiguous') return undefined;
    return target ? `подпись к ${target.name}` : 'присоединённая подпись: документ внутри файла';
}

/** Шаг «Документы»: файлы держим в браузере, уйдут вместе с анкетой. Подпись X.sig относится к документу X. */
export function DocumentsStep({ draft, hrefFor, errors, serverErrors }: StepProps) {
    const files = draft.value.files;
    const gate = useStepGate(errors, serverErrors, hrefFor('review'));
    const [storeError, setStoreError] = useState<string>();

    const add = async (picked: File[]) => {
        setStoreError(undefined);
        const entries: FileEntry[] = [];
        for (const file of picked) {
            // ключ переживает перезагрузку вместе с черновиком — счётчик не подойдёт
            const key = crypto.randomUUID();
            try {
                await putFile(key, file);
                const name = safeFileName(file.name);
                entries.push({ key, name, size: file.size, type: 'OTHER' });
            } catch {
                setStoreError(`Не удалось сохранить «${file.name}» в браузере — попробуйте ещё раз`);
            }
        }
        if (entries.length) draft.update((prev) => ({ files: [...prev.files, ...entries] }));
    };

    const remove = (key: string) => {
        void deleteFile(key).catch(() => undefined);
        draft.update((prev) => ({ files: prev.files.filter((f) => f.key !== key) }));
    };

    // ошибки data-collector приходят по индексу части files — порядок частей тот же, что в списке
    const errorOf = (file: FileEntry, i: number) => gate.errorOf(file.key) ?? serverErrors[`files[${i}]`];

    return (
        <StepLayout
            title='Документы'
            subtitle='Необязательно: без документов решение займёт больше времени'
            backHref={hrefFor('collateral')}
            onNext={gate.onNext}
            hint={gate.hint}
        >
            <FormCard>
                <Dropzone onDrop={(picked) => void add(picked)} block={true}>
                    <div className='documents-step__zone'>
                        <Typography.Text view='primary-large' weight='bold'>
                            Перетащите файлы сюда
                        </Typography.Text>
                        <Typography.Text view='primary-small' color='secondary'>
                            PDF, DOCX, XLSX, JPG, PNG, XML · подписи SIG, P7S · до 10 файлов по 20 МБ
                        </Typography.Text>
                        <Attach
                            accept={ACCEPT}
                            multiple={true}
                            value={[]}
                            noFileText=''
                            size={40}
                            buttonContent='Выбрать на компьютере'
                            buttonProps={{ view: 'secondary' }}
                            onChange={(_, { files: picked }) => void add(picked)}
                        />
                    </div>
                </Dropzone>
                <Plate view='common' rounded={true}>
                    Открепленную подпись называйте как документ с окончанием .sig: «balance.pdf» и «balance.pdf.sig».
                </Plate>
                {storeError && <Plate view='negative' title={storeError} />}
                {gate.errorOf('files') && <Plate view='negative' title={gate.errorOf('files')} />}

                {files.length > 0 && (
                    <ul className='documents-step__list' aria-label='Файлы заявки'>
                        {files.map((f, i) => {
                            const error = errorOf(f, i);
                            const detached = isDetachedSignature(f, files);
                            const note = isSignatureName(f.name) ? signatureNote(f, files) : undefined;
                            return (
                                <li key={f.key} className='documents-step__item'>
                                    <FileUploadItem
                                        id={f.key}
                                        title={f.name}
                                        size={f.size}
                                        uploadStatus={error ? 'ERROR' : 'UPLOADED'}
                                        error={error}
                                        showDelete={true}
                                        onDelete={() => remove(f.key)}
                                    >
                                        <FileUploadItem.StatusControl />
                                        <FileUploadItem.Content />
                                        <FileUploadItem.Actions />
                                    </FileUploadItem>
                                    {note && (
                                        <Typography.Text view='primary-small' color='secondary'>
                                            {note}
                                        </Typography.Text>
                                    )}
                                    {!detached && (
                                        <Select
                                            label={`Что за документ: ${f.name}`}
                                            size={48}
                                            block={true}
                                            options={TYPE_OPTIONS}
                                            selected={f.type ?? 'OTHER'}
                                            onChange={({ selected }) =>
                                                selected &&
                                                draft.update((prev) => ({
                                                    files: prev.files.map((x) => (x.key === f.key ? { ...x, type: selected.key as CollectorDocumentType } : x)),
                                                }))
                                            }
                                        />
                                    )}
                                </li>
                            );
                        })}
                    </ul>
                )}
            </FormCard>
        </StepLayout>
    );
}
