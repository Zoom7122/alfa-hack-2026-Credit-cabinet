import { Status } from '@alfalab/core-components/status';
import { Table } from '@alfalab/core-components/table';
import { Typography } from '@alfalab/core-components/typography';

import type { Document } from '../../../api/types';
import { DOCUMENT_LABELS } from '../../../shared/lib/dictionaries';
import { type Badge, recognitionBadge, signatureBadge } from '../../../shared/lib/documentBadges';
import { formatFileSize, formatUpdated, shortName } from '../../../shared/lib/format';
import { TEST_DOCUMENT_NOTICE } from '../../../shared/lib/testDocument';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';

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

/** Вкладка «Документы» досье: что приложил клиент, что распознали, чем подписано. */
export function BoDocumentsTab({ documents }: { documents: Document[] }) {
    return (
        <FormCard title='Документы заявки'>
            {documents.length === 0 ? (
                <Typography.Text view='primary-medium' color='secondary'>
                    Клиент ничего не приложил
                </Typography.Text>
            ) : (
                <Table wrapper={false}>
                    <Table.THead>
                        <Table.THeadCell>Документ</Table.THeadCell>
                        <Table.THeadCell>Тип</Table.THeadCell>
                        <Table.THeadCell>Распознавание</Table.THeadCell>
                        <Table.THeadCell>Подпись</Table.THeadCell>
                        <Table.THeadCell>Загружен</Table.THeadCell>
                    </Table.THead>
                    <Table.TBody>
                        {documents.map((doc) => (
                            <Table.TRow key={doc.id}>
                                <Table.TCell>
                                    <Typography.Text view='primary-small' weight='bold' tag='div'>
                                        {doc.file_name}
                                    </Typography.Text>
                                    {doc.is_test && <Typography.Text view='primary-small' color='secondary' tag='div'>{TEST_DOCUMENT_NOTICE}</Typography.Text>}
                                    <Typography.Text view='primary-small' color='secondary' tag='div'>
                                        {formatFileSize(doc.size_bytes)}
                                    </Typography.Text>
                                </Table.TCell>
                                <Table.TCell>
                                    <Typography.Text view='primary-small'>{DOCUMENT_LABELS[doc.type]}</Typography.Text>
                                </Table.TCell>
                                <Table.TCell>
                                    <BadgeCell badge={recognitionBadge(doc)} />
                                </Table.TCell>
                                <Table.TCell>
                                    <BadgeCell badge={signatureBadge(doc)} />
                                </Table.TCell>
                                <Table.TCell>
                                    <Typography.Text view='primary-small' tag='div'>
                                        {doc.uploaded_by ? shortName(doc.uploaded_by.full_name) : 'Банк'}
                                    </Typography.Text>
                                    <Typography.Text view='primary-small' color='secondary' tag='div'>
                                        {formatUpdated(doc.uploaded_at)}
                                    </Typography.Text>
                                </Table.TCell>
                            </Table.TRow>
                        ))}
                    </Table.TBody>
                </Table>
            )}
        </FormCard>
    );
}
