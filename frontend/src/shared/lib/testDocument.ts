import metadata from './testDocumentMetadata.json';

export const TEST_DOCUMENT_NOTICE = 'Тестовый документ: данные не участвуют в скоринге';
export const TEST_DOCUMENT_SHA256 = metadata.sha256;

/** Used by the browser preview and MSW; the backend independently checks stored bytes. */
export async function isTestDocument(file: File): Promise<boolean> {
    if (file.size !== metadata.size_bytes) return false;
    const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer());
    const hash = Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('');
    return hash === metadata.sha256;
}

export async function createTestDocument(): Promise<File> {
    const response = await fetch(`${import.meta.env.BASE_URL}test-documents/${metadata.file_name}`);
    if (!response.ok) throw new Error('Не удалось загрузить тестовый PDF');
    const file = new File([await response.blob()], metadata.file_name, { type: 'application/pdf' });
    if (!await isTestDocument(file)) throw new Error('Тестовый PDF повреждён. Обновите страницу и попробуйте ещё раз');
    return file;
}
