import { useState } from 'react';

import { downloadDocument } from './queries';
import type { Document } from './types';

/** Скачивание по кнопке: одно за раз (двойной клик не качает дважды), с ошибкой для показа. */
export function useDownload(applicationId: string) {
    const [pendingId, setPendingId] = useState<string>();
    const [error, setError] = useState<string>();
    const download = (doc: Document) => {
        if (pendingId) return;
        setPendingId(doc.id);
        setError(undefined);
        downloadDocument(applicationId, doc)
            .catch(() => setError(`Не удалось скачать: ${doc.file_name}`))
            .finally(() => setPendingId(undefined));
    };
    return { download, pendingId, error };
}
