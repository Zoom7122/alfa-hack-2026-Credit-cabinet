/// <reference types="vite/client" />

interface ImportMetaEnv {
    /** 'false' — отключить моки MSW и ходить в настоящий API */
    readonly VITE_API_MOCK?: string;
    /** База REST API, по умолчанию /api/v1 */
    readonly VITE_API_BASE_URL?: string;
}

interface ImportMeta {
    readonly env: ImportMetaEnv;
}
