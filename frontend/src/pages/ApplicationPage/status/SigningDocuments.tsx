import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Typography } from '@alfalab/core-components/typography';

import type { Document, Offer } from '../../../api/types';
import { useDownload } from '../../../api/useDownload';
import { formatMoney, formatUpdated, pluralize } from '../../../shared/lib/format';
import { isSchedule } from './signingDocs';
import './SigningDocuments.css';

type Props = {
    applicationId: string;
    documents: Document[];
    offer?: Offer;
    title?: string;
    actionLabel?: string;
};

/** Подпись под документом: когда сформирован договор, сколько платежей в графике. */
function subtitle(doc: Document, offer: Offer | undefined) {
    if (doc.type === 'LOAN_AGREEMENT_DRAFT') return `Сформирован ${formatUpdated(doc.uploaded_at)}`;
    if (isSchedule(doc) && offer) {
        return `${offer.term_months} ${pluralize(offer.term_months, 'платёж', 'платежа', 'платежей')} по ${formatMoney(offer.monthly_payment)}`;
    }
    return undefined;
}

const title = (doc: Document) => doc.file_name.replace(/\.pdf$/i, '');

/** Экран 11: договор, график и договор поручительства, сформированные банком. */
export function SigningDocuments({ applicationId, documents, offer, title: heading = 'Документы на подпись', actionLabel = 'Открыть' }: Props) {
    const { download, pendingId, error } = useDownload(applicationId);
    return (
        <section className='signing-documents' aria-labelledby='signing-documents-title'>
            <Typography.Title id='signing-documents-title' tag='h2' view='xsmall' font='system'>
                {heading}
            </Typography.Title>
            <ul className='signing-documents__list'>
                {documents.map((doc) => (
                    <li key={doc.id} className='signing-documents__item'>
                        <span className='signing-documents__badge' aria-hidden='true'>
                            <Typography.Text view='caps' weight='bold' color='secondary'>
                                PDF
                            </Typography.Text>
                        </span>
                        <span className='signing-documents__text'>
                            <Typography.Text view='primary-medium' weight='bold'>
                                {title(doc)}
                            </Typography.Text>
                            {subtitle(doc, offer) && (
                                <Typography.Text view='primary-small' color='secondary'>
                                    {subtitle(doc, offer)}
                                </Typography.Text>
                            )}
                        </span>
                        <Button
                            view='text'
                            size={32}
                            onClick={() => download(doc)}
                            loading={pendingId === doc.id}
                            disabled={Boolean(pendingId) && pendingId !== doc.id}
                            aria-label={`${actionLabel}: ${title(doc)}`}
                        >
                            {actionLabel}
                        </Button>
                    </li>
                ))}
            </ul>
            {error && <Plate view='negative' title={error} />}
        </section>
    );
}
